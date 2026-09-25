package compaction

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/session"
)

// sizedContent returns content of exactly `tokens` estimated tokens (4 chars
// per token), so fixtures control the cut budget through message size.
func sizedContent(tokens int) string {
	return strings.Repeat("x", tokens*4)
}

// msgEntry builds a persisted message entry of `tokens` estimated tokens.
// `usage` is the provider-reported context size, which only feeds TokensBefore
// (llm.Message.Usage is json:"-" and does not survive a reload, so it lives on
// the entry wrapper).
func msgEntry(id string, role llm.Role, tokens, usage int) session.MessageEntry {
	return session.SessionMessageEntry{
		SessionBaseEntry: session.SessionBaseEntry{ID: id},
		Message:          llm.Message{Role: role, Content: sizedContent(tokens)},
		Usage:            llm.Usage{TotalTokens: usage},
	}
}

func TestPrepareCompact_AlreadyCompacted_ReturnsEmptyPreparation(t *testing.T) {
	entries := []session.MessageEntry{
		msgEntry("e1", llm.RoleUser, 10, 0),
		session.CompactionEntry{
			SessionBaseEntry: session.SessionBaseEntry{ID: "comp1"},
			Compaction: session.Compaction{
				Summary: "prev",
			},
		},
	}
	settings := Settings{keepRecentTokens: 100}

	prep, err := PrepareCompact(entries, settings)

	assert.NoError(t, err)
	assert.NotNil(t, prep)
	assert.Empty(t, prep.FirstKeptEntryId)
	assert.Nil(t, prep.MessagesToSummarize)
}

func TestPrepareCompact_SessionNeedsMigration_ReturnsError(t *testing.T) {
	entries := []session.MessageEntry{
		session.SessionMessageEntry{
			SessionBaseEntry: session.SessionBaseEntry{ID: ""},
			Message:          llm.Message{Role: llm.RoleUser},
			Usage:            llm.Usage{TotalTokens: 10},
		},
	}
	settings := Settings{keepRecentTokens: 100}

	prep, err := PrepareCompact(entries, settings)

	assert.Error(t, err)
	assert.Nil(t, prep)
	assert.Contains(t, err.Error(), "migration")
}

func TestPrepareCompact_NoPreviousCompaction_SplitsByKeepRecentTokens(t *testing.T) {
	entries := []session.MessageEntry{
		msgEntry("e1", llm.RoleUser, 10, 0),
		msgEntry("e2", llm.RoleAssistant, 20, 20),
		msgEntry("e3", llm.RoleUser, 30, 0),
	}
	settings := Settings{keepRecentTokens: 25}

	prep, err := PrepareCompact(entries, settings)

	assert.NoError(t, err)
	assert.NotNil(t, prep)
	assert.Equal(t, "e3", prep.FirstKeptEntryId)
	assert.False(t, prep.IsMidTurnCut)
	assert.Len(t, prep.MessagesToSummarize, 2)
	assert.Equal(t, llm.RoleUser, prep.MessagesToSummarize[0].Role)
	assert.Equal(t, llm.RoleAssistant, prep.MessagesToSummarize[1].Role)
	// TokensBefore is the provider-reported context size, not the sum of the
	// estimated message sizes the cut uses.
	assert.Equal(t, 20, prep.TokensBefore)
	assert.Empty(t, prep.PreviousSummary)
	assert.Nil(t, prep.PreviousPreserveData)
}

// Everything fits in keepRecentTokens, so there is nothing to summarize: the
// preparation must stay empty rather than persist a "No prior history." summary
// over the previous one.
func TestPrepareCompact_UnderTokenLimit_ReturnsEmptyPreparation(t *testing.T) {
	entries := []session.MessageEntry{
		msgEntry("e1", llm.RoleUser, 10, 0),
		msgEntry("e2", llm.RoleAssistant, 20, 20),
	}
	settings := Settings{keepRecentTokens: 100}

	prep, err := PrepareCompact(entries, settings)

	assert.NoError(t, err)
	assert.NotNil(t, prep)
	assert.Empty(t, prep.FirstKeptEntryId)
	assert.Empty(t, prep.MessagesToSummarize)
	assert.Empty(t, prep.TurnPrefixMessages)
}

func TestPrepareCompact_WithPreviousCompaction_SetsSummaryAndPreserveData(t *testing.T) {
	entries := []session.MessageEntry{
		session.CompactionEntry{
			SessionBaseEntry: session.SessionBaseEntry{ID: "comp1"},
			Compaction: session.Compaction{
				Summary:      "old summary",
				PreserveData: map[string]any{"k": "v"},
			},
		},
		msgEntry("e1", llm.RoleUser, 10, 0),
		msgEntry("e2", llm.RoleAssistant, 10, 10),
		msgEntry("e3", llm.RoleUser, 10, 0),
		msgEntry("e4", llm.RoleAssistant, 10, 40),
	}
	// Budget reached on e3 (the last two messages cost 20), so e1+e2 are
	// summarized and the previous summary is carried for the update prompt.
	settings := Settings{keepRecentTokens: 20}

	prep, err := PrepareCompact(entries, settings)

	assert.NoError(t, err)
	assert.NotNil(t, prep)
	assert.Equal(t, "old summary", prep.PreviousSummary)
	assert.Equal(t, map[string]any{"k": "v"}, prep.PreviousPreserveData)
	assert.Equal(t, "e3", prep.FirstKeptEntryId)
	assert.False(t, prep.IsMidTurnCut)
	assert.Len(t, prep.MessagesToSummarize, 2)
}

func TestPrepareCompact_MidTurnCut_SplitsTurnPrefix(t *testing.T) {
	entries := []session.MessageEntry{
		msgEntry("e1", llm.RoleUser, 10, 0),
		msgEntry("e2", llm.RoleAssistant, 10, 10),
		msgEntry("e3", llm.RoleUser, 10, 0),
		msgEntry("e4", llm.RoleAssistant, 30, 40),
	}
	settings := Settings{keepRecentTokens: 20}

	prep, err := PrepareCompact(entries, settings)

	assert.NoError(t, err)
	assert.NotNil(t, prep)
	// The budget is reached on the newest assistant message, which is not a
	// turn start: the user message opening that turn lands in the prefix bucket
	// and everything before it is summarized.
	assert.Equal(t, "e4", prep.FirstKeptEntryId)
	assert.True(t, prep.IsMidTurnCut)
	assert.Len(t, prep.MessagesToSummarize, 2)
	assert.Equal(t, llm.RoleAssistant, prep.MessagesToSummarize[1].Role)
	assert.Len(t, prep.TurnPrefixMessages, 1)
	assert.Equal(t, llm.RoleUser, prep.TurnPrefixMessages[0].Role)
}

// The summary cap follows the headroom compaction frees, and a summary that
// stopped at that cap must abort the compaction instead of becoming the new
// session summary.
func TestCompact_TruncatedSummary_ReturnsError(t *testing.T) {
	entries := []session.MessageEntry{
		msgEntry("e1", llm.RoleUser, 10, 0),
		msgEntry("e2", llm.RoleAssistant, 20, 20),
		msgEntry("e3", llm.RoleUser, 30, 0),
	}
	settings := Settings{reverseTokens: 16384, keepRecentTokens: 25}

	prep, err := PrepareCompact(entries, settings)
	require.NoError(t, err)
	require.Equal(t, 16384, prep.ReserveTokens)

	c := &captureCompactor{truncated: true}
	comp, err := Compact(t.Context(), *prep, c)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "generation hit the token cap")
	assert.Empty(t, comp.Summary)
	assert.Equal(t, []int{13107}, c.maxTokens, "0.8 * reserveTokens")
}
