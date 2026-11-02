package evidence

import (
        "path/filepath"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

func observeAt(l *Ledger, command, output string, failed bool) Verdict {
        return l.Observe(command, output, failed, time.Now())
}

const (
        revertGoTestPass = "ok  \tgithub.com/damien1141/a1/internal/auth\t0.1s"
        revertGoTestFail = "FAIL\tgithub.com/damien1141/a1/internal/auth\t0.1s\nFAIL"
)

// TestRevertToUndoesRegression proves the evidence-state counterpart of a
// workspace rollback: a regressed (protected, failing) check returns to its
// verified pass status, the protected set never shrinks, and a re-applied
// break reads as a FRESH regression instead of fail→fail neutral.
func TestRevertToUndoesRegression(t *testing.T) {
        l := NewLedger("")
        observeAt(l, "go build ./...", "", false)                // protect go-build
        observeAt(l, "go test ./internal/auth/", revertGoTestPass, false) // protect go-test

        pre := l.State()

        // Candidate regresses both checks.
        v := observeAt(l, "go build ./...", "", true)
        require.True(t, v.Regression())
        v = observeAt(l, "go test ./internal/auth/", revertGoTestFail, true)
        require.True(t, v.Regression())
        require.Len(t, l.Regressions(), 2)

        l.RevertTo(pre, []string{"go-build", "go-test:github.com/damien1141/a1/internal/auth"}, "candidate-rollback")

        assert.Empty(t, l.Regressions(), "reverted checks must read as passing again")
        assert.Equal(t, 2, l.ProtectedCount(), "the ratchet never shrinks")
        for _, rec := range l.State() {
                assert.Equal(t, Pass, rec.Status, rec.Check)
        }

        // Re-applying the same broken edit: pass→fail again, not neutral.
        v = observeAt(l, "go build ./...", "", true)
        assert.True(t, v.Regression(), "a repeat offense after rollback is a fresh regression")
}

// TestRevertToOnlyUndoesRegressions pins the guardrails: never flips a
// passing or unknown check, and skips checks whose pre-record was failing
// (reverting is strictly a regression-undo, not a state editor).
func TestRevertToOnlyUndoesRegressions(t *testing.T) {
        l := NewLedger("")
        observeAt(l, "go build ./...", "", false)                         // go-build: pass (protected)
        observeAt(l, "go test ./internal/auth/", revertGoTestFail, true) // go-test: fail (never passed)
        pre := l.State()

        // go-test is failing but its pre-record is a Fail: nothing to revert to.
        l.RevertTo(pre, []string{"go-test:github.com/damien1141/a1/internal/auth"}, "x")
        rec, ok := stateOf(l, "go-test:github.com/damien1141/a1/internal/auth")
        require.True(t, ok)
        assert.Equal(t, Fail, rec.Status, "a check that was already failing stays failing")

        // go-build is passing: revert is a no-op, not a rewrite.
        l.RevertTo(pre, []string{"go-build"}, "x")
        rec, _ = stateOf(l, "go-build")
        assert.Equal(t, Pass, rec.Status)
        assert.NotContains(t, rec.Command, "harness:", "no receipt written for a no-op revert")

        // Unknown check: no-op, no panic.
        l.RevertTo(pre, []string{"nope"}, "x")
}

// TestRevertToReceiptsReplay proves a session resume replays the reversion:
// the harness-written receipt keeps the ledger coherent across restarts.
func TestRevertToReceiptsReplay(t *testing.T) {
        path := filepath.Join(t.TempDir(), "evidence.jsonl")
        l := NewLedger(path)
        observeAt(l, "go build ./...", "", false)
        pre := l.State()
        observeAt(l, "go build ./...", "", true)
        require.Len(t, l.Regressions(), 1)

        l.RevertTo(pre, []string{"go-build"}, "candidate-rollback")
        assert.Empty(t, l.Regressions())

        // Fresh ledger replays the same receipts.
        reloaded := NewLedger(path)
        require.NoError(t, reloaded.Load())
        assert.Empty(t, reloaded.Regressions(), "replay must arrive at the reverted (passing) state")
        rec, ok := stateOf(reloaded, "go-build")
        require.True(t, ok)
        assert.Equal(t, Pass, rec.Status)
        assert.Equal(t, "harness:candidate-rollback", rec.Command, "the receipt records its harness origin")
}

// TestVerdictAny pins the candidate marker: recognized-but-unchanged runs
// count, unclassified commands do not.
func TestVerdictAny(t *testing.T) {
        l := NewLedger("")

        v := observeAt(l, "ls -la", "total 0", false)
        assert.False(t, v.Any(), "unclassified command is not a candidate")

        v = observeAt(l, "go build ./...", "", false)
        assert.True(t, v.Any())

        v = observeAt(l, "go build ./...", "", false)
        assert.True(t, v.Any(), "neutral re-verification is still a candidate marker")
}

func stateOf(l *Ledger, check string) (Record, bool) {
        for _, rec := range l.State() {
                if rec.Check == check {
                        return rec, true
                }
        }
        return Record{}, false
}
