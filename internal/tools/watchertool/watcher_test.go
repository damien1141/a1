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

	// An explicit zero timestamp makes the check independent of whatever the
	// package-global lastCheck holds: sibling tests run in the same process
	// and would otherwise seed this one with their own scan time.
	epoch := time.Time{}.Format(time.RFC3339)
	raw, _ := json.Marshal(watcherInput{Path: ".", Since: epoch, Glob: "*.go"})
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

// TestRunWatcher_LastCheckSeesEveryReportedFile checks the scan-clock contract
// from the observable side: a completed scan records lastCheck, and that
// instant is at or after every file it reported. If lastCheck were captured
// before the walk finished, a file written during the walk would be silently
// swallowed until a later scan.
func TestRunWatcher_LastCheckSeesEveryReportedFile(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "seen.go", "package main\n")

	raw, _ := json.Marshal(watcherInput{Path: "."})
	out, err := runWatcher(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "seen.go")

	recorded := lastCheckSnapshot(t)
	require.False(t, recorded.IsZero(), "a completed scan records lastCheck")
	assert.GreaterOrEqual(t, recorded, fileModTime(t, filepath.Join(root, "seen.go")),
		"lastCheck must not precede a file the scan reported, or the next scan re-reports it")

	// Only a genuinely newer write shows up on the default (lastCheck) scan.
	time.Sleep(2 * time.Second)
	writeFile(t, root, "fresh.go", "package main\n")
	out, err = runWatcher(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "fresh.go", "a newer write is reported")
	assert.NotContains(t, out.Content, "seen.go", "an already-reported file is not resurrected")
}

// TestRunWatcher_ExplicitSinceUnaffectedByLastCheck guards the other half: an
// explicit since is never masked by the package-global lastCheck, which is
// what made sibling tests order-dependent.
func TestRunWatcher_ExplicitSinceUnaffectedByLastCheck(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "stable.go", "package main\n")

	raw, _ := json.Marshal(watcherInput{Path: "."})
	_, err := runWatcher(t.Context(), raw)
	require.NoError(t, err)

	epoch := time.Time{}.Format(time.RFC3339)
	raw, _ = json.Marshal(watcherInput{Path: ".", Since: epoch})
	for range 3 {
		out, err := runWatcher(t.Context(), raw)
		require.NoError(t, err)
		assert.Contains(t, out.Content, "stable.go", "an explicit since must not be masked by lastCheck")
	}
}

func lastCheckSnapshot(t *testing.T) time.Time {
	t.Helper()
	lastCheckMu.Lock()
	defer lastCheckMu.Unlock()
	return lastCheck
}

func fileModTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.ModTime()
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
