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

        recorded := lastCheckSnapshot(t, root)
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
// explicit since is never masked by the per-path lastCheck, which is
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

// TestRunWatcher_LastCheckIsPerPath is the regression test for the bug where
// lastCheck was a single package-global time.Time. Scanning directory A and
// then scanning directory B for the first time must not have B inherit A's
// lastCheck — B's first scan must see every file as changed (since zero).
func TestRunWatcher_LastCheckIsPerPath(t *testing.T) {
        dirA := t.TempDir()
        dirB := t.TempDir()

        // Pre-create a file in B and let its mtime settle before any scan runs.
        writeFile(t, dirB, "old.go", "package main\n")
        time.Sleep(2 * time.Second)

        // First scan of A — under the old bug this set the global lastCheck to "now",
        // which is *after* old.go's mtime.
        rawA, _ := json.Marshal(watcherInput{Path: dirA})
        _, err := runWatcher(t.Context(), rawA)
        require.NoError(t, err)

        // First scan of B must see old.go even though A was just scanned.
        rawB, _ := json.Marshal(watcherInput{Path: dirB})
        out, err := runWatcher(t.Context(), rawB)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "old.go",
                "first scan of B must see every file as changed — per-path lastCheck must not leak from A")
}

func lastCheckSnapshot(t *testing.T, path string) time.Time {
        t.Helper()
        // runWatcher keys lastCheck by the cleaned absolute path that
        // tooldef.ResolveToCwd produces. Match that here so we read the right
        // entry. We must NOT call runWatcher from inside this helper — that
        // would mutate the very state we're trying to observe.
        abs := path
        if !filepath.IsAbs(path) {
                wd, err := os.Getwd()
                require.NoError(t, err)
                abs = filepath.Join(wd, path)
        }
        abs = filepath.Clean(abs)
        lastCheckMu.Lock()
        defer lastCheckMu.Unlock()
        if lastCheck == nil {
                return time.Time{}
        }
        return lastCheck[abs]
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
