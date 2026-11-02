package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

// The checklist row is synthesized by the transcript mapper rather than
// projected from a session, and the transcript only re-syncs on a session event.
// So an existing TODO.md was invisible until the user sent their first prompt —
// the row existed but was never projected. This is the regression test for that.
func TestExistingChecklistRendersWithoutASession(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ChecklistFileName),
		[]byte("- [~] live task\n- [ ] open task\n- [x] done task\n"), 0o600))

	e := NewEditor(nil, nil, nil, nil, components.DefaultTheme(), dir, "", "", 0, nil, false)
	require.NotNil(t, e)

	entries := e.transcript.Entries()
	require.NotEmpty(t, entries, "the checklist row must be projected at startup")
	assert.Contains(t, e.transcript.EntryIDs(), "checklist", "the checklist entry must be present")

	s := entries[0].Draw(components.DrawContext{Max: components.Size{Width: 100, Height: 30}})
	got := components.SurfaceText(s)
	assert.Contains(t, got, "TODO.md", "the file name is in the header")
	assert.Contains(t, got, "1 in progress", "the in-flight task is counted")
	assert.Contains(t, got, "1 done")
	assert.Contains(t, got, "1 open")
}

// A project with no TODO.md must stay silent rather than render an empty row.
func TestNoChecklistFileRendersNothing(t *testing.T) {
	e := NewEditor(nil, nil, nil, nil, components.DefaultTheme(), t.TempDir(), "", "", 0, nil, false)
	require.NotNil(t, e)
	assert.NotContains(t, e.transcript.EntryIDs(), "checklist", "no file, no row")
}
