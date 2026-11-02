package agent

import (
        "context"
        "encoding/json"
        "path/filepath"
        "testing"

        "github.com/damien1141/a1/internal/evidence"
        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/tools"
        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

var (
        evidenceGoTestPass = "ok  \tgithub.com/damien1141/a1/internal/auth\t0.1s"
        evidenceGoTestFail = "FAIL\tgithub.com/damien1141/a1/internal/auth\t0.1s\nFAIL"
)

func newTestExecutorWithLedger(t *testing.T) (*Executor, *evidence.Ledger) {
        t.Helper()
        e := NewExecutor(nil, permission.AllowAll{}, nil, nil, nil)
        ledger := evidence.NewLedger("")
        e.SetEvidenceLedger(ledger)
        return e, ledger
}

// TestRecordEvidenceRatchetAndNote covers the full advisory path: a fail on
// a never-passed check is new information, the pass protects the check, and
// a later fail produces the regression note the model reads next round.
func TestRecordEvidenceRatchetAndNote(t *testing.T) {
        e, ledger := newTestExecutorWithLedger(t)

        note := e.recordEvidence(t.Context(), json.RawMessage(`{"command":"go test ./internal/auth/"}`), evidenceGoTestFail+"\n(exit error: exit status 1)")
        assert.Empty(t, note, "failing a never-passed check is new information, not a regression")

        note = e.recordEvidence(t.Context(), json.RawMessage(`{"command":"go test ./internal/auth/"}`), evidenceGoTestPass)
        assert.Empty(t, note, "first pass is new evidence, not a regression")

        note = e.recordEvidence(t.Context(), json.RawMessage(`{"command":"go test ./internal/auth/"}`), evidenceGoTestPass)
        assert.Empty(t, note, "repeated pass is neutral")

        note = e.recordEvidence(t.Context(), json.RawMessage(`{"command":"go test ./internal/auth/"}`), evidenceGoTestFail+"\n(exit error: exit status 1)")
        require.NotEmpty(t, note, "protected regression must produce a note")
        assert.Contains(t, note, "[evidence] PROTECTED REGRESSION")
        assert.Contains(t, note, "go-test:github.com/damien1141/a1/internal/auth")
        require.Len(t, ledger.Regressions(), 1)
}

// TestRecordEvidenceIgnoresCanceledRuns pins that canceled/timed-out bash
// output never reaches the ledger.
func TestRecordEvidenceIgnoresCanceledRuns(t *testing.T) {
        e, ledger := newTestExecutorWithLedger(t)

        out := evidenceGoTestPass + "\n(command canceled or timed out)"
        note := e.recordEvidence(t.Context(), json.RawMessage(`{"command":"go test ./..."}`), out)
        assert.Empty(t, note)
        assert.Equal(t, 0, ledger.ProtectedCount(), "canceled run must not record")
}

// TestRecordEvidenceIgnoresNonBashShapes covers malformed args, empty
// commands, and unclassifiable commands.
func TestRecordEvidenceIgnoresNonBashShapes(t *testing.T) {
        e, ledger := newTestExecutorWithLedger(t)

        assert.Empty(t, e.recordEvidence(t.Context(), json.RawMessage(`{invalid`), "out"))
        assert.Empty(t, e.recordEvidence(t.Context(), json.RawMessage(`{"command":"  "}`), "out"))
        assert.Empty(t, e.recordEvidence(t.Context(), json.RawMessage(`{"command":"ls -la"}`), "total 0"))
        assert.Equal(t, 0, ledger.ProtectedCount())
}

// TestNilLedgerExecutorSafety mirrors the nil-safe convention used by every
// other executor hook.
func TestNilLedgerExecutorSafety(t *testing.T) {
        e := NewExecutor(nil, permission.AllowAll{}, nil, nil, nil)
        e.SetEvidenceLedger(nil)
        assert.NotPanics(t, func() {
                note := e.recordEvidence(t.Context(), json.RawMessage(`{"command":"go test ./..."}`), evidenceGoTestPass)
                assert.Empty(t, note)
        })
}

// stubBashTool returns a bash tool whose output is scripted by the test.
func stubBashTool(output func() string) tools.Tool {
        return tools.Tool{
                Definition: llm.ToolDefinition{Name: "bash"},
                Run: func(context.Context, json.RawMessage) (tools.Result, error) {
                        return tools.Result{Content: output(), Output: output()}, nil
                },
        }
}

// TestEvidenceNoteRidesOnToolMessage verifies the executor appends the
// regression note to the model-visible tool message through the public Run
// path.
func TestEvidenceNoteRidesOnToolMessage(t *testing.T) {
        var scripted string
        reg := tools.Registry{"bash": stubBashTool(func() string { return scripted })}
        e := NewExecutor(reg, permission.AllowAll{}, nil, nil, nil)
        ledger := evidence.NewLedger("")
        e.SetEvidenceLedger(ledger)

        // Protect the check first: the stub command passes.
        scripted = evidenceGoTestPass
        msgs, _, _ := e.Run(t.Context(), []llm.ToolCall{{
                ID:       "c1",
                Function: llm.Function{Name: "bash", Arguments: `{"command":"go test ./..."}`},
        }}, func(session.ToolData) bool { return true })
        require.Len(t, msgs, 1)
        assert.NotContains(t, msgs[0].Content, "[evidence]")

        // The same check now fails: the note must ride on the tool message.
        scripted = evidenceGoTestFail + "\n(exit error: exit status 1)"
        msgs, _, _ = e.Run(t.Context(), []llm.ToolCall{{
                ID:       "c2",
                Function: llm.Function{Name: "bash", Arguments: `{"command":"go test ./..."}`},
        }}, func(session.ToolData) bool { return true })
        require.Len(t, msgs, 1)
        assert.Contains(t, msgs[0].Content, "[evidence] PROTECTED REGRESSION")
        assert.Contains(t, msgs[0].Content, "go-test:github.com/damien1141/a1/internal/auth")
        require.Len(t, ledger.Regressions(), 1)
}

// TestEngineInstallsDefaultEvidenceLedger verifies NewEngine always wires a
// ledger, persisted next to the session file when persistence is on.
func TestEngineInstallsDefaultEvidenceLedger(t *testing.T) {
        dir := t.TempDir()
        sess, err := NewSession(WithCwd(dir), WithSessionDir(dir), WithPersist(true))
        require.NoError(t, err)

        engine, err := NewEngine(llm.ModelConfig{Name: "fake", APIKey: "x"}, sess)
        require.NoError(t, err)
        require.NotNil(t, engine.EvidenceLedger())

        if f := engine.SessionFile(); f != "" {
                assert.Equal(t, filepath.Join(filepath.Dir(f), "evidence.jsonl"), engine.EvidenceLedger().Path())
        }

        // Explicit wiring wins.
        custom := evidence.NewLedger(filepath.Join(dir, "custom.jsonl"))
        engine.SetEvidenceLedger(custom)
        assert.Same(t, custom, engine.EvidenceLedger())
}

// TestRecordEvidenceExitCodeFeedsWatches pins the exit-code path end to
// end: the hook parses "exit status N" from the bash output marker and
// feeds it to ObserveExit, so a contract-declared exit watch records the
// real code — including the regression flip when the code changes.
func TestRecordEvidenceExitCodeFeedsWatches(t *testing.T) {
        e, ledger := newTestExecutorWithLedger(t)
        require.True(t, ledger.Watch("exit:0:make lint"))

        note := e.recordEvidence(t.Context(), json.RawMessage(`{"command":"make lint"}`), "all good")
        assert.Empty(t, note)
        st := ledger.CheckStatus("exit:0:make lint")
        assert.Equal(t, evidence.CheckPassing, st.State)

        // Signal deaths carry no code; failed-ness still judges a zero
        // expectation (a regression from the passing state).
        note = e.recordEvidence(t.Context(), json.RawMessage(`{"command":"make lint"}`), "make: killed\n(exit error: signal: killed)")
        require.NotEmpty(t, note)
        assert.Contains(t, note, "exit:0:make lint")
        st = ledger.CheckStatus("exit:0:make lint")
        assert.Equal(t, evidence.CheckFailing, st.State)
        assert.Contains(t, st.Detail, "non-zero exit")

        // A coded failure while already failing is neutral (fail → fail), but
        // the parsed code still lands as the record's detail.
        note = e.recordEvidence(t.Context(), json.RawMessage(`{"command":"make lint"}`), "Makefile:4: *** missing separator.  Stop.\n(exit error: exit status 2)")
        assert.Empty(t, note)
        st = ledger.CheckStatus("exit:0:make lint")
        assert.Equal(t, evidence.CheckFailing, st.State)
        assert.Contains(t, st.Detail, "exit 2")
}

// TestRecordEvidencePolyglotSuites pins the widened classifier through the
// real hook path: a JS test suite records js-test evidence.
func TestRecordEvidencePolyglotSuites(t *testing.T) {
        e, ledger := newTestExecutorWithLedger(t)

        note := e.recordEvidence(t.Context(), json.RawMessage(`{"command":"npm test"}`), "Tests:       3 passed, 3 total")
        assert.Empty(t, note)
        st := ledger.CheckStatus("js-test")
        assert.Equal(t, evidence.CheckPassing, st.State)

        out := "FAIL src/calc.test.js\n  ● add › adds numbers\n\nTests: 1 failed, 1 passed, 2 total\n(exit error: exit status 1)"
        note = e.recordEvidence(t.Context(), json.RawMessage(`{"command":"npm test"}`), out)
        require.NotEmpty(t, note)
        assert.Contains(t, note, "js-test")
}

// TestRecordEvidenceConfigChecks pins config-defined checks through the
// hook: the named command records check:<name> with the pattern gate.
func TestRecordEvidenceConfigChecks(t *testing.T) {
        e, ledger := newTestExecutorWithLedger(t)
        require.NoError(t, ledger.SetChecks([]evidence.CheckDef{
                {Name: "health", Command: "curl -s localhost:3000/health", Pattern: `"status":"ok"`},
        }))

        note := e.recordEvidence(t.Context(), json.RawMessage(`{"command":"curl -s localhost:3000/health"}`), `{"status":"ok"}`)
        assert.Empty(t, note)
        assert.Equal(t, evidence.CheckPassing, ledger.CheckStatus("check:health").State)

        note = e.recordEvidence(t.Context(), json.RawMessage(`{"command":"curl -s localhost:3000/health"}`), `{"status":"degraded"}`)
        assert.NotEmpty(t, note, "passing-to-failing transition on a protected check must note")
        assert.Equal(t, evidence.CheckFailing, ledger.CheckStatus("check:health").State)
}
