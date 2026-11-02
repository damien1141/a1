package searchtool

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchTool_Definition(t *testing.T) {
	tool := SearchTool()
	assert.Equal(t, "search", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "three planes")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

// TestRunSearch_AutoFindsTextMatches is the smoke test: a query that's an
// identifier in a small fake workspace must return at least one file with a
// text-plane badge.
func TestRunSearch_AutoFindsTextMatches(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"),
		[]byte("package main\n\nfunc Foo() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "caller.go"),
		[]byte("package main\n\nfunc use() { Foo() }\n"), 0o644))

	raw, _ := json.Marshal(searchInput{Query: "Foo", Path: root, Limit: 10})
	out, err := runSearch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "search \"Foo\"")
	// Both files contain the text "Foo" — both should appear, with a
	// text-plane badge.
	assert.Contains(t, out.Content, "main.go")
	assert.Contains(t, out.Content, "caller.go")
	assert.Contains(t, out.Content, "text:")
}

// TestRunSearch_ModeTextOnly verifies mode=text skips the symbol and file
// planes. The renderer header should report only the text plane.
func TestRunSearch_ModeTextOnly(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"),
		[]byte("package main\n\nfunc Foo() {}\n"), 0o644))

	raw, _ := json.Marshal(searchInput{Query: "Foo", Mode: "text", Path: root, Limit: 10})
	out, err := runSearch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "(text)")
	assert.NotContains(t, out.Content, "symbol:")
	assert.NotContains(t, out.Content, "[file]")
}

// TestRunSearch_LimitCapsFiles verifies the per-file limit is honored.
func TestRunSearch_LimitCapsFiles(t *testing.T) {
	root := t.TempDir()
	// 5 files, each containing "Foo" — limit=2 must surface only 2.
	for i := 0; i < 5; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "file"+string(rune('a'+i))+".go"),
			[]byte("package main\n\nfunc Foo() {}\n"), 0o644))
	}
	raw, _ := json.Marshal(searchInput{Query: "Foo", Mode: "text", Path: root, Limit: 2})
	out, err := runSearch(t.Context(), raw)
	require.NoError(t, err)
	// Count @file headers — should be exactly 2.
	n := strings.Count(out.Content, "@file ")
	assert.Equal(t, 2, n, "limit must cap the number of files returned: %s", out.Content)
}

// TestRunSearch_IgnoreCase makes the text plane case-insensitive.
func TestRunSearch_IgnoreCase(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"),
		[]byte("package main\n\nfunc FOO() {}\n"), 0o644))

	raw, _ := json.Marshal(searchInput{Query: "foo", Mode: "text", Path: root, Limit: 10, IgnoreCase: true})
	out, err := runSearch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "main.go",
		"case-insensitive search must find FOO when querying for foo")
}

// TestRunSearch_InvalidMode rejects bad modes with a clear error.
func TestRunSearch_InvalidMode(t *testing.T) {
	raw, _ := json.Marshal(searchInput{Query: "x", Mode: "invalid"})
	_, err := runSearch(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown mode")
}

// TestRunSearch_EmptyQuery rejects empty queries early — the tool would
// otherwise spin rg with a zero-length pattern and silently match nothing.
func TestRunSearch_EmptyQuery(t *testing.T) {
	raw, _ := json.Marshal(searchInput{Query: "   "})
	_, err := runSearch(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query is required")
}

// TestRunSearch_PathNotFound surfaces a clear error for nonexistent paths.
func TestRunSearch_PathNotFound(t *testing.T) {
	raw, _ := json.Marshal(searchInput{Query: "Foo", Path: "/nonexistent/abc/123"})
	_, err := runSearch(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path not found")
}

// TestLooksLikeIdentifier covers the cheap heuristic that auto mode uses to
// decide whether to also query the graph. Whitespace, path separators, and
// a leading digit disable the symbol plane. A dot is allowed (e.g. for
// qualified names like "Foo.Bar") — looksLikePathFragment handles the
// file-extension check separately.
func TestLooksLikeIdentifier(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Foo", true},
		{"auth.go", true},  // has a dot but no path sep — looks like an identifier
		{"Foo Bar", false}, // whitespace
		{"foo/bar", false}, // path separator
		{"1stVar", false},  // leading digit
		{"", false},
		{"FooBar_baz", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, looksLikeIdentifier(tc.in))
		})
	}
}

// TestLooksLikePathFragment covers the heuristic for the file plane.
func TestLooksLikePathFragment(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"foo.go", true},
		{"foo/bar", true},
		{"foo", false},        // bare identifier
		{".gitignore", false}, // leading dot, no extension suffix
		{"a.b", true},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, looksLikePathFragment(tc.in))
		})
	}
}

// TestShortPath verifies the renderer's label compression for module-style
// import paths.
func TestShortPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"github.com/damien1141/a1/internal/graph", "internal/graph"},
		{"fmt", "fmt"},         // single segment: returned as-is
		{"os/exec", "os/exec"}, // two segments: returned as-is
		{"a/b/c", "b/c"},       // three segments: last two
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, shortPath(tc.in))
		})
	}
}

// === single-plane escape hatches (Tier A.2) ===

func TestSearchTool_ModeEnumIncludesEscapeHatches(t *testing.T) {
	tool := SearchTool()
	modeParam, ok := tool.Definition.Params.Properties["mode"].(map[string]any)
	require.True(t, ok, "mode param should be present")
	enum, _ := modeParam["enum"].([]string)
	// All eight modes are advertised in the schema enum.
	expected := []string{"auto", "text", "symbol", "file", "grep", "find", "graph", "rank"}
	assert.Equal(t, expected, enum)
}

func TestRunSearch_ModeGrepDispatchesToGrepTool(t *testing.T) {
	// mode=grep should route through greptool.GrepTool().Run — the
	// output should look like a grep result (matches with line numbers),
	// not the combined-search renderer's per-file blocks.
	// Note: greptool uses `pattern` (a regex/literal), not `query`.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "haystack.txt"), []byte("needle here\nother line\nneedle again\n"), 0o644))
	t.Chdir(dir)

	tool := SearchTool()
	b, _ := json.Marshal(map[string]any{
		"mode":    "grep",
		"pattern": "needle",
		"path":    ".",
	})
	out, err := tool.Run(t.Context(), b)
	require.NoError(t, err)
	// grep output includes line numbers like "1:needle here".
	assert.Contains(t, out.Content, "needle here")
	assert.Contains(t, out.Content, "haystack.txt")
}

func TestRunSearch_ModeFindDispatchesToFindTool(t *testing.T) {
	// mode=find should route through findtool.FindTool().Run — the
	// output should be a list of file paths, not the combined renderer.
	// Note: findtool uses `pattern` (a glob), not `query`, and shells out
	// to the `fd` binary — skip if fd isn't installed.
	if _, err := exec.LookPath("fd"); err != nil {
		t.Skip("fd binary not installed — findtool requires it")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target.go"), []byte("package x\n"), 0o644))
	t.Chdir(dir)

	tool := SearchTool()
	b, _ := json.Marshal(map[string]any{
		"mode":    "find",
		"pattern": "*.go",
		"path":    ".",
	})
	out, err := tool.Run(t.Context(), b)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "target.go")
}

func TestRunSearch_ModeRankDispatchesToRankTool(t *testing.T) {
	// mode=rank should route through ranktool.RankTool().Run — the
	// output should be a scored list, not the combined renderer.
	// Note: ranktool uses `query` (a glob filter), not `mode`.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\n"), 0o644))
	t.Chdir(dir)

	tool := SearchTool()
	b, _ := json.Marshal(map[string]any{
		"mode": "rank",
		"path": ".",
		"glob": "*.go",
	})
	out, err := tool.Run(t.Context(), b)
	require.NoError(t, err)
	// Rank output uses tab-separated score + path.
	assert.Contains(t, out.Content, "x.go")
}

func TestRunSearch_DefaultModeIsAuto(t *testing.T) {
	// No mode field — defaults to "auto" (backward compat).
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "haystack.txt"), []byte("needle here\n"), 0o644))
	t.Chdir(dir)

	tool := SearchTool()
	b, _ := json.Marshal(map[string]any{"query": "needle", "path": "."})
	out, err := tool.Run(t.Context(), b)
	require.NoError(t, err)
	// auto mode uses the combined renderer's "search … — N files" header.
	assert.Contains(t, out.Content, "search")
}

func TestRunSearch_UnknownModeStillErrors(t *testing.T) {
	tool := SearchTool()
	b, _ := json.Marshal(map[string]any{"mode": "everything", "query": "x"})
	_, err := tool.Run(t.Context(), b)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown mode")
}

// === overhaul: pattern alias, glob, literal, context, listing, errors ===

// TestRunSearch_PatternAlias verifies `pattern` is accepted where `query`
// is expected — models coming from grep/find carry the habit over.
func TestRunSearch_PatternAlias(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"),
		[]byte("package main\n\nfunc Foo() {}\n"), 0o644))

	raw, _ := json.Marshal(map[string]any{"pattern": "Foo", "path": root})
	out, err := runSearch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "main.go")
}

// TestRunSearch_GlobFiltersTextPlane verifies the glob parameter gates the
// text plane via rg --glob.
func TestRunSearch_GlobFiltersTextPlane(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"),
		[]byte("package a\n\nfunc Foo() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.txt"),
		[]byte("func Foo() {}\n"), 0o644))

	raw, _ := json.Marshal(map[string]any{"query": "Foo", "path": root, "glob": "*.go", "mode": "text"})
	out, err := runSearch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "a.go")
	assert.NotContains(t, out.Content, "b.txt", "glob must filter out non-matching files")
}

// TestRunSearch_LiteralTreatsRegexMetacharsAsText verifies literal:true
// finds text that would not compile (or would match differently) as a regex.
func TestRunSearch_LiteralTreatsRegexMetacharsAsText(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "re.txt"),
		[]byte("calls foo( with a paren\nplain line\n"), 0o644))

	// Non-literal: an unbalanced paren is an invalid regex → a helpful error.
	raw, _ := json.Marshal(map[string]any{"query": "foo( with", "path": root, "mode": "text"})
	_, err := runSearch(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "literal:true")

	// Literal: the same query is plain text and matches.
	raw, _ = json.Marshal(map[string]any{"query": "foo( with", "path": root, "mode": "text", "literal": true})
	out, err := runSearch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "re.txt")
	assert.Contains(t, out.Content, "foo( with a paren")
}

// TestRunSearch_ContextLines verifies context=N renders surrounding lines
// with the · marker while match lines keep the | marker.
func TestRunSearch_ContextLines(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "ctx.go"),
		[]byte("line one\nline two\nthe needle\nline four\nline five\n"), 0o644))

	raw, _ := json.Marshal(map[string]any{"query": "needle", "path": root, "mode": "text", "context": 1})
	out, err := runSearch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "the needle")
	// Context lines around the match, marked with ·.
	assert.Contains(t, out.Content, "· line two")
	assert.Contains(t, out.Content, "· line four")
	assert.Contains(t, out.Content, "| the needle")
}

// TestRunSearch_ListingMode verifies the empty-query + path listing:
// dirs first, size + mtime per entry, truncation notice.
func TestRunSearch_ListingMode(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "file.go"), []byte("package x\n"), 0o644))

	raw, _ := json.Marshal(map[string]any{"path": root, "limit": 1})
	out, err := runSearch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "list ")
	// Dirs sort before files.
	assert.Less(t, strings.Index(out.Content, "sub/"), strings.Index(out.Content, "file.go"))
	// Truncation is called out: limit=1 with 2 entries present.
	assert.Contains(t, out.Content, "truncated after 1")

	// Full listing shows the file with its metadata.
	raw, _ = json.Marshal(map[string]any{"path": root})
	out, err = runSearch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "file.go")
	assert.Contains(t, out.Content, "B") // human size, e.g. "10 B"
	// Relative mtime (a just-written file reads "just now"; anything
	// older reads "… ago").
	assert.True(t, strings.Contains(out.Content, "ago") || strings.Contains(out.Content, "just now"),
		"listing should show a relative mtime, got: %s", out.Content)
	assert.Contains(t, out.Detail, "entries")
}

// TestRunSearch_ListingModeRequiresDirectory verifies a file path with an
// empty query errors rather than listing.
func TestRunSearch_ListingModeRequiresDirectory(t *testing.T) {
	root := t.TempDir()
	fp := filepath.Join(root, "plain.txt")
	require.NoError(t, os.WriteFile(fp, []byte("x\n"), 0o644))

	raw, _ := json.Marshal(map[string]any{"path": fp})
	_, err := runSearch(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query is required")
}

// TestRunSearch_NoResultsHints verifies the zero-hit path suggests next
// steps instead of a bare statement.
func TestRunSearch_NoResultsHints(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644))

	raw, _ := json.Marshal(map[string]any{"query": "zzzNoSuchThing", "path": root, "mode": "text"})
	out, err := runSearch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "No results")
	assert.Contains(t, out.Content, "ignoreCase")
}

// TestGlobMatchesPath covers the simple glob matcher used by the symbol
// and file planes.
func TestGlobMatchesPath(t *testing.T) {
	cases := []struct {
		glob, p string
		want    bool
	}{
		{"", "anything/at/all", true},
		{"*.go", "internal/web/server.go", true},
		{"*.go", "internal/web/server.ts", false},
		{"*_test.go", "pkg/foo_test.go", true},
		{"web/*", "web/server.go", true},
		{"web/*", "internal/web/server.go", false},
	}
	for _, tc := range cases {
		t.Run(tc.glob+"|"+tc.p, func(t *testing.T) {
			assert.Equal(t, tc.want, globMatchesPath(tc.glob, tc.p))
		})
	}
}

// TestHumanSize covers the compact size renderer.
func TestHumanSize(t *testing.T) {
	assert.Equal(t, "980 B", humanSize(980))
	assert.Equal(t, "4.0 KB", humanSize(4096))
	assert.Equal(t, "1.0 MB", humanSize(1<<20))
}

// TestRelTime covers the relative age renderer boundaries.
func TestRelTime(t *testing.T) {
	assert.Equal(t, "just now", relTime(time.Now()))
	assert.Equal(t, "30m ago", relTime(time.Now().Add(-30*time.Minute)))
	assert.Equal(t, "2h ago", relTime(time.Now().Add(-2*time.Hour)))
	assert.Equal(t, "3d ago", relTime(time.Now().Add(-72*time.Hour)))
}
