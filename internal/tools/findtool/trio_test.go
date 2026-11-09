package findtool

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// findFixtureTree is the shared fixture: hidden files and dirs, nested dirs,
// mixed-case extensions, and names shaped for every glob construct the tool
// documents (* / ** / ? / [abc] / {a,b}). No .gitignore/.ignore files, so the
// reference walk does not need to re-implement fd's ignore rules — those are
// covered by TestRunFD_MatchLimitAndGitignore.
var findFixtureTree = map[string]string{
	"a.txt":              "x",
	"one.txt":            "x",
	"B.TXT":              "x",
	"notes.md":           "x",
	".dotfile.md":        "x",
	"other.rs":           "x",
	"sub/b.txt":          "x",
	"sub/c.go":           "x",
	"sub/deep/d.txt":     "x",
	"nested/leaf.md":     "x",
	"nested/mid/leaf.md": "x",
}

func writeFindTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		fp := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(fp), 0o755), "fixture dir for %s", rel)
		require.NoError(t, os.WriteFile(fp, []byte(content), 0o644), "fixture file %s", rel)
	}
	return root
}

// fdGlobChars matches pat against s (both already case-folded) for the
// non-recursive glob constructs: '*' and '?' stay within one path component,
// '[...]' is a character class ('!' or '^' negates), everything else is
// literal. Runs of '*' (like the '**' basename case) just behave as '*'.
func fdGlobChars(pat, s string) bool {
	if pat == "" {
		return s == ""
	}
	if pat[0] == '*' {
		for i := 0; i <= len(s); {
			if fdGlobChars(pat[1:], s[i:]) {
				return true
			}
			if i == len(s) || s[i] == '/' {
				break
			}
			_, size := utf8.DecodeRuneInString(s[i:])
			i += size
		}
		return false
	}
	if s == "" {
		return false
	}
	pr, psize := utf8.DecodeRuneInString(pat)
	sr, ssize := utf8.DecodeRuneInString(s)
	switch pr {
	case '?':
		return sr != '/' && fdGlobChars(pat[psize:], s[ssize:])
	case '[':
		end := strings.IndexByte(pat[1:], ']')
		if end < 0 {
			return false
		}
		body, after := pat[1:end+1], pat[end+2:]
		neg := strings.HasPrefix(body, "!") || strings.HasPrefix(body, "^")
		if neg {
			body = body[1:]
		}
		if sr == '/' || classHasRune(body, sr) == neg {
			return false
		}
		return fdGlobChars(after, s[ssize:])
	default:
		return pr == sr && fdGlobChars(pat[psize:], s[ssize:])
	}
}

// classHasRune reports whether the class body (no brackets) contains r,
// honoring 'a-z' ranges. A '-' at the start or end is literal.
func classHasRune(body string, r rune) bool {
	rs := []rune(body)
	for i := 0; i < len(rs); i++ {
		if i+2 < len(rs) && rs[i+1] == '-' {
			if r >= rs[i] && r <= rs[i+2] {
				return true
			}
			i += 2
			continue
		}
		if rs[i] == r {
			return true
		}
	}
	return false
}

// fdGlobMatch implements fd's documented glob dialect (globset with literal
// separators, always case-insensitive because runFD passes --ignore-case).
// Patterns are pre-folded to lowercase, as is the candidate string.
//   - '{a,b}' alternation, '[abc]' classes, '?' one rune, '*' a run within
//     one component;
//   - '**' as a whole path component spans any number of components — the
//     reason patterns like **/*.txt also match root-level files.
func fdGlobMatch(pat, s string) bool {
	if i := strings.IndexByte(pat, '{'); i >= 0 {
		end := strings.IndexByte(pat[i+1:], '}')
		if end < 0 {
			return false
		}
		end += i + 1
		for _, alt := range strings.Split(pat[i+1:end], ",") {
			if fdGlobMatch(pat[:i]+alt+pat[end+1:], s) {
				return true
			}
		}
		return false
	}

	// Leading '**/': zero or more whole components. With fd --full-path the
	// candidate is the absolute path, so this also swallows the temp-root
	// prefix; fixture dir names are chosen to never collide with the patterns.
	if strings.HasPrefix(pat, "**/") {
		if fdGlobMatch(pat[3:], s) {
			return true
		}
		if i := strings.IndexByte(s, '/'); i >= 0 {
			return fdGlobMatch(pat, s[i+1:])
		}
		return false
	}
	// Middle '/**/': zero or more components between its neighbors.
	if i := strings.Index(pat, "/**/"); i >= 0 {
		pre, post := pat[:i], pat[i+4:]
		for k := 0; k < len(s); k++ {
			if s[k] != '/' || !fdGlobMatch(pre, s[:k]) {
				continue
			}
			if fdGlobMatch(post, s[k+1:]) {
				return true
			}
			for m := k + 1; m < len(s); m++ {
				if s[m] == '/' && fdGlobMatch(post, s[m+1:]) {
					return true
				}
			}
		}
		return false
	}
	// Trailing '/**': the separator plus anything.
	if strings.HasSuffix(pat, "/**") {
		pre := pat[:len(pat)-3]
		for k := 0; k < len(s); k++ {
			if s[k] == '/' && fdGlobMatch(pre, s[:k]) {
				return true
			}
		}
		return false
	}
	return fdGlobChars(pat, s)
}

// refFind re-derives the tool's result set with filepath.WalkDir (hidden
// entries included, like fd --hidden; regular files only, like --type f) plus
// the fd-dialect matcher above, applying runFD's own basename-vs-full-path
// and '**/'-prepend rules. Sorted output: fd's parallel walk gives no
// emission-order contract, so sets are compared sorted.
func refFind(t *testing.T, root, pattern string) []string {
	t.Helper()
	pat := strings.ToLower(pattern)
	fullPath := strings.Contains(pattern, "/")
	if fullPath && !strings.HasPrefix(pattern, "/") && !strings.HasPrefix(pattern, "**/") && pattern != "**" {
		pat = "**/" + pat
	}
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		ok := false
		if fullPath {
			// fd --full-path matches the absolute path it prints.
			ok = fdGlobMatch(pat, strings.ToLower(filepath.ToSlash(p)))
		} else {
			ok = fdGlobMatch(pat, strings.ToLower(path.Base(rel)))
		}
		if ok {
			out = append(out, rel)
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(out)
	return out
}

func runFindJSON(t *testing.T, args map[string]any) (string, string) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	res, err := runFind(t.Context(), raw)
	require.NoError(t, err)
	return res.Content, res.Detail
}

// oracle: stdlib equivalence — filepath.WalkDir over the same fixture tree
// (hidden files included) plus a self-contained matcher for fd's documented
// glob dialect re-derives the exact result set of every pattern shape the
// tool advertises: basename globs, case folding, recursive **, anchored
// subdirectory patterns, single-char, classes, and brace alternation.
func TestRunFind_ResultSetVsWalkDirGlob_Differential(t *testing.T) {
	requireFD(t)
	root := writeFindTree(t, findFixtureTree)
	t.Chdir(root)

	cases := []struct{ name, pattern string }{
		{"basename glob includes hidden files", "*.md"},
		{"case-insensitive extension", "*.TXT"},
		{"recursive glob matches root level too", "**/*.txt"},
		{"anchored subdir glob stays within one component", "sub/*.txt"},
		{"trailing recursive component", "deep/**"},
		{"middle recursive component matches zero dirs too", "nested/**/leaf.md"},
		{"single char case-folded", "?.txt"},
		{"character class case-folded", "[AB].txt"},
		{"brace alternation", "*.{go,rs}"},
		{"match-everything", "**"},
		{"no matches", "*.zzz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := refFind(t, root, tc.pattern)
			content, detail := runFindJSON(t, map[string]any{"pattern": tc.pattern})
			if len(want) == 0 {
				assert.Equal(t, "No files found", content)
				assert.Equal(t, "0 files", detail)
				return
			}
			got := strings.Split(strings.TrimSpace(content), "\n")
			sort.Strings(got)
			assert.Equal(t, want, got, "sorted result set must equal the WalkDir+glob reference")
			assert.Equal(t, fmt.Sprintf("%d files", len(want)), detail)
		})
	}
}

// oracle: stdlib equivalence — a limit below the WalkDir reference's match
// count must cut the output to exactly that many lines, every line drawn
// from the reference set (fd's parallel walk picks which ones, so only
// membership is deterministic), and a single-match pattern at limit=1 pins
// the deterministic truncation rendering byte for byte.
func TestRunFind_TruncationVsReferenceSet_Differential(t *testing.T) {
	requireFD(t)
	root := writeFindTree(t, findFixtureTree)
	t.Chdir(root)

	t.Run("limit cuts the reference set", func(t *testing.T) {
		want := refFind(t, root, "**/*.txt")
		require.Len(t, want, 5, "reference must see five .txt files")

		content, detail := runFindJSON(t, map[string]any{"pattern": "**/*.txt", "limit": 2})
		lines := strings.Split(strings.TrimSpace(content), "\n")
		require.Len(t, lines, 3, "two files plus the limit notice")
		for _, l := range lines[:2] {
			assert.Contains(t, want, l, "truncated picks must come from the reference set")
		}
		assert.Contains(t, lines[2], "(2 results limit reached")
		assert.Contains(t, lines[2], "Use limit=4 for more")
		assert.Equal(t, "2 files", detail)
	})

	t.Run("single match at limit one", func(t *testing.T) {
		want := refFind(t, root, "sub/*.txt")
		require.Equal(t, []string{"sub/b.txt"}, want, "reference sanity")

		content, detail := runFindJSON(t, map[string]any{"pattern": "sub/*.txt", "limit": 1})
		assert.Equal(t, "sub/b.txt\n(1 results limit reached. Use limit=2 for more, or refine pattern/path.)", content)
		assert.Equal(t, "1 files", detail)
	})
}
