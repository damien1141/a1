package agent

import (
        "context"
        "sort"
        "strconv"
        "strings"

        "github.com/damien1141/a1/internal/debuglog"
        "github.com/damien1141/a1/internal/evidence"
        "github.com/damien1141/a1/internal/permission"
)

// defaultEvidenceStallLimit is how many consecutive non-improving
// verification rounds a turn tolerates before the verdict gate stops it and
// surfaces the residual failure set. Three failed fix attempts in a row is
// the Gate 3 signal (same fix failing twice means the diagnosis is wrong);
// one more confirms it. A stop is a brake, not a wall: the user re-prompts
// and the turn continues under a fresh counter.
const defaultEvidenceStallLimit = 3

// evidenceSnapshots is the snapshot surface the verdict gate needs. It is
// satisfied by *snapshot.Manager; tests inject a recording fake.
type evidenceSnapshots interface {
        Create(ctx context.Context) (string, error)
        Rollback(ctx context.Context, name string) error
        Delete(ctx context.Context, name string) error
}

// turnEvidenceGate is the per-turn verdict gate (VeriLoop Phase 2): the
// model proposes mutations, the harness compares protected evidence before
// and after each candidate batch and decides what happens to the workspace.
//
// A batch is a CANDIDATE only when it produced check evidence (at least one
// classified build/test/vet run). Batches without evidence are intermediate
// work — ungoverned by this gate (Phase 3 adds turn-end verification for
// those). Candidates are judged with the VeriLoop rule set:
//
//      Regressed       any protected check flipped pass → fail
//      StrictlyBetter  no regression + at least one improvement
//      Equal           no regression, no improvement
//
// Regressed candidates roll the workspace back to the last verified
// baseline in enforced modes (autopilot, headless-strict) and revert the
// ledger's last-known status for the regressed checks so ledger and
// workspace stay coherent; interactive-family modes stay advisory (the
// Phase 1 note already rode the tool result). StrictlyBetter and Equal
// candidates are safe to keep: StrictlyBetter advances the baseline and
// resets the stall counter, Equal burns a round and counts toward it.
// stallLimit consecutive non-improving candidates stop the turn with the
// residual failure set instead of burning budget.
type turnEvidenceGate struct {
        snapshots  evidenceSnapshots
        ledger     *evidence.Ledger
        mode       func() permission.Mode
        stallLimit int

        // baseline names the snapshot the workspace rolls back to. "" = no
        // verified restore point yet this turn.
        baseline string
        // stale is the previous turn's baseline name; deleted once this turn
        // creates its own, so engine-owned refs never accumulate.
        stale string
        // onBaseline reports every baseline change so the engine can hand the
        // name to the next turn's gate (which will treat it as stale).
        onBaseline func(name string)
        // broken marks the snapshot subsystem unavailable for the rest of the
        // turn (not a git repo, git failure). Rollback degrades to advisory.
        broken bool
        // stall counts consecutive non-improving candidates.
        stall int
        // justRolledBack grants the candidate right after a rollback one free
        // Equal round: the rollback note instructs the model to re-run the
        // checks, and that mandated re-verification must not count against it.
        justRolledBack bool
}

func newTurnEvidenceGate(
        snapshots evidenceSnapshots,
        ledger *evidence.Ledger,
        mode func() permission.Mode,
        stallLimit int,
        staleBaseline string,
        onBaseline func(string),
) *turnEvidenceGate {
        return &turnEvidenceGate{
                snapshots:  snapshots,
                ledger:     ledger,
                mode:       mode,
                stallLimit: stallLimit,
                stale:      staleBaseline,
                onBaseline: onBaseline,
        }
}

// evidenceAction is what the gate decided for the batch. The loop appends
// Note as a system note (model sees it next round, user sees it in the
// transcript); Stop ends the turn after the note lands.
type evidenceAction struct {
        Note string
        Stop bool
}

// beforeBatch runs before a tool batch executes. A mutating batch facing
// protected evidence gets a baseline snapshot first — cheap ref write, and
// the only moment a restore point can be taken BEFORE the mutations land.
func (g *turnEvidenceGate) beforeBatch(ctx context.Context, mutating bool) {
        if g == nil || g.ledger == nil || !mutating || g.broken || g.baseline != "" {
                return
        }
        if g.ledger.ProtectedCount() == 0 {
                return // nothing protected yet: the ratchet has nothing to defend
        }
        g.snapshotBaseline(ctx)
}

// afterBatch judges the completed batch. pre is the ledger state captured
// before the batch ran; observed reports whether any classified check ran
// (the candidate marker). The returned action tells the loop what to do.
func (g *turnEvidenceGate) afterBatch(ctx context.Context, pre []evidence.Record, observed, mutating bool) evidenceAction {
        if g == nil || g.ledger == nil || !observed {
                return evidenceAction{}
        }
        post := g.ledger.State()
        regressed, improved := diffEvidence(pre, post)

        var note string
        if len(regressed) > 0 {
                // Regressed candidate: never counts as progress, never refreshes
                // the baseline. Enforced modes additionally reject it outright.
                g.stall++
                note = g.rejectCandidate(ctx, regressed, pre, mutating)
        } else {
                if len(improved) > 0 {
                        g.stall = 0 // StrictlyBetter
                } else if g.justRolledBack {
                        g.justRolledBack = false // mandated post-rollback re-verification
                } else {
                        g.stall++ // Equal: a round burned on no new evidence
                }
                // A clean candidate (no regression, no residual protected failure)
                // that mutated the workspace advances the restore point: the next
                // rollback target is this verified state, not the turn start.
                if len(g.ledger.Regressions()) == 0 && mutating {
                        g.snapshotBaseline(ctx)
                }
        }

        if g.stallLimit > 0 && g.stall >= g.stallLimit {
                return evidenceAction{Note: joinNotes(note, g.stallNote()), Stop: true}
        }
        return evidenceAction{Note: note}
}

// rejectCandidate handles a regressed candidate. Returns the system note
// ("" when the Phase 1 advisory note already said everything).
func (g *turnEvidenceGate) rejectCandidate(ctx context.Context, regressed, pre []evidence.Record, mutating bool) string {
        g.justRolledBack = false
        if !g.enforced() {
                // Interactive-family modes: the advisory regression note already
                // rode the tool result the model reads this round. The APPA mode
                // is the strictness dial; the user is watching.
                return ""
        }
        if !mutating {
                // The regression happened without the model mutating anything:
                // the workspace drifted from the verified state on its own
                // (concurrent edits, generated files, environment). Rolling back
                // could destroy someone else's work — surface it instead.
                return driftedNote(regressed)
        }
        if g.snapshots == nil || g.baseline == "" {
                return noBaselineNote(regressed, g.broken)
        }
        if err := g.snapshots.Rollback(ctx, g.baseline); err != nil {
                return rollbackFailedNote(regressed, err)
        }
        // Evidence-state counterpart of the workspace restore: re-admit the
        // regressed checks at their pre-candidate (verified) status. Without
        // this, the ledger would keep claiming they fail in a workspace that
        // was just restored to a state where they passed — and re-applying the
        // same broken edit would read as fail→fail (neutral), blinding the
        // ratchet to the repeat offense.
        checks := make([]string, len(regressed))
        for i, r := range regressed {
                checks[i] = r.Check
        }
        g.ledger.RevertTo(pre, checks, "candidate-rollback")
        g.justRolledBack = true
        return rolledBackNote(regressed, g.baseline)
}

// snapshotBaseline captures the current working tree as the rollback
// target, replacing any previous baseline (and deleting the stale one from
// an earlier turn) so at most one engine-owned ref is live at a time. A
// create failure marks the subsystem broken for the rest of the turn —
// enforced rollback then degrades to advisory instead of retrying git
// every batch.
func (g *turnEvidenceGate) snapshotBaseline(ctx context.Context) {
        if g.broken || g.snapshots == nil {
                return
        }
        name, err := g.snapshots.Create(ctx)
        if err != nil {
                g.broken = true
                debuglog.Logf("agent: evidence baseline snapshot: %v", err)
                return
        }
        for _, old := range []string{g.stale, g.baseline} {
                if old != "" && old != name {
                        if delErr := g.snapshots.Delete(ctx, old); delErr != nil {
                                debuglog.Logf("agent: evidence baseline cleanup %s: %v", old, delErr)
                        }
                }
        }
        g.baseline = name
        if g.onBaseline != nil {
                g.onBaseline(name)
        }
}

// enforced reports whether the live permission mode mandates rejection of
// regressed candidates. Autopilot and headless-strict run without a human
// watching each step, so the harness protects the verified state itself.
// Unknown gates (nil gate, AllowAll) read as interactive — advisory.
func (g *turnEvidenceGate) enforced() bool {
        if g.mode == nil {
                return false
        }
        switch g.mode() {
        case permission.ModeAutopilot, permission.ModeHeadlessStrict:
                return true
        }
        return false
}

func (g *turnEvidenceGate) stallNote() string {
        var b strings.Builder
        b.WriteString("System note: [evidence] stopping this turn — ")
        b.WriteString(strconv.Itoa(g.stallLimit))
        b.WriteString(" consecutive verification rounds without improvement.")
        if residual := g.ledger.Regressions(); len(residual) > 0 {
                b.WriteString(" Residual failing checks:")
                b.WriteString(formatChecks(residual))
        } else {
                b.WriteString(" No protected check is failing — the rounds produced no new evidence.")
        }
        b.WriteString("\nThe current approach is not converging. Re-read the failures above and reconsider the diagnosis (Gate 3: the same fix failing twice means the diagnosis is wrong, not the code). The user can re-prompt to continue with a different approach.")
        return b.String()
}

// diffEvidence compares pre/post ledger states. regressed holds protected
// checks that flipped pass → fail; improved holds checks that flipped
// fail → pass or passed for the first time. Within-batch oscillation (fail
// then pass in one batch) resolves to the final state, matching what the
// ledger actually admits.
func diffEvidence(pre, post []evidence.Record) (regressed, improved []evidence.Record) {
        preMap := make(map[string]evidence.Status, len(pre))
        for _, r := range pre {
                preMap[r.Check] = r.Status
        }
        for _, r := range post {
                prev, existed := preMap[r.Check]
                switch {
                case r.Status == evidence.Fail && existed && prev == evidence.Pass:
                        regressed = append(regressed, r)
                case r.Status == evidence.Pass && (!existed || prev == evidence.Fail):
                        improved = append(improved, r)
                }
        }
        sort.Slice(regressed, func(i, j int) bool { return regressed[i].Check < regressed[j].Check })
        sort.Slice(improved, func(i, j int) bool { return improved[i].Check < improved[j].Check })
        return regressed, improved
}

func rolledBackNote(regressed []evidence.Record, baseline string) string {
        var b strings.Builder
        b.WriteString("System note: [evidence] candidate rejected — protected regression, rolled back to the last verified state")
        if baseline != "" {
                b.WriteString(" (snapshot " + baseline + ")")
        }
        b.WriteString(":")
        b.WriteString(formatChecks(regressed))
        b.WriteString("\nEvery workspace change made since that snapshot was discarded. Re-apply only the parts of your change that are still needed, then re-run the checks to re-verify. Fix the regression — do not delete, skip, or weaken the failing checks to go green.")
        return b.String()
}

func rollbackFailedNote(regressed []evidence.Record, err error) string {
        var b strings.Builder
        b.WriteString("System note: [evidence] protected regression — automatic rollback failed (")
        b.WriteString(err.Error())
        b.WriteString("):")
        b.WriteString(formatChecks(regressed))
        b.WriteString("\nThe workspace keeps the regressed state. Fix the regression before continuing; do not delete, skip, or weaken the failing checks to go green.")
        return b.String()
}

func noBaselineNote(regressed []evidence.Record, broken bool) string {
        var b strings.Builder
        b.WriteString("System note: [evidence] protected regression — no verified snapshot to roll back to ")
        if broken {
                b.WriteString("(snapshotting is unavailable: not a git repository, or git failed)")
        } else {
                b.WriteString("(the regressed check first passed during this same round, so no earlier verified state exists)")
        }
        b.WriteString(":")
        b.WriteString(formatChecks(regressed))
        b.WriteString("\nFix the regression before continuing; do not delete, skip, or weaken the failing checks to go green.")
        return b.String()
}

func driftedNote(regressed []evidence.Record) string {
        var b strings.Builder
        b.WriteString("System note: [evidence] protected regression observed on a non-mutating round — the workspace no longer matches the last verified state (changed outside this agent's tool calls):")
        b.WriteString(formatChecks(regressed))
        b.WriteString("\nRe-run the checks and repair whatever drifted before continuing.")
        return b.String()
}

// formatChecks renders a check list the way RegressionNote does, so all
// evidence notes share one voice.
func formatChecks(records []evidence.Record) string {
        var b strings.Builder
        for _, r := range records {
                b.WriteString("\n- " + r.Check)
                if d := strings.TrimSpace(r.Detail); d != "" {
                        if len(d) > 160 {
                                d = d[:160] + "…"
                        }
                        b.WriteString(" — " + d)
                }
        }
        return b.String()
}

func joinNotes(a, b string) string {
        switch {
        case a == "":
                return b
        case b == "":
                return a
        default:
                return a + "\n\n" + b
        }
}
