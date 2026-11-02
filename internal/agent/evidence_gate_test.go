package agent

import (
        "context"
        "encoding/json"
        "errors"
        "strconv"
        "testing"
        "time"

        "github.com/damien1141/a1/internal/evidence"
        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/tools"
        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// fakeSnapshots is a recording stand-in for the git snapshot manager.
type fakeSnapshots struct {
        createErr    error
        rollbackErr  error
        created      []string
        rolledBackTo []string
        deleted      []string
        seq          int
}

func (f *fakeSnapshots) Create(context.Context) (string, error) {
        if f.createErr != nil {
                return "", f.createErr
        }
        f.seq++
        name := "snap-" + strconv.Itoa(f.seq)
        f.created = append(f.created, name)
        return name, nil
}

func (f *fakeSnapshots) Rollback(_ context.Context, name string) error {
        if f.rollbackErr != nil {
                return f.rollbackErr
        }
        f.rolledBackTo = append(f.rolledBackTo, name)
        return nil
}

func (f *fakeSnapshots) Delete(_ context.Context, name string) error {
        f.deleted = append(f.deleted, name)
        return nil
}

// drive runs one batch through the gate in loop order: baseline first, then
// the pre-candidate evidence state, then the batch's observations, then the
// verdict.
func drive(
        ctx context.Context,
        g *turnEvidenceGate,
        l *evidence.Ledger,
        mutating, observed bool,
        obs func(),
) evidenceAction {
        g.beforeBatch(ctx, mutating)
        pre := l.State()
        if obs != nil {
                obs()
        }
        return g.afterBatch(ctx, pre, observed, mutating)
}

func observe(l *evidence.Ledger, command, output string, failed bool) {
        l.Observe(command, output, failed, time.Now())
}

// protectedAuthLedger seeds a ledger with one protected go-test check.
func protectedAuthLedger(t *testing.T) *evidence.Ledger {
        t.Helper()
        l := evidence.NewLedger("")
        observe(l, "go test ./internal/auth/", evidenceGoTestPass, false)
        require.Equal(t, 1, l.ProtectedCount())
        return l
}

func gateFor(snaps evidenceSnapshots, l *evidence.Ledger, mode permission.Mode, stallLimit int) *turnEvidenceGate {
        return newTurnEvidenceGate(snaps, l, func() permission.Mode { return mode }, stallLimit, "", nil)
}

// TestVerdictGateEnforcedRollbackOnRegression is the Phase 2 centerpiece:
// in autopilot a mutating candidate that breaks a protected check is rolled
// back to the pre-candidate baseline, the ledger re-admits the check, and
// the model is told what was rejected and why.
func TestVerdictGateEnforcedRollbackOnRegression(t *testing.T) {
        for _, mode := range []permission.Mode{permission.ModeAutopilot, permission.ModeHeadlessStrict} {
                t.Run(string(mode), func(t *testing.T) {
                        l := protectedAuthLedger(t)
                        snaps := &fakeSnapshots{}
                        g := gateFor(snaps, l, mode, 3)

                        action := drive(t.Context(), g, l, true, true, func() {
                                observe(l, "go test ./internal/auth/", evidenceGoTestFail, true)
                        })

                        require.Len(t, snaps.created, 1, "mutating batch with protected evidence must baseline first")
                        require.Len(t, snaps.rolledBackTo, 1, "enforced mode must roll the candidate back")
                        assert.Equal(t, snaps.created[0], snaps.rolledBackTo[0], "rollback targets the pre-candidate baseline")
                        assert.Contains(t, action.Note, "candidate rejected")
                        assert.Contains(t, action.Note, "go-test:github.com/damien1141/a1/internal/auth")
                        assert.Contains(t, action.Note, "do not delete, skip, or weaken")
                        assert.False(t, action.Stop, "first regression is a stop only when the stall limit says so")
                        assert.Empty(t, l.Regressions(), "the ledger re-admits the regressed check at its verified status")

                        // Re-applying the same broken edit is a FRESH regression: the
                        // rollback restored both workspace and ledger, so the ratchet
                        // sees pass→fail again and rejects a second time.
                        action = drive(t.Context(), g, l, true, true, func() {
                                observe(l, "go test ./internal/auth/", evidenceGoTestFail, true)
                        })
                        require.Len(t, snaps.rolledBackTo, 2, "repeat offense must be rejected again")
                        assert.Contains(t, action.Note, "candidate rejected")
                })
        }
}

// TestVerdictGateAdvisoryPassthroughInteractive pins the other half of the
// APPA strictness dial: interactive-family modes never roll back — the
// Phase 1 advisory note already rode the tool result, the ledger keeps the
// failure, and the turn continues.
func TestVerdictGateAdvisoryPassthroughInteractive(t *testing.T) {
        for _, mode := range []permission.Mode{permission.ModeInteractive, permission.ModeReadonly, permission.ModeYolo} {
                t.Run(string(mode), func(t *testing.T) {
                        l := protectedAuthLedger(t)
                        snaps := &fakeSnapshots{}
                        g := gateFor(snaps, l, mode, 3)

                        action := drive(t.Context(), g, l, true, true, func() {
                                observe(l, "go test ./internal/auth/", evidenceGoTestFail, true)
                        })

                        assert.Empty(t, snaps.rolledBackTo, "advisory modes never roll back")
                        assert.Empty(t, action.Note, "the Phase 1 note rode the tool result; no engine note")
                        assert.False(t, action.Stop)
                        require.Len(t, l.Regressions(), 1, "advisory keeps the failure on record")
                })
        }
}

// TestVerdictGateUnknownModeIsAdvisory: a nil gate or AllowAll reads as ""
// from ModeOf — treated as interactive, never enforced.
func TestVerdictGateUnknownModeIsAdvisory(t *testing.T) {
        l := protectedAuthLedger(t)
        snaps := &fakeSnapshots{}
        g := newTurnEvidenceGate(snaps, l, nil, 3, "", nil)

        action := drive(t.Context(), g, l, true, true, func() {
                observe(l, "go test ./internal/auth/", evidenceGoTestFail, true)
        })
        assert.Empty(t, snaps.rolledBackTo)
        assert.Empty(t, action.Note)
}

// TestVerdictGateBaselineLifecycle covers the commit-point ratchet: the
// stale baseline from a previous turn is deleted, a clean improving
// candidate advances the baseline (and deletes the one it replaces), and a
// regression restores the LATEST verified state, not the turn start.
func TestVerdictGateBaselineLifecycle(t *testing.T) {
        l := evidence.NewLedger("")
        observe(l, "go build ./...", "", false) // protect go-build

        snaps := &fakeSnapshots{}
        var baselines []string
        g := newTurnEvidenceGate(snaps, l, func() permission.Mode { return permission.ModeInteractive }, 3,
                "stale-previous-turn", func(name string) { baselines = append(baselines, name) })

        // Candidate 1: adds new protected evidence (go vet first pass).
        action := drive(t.Context(), g, l, true, true, func() {
                observe(l, "go vet ./...", "", false)
        })
        assert.Empty(t, action.Note)
        require.Len(t, snaps.created, 2, "turn baseline, then advance on the clean improving candidate")
        assert.Contains(t, snaps.deleted, "stale-previous-turn", "previous turn's baseline is cleaned up")
        assert.Contains(t, snaps.deleted, snaps.created[0], "superseded baseline is deleted")
        require.Len(t, baselines, 2)
        assert.Equal(t, snaps.created[1], baselines[1], "engine is told the live baseline for the next turn")

        // Candidate 2 regresses: rollback must target created[1] (the advanced
        // baseline), not the turn-start snapshot.
        mode := permission.ModeAutopilot
        g.mode = func() permission.Mode { return mode }
        action = drive(t.Context(), g, l, true, true, func() {
                observe(l, "go build ./...", "", true)
        })
        require.Len(t, snaps.rolledBackTo, 1)
        assert.Equal(t, snaps.created[1], snaps.rolledBackTo[0], "rollback restores the latest verified state")
        assert.Contains(t, action.Note, "candidate rejected")
}

// TestVerdictGateStallDetection drives the anti-thrash policy: Equal
// candidates (verification with no new evidence) accumulate; a StrictlyBetter
// candidate resets the counter; hitting the limit stops the turn with the
// residual failure set.
func TestVerdictGateStallDetection(t *testing.T) {
        l := protectedAuthLedger(t)
        snaps := &fakeSnapshots{}
        g := gateFor(snaps, l, permission.ModeInteractive, 3)

        // Round 1: re-verification of a passing check — Equal, stall=1.
        action := drive(t.Context(), g, l, true, true, func() {
                observe(l, "go test ./internal/auth/", evidenceGoTestPass, false)
        })
        assert.False(t, action.Stop)

        // Round 2: same — stall=2.
        action = drive(t.Context(), g, l, true, true, func() {
                observe(l, "go test ./internal/auth/", evidenceGoTestPass, false)
        })
        assert.False(t, action.Stop)

        // Round 3: new protected evidence — StrictlyBetter resets the counter.
        action = drive(t.Context(), g, l, true, true, func() {
                observe(l, "go vet ./...", "", false)
        })
        assert.False(t, action.Stop)

        // Rounds 4-6: three consecutive Equals → stop with the residual note.
        for i := 0; i < 2; i++ {
                action = drive(t.Context(), g, l, true, true, func() {
                        observe(l, "go vet ./...", "", false)
                })
                assert.False(t, action.Stop, "round %d should not trip the limit", i+4)
        }
        action = drive(t.Context(), g, l, true, true, func() {
                observe(l, "go vet ./...", "", false)
        })
        require.True(t, action.Stop, "third consecutive non-improving round stops the turn")
        assert.Contains(t, action.Note, "stopping this turn")
        assert.Contains(t, action.Note, "3 consecutive verification rounds")
        assert.Contains(t, action.Note, "No protected check is failing", "residual set is empty here")
}

// TestVerdictGateStallSurfacesResidualFailures: the stop note lists the
// failing protected checks when the workspace is still broken.
func TestVerdictGateStallSurfacesResidualFailures(t *testing.T) {
        l := protectedAuthLedger(t)
        g := gateFor(&fakeSnapshots{}, l, permission.ModeInteractive, 2)

        // Round 1: regression (advisory — note empty, stall=1).
        drive(t.Context(), g, l, true, true, func() {
                observe(l, "go test ./internal/auth/", evidenceGoTestFail, true)
        })

        // Round 2: still failing, no improvement — stall=2 → stop.
        action := drive(t.Context(), g, l, true, true, func() {
                observe(l, "go test ./internal/auth/", evidenceGoTestFail, true)
        })
        require.True(t, action.Stop)
        assert.Contains(t, action.Note, "go-test:github.com/damien1141/a1/internal/auth",
                "the residual failing check must be surfaced")
}

// TestVerdictGatePostRollbackAmnesty: the round right after a rollback is
// the mandated re-verification — an Equal there must not count toward the
// stall limit, but the one after it does.
func TestVerdictGatePostRollbackAmnesty(t *testing.T) {
        l := protectedAuthLedger(t)
        snaps := &fakeSnapshots{}
        g := gateFor(snaps, l, permission.ModeAutopilot, 2)

        // Regression → rollback, stall=1.
        action := drive(t.Context(), g, l, true, true, func() {
                observe(l, "go test ./internal/auth/", evidenceGoTestFail, true)
        })
        require.Len(t, snaps.rolledBackTo, 1)
        assert.False(t, action.Stop)

        // Mandated re-verification on the restored workspace: pass, Equal —
        // amnesty holds the counter at 1.
        action = drive(t.Context(), g, l, true, true, func() {
                observe(l, "go test ./internal/auth/", evidenceGoTestPass, false)
        })
        assert.False(t, action.Stop, "the rollback-mandated re-verification is free")
        assert.Empty(t, action.Note)

        // A second Equal is real thrash: stall=2 → stop.
        action = drive(t.Context(), g, l, true, true, func() {
                observe(l, "go test ./internal/auth/", evidenceGoTestPass, false)
        })
        assert.True(t, action.Stop)
}

// TestVerdictGateDegradations covers the failure modes: snapshot create
// failure degrades enforced mode to a no-baseline note; rollback failure
// keeps the workspace but says so; a regression on a non-mutating round is
// drift, never rolled back.
func TestVerdictGateDegradations(t *testing.T) {
        t.Run("create fails", func(t *testing.T) {
                l := protectedAuthLedger(t)
                snaps := &fakeSnapshots{createErr: errors.New("not a git repository")}
                g := gateFor(snaps, l, permission.ModeAutopilot, 3)

                action := drive(t.Context(), g, l, true, true, func() {
                        observe(l, "go test ./internal/auth/", evidenceGoTestFail, true)
                })
                assert.Empty(t, snaps.rolledBackTo)
                assert.Contains(t, action.Note, "no verified snapshot to roll back to")
                assert.Contains(t, action.Note, "snapshotting is unavailable")
                assert.Len(t, l.Regressions(), 1, "ledger keeps the observed failure")
        })

        t.Run("rollback fails", func(t *testing.T) {
                l := protectedAuthLedger(t)
                snaps := &fakeSnapshots{rollbackErr: errors.New("git exploded")}
                g := gateFor(snaps, l, permission.ModeAutopilot, 3)

                action := drive(t.Context(), g, l, true, true, func() {
                        observe(l, "go test ./internal/auth/", evidenceGoTestFail, true)
                })
                assert.Contains(t, action.Note, "automatic rollback failed")
                assert.Contains(t, action.Note, "git exploded")
                assert.Len(t, l.Regressions(), 1, "no revert without a successful rollback")
        })

        t.Run("non-mutating drift", func(t *testing.T) {
                l := protectedAuthLedger(t)
                snaps := &fakeSnapshots{}
                g := gateFor(snaps, l, permission.ModeAutopilot, 3)

                action := drive(t.Context(), g, l, false, true, func() {
                        observe(l, "go test ./internal/auth/", evidenceGoTestFail, true)
                })
                assert.Empty(t, snaps.rolledBackTo, "drift is surfaced, never rolled back")
                assert.Empty(t, snaps.created, "no baseline for a non-mutating batch")
                assert.Contains(t, action.Note, "non-mutating round")
        })

        t.Run("no observations is not a candidate", func(t *testing.T) {
                l := protectedAuthLedger(t)
                snaps := &fakeSnapshots{}
                g := gateFor(snaps, l, permission.ModeAutopilot, 1)

                // Mutating batch that ran no checks (edits + ls): not a candidate.
                action := drive(t.Context(), g, l, true, false, func() {
                        observe(l, "ls -la", "total 0", false) // unclassified: no ledger change
                })
                assert.Empty(t, action.Note)
                assert.False(t, action.Stop)
        })
}

// TestBatchMutatesAndDrain wires the executor side: mutating-batch
// detection mirrors readableBatch's conservative rules, and verdicts
// collected during Run drain exactly once.
func TestBatchMutatesAndDrain(t *testing.T) {
        read := tools.Tool{
                Definition: llm.ToolDefinition{Name: "read", Readable: true},
                Run: func(context.Context, json.RawMessage) (tools.Result, error) {
                        return tools.Result{Content: "x"}, nil
                },
        }
        reg := tools.Registry{
                "bash": stubBashTool(func() string { return evidenceGoTestPass }),
                "read": read,
        }
        e := NewExecutor(reg, permission.AllowAll{}, nil, nil, nil)
        e.SetEvidenceLedger(evidence.NewLedger(""))

        bashCall := llm.ToolCall{ID: "c1", Function: llm.Function{Name: "bash", Arguments: `{"command":"go test ./..."}`}}
        readCall := llm.ToolCall{ID: "c2", Function: llm.Function{Name: "read", Arguments: `{}`}}

        assert.True(t, e.BatchMutates([]llm.ToolCall{bashCall}), "bash always mutates")
        assert.True(t, e.BatchMutates([]llm.ToolCall{{Function: llm.Function{Name: "typoed"}}}), "unknown tool mutates")
        assert.True(t, e.BatchMutates([]llm.ToolCall{readCall, bashCall}), "mixed batch mutates")
        assert.False(t, e.BatchMutates([]llm.ToolCall{readCall}), "read-only batch does not")
        assert.False(t, e.BatchMutates(nil))

        msgs, _, _ := e.Run(t.Context(), []llm.ToolCall{bashCall}, func(session.ToolData) bool { return true })
        require.Len(t, msgs, 1)

        verdicts := e.DrainEvidenceVerdicts()
        require.Len(t, verdicts, 1, "one classified bash call = one verdict")
        assert.True(t, verdicts[0].Any())
        assert.Empty(t, e.DrainEvidenceVerdicts(), "drain clears the collector")

        // A ledger swap drops stale verdicts with it.
        e.SetEvidenceLedger(evidence.NewLedger(""))
        assert.Empty(t, e.DrainEvidenceVerdicts())
}

// TestEngineEvidenceGateWiring covers the engine plumbing: default and
// overridden stall limits, injected snapshot backend, live permission-mode
// reading, and baseline hand-off across turns.
func TestEngineEvidenceGateWiring(t *testing.T) {
        dir := t.TempDir()
        sess, err := NewSession(WithCwd(dir), WithSessionDir(dir), WithPersist(true))
        require.NoError(t, err)

        engine, err := NewEngine(llm.ModelConfig{Name: "fake", APIKey: "x"}, sess)
        require.NoError(t, err)
        assert.Equal(t, defaultEvidenceStallLimit, engine.StallLimit())

        engine.SetStallLimit(0)
        assert.Equal(t, 0, engine.StallLimit(), "0 disables the stall stop")
        engine.SetStallLimit(-3)
        assert.Equal(t, 0, engine.StallLimit(), "negative clamps to disabled")

        overridden, err := NewEngine(llm.ModelConfig{Name: "fake", APIKey: "x"}, sess, WithStallLimit(5))
        require.NoError(t, err)
        assert.Equal(t, 5, overridden.StallLimit())

        // Without a permission gate the mode is unknown → advisory.
        snaps := &fakeSnapshots{}
        engine.setEvidenceSnapshots(snaps)
        observe(engine.EvidenceLedger(), "go test ./internal/auth/", evidenceGoTestPass, false)
        require.Equal(t, 1, engine.EvidenceLedger().ProtectedCount())

        g := engine.newEvidenceGate()
        action := drive(t.Context(), g, engine.EvidenceLedger(), true, true, func() {
                observe(engine.EvidenceLedger(), "go test ./internal/auth/", evidenceGoTestFail, true)
        })
        assert.Empty(t, snaps.rolledBackTo, "no gate wired → advisory")
        assert.Empty(t, action.Note)

        // Wire an autopilot gate: the SAME mode func (read live) now enforces.
        // Re-verify first (the advisory round left the check failing on record),
        // then break it again — a fresh pass→fail regression under autopilot.
        gate, err := permission.NewGate(permission.Policy{Mode: permission.ModeAutopilot}, dir)
        require.NoError(t, err)
        engine.SetPermission(gate, nil)
        observe(engine.EvidenceLedger(), "go test ./internal/auth/", evidenceGoTestPass, false)
        require.Empty(t, engine.EvidenceLedger().Regressions())

        action = drive(t.Context(), engine.newEvidenceGate(), engine.EvidenceLedger(), true, true, func() {
                observe(engine.EvidenceLedger(), "go test ./internal/auth/", evidenceGoTestFail, true)
        })
        require.Len(t, snaps.rolledBackTo, 1, "autopilot gate enforces the rollback")
        assert.Contains(t, action.Note, "candidate rejected")
}
