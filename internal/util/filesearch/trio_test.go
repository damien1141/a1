package filesearch

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// oracle: stdlib regexp.QuoteMeta — the hand-rolled escaper must produce the
// same literal pattern as the stdlib for every metacharacter, unicode text,
// and empty string, and the escaped pattern must behave as a literal
// substring (regexp match == strings.Contains) where an unescaped pattern
// would not.
func TestEscapeRegex_MatchesQuoteMeta_Differential(t *testing.T) {
	corpus := []string{
		"plain",
		`a.b*c?d(e)f[g]h{i}j|k^l$m\n`,
		"你好.世界",
		"",
		"x.y",
		"manager.go",
		"C++",
		"a|b",
		"[]",
		"((()))",
		"\n\t",
		"50%",
		"café+",
	}
	for _, s := range corpus {
		assert.Equal(t, regexp.QuoteMeta(s), escapeRegex(s), "escapeRegex(%q)", s)
	}

	// Behavioral half: the escaped query must have literal-substring
	// semantics. "x.y" must not regex-match "xzy" the way a raw dot would,
	// and "a+b" must not match "aab" the way a raw plus would.
	pairs := []struct {
		query, text string
	}{
		{"x.y", "xzy"},
		{"x.y", "notes.x.y"},
		{"a+b", "aab"},
		{"a+b", "xa+by"},
		{"manager.go", "dir/manager.go"},
		{"manager.go", "manager-gos"},
		{"(", "stray paren ("},
		{"50%", "cost: 50% off"},
	}
	for _, p := range pairs {
		re := regexp.MustCompile(escapeRegex(p.query))
		assert.Equal(t, strings.Contains(p.text, p.query), re.MatchString(p.text),
			"escaped %q vs %q", p.query, p.text)
	}
}

// refWalkFiles enumerates regular files with the standard library alone.
func refWalkFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
		if werr != nil || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	require.NoError(t, err)
	return files
}

// refMatch re-derives fd's contract over the walk: a file matches when the
// query occurs in its full path (root included, the way fd's --full-path
// sees it), case-insensitively while the query has no uppercase letter
// (fd's smart-case default). An empty query matches every file.
func refMatch(root string, files []string, query string) []string {
	var out []string
	for _, f := range files {
		full := root + "/" + f
		hit := true
		if query != "" {
			if strings.ContainsAny(query, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
				hit = strings.Contains(full, query)
			} else {
				hit = strings.Contains(strings.ToLower(full), strings.ToLower(query))
			}
		}
		if hit {
			out = append(out, f)
		}
	}
	return out
}

// oracle: filepath.WalkDir + strings reference — fd's matching semantics
// (full-path literal substring, smart case) are re-derived over a fixture
// tree with the standard library and compared set-wise with Search's
// output. The metacharacter cases would diverge if escapeRegex were broken.
func TestSearch_MatchesWalkDirReference_Differential(t *testing.T) {
	ResetResolveFDForTest()
	if _, err := ResolveFD(); err != nil {
		t.Skip("fd not installed:", err)
	}

	dir := t.TempDir()
	fixture := map[string]string{
		"Readme.MD":            "x",
		"README.md":            "x",
		"notes.txt":            "x",
		"x.y":                  "x",
		"xzy.txt":              "x",
		"docs/helper.go":       "x",
		"docs/deep/manager.go": "x",
		"src/main.go":          "x",
		"src/util/manager.go":  "x",
		"Makefile":             "x",
	}
	for rel, content := range fixture {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}

	all := refWalkFiles(t, dir)
	require.Len(t, all, len(fixture), "fixture must have exactly the planted files")
	ref := func(query string) []string { return refMatch(dir, all, query) }

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	queries := []string{
		"",         // list everything
		"manager",  // case-insensitive: matches both manager.go files
		"Manager",  // case-sensitive: no path carries this literal
		"readme",   // case-insensitive: both readme spellings
		"Readme",   // case-sensitive: only Readme.MD
		"x.y",      // literal dot: x.y only, never xzy.txt
		"xzy",      // plain substring
		"docs",     // directory segment in the path
		".go",      // escaped dot still means literal ".go"
		"Makefile", // exact base name
	}
	for _, query := range queries {
		t.Run("query:"+query, func(t *testing.T) {
			got, truncated, err := Search(ctx, dir, query, 500)
			require.NoError(t, err)
			assert.False(t, truncated, "limit 500 exceeds the fixture size")
			assert.ElementsMatch(t, ref(query), got, "fd results must equal the WalkDir reference")
		})
	}

	// Under a tight limit the caller gets exactly limit paths, all valid,
	// and is told the list is partial.
	limited, truncated, err := Search(ctx, dir, "", 3)
	require.NoError(t, err)
	require.Len(t, limited, 3)
	require.True(t, truncated, "more files exist beyond the limit")
	valid := map[string]bool{}
	for _, f := range ref("") {
		valid[f] = true
	}
	for _, p := range limited {
		assert.True(t, valid[p], "returned path %q must come from the fixture", p)
	}
}
