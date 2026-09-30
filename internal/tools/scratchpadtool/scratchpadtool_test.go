package scratchpadtool

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScratchpadTool_Definition(t *testing.T) {
	tool := ScratchpadTool()
	assert.Equal(t, "scratchpad", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "scratchpad")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunScratchpad_RequiresAction(t *testing.T) {
	raw, _ := json.Marshal(scratchpadInput{})
	out, err := runScratchpad(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "action is required")
	assert.Empty(t, out.Content)
}

func TestCreateNote(t *testing.T) {
	dir := t.TempDir()
	notes := []scratchpadNote{}
	in := scratchpadInput{Action: "create", Title: "Decision", Content: "Use Go", Tags: "arch,go"}
	out, err := createNote(t.Context(), dir, notes, in)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Decision")
	assert.Contains(t, out.Content, "Use Go")
	assert.Contains(t, out.Content, "arch, go")
	assert.NotEmpty(t, out.Detail)
}

func TestListNotes(t *testing.T) {
	now := time.Now()
	notes := []scratchpadNote{
		{ID: "1", Title: "Alpha", Content: "foo", Tags: []string{"a"}, UpdatedAt: now},
		{ID: "2", Title: "Beta", Content: "bar", Tags: []string{"b"}, UpdatedAt: now},
	}
	out, err := listNotes(t.Context(), notes, scratchpadInput{Action: "list", Limit: 10})
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Alpha")
	assert.Contains(t, out.Content, "Beta")
}

func TestListNotesQuery(t *testing.T) {
	now := time.Now()
	notes := []scratchpadNote{
		{ID: "1", Title: "Alpha", Content: "foo", Tags: []string{}, UpdatedAt: now},
		{ID: "2", Title: "Beta", Content: "bar", Tags: []string{}, UpdatedAt: now},
	}
	out, err := listNotes(t.Context(), notes, scratchpadInput{Action: "list", Query: "foo"})
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Alpha")
	assert.NotContains(t, out.Content, "Beta")
}

func TestReadNote(t *testing.T) {
	now := time.Now()
	notes := []scratchpadNote{
		{ID: "1", Title: "Alpha", Content: "secret", Tags: nil, UpdatedAt: now},
	}
	out, err := readNote(t.Context(), notes, scratchpadInput{Action: "read", ID: "1"})
	require.NoError(t, err)
	assert.Contains(t, out.Content, "secret")
}

func TestReadNoteMissing(t *testing.T) {
	_, err := readNote(t.Context(), nil, scratchpadInput{Action: "read", ID: "missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpdateNote(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	notes := []scratchpadNote{
		{ID: "1", Title: "Old", Content: "old", Tags: nil, UpdatedAt: now},
	}
	out, err := updateNote(
		t.Context(),
		dir,
		notes,
		scratchpadInput{Action: "update", ID: "1", Title: "New", Content: "new"},
	)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "New")
	assert.Contains(t, out.Content, "new")
}

func TestDeleteNote(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	notes := []scratchpadNote{
		{ID: "1", Title: "Old", Content: "old", Tags: nil, UpdatedAt: now},
	}
	out, err := deleteNote(t.Context(), dir, notes, scratchpadInput{Action: "delete", ID: "1"})
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Deleted")
}

func TestDeleteNoteMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := deleteNote(t.Context(), dir, nil, scratchpadInput{Action: "delete", ID: "missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSaveAndLoadNotes(t *testing.T) {
	dir := t.TempDir()
	notes := []scratchpadNote{
		{ID: "1", Title: "T", Content: "C", Tags: []string{"a"}, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	require.NoError(t, saveNotes(dir, notes))
	got, err := loadNotes(dir)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "T", got[0].Title)
}

func TestNotesPath(t *testing.T) {
	assert.Equal(t, filepath.Join("/tmp", scratchpadFile), notesPath("/tmp"))
}
