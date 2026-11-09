package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/agent"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/session"
)

// writeCompactedSession persists a session whose branch carries a compaction:
// four messages, a compaction that retains from the third, then a fifth. It
// returns the session id. This is the on-disk shape /resume reopens.
func writeCompactedSession(t *testing.T, dir string) string {
	t.Helper()
	m, err := session.NewSessionManager(t.TempDir(),
		session.WithSessionDir(dir),
		session.WithShouldFlush(true),
	)
	require.NoError(t, err)
	for _, msg := range []llm.Message{
		{Role: llm.RoleUser, Content: "one"},
		{Role: llm.RoleAssistant, Content: "two"},
	} {
		_, err = m.Append(msg)
		require.NoError(t, err)
	}
	// The compaction retains everything from the user message "three" down.
	kept := m.LeafID()
	_, err = m.Append(llm.Message{Role: llm.RoleUser, Content: "three"})
	require.NoError(t, err)
	_, err = m.AppendCompaction(session.Compaction{
		Summary:          "summarized",
		FirstKeptEntryID: kept,
	})
	require.NoError(t, err)
	_, err = m.Append(llm.Message{Role: llm.RoleAssistant, Content: "four"})
	require.NoError(t, err)
	return m.ID()
}

// A resumed session's transcript is the human's scrollback, and a compaction
// cuts only the MODEL's context. ReplaySnapshot must therefore walk the full
// branch: every pre-compaction message stays above the cut marker, even
// though the context path (what the next request sends) stops at the
// compaction's retained tail. Replaying PathEntries instead made a resumed
// TUI open with its early history silently gone.
func TestReplaySnapshotKeepsPreCompactionScrollback(t *testing.T) {
	dir := t.TempDir()
	id := writeCompactedSession(t, dir)

	sess, err := agent.NewSession(
		agent.WithCwd(t.TempDir()),
		agent.WithSessionDir(dir),
		agent.WithResumeID(id),
	)
	require.NoError(t, err)
	eng, err := agent.NewEngine(llm.ModelConfig{Name: "fake", APIKey: "x"}, sess,
		agent.WithGate(permission.AllowAll{}),
		agent.WithTools(nil),
	)
	require.NoError(t, err)
	c := &EngineController{engine: eng}

	snap := c.ReplaySnapshot()

	var texts []string
	sawMarker := false
	for _, m := range snap.Messages {
		if m.Role == session.RoleCompaction {
			sawMarker = true
			continue
		}
		texts = append(texts, m.Text)
	}
	assert.True(t, sawMarker, "the compaction renders as a marker row")
	assert.Equal(t, []string{"one", "two", "three", "four"}, texts,
		"pre-compaction messages stay in the resumed transcript")

	// Contrast: the context path keeps only the retained tail — the cut
	// must still apply to what the model sees.
	var ctxTexts []string
	for _, e := range sess.PathEntries() {
		if me, ok := e.(session.SessionMessageEntry); ok {
			ctxTexts = append(ctxTexts, me.Message.Content)
		}
	}
	assert.Equal(t, []string{"two", "three", "four"}, ctxTexts,
		"the model context still stops at the compaction's retained tail")
}
