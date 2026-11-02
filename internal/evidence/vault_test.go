package evidence_test

import (
        "encoding/json"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/evidence"
        "github.com/damien1141/a1/internal/vault"
)

// TestVaultLedgerPersistReplayPrune mirrors the file-era persistence
// tests against the vault backing: receipts persist, replay rebuilds the
// ratchet, and pruning rewrites the minimal set transactionally.
func TestVaultLedgerPersistReplayPrune(t *testing.T) {
        v, err := vault.Open(t.TempDir())
        require.NoError(t, err)
        defer v.Close()
        scope := "/tmp/proj-sessions"

        l := evidence.NewVaultLedger(scope, v)
        require.NoError(t, l.Load())
        assert.Empty(t, l.State(), "fresh scope has no evidence")

        l.ObserveExit("go test ./internal/auth/", "ok  \tgithub.com/x/auth\t0.1s", 0, false, time.Now())
        l.ObserveExit("go test ./internal/auth/", "FAIL\tgithub.com/x/auth", 1, true, time.Now())
        assert.Len(t, l.Regressions(), 1, "pass→fail is a protected regression")

        // A second ledger over the same scope replays the receipts.
        l2 := evidence.NewVaultLedger(scope, v)
        require.NoError(t, l2.Load())
        assert.Len(t, l2.State(), 1)
        assert.Equal(t, 1, l2.ProtectedCount(), "protection survives replay")
        assert.Len(t, l2.Regressions(), 1)

        // Prune: the rewritten receipt set must be replay-equivalent — for a
        // regressed check that means its last PASS receipt followed by the
        // current FAIL (the pass receipt is what keeps it protected on replay).
        l2.Observe("go vet ./...", "stderr: unused import", true, time.Now())
        l2.Observe("go vet ./...", "stderr: more", true, time.Now())
        minimal := []string{
                mustJSON(t, evidence.Record{Check: "go-test:github.com/x/auth", Status: evidence.Pass,
                        At: time.Now(), Command: "go test ./internal/auth/"}),
                mustJSON(t, evidence.Record{Check: "go-test:github.com/x/auth", Status: evidence.Fail,
                        At: time.Now(), Command: "go test ./internal/auth/", Detail: "exit 1"}),
                mustJSON(t, evidence.Record{Check: "go-vet", Status: evidence.Fail,
                        At: time.Now(), Command: "go vet ./...", Detail: "stderr: more"}),
        }
        require.NoError(t, v.ReplaceEvidenceReceipts(scope, minimal))
        l3 := evidence.NewVaultLedger(scope, v)
        require.NoError(t, l3.Load())
        assert.Equal(t, 1, l3.ProtectedCount(), "the pass receipt keeps the check protected")
        assert.Len(t, l3.Regressions(), 1, "and the fail after it keeps the regression visible")

        // The reported path keeps the file-era shape (screenshot baselines
        // derive their directory from it).
        assert.Contains(t, l.Path(), scope)
        assert.Contains(t, l.Path(), "evidence.jsonl")
}

func mustJSON(t *testing.T, rec evidence.Record) string {
        t.Helper()
        b, err := json.Marshal(rec)
        require.NoError(t, err)
        return string(b)
}
