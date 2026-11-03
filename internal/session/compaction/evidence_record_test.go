package compaction

import (
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/session"
)

func TestWrapEvidenceRecord(t *testing.T) {
        assert.Empty(t, wrapEvidenceRecord(""))
        assert.Empty(t, wrapEvidenceRecord("   \n  "))

        wrapped := wrapEvidenceRecord("Protected checks: 2")
        assert.True(t, strings.HasPrefix(wrapped, evidenceRecordOpen+"\n"))
        assert.True(t, strings.HasSuffix(wrapped, "\n"+evidenceRecordClose))
        assert.Contains(t, wrapped, "Protected checks: 2")
}

func TestStripEvidenceRecord(t *testing.T) {
        record := wrapEvidenceRecord("state A")

        // No markers: untouched.
        plain := "just a summary"
        assert.Equal(t, plain, stripEvidenceRecord(plain))

        // One record between narrative text: record removed, text kept.
        mixed := "before" + "\n\n" + record + "\n\n" + "after"
        assert.Equal(t, "before\n\nafter", stripEvidenceRecord(mixed))

        // Two records: both removed.
        two := "a" + record + "b" + wrapEvidenceRecord("state B") + "c"
        assert.Equal(t, "abc", stripEvidenceRecord(two))

        // Unclosed opening marker: not ours to interpret — left untouched.
        unclosed := "text <evidence-record> still open"
        assert.Equal(t, unclosed, stripEvidenceRecord(unclosed))

        // Round-trip: stripping what wrapping added yields the input.
        assert.Equal(t, "summary", stripEvidenceRecord("summary\n\n"+record))
}

func TestCompactAppendsEvidenceRecord(t *testing.T) {
        prep := CompactionPreparation{
                MessagesToSummarize: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
                EvidenceState:       "Protected checks: 3\nPassing now: go-build",
        }
        comp, err := Compact(t.Context(), prep, &captureCompactor{})
        require.NoError(t, err)

        assert.Equal(t,
                "SUMMARY\n\n"+evidenceRecordOpen+"\nProtected checks: 3\nPassing now: go-build\n"+evidenceRecordClose,
                comp.Summary,
                "evidence record is appended verbatim after the model summary")
}

// TestCompactEvidenceRecordAfterFileOps pins section order: model summary,
// then file operations, then the evidence record — harness state last, so
// it can never be mistaken for summarizer output.
func TestCompactEvidenceRecordAfterFileOps(t *testing.T) {
        prep := CompactionPreparation{
                MessagesToSummarize: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
                FileOps:             FileOperation{edited: []string{"main.go"}},
                EvidenceState:       "Protected checks: 1",
        }
        comp, err := Compact(t.Context(), prep, &captureCompactor{})
        require.NoError(t, err)

        summaryAt := strings.Index(comp.Summary, "SUMMARY")
        filesAt := strings.Index(comp.Summary, "main.go")
        evidenceAt := strings.Index(comp.Summary, evidenceRecordOpen)
        require.GreaterOrEqual(t, summaryAt, 0)
        require.GreaterOrEqual(t, filesAt, 0)
        require.GreaterOrEqual(t, evidenceAt, 0)
        assert.Less(t, summaryAt, filesAt)
        assert.Less(t, filesAt, evidenceAt)
}

func TestCompactNoEvidenceStateNoRecord(t *testing.T) {
        prep := CompactionPreparation{
                MessagesToSummarize: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
        }
        comp, err := Compact(t.Context(), prep, &captureCompactor{})
        require.NoError(t, err)
        assert.NotContains(t, comp.Summary, evidenceRecordOpen,
                "a session with no evidence gets no record — chat compaction is unchanged")
}

// TestPrepareCompactStripsPreviousEvidenceRecord: the previous summary's
// evidence record is stale by definition — PrepareCompact removes it before
// the update prompt ever sees it, so the fresh record the harness appends is
// the only one in play.
func TestPrepareCompactStripsPreviousEvidenceRecord(t *testing.T) {
        oldRecord := wrapEvidenceRecord("Protected checks: 99 (STALE)")
        entries := []session.MessageEntry{
                msgEntry("e1", llm.RoleUser, 10, 0),
                msgEntry("e2", llm.RoleAssistant, 10, 0),
                session.CompactionEntry{
                        SessionBaseEntry: session.SessionBaseEntry{ID: "comp1"},
                        Compaction: session.Compaction{
                                Summary:          "narrative before\n\n" + oldRecord + "\n\nnarrative after",
                                FirstKeptEntryID: "e2",
                        },
                },
                msgEntry("e3", llm.RoleUser, 10, 0),
                msgEntry("e4", llm.RoleAssistant, 40, 40),
        }
        settings := Settings{keepRecentTokens: 10}

        prep, err := PrepareCompact(t.Context(), entries, settings)
        require.NoError(t, err)

        assert.Equal(t, "narrative before\n\nnarrative after", prep.PreviousSummary)
        assert.NotContains(t, prep.PreviousSummary, "STALE")
}
