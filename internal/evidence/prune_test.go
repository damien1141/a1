package evidence

import (
        "bufio"
        "fmt"
        "os"
        "path/filepath"
        "strings"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// goTestPackagesOutput builds a `go test ./...` output over num packages;
// every package whose index is a multiple of failEvery fails.
func goTestPackagesOutput(num, failEvery int) string {
        var b strings.Builder
        for i := 0; i < num; i++ {
                pkg := fmt.Sprintf("github.com/x/p%02d", i)
                if failEvery > 0 && i%failEvery == 0 {
                        fmt.Fprintf(&b, "FAIL\t%s\t0.01s\n", pkg)
                } else {
                        fmt.Fprintf(&b, "ok\t%s\t0.01s\n", pkg)
                }
        }
        return b.String()
}

func countReceiptLines(t *testing.T, path string) int {
        t.Helper()
        f, err := os.Open(path)
        require.NoError(t, err)
        defer f.Close()
        n := 0
        sc := bufio.NewScanner(f)
        for sc.Scan() {
                if strings.TrimSpace(sc.Text()) != "" {
                        n++
                }
        }
        require.NoError(t, sc.Err())
        return n
}

func stateMap(l *Ledger) map[string]Record {
        out := map[string]Record{}
        for _, rec := range l.State() {
                out[rec.Check] = rec
        }
        return out
}

// TestPruneReplayEquivalence is the core pruning invariant: after a long
// mixed history with aggressive pruning interleaved, replaying the pruned
// receipt file reproduces state, protection, and regressions exactly. Prunes
// land between passes and their later fails — the case where dropping a
// check's pass receipt would silently unprotect it.
func TestPruneReplayEquivalence(t *testing.T) {
        path := filepath.Join(t.TempDir(), "evidence.jsonl")
        l := newLedgerEvery(path, 25)

        for round := 0; round < 10; round++ {
                out := goTestPackagesOutput(12, 3)
                if round%2 == 0 {
                        // Even rounds: everything passes (protection grows).
                        out = goTestPackagesOutput(12, 0)
                }
                l.Observe("go test ./...", out, round%2 != 0, now.Add(time.Duration(round)*time.Minute))
        }
        // A build check that passes then regresses, with prunes around it.
        l.Observe("go build ./...", "", false, now)
        l.Observe("go build ./...", "main.go:1:1: undefined: Fix", true, now)

        wantState := stateMap(l)
        wantProtected := l.ProtectedCount()
        wantRegressions := l.Regressions()
        require.NotEmpty(t, wantRegressions, "fixture must end with protected regressions")
        require.Equal(t, 13, wantProtected) // 12 packages + go-build

        reloaded := NewLedger(path)
        require.NoError(t, reloaded.Load())
        assert.Equal(t, wantState, stateMap(reloaded))
        assert.Equal(t, wantProtected, reloaded.ProtectedCount())
        assert.Equal(t, wantRegressions, reloaded.Regressions())
}

// TestPruneKeepsProtectionForRegressedCheck pins the ratchet case: a check
// that passed, then failed, must replay as protected-and-failing even when
// the file was pruned between the two receipts. Without the last-pass
// receipt surviving in the file, replay would see only the fail and the
// regression would read as neutral fail→fail — the ratchet goes blind.
func TestPruneKeepsProtectionForRegressedCheck(t *testing.T) {
        path := filepath.Join(t.TempDir(), "evidence.jsonl")
        l := newLedgerEvery(path, 1) // prune after every receipt

        l.Observe("go test ./pkg/", "ok\tsome/pkg\t0.01s\n", false, now)
        l.Observe("go test ./pkg/", "FAIL\tsome/pkg\t0.01s\n", true, now)

        require.Equal(t, 1, l.ProtectedCount())
        require.Len(t, l.Regressions(), 1)

        reloaded := NewLedger(path)
        require.NoError(t, reloaded.Load())
        assert.Equal(t, 1, reloaded.ProtectedCount(),
                "the pass receipt must survive pruning so the check stays protected on replay")
        assert.Len(t, reloaded.Regressions(), 1)
        assert.Equal(t, Fail, stateMap(reloaded)["go-test:some/pkg"].Status)
}

// TestPruneBoundsReceiptFile proves the file stays bounded across a long
// session: after 1200 receipts over 40 checks with a prune every 50, the
// file holds at most last-pass + last record per check plus one prune
// interval — never the full history.
func TestPruneBoundsReceiptFile(t *testing.T) {
        path := filepath.Join(t.TempDir(), "evidence.jsonl")
        l := newLedgerEvery(path, 50)

        const checks = 40
        for round := 0; round < 30; round++ {
                l.Observe("go test ./...", goTestPackagesOutput(checks, 7), true, now)
        }

        lines := countReceiptLines(t, path)
        assert.LessOrEqual(t, lines, 2*checks+50,
                "receipt file must stay bounded: pruned set plus at most one prune interval")
        assert.GreaterOrEqual(t, lines, checks, "bounded does not mean empty — every observed check is still present")

        reloaded := NewLedger(path)
        require.NoError(t, reloaded.Load())
        assert.Equal(t, stateMap(l), stateMap(reloaded))
        assert.Equal(t, l.ProtectedCount(), reloaded.ProtectedCount())
}

// TestPruneAtLoadSelfHeals covers session resume against a legacy receipt
// file that grew past the prune interval (written with pruning disabled):
// Load replays it, then compacts it so the next resume replays the minimal
// set instead of the whole history.
func TestPruneAtLoadSelfHeals(t *testing.T) {
        path := filepath.Join(t.TempDir(), "evidence.jsonl")
        l := newLedgerEvery(path, 0) // pruning disabled while writing history
        for round := 0; round < 100; round++ {
                l.Observe("go test ./...", goTestPackagesOutput(12, 5), true, now)
        }
        require.Equal(t, 1200, countReceiptLines(t, path))

        resumed := NewLedger(path) // production prune interval
        require.NoError(t, resumed.Load())
        assert.LessOrEqual(t, countReceiptLines(t, path), 2*12,
                "Load must compact an overgrown receipt file")
        assert.Equal(t, stateMap(l), stateMap(resumed))
        assert.Equal(t, l.ProtectedCount(), resumed.ProtectedCount())
        assert.Equal(t, l.Regressions(), resumed.Regressions())
}

// TestPruneMemoryOnlyLedger: a memory-only ledger (in-memory session) never
// touches the filesystem, pruning included.
func TestPruneMemoryOnlyLedger(t *testing.T) {
        l := newLedgerEvery("", 1)
        l.Observe("go build ./...", "", false, now)
        l.Observe("go build ./...", "x.go:1:1: bad", true, now)

        assert.Empty(t, l.Path())
        assert.Len(t, l.Regressions(), 1)
        assert.Equal(t, 1, l.ProtectedCount())
}

// TestRevertToReceiptSurvivesPrune: the harness-written rollback receipt
// (harness:candidate-rollback) is a Pass record like any other; after a
// prune and reload the reverted check still reads passing and protected,
// and its receipt still names the harness cause.
func TestRevertToReceiptSurvivesPrune(t *testing.T) {
        path := filepath.Join(t.TempDir(), "evidence.jsonl")
        l := newLedgerEvery(path, 1)

        l.Observe("go build ./...", "", false, now)
        l.Observe("go build ./...", "main.go:1:1: broken", true, now)
        require.Len(t, l.Regressions(), 1)

        l.RevertTo([]Record{{Check: "go-build", Status: Pass}}, []string{"go-build"}, "candidate-rollback")

        reloaded := NewLedger(path)
        require.NoError(t, reloaded.Load())
        rec := stateMap(reloaded)["go-build"]
        assert.Equal(t, Pass, rec.Status)
        assert.Equal(t, 1, reloaded.ProtectedCount())
        assert.Equal(t, "harness:candidate-rollback", rec.Command)
        assert.Empty(t, reloaded.Regressions())
}
