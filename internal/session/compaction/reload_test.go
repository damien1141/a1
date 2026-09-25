package compaction

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/session"
)

// seedSession writes n user/assistant turns of growing size, each assistant
// reporting a much larger cumulative context size, and returns the persisted
// session file.
func seedSession(t *testing.T, turns int) (path string, live []session.MessageEntry) {
	t.Helper()
	dir := t.TempDir()
	m, err := session.NewSessionManager(dir, session.WithSessionDir(dir), session.WithShouldFlush(true))
	require.NoError(t, err)

	for i := range turns {
		_, err = m.Append(llm.Message{Role: llm.RoleUser, Content: sizedContent(10)})
		require.NoError(t, err)
		_, err = m.Append(llm.Message{
			Role:    llm.RoleAssistant,
			Content: sizedContent(10 * (i + 1)),
			Usage:   llm.Usage{TotalTokens: 5000 * (i + 1)},
		})
		require.NoError(t, err)
	}
	return m.File(), m.BuildContext()
}

// Compaction reads usage from the entry wrapper, which is the only copy that
// survives persistence: reading llm.Message.Usage instead made every resumed
// session look like it had spent zero tokens, so TokensBefore was lost. The cut
// itself must come out identical in memory and after a reload.
func TestPrepareCompact_ReloadedSessionMatchesInMemory(t *testing.T) {
	path, liveEntries := seedSession(t, 4)
	settings := Settings{keepRecentTokens: 100}

	inMemory, err := PrepareCompact(liveEntries, settings)
	require.NoError(t, err)
	require.NotEmpty(t, inMemory.FirstKeptEntryId)

	reloaded, err := session.OpenSession(path)
	require.NoError(t, err)

	afterReload, err := PrepareCompact(reloaded.BuildContext(), settings)
	require.NoError(t, err)

	assert.Equal(t, inMemory.FirstKeptEntryId, afterReload.FirstKeptEntryId)
	assert.Equal(t, inMemory.TokensBefore, afterReload.TokensBefore)
	assert.NotZero(t, afterReload.TokensBefore)
	assert.NotEmpty(t, afterReload.MessagesToSummarize)
	// The trailing budget is kept, not just the newest message.
	assert.NotEqual(t, liveEntries[len(liveEntries)-1].GetID(), afterReload.FirstKeptEntryId)
}

func TestFindCutIndex_ReloadedEntriesRespectTokenBudget(t *testing.T) {
	path, _ := seedSession(t, 4)

	reloaded, err := session.OpenSession(path)
	require.NoError(t, err)
	entries := reloaded.BuildContext()

	cutPoints := make([]int, 0, len(entries))
	for i, entry := range entries {
		if entry.GetType() == session.EntryMessage {
			cutPoints = append(cutPoints, i)
		}
	}

	// Message sizes are 10, 10, 10, 20, 10, 30, 10, 40 tokens; a 25-token budget
	// is blown by the tail alone, so the cut must not fall back to the earliest
	// cut point.
	cutIndex := findCutIndex(entries, 0, len(entries), 25, cutPoints)
	assert.NotEqual(t, cutPoints[0], cutIndex)
}
