package transcript_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/block"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/transcript"
)

// A raw string literal: the file content has both quote kinds in it.
const checklistText = `# Plan

- [x] Read the current writer
  - handles escaped quotes
- [~] Patch the parser
- [ ] Update the tests
- [ ] Update the docs
`

func writeChecklist(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "TODO.md")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func TestMapperChecklistRowLeadsTranscript(t *testing.T) {
	dir := t.TempDir()
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.Checklist = transcript.NewChecklistSource(writeChecklist(t, dir, checklistText))

	entries, ids, _ := m.Sync(nil, nil, session.Snapshot{
		Messages: []session.Message{{
			ID: "m1", Role: session.RoleAssistant, State: session.StateComplete,
			Content: []session.ContentBlock{{Type: session.BlockToolUse, ID: "c1", Name: "read", Complete: true}},
		}},
		Tools: map[string]session.ToolRun{"c1": {
			ToolUseID: "c1", Name: "read", Status: session.ToolDone, Detail: "haiku.txt",
		}},
	})

	require.Len(t, entries, 2, "the checklist row plus the tool row")
	assert.Equal(t, transcript.ChecklistSourceEntryID, ids[0], "the row leads")
	cl, ok := entries[0].(*block.ChecklistBlock)
	require.True(t, ok, "got %T", entries[0])

	done, open, _ := cl.Counts()
	assert.Equal(t, 1, done)
	assert.Equal(t, 2, open)
}

func TestMapperChecklistAbsentHidesRow(t *testing.T) {
	dir := t.TempDir()
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.Checklist = transcript.NewChecklistSource(filepath.Join(dir, "nope.md"))
	entries, _, _ := m.Sync(nil, nil, session.Snapshot{})
	assert.Empty(t, entries, "no task file, no row")
}

func TestMapperChecklistAppearsWhenFileIsCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TODO.md")
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.Checklist = transcript.NewChecklistSource(path)

	entries, _, _ := m.Sync(nil, nil, session.Snapshot{})
	assert.Empty(t, entries)

	// The agent creates the file mid-session; the row appears on the next sync.
	require.NoError(t, os.WriteFile(path, []byte(checklistText), 0o644))
	// The first Sync stat-found nothing, so loaded is false and the next stat
	// is a change regardless of mtime granularity.
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, os.WriteFile(path, []byte("- [ ] first\n- [x] second\n"), 0o644))

	entries, ids, dirty := m.Sync(nil, nil, session.Snapshot{})
	require.Len(t, entries, 1)
	assert.Equal(t, transcript.ChecklistSourceEntryID, ids[0])
	assert.Contains(t, dirty, 0)
}

func TestMapperChecklistRefreshPicksEdits(t *testing.T) {
	dir := t.TempDir()
	path := writeChecklist(t, dir, checklistText)
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.Checklist = transcript.NewChecklistSource(path)

	entries, _, _ := m.Sync(nil, nil, session.Snapshot{})
	done, _, _ := entries[0].(*block.ChecklistBlock).Counts()
	assert.Equal(t, 1, done, "one ticked item in the fixture")

	time.Sleep(5 * time.Millisecond)
	require.NoError(t, os.WriteFile(path, []byte("- [x] a\n- [x] b\n- [x] c\n"), 0o644))
	entries, _, dirty := m.Sync(nil, nil, session.Snapshot{})
	require.Len(t, entries, 1)
	done, _, _ = entries[0].(*block.ChecklistBlock).Counts()
	assert.Equal(t, 3, done, "the row re-read once the file changed")
	assert.Contains(t, dirty, 0, "and the entry is marked dirty so the height refreshes")
}

func TestMapperChecklistUserToggleWins(t *testing.T) {
	dir := t.TempDir()
	path := writeChecklist(t, dir, checklistText)
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.Checklist = transcript.NewChecklistSource(path)

	first, _, _ := m.Sync(nil, nil, session.Snapshot{})
	// Auto-expand is off here, so the row starts collapsed and one Enter opens
	// it — the toggle path is what records the user's choice.
	assert.False(t, first[0].(*block.ChecklistBlock).Expanded)
	first[0].Handle(&components.EventContext{}, xui.KeyEvent{Press: true, Code: xui.KeyEnter})

	second, _, _ := m.Sync(first, []string{transcript.ChecklistSourceEntryID}, session.Snapshot{})
	assert.True(t, second[0].(*block.ChecklistBlock).Expanded, "the open sticks")

	// And so does a later collapse.
	second[0].Handle(&components.EventContext{}, xui.KeyEvent{Press: true, Code: xui.KeyEnter})
	third, _, _ := m.Sync(second, []string{transcript.ChecklistSourceEntryID}, session.Snapshot{})
	assert.False(t, third[0].(*block.ChecklistBlock).Expanded, "and the close sticks")
}

func TestMapperChecklistStalenessCountsEdits(t *testing.T) {
	dir := t.TempDir()
	path := writeChecklist(t, dir, checklistText)
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.Checklist = transcript.NewChecklistSource(path)

	entries, _, _ := m.Sync(nil, nil, session.Snapshot{
		Messages: []session.Message{{
			ID: "m1", Role: session.RoleAssistant, State: session.StateComplete,
			Content: []session.ContentBlock{
				{Type: session.BlockToolUse, ID: "e1", Name: "edit", Complete: true},
				{Type: session.BlockToolUse, ID: "r1", Name: "read", Complete: true},
				{Type: session.BlockToolUse, ID: "e2", Name: "write", Complete: true},
			},
		}},
		Tools: map[string]session.ToolRun{
			"e1": {ToolUseID: "e1", Name: "edit", Status: session.ToolDone, Detail: "a.go"},
			"r1": {ToolUseID: "r1", Name: "read", Status: session.ToolDone, Detail: "b.go"},
			"e2": {ToolUseID: "e2", Name: "write", Status: session.ToolDone, Detail: "c.go"},
		},
	})
	cl := entries[0].(*block.ChecklistBlock)
	assert.Equal(t, 2, cl.StaleFor, "edit and write count; the read does not")
}
