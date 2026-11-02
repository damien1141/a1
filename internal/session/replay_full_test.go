package session

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// A compaction must cut the MODEL context but never the UI scrollback:
// FullBranch + ReplaySnapshot keep pre-compaction messages reachable and
// render the cut itself as a marker row, while BuildContext (the model
// context) stops at the retained tail.
func TestFullBranchKeepsPreCompactionScrollback(t *testing.T) {
	m := NewManager(t.TempDir())

	_, err := m.Append(llm.Message{Role: llm.RoleUser, Content: "one"})
	require.NoError(t, err)
	_, err = m.Append(llm.Message{Role: llm.RoleAssistant, Content: "two"})
	require.NoError(t, err)
	// The compaction retains everything from the assistant entry down.
	kept := m.LeafID()
	_, err = m.AppendCompaction(Compaction{Summary: "summarized", FirstKeptEntryID: kept})
	require.NoError(t, err)
	_, err = m.Append(llm.Message{Role: llm.RoleUser, Content: "three"})
	require.NoError(t, err)
	_, err = m.Append(llm.Message{Role: llm.RoleAssistant, Content: "four"})
	require.NoError(t, err)

	full := m.FullBranch()
	require.Len(t, full, 5, "every branch entry, header excluded, oldest first")

	snap := ReplaySnapshot(full, nil)
	var texts []string
	sawMarker := false
	for _, msg := range snap.Messages {
		if msg.Role == RoleCompaction {
			sawMarker = true
			continue
		}
		texts = append(texts, msg.Text)
	}
	assert.True(t, sawMarker, "the compaction renders as a marker row")
	assert.Equal(t, []string{"one", "two", "three", "four"}, texts,
		"pre-compaction messages stay in the scrollback")

	// Contrast: the model context stops at the newest compaction's tail —
	// "one" is summarized away there, and only there.
	var ctxTexts []string
	for _, e := range m.BuildContext() {
		if me, ok := e.(SessionMessageEntry); ok {
			ctxTexts = append(ctxTexts, me.Message.Content)
		}
	}
	assert.Equal(t, []string{"two", "three", "four"}, ctxTexts,
		"BuildContext keeps only the compaction's retained tail")
}

// Tool sources must survive persist/reload: the run reconstructed from the
// persisted entries carries the structured cards the web transcript
// renders as pills.
func TestReplayRestoresToolSources(t *testing.T) {
	m := NewManager(t.TempDir())

	_, err := m.Append(llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID:       "call-1",
			Function: llm.Function{Name: "fetch", Arguments: `{"mode":"search","query":"go landlock"}`},
		}},
	})
	require.NoError(t, err)
	_, err = m.AppendToolResult(
		llm.Message{Role: llm.RoleTool, ToolCallID: "call-1", Content: "results"},
		[]ToolSource{{
			Rank: 1, URL: "https://example.com", Title: "Example",
			Host: "example.com", FaviconURL: "https://example.com/f.png",
		}},
	)
	require.NoError(t, err)

	snap := ReplaySnapshot(m.FullBranch(), nil)
	run, ok := snap.Tools["call-1"]
	require.True(t, ok, "the tool run is reconstructed from the persisted entries")
	require.Len(t, run.Sources, 1)
	assert.Equal(t, "https://example.com", run.Sources[0].URL)
	assert.Equal(t, "fetch", run.Name, "Apply's merge carries the name from the tool_use block")
	assert.Equal(t, "results", run.Output)
}

// Both backings (jsonl file and vault rows) persist the marshaled entry
// line, so the sources field riding that line is the whole wire contract.
func TestToolSourcesSurviveEntryJSONRoundTrip(t *testing.T) {
	entry := SessionMessageEntry{
		SessionBaseEntry: SessionBaseEntry{Type: EntryMessage, ID: "e1"},
		Message:          llm.Message{Role: llm.RoleTool, ToolCallID: "call-1", Content: "results"},
		ToolSources:      []ToolSource{{Rank: 1, URL: "https://example.com"}},
	}
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"tool_sources"`, "the field rides the persisted line")

	var back SessionMessageEntry
	require.NoError(t, json.Unmarshal(data, &back))
	require.Len(t, back.ToolSources, 1)
	assert.Equal(t, "https://example.com", back.ToolSources[0].URL)
}
