package watchertool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatcherTool_Definition(t *testing.T) {
	tool := WatcherTool()
	assert.Equal(t, "watcher", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "changed")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunWatcher_DetectsNewFile(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	raw, _ := json.Marshal(watcherInput{Path: "."})
	out, err := runWatcher(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No files changed since 0001-01-01T00:00:00Z", out.Content)

	time.Sleep(2 * time.Second)
	writeFile(t, root, "new.go", "package main\n")

	raw, _ = json.Marshal(watcherInput{Path: "."})
	out, err = runWatcher(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "new.go")
}

func TestRunWatcher_DetectsModification(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "main.go", "package main\n")

	raw, _ := json.Marshal(watcherInput{Path: "."})
	_, err := runWatcher(t.Context(), raw)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)
	writeFile(t, root, "main.go", "package main\n// edited\n")

	raw, _ = json.Marshal(watcherInput{Path: "."})
	out, err := runWatcher(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "main.go")
}

func TestRunWatcher_SinceTimestamp(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "main.go", "package main\n")

	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	raw, _ := json.Marshal(watcherInput{Path: ".", Since: past})
	out, err := runWatcher(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "main.go")
}

func TestRunWatcher_WithGlob(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "main.go", "package main\n")
	writeFile(t, root, "notes.txt", "notes\n")

	raw, _ := json.Marshal(watcherInput{Path: ".", Glob: "*.go"})
	out, err := runWatcher(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "main.go")
	assert.NotContains(t, out.Content, "notes.txt")
}

func TestRunWatcher_NotADirectory(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "file.txt", "x")

	raw, _ := json.Marshal(watcherInput{Path: "file.txt"})
	out, err := runWatcher(t.Context(), raw)
	require.Error(t, err)
	assert.Empty(t, out.Content)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
