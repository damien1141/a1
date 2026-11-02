package agent

import (
        "fmt"
        "strings"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/session"
)

const vetFailOutput = "auth/handler.go:12:34: this value of x is never used\n(exit error: exit status 1)"

const authTestFailOutput = `--- FAIL: TestLogin (0.00s)
    auth_test.go:42: session not cleared
FAIL    github.com/damien1141/a1/internal/auth  0.103s`

// newEvidenceRecordEngine builds an engine whose client talks to a
// compactingTextServer: streaming requests answer with text, non-streaming
// requests answer with a compact summary.
func newEvidenceRecordEngine(t *testing.T, serverURL string) *Engine {
        t.Helper()
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        engine, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: serverURL, APIKey: "x"},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools(nil), // no tool ever runs in these tests
        )
        require.NoError(t, err)
        return engine
}

// TestRenderEvidenceRecordChatInert pins the Phase 4 chat guarantee: a
// session that never ran a recognized check and never declared a contract
// renders no evidence record, so its compaction summaries are
// byte-identical to pre-evidence compaction. Regular chat use is unchanged.
func TestRenderEvidenceRecordChatInert(t *testing.T) {
        engine := newEvidenceRecordEngine(t, "")
        assert.Empty(t, engine.renderEvidenceRecord())
}

func TestRenderEvidenceRecordFull(t *testing.T) {
        engine := newEvidenceRecordEngine(t, "")
        ledger := engine.EvidenceLedger()

        // go-build: protected and passing.
        ledger.Observe("go build ./...", "", false, time.Now())
        // go-vet: protected, then regressed.
        ledger.Observe("go vet ./...", "", false, time.Now())
        ledger.Observe("go vet ./...", vetFailOutput, true, time.Now())
        // auth package: failing, never passed.
        ledger.Observe("go test ./internal/auth/", authTestFailOutput, true, time.Now())

        engine.turnContract.declare([]string{
                "go-build",
                "go-vet",
                "go-test:github.com/damien1141/a1/internal/auth",
        })

        record := engine.renderEvidenceRecord()

        assert.Contains(t, record, "Protected checks (passed at least once): 2")
        assert.Contains(t, record, "Passing now: go-build")
        assert.Contains(t, record, "Protected regressions (were passing, now failing")
        assert.Contains(t, record, "go-vet — auth/handler.go:12:34: this value of x is never used")
        assert.Contains(t, record, "Failing, never passed")
        assert.Contains(t, record, "go-test:github.com/damien1141/a1/internal/auth")
        assert.Contains(t, record, "Declared done-criteria (this turn's contract")
        assert.Contains(t, record, "go-build: PASSING")
        assert.Contains(t, record, "go-vet: FAILING")
        assert.Contains(t, record, "go-test:github.com/damien1141/a1/internal/auth: FAILING")
        assert.Contains(t, record, "Residual: 2 declared check(s) not green")
        assert.NotContains(t, record, "contract satisfied")
}

// TestRenderEvidenceRecordCaps: a big workspace lists the head of each
// section and counts the rest — the record stays bounded.
func TestRenderEvidenceRecordCaps(t *testing.T) {
        engine := newEvidenceRecordEngine(t, "")
        var b strings.Builder
        for i := 0; i < 40; i++ {
                if i > 0 {
                        b.WriteString("\n")
                }
                b.WriteString("ok\tgithub.com/x/p/pkg")
                b.WriteString(fmt.Sprintf("%02d", i))
                b.WriteString("\t0.01s")
        }
        engine.EvidenceLedger().Observe("go test ./...", b.String(), false, time.Now())

        record := engine.renderEvidenceRecord()
        assert.Contains(t, record, "Protected checks (passed at least once): 40")
        assert.Contains(t, record, "Passing now: go-test:github.com/x/p/pkg00")
        assert.Contains(t, record, "(+10 more)")
}

// TestRenderEvidenceRecordContractOnly: an empty ledger with a declared
// contract still renders — after a mid-turn compaction the model must not
// forget it declared done-criteria, even before any check has run.
func TestRenderEvidenceRecordContractOnly(t *testing.T) {
        engine := newEvidenceRecordEngine(t, "")
        engine.turnContract.declare([]string{"go-build", "go-vet"})

        record := engine.renderEvidenceRecord()
        assert.Contains(t, record, "Declared done-criteria")
        assert.Contains(t, record, "go-build: NO EVIDENCE — never observed passing; run it")
        assert.Contains(t, record, "go-vet: NO EVIDENCE — never observed passing; run it")
        assert.Contains(t, record, "Residual: 2 declared check(s) not green")
        assert.NotContains(t, record, "Passing now")
}

// TestForceCompactAppendsEvidenceRecord is the end-to-end wiring check: a
// forced compaction on an engine with evidence and a declared contract
// persists a summary that ends with the harness evidence record.
func TestForceCompactAppendsEvidenceRecord(t *testing.T) {
        server := compactingTextServer(nil)
        defer server.Close()
        engine := newEvidenceRecordEngine(t, server.URL)
        seedHistory(t, engine.session, 3) // enough history to have something to fold

        ledger := engine.EvidenceLedger()
        ledger.Observe("go build ./...", "", false, time.Now())
        ledger.Observe("go test ./internal/auth/", authTestFailOutput, true, time.Now())
        engine.turnContract.declare([]string{"go-build", "go-test:github.com/damien1141/a1/internal/auth"})

        did, err := engine.ForceCompact(t.Context(), func(session.Event, error) bool { return true })
        require.NoError(t, err)
        assert.True(t, did, "seeded history must produce a real compaction")

        entries := engine.session.PathEntries()
        var compactionEntry session.CompactionEntry
        found := false
        for _, e := range entries {
                if ce, ok := e.(session.CompactionEntry); ok {
                        compactionEntry = ce
                        found = true
                }
        }
        require.True(t, found, "a compaction entry must be persisted")

        summary := compactionEntry.Compaction.Summary
        assert.Contains(t, summary, "<evidence-record>")
        assert.True(t, strings.HasSuffix(summary, "</evidence-record>"),
                "the harness record is the last block of the summary")
        assert.Contains(t, summary, "Protected checks (passed at least once): 1")
        assert.Contains(t, summary, "go-test:github.com/damien1141/a1/internal/auth")
        assert.Contains(t, summary, "Residual: 1 declared check(s) not green")
}

// TestForceCompactChatSummaryUnchanged: the same forced compaction on a
// session with no evidence and no contract persists a plain summary with no
// evidence record — regular chat compaction is untouched by Phase 4.
func TestForceCompactChatSummaryUnchanged(t *testing.T) {
        server := compactingTextServer(nil)
        defer server.Close()
        engine := newEvidenceRecordEngine(t, server.URL)
        seedHistory(t, engine.session, 3)

        did, err := engine.ForceCompact(t.Context(), func(session.Event, error) bool { return true })
        require.NoError(t, err)
        assert.True(t, did)

        entries := engine.session.PathEntries()
        for _, e := range entries {
                if ce, ok := e.(session.CompactionEntry); ok {
                        assert.NotContains(t, ce.Compaction.Summary, "<evidence-record>",
                                ""+
                                        "no evidence and no contract means no record — the summary holds only "+
                                        "model-written text (history summary plus, here, the mid-turn turn-prefix join)")
                }
        }
}
