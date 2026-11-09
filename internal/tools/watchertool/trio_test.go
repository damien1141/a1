package watchertool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// watcherFixture builds a tree whose mtimes are pinned with os.Chtimes
// around a fixed `since` instant, so change detection is deterministic
// without sleeps or wall-clock luck.
func watcherFixture(t *testing.T) (root string, since time.Time) {
	t.Helper()
	since = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	root = t.TempDir()
	writeFile(t, root, "old.go", "package old\n")
	writeFile(t, root, "new.go", "package new\n")
	writeFile(t, root, "equal.go", "package equal\n")
	writeFile(t, root, "stale.txt", "stale\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	writeFile(t, root, filepath.Join("sub", "inner.go"), "package inner\n")
	writeFile(t, root, filepath.Join("sub", "old.txt"), "old notes\n")

	// equal.go sits exactly at the boundary: ModTime == since is not
	// "after", so it must stay unreported.
	for path, mtime := range map[string]time.Time{
		"old.go":       since.Add(-time.Hour),
		"new.go":       since.Add(time.Hour),
		"equal.go":     since,
		"stale.txt":    since.Add(-2 * time.Hour),
		"sub/inner.go": since.Add(30 * time.Minute),
		"sub/old.txt":  since.Add(2 * time.Hour),
	} {
		p := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.Chtimes(p, mtime, mtime))
	}
	return root, since
}

// naiveChanged re-derives the changed set with an os.ReadDir recursion and
// ModTime().After on DirEntry.Info — not the tool's filepath.Walk closure.
func naiveChanged(t *testing.T, root, rel, glob string, since time.Time) []string {
	t.Helper()
	var changed []string
	entries, err := os.ReadDir(filepath.Join(root, rel))
	require.NoError(t, err)
	for _, e := range entries {
		childRel := filepath.ToSlash(filepath.Join(rel, e.Name()))
		if e.IsDir() {
			changed = append(changed, naiveChanged(t, root, childRel, glob, since)...)
			continue
		}
		if glob != "" {
			matched, err := filepath.Match(glob, e.Name())
			require.NoError(t, err)
			if !matched {
				continue
			}
		}
		info, err := e.Info()
		require.NoError(t, err)
		if info.ModTime().After(since) {
			changed = append(changed, childRel)
		}
	}
	return changed
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// oracle: naive reference implementation in this test — an os.ReadDir
// recursion (not the tool's filepath.Walk) comparing pinned mtimes against
// the explicit `since`, with the same base-name glob applied via
// filepath.Match; no reliance on the package-global lastCheck clock.
func TestRunWatcher_ChangedSet_Differential(t *testing.T) {
	root, since := watcherFixture(t)
	t.Chdir(root)

	t.Run("go glob", func(t *testing.T) {
		raw, err := json.Marshal(watcherInput{Path: ".", Since: since.Format(time.RFC3339), Glob: "*.go"})
		require.NoError(t, err)

		res, err := runWatcher(t.Context(), raw)
		require.NoError(t, err)

		want := naiveChanged(t, root, "", "*.go", since)
		require.Len(t, want, 2, "oracle expects new.go and sub/inner.go only")
		assert.Equal(t, sortedCopy(want), strings.Split(res.Content, "\n"))
		assert.Equal(t, "2 changed since 2024-01-01T00:00:00Z", res.Detail)
		assert.Equal(t, res.Content, res.Output)
	})

	t.Run("no glob keeps non-go files", func(t *testing.T) {
		raw, err := json.Marshal(watcherInput{Path: ".", Since: since.Format(time.RFC3339)})
		require.NoError(t, err)

		res, err := runWatcher(t.Context(), raw)
		require.NoError(t, err)

		want := naiveChanged(t, root, "", "", since)
		require.Len(t, want, 3, "oracle expects new.go, sub/inner.go and sub/old.txt")
		assert.Equal(t, sortedCopy(want), strings.Split(res.Content, "\n"))
		assert.Equal(t, "3 changed since 2024-01-01T00:00:00Z", res.Detail)
	})

	t.Run("future since reports nothing", func(t *testing.T) {
		future := since.Add(24 * time.Hour)
		raw, err := json.Marshal(watcherInput{Path: ".", Since: future.Format(time.RFC3339)})
		require.NoError(t, err)

		res, err := runWatcher(t.Context(), raw)
		require.NoError(t, err)

		assert.Empty(t, naiveChanged(t, root, "", "", future))
		assert.Equal(t, "No files changed since 2024-01-02T00:00:00Z", res.Content)
		assert.Equal(t, "0 changed", res.Detail)
	})
}

// oracle: naive reference implementation in this test — a map-based decode
// plus independent trim/default handling over normal, edge, and hostile
// inputs, which the detail helper must survive.
func TestWatcherDetailFromArgs_Differential(t *testing.T) {
	cases := []struct{ name, input string }{
		{"path", `{"path": "internal/tools"}`},
		{"padded path", `{"path": "  .  "}`},
		{"empty object", `{}`},
		{"empty path", `{"path": ""}`},
		{"whitespace path", `{"path": "   "}`},
		{"null path", `{"path": null}`},
		{"non-string path", `{"path": 7}`},
		{"other fields only", `{"since": "2024-01-01T00:00:00Z", "glob": "*.go"}`},
		{"not json", `nope`},
	}
	tool := WatcherTool()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			_ = json.Unmarshal([]byte(tc.input), &m)
			p, _ := m["path"].(string)
			p = strings.TrimSpace(p)
			if p == "" {
				p = "."
			}
			assert.Equal(t, "watcher "+p, tool.DetailFromArgs(json.RawMessage(tc.input)))
		})
	}
}
