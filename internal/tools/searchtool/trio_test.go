package searchtool

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// refLine is one text-plane hit as derived by the stdlib reference: a
// 1-based line number plus the line's text.
type refLine struct {
	num  int
	text string
}

// writeFixtureTree materializes a small deterministic workspace: rel slash
// paths → file bodies. Parent dirs (including hidden ones) are created on
// demand.
func writeFixtureTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		fp := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(fp), 0o755), "fixture dir for %s", rel)
		require.NoError(t, os.WriteFile(fp, []byte(content), 0o644), "fixture file %s", rel)
	}
	return root
}

// refTextPlane re-derives the text plane with the standard library alone:
// filepath.WalkDir enumerates files (hidden ones included, matching rg
// --hidden), regexp or strings.Contains decides per line whether it matches,
// and a simple *.ext glob filters by base name the way the fixture uses it.
func refTextPlane(t *testing.T, root, query string, literal, ignoreCase bool, glob string) map[string][]refLine {
	t.Helper()
	var re *regexp.Regexp
	if !literal {
		re = regexp.MustCompile(query)
	}
	needle := query
	if literal && ignoreCase {
		needle = strings.ToLower(query)
	}
	out := map[string][]refLine{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if glob != "" {
			baseOK, _ := path.Match(glob, path.Base(rel))
			wholeOK, _ := path.Match(glob, rel)
			if !baseOK && !wholeOK {
				return nil
			}
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		body := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		for i, line := range body {
			var hit bool
			switch {
			case literal && ignoreCase:
				hit = strings.Contains(strings.ToLower(line), needle)
			case literal:
				hit = strings.Contains(line, query)
			case ignoreCase:
				hit = re.MatchString(strings.ToLower(line))
			default:
				hit = re.MatchString(line)
			}
			if hit {
				out[rel] = append(out[rel], refLine{num: i + 1, text: line})
			}
		}
		return nil
	})
	require.NoError(t, err)
	return out
}

var (
	gotMatchRe  = regexp.MustCompile(`^  text: *(\d+)\| (.*)$`)
	gotAnyRe    = regexp.MustCompile(`^  text: *(\d+)([|·]) (.*)$`)
	gotHeaderRe = regexp.MustCompile(`^@file (\S+) \[text:(\d+)\]$`)
)

// parseTextBlocks extracts the rendered match lines per file from the
// combined renderer's output, keyed by the same slash-relative paths the
// reference produces. Badges are returned separately (file → match count).
func parseTextBlocks(content string) (map[string][]refLine, map[string]int) {
	matches := map[string][]refLine{}
	badges := map[string]int{}
	cur := ""
	for _, line := range strings.Split(content, "\n") {
		if m := gotHeaderRe.FindStringSubmatch(line); m != nil {
			cur = m[1]
			badges[cur], _ = strconv.Atoi(m[2])
			continue
		}
		if m := gotMatchRe.FindStringSubmatch(line); m != nil && cur != "" {
			num, _ := strconv.Atoi(m[1])
			matches[cur] = append(matches[cur], refLine{num: num, text: m[2]})
		}
	}
	return matches, badges
}

func runSearchJSON(t *testing.T, args map[string]any) (string, string) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	res, err := SearchTool().Run(t.Context(), raw)
	require.NoError(t, err)
	return res.Content, res.Detail
}

// refDisplay is one rendered display line: match lines carry hit=true,
// context lines false.
type refDisplay struct {
	num  int
	text string
	hit  bool
}

// refContextDisplay re-derives the context=N renderer semantics: the union
// of [m-N, m+N] windows around every match, in line order, each line marked
// as a match only when the pattern itself matched it. Seam lines shared by
// adjacent windows appear once.
func refContextDisplay(body string, matchSet map[int]bool, ctxN int) []refDisplay {
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	seen := map[int]bool{}
	var out []refDisplay
	for m := range matchSet {
		for ln := m - ctxN; ln <= m+ctxN; ln++ {
			if ln < 1 || ln > len(lines) || seen[ln] {
				continue
			}
			seen[ln] = true
			out = append(out, refDisplay{num: ln, text: lines[ln-1], hit: matchSet[ln]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].num < out[j].num })
	return out
}

// parseDisplayLines extracts the ordered display lines (match and context)
// for one file block from the renderer output.
func parseDisplayLines(content, file string) []refDisplay {
	var out []refDisplay
	inBlock := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "@file ") {
			inBlock = strings.HasPrefix(strings.TrimPrefix(line, "@file "), file+" [")
			continue
		}
		if !inBlock {
			continue
		}
		if m := gotAnyRe.FindStringSubmatch(line); m != nil {
			num, _ := strconv.Atoi(m[1])
			out = append(out, refDisplay{num: num, text: m[3], hit: m[2] == "|"})
		}
	}
	return out
}

// oracle: stdlib equivalence — Go regexp over a filepath.WalkDir walk of the
// same fixture tree (hidden files included, like rg --hidden) is compared
// match-set for match-set against the ripgrep-backed text plane, including
// per-file badge counts, literal mode, ignoreCase, and the glob filter.
func TestRunSearch_TextPlaneVsRegexpWalk_Differential(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg binary not installed — text plane requires it")
	}

	tree := map[string]string{
		"alpha.go":           "package alpha\n\nfunc Use() {\n\t_ = needle42\n\t_ = needle7\n}\n",
		"sub/beta.txt":       "plain line\nNEEDLE9 stays uppercase, no hit\nneedle0 here\n",
		".hidden/gamma.md":   "hidden needle99\nnothing else\n",
		"delta.go":           "no matches at all\n",
		"sub/deeper/eps.txt": "needle5 deep\nneedle6 deeper\nneedle7 deepest\n",
	}
	root := writeFixtureTree(t, tree)

	t.Run("regex query", func(t *testing.T) {
		const query = `needle[0-9]+`
		want := refTextPlane(t, root, query, false, false, "")
		require.Len(t, want, 4, "reference must find 4 files, got %v", want)

		content, detail := runSearchJSON(t, map[string]any{"query": query, "path": root, "mode": "text", "limit": 10})
		got, badges := parseTextBlocks(content)
		assert.Equal(t, want, got, "rendered matches must equal the regexp+WalkDir reference")
		for rel, ms := range want {
			assert.Equal(t, len(ms), badges[rel], "text badge for %s", rel)
		}
		assert.Equal(t, fmt.Sprintf("%d files across text", len(want)), detail)
	})

	t.Run("literal query with regex metacharacters", func(t *testing.T) {
		// As a regex "x+y" would also match "xy"/"xxy" — only the literal
		// reading may match the "x+y" line itself.
		litRoot := writeFixtureTree(t, map[string]string{
			"a.txt": "has x+y literal\nhas xy regex-only\nhas xxy regex-only\n",
		})
		want := refTextPlane(t, litRoot, "x+y", true, false, "")
		require.Len(t, want["a.txt"], 1, "literal reference must match only the first line")

		content, _ := runSearchJSON(t, map[string]any{"query": "x+y", "path": litRoot, "mode": "text", "literal": true})
		got, _ := parseTextBlocks(content)
		assert.Equal(t, want, got, "literal:true must equal strings.Contains reference")
	})

	t.Run("ignoreCase with glob filter", func(t *testing.T) {
		globRoot := writeFixtureTree(t, map[string]string{
			"one.go":      "quoka here\nnothing\nQUOKA shouted\n",
			"two.txt":     "QuOkA filtered out by glob\n",
			"d/three.gox": "quoka wrong extension\n",
		})
		want := refTextPlane(t, globRoot, "quoka", false, true, "*.go")
		require.Len(t, want["one.go"], 2, "reference: two case-insensitive hits in one.go")
		require.NotContains(t, want, "two.txt", "reference: glob must drop the txt file")
		require.NotContains(t, want, "d/three.gox", "reference: glob must drop the .gox file")

		content, _ := runSearchJSON(t, map[string]any{
			"query": "quoka", "path": globRoot, "mode": "text", "ignoreCase": true, "glob": "*.go",
		})
		got, _ := parseTextBlocks(content)
		assert.Equal(t, want, got, "ignoreCase+glob must equal the case-folded, glob-filtered reference")
	})
}

// oracle: stdlib equivalence — the context=N display (matches marked |,
// context marked ·, seam lines deduplicated) is re-derived from the raw
// fixture plus a regexp match set and compared line for line, including
// overlapping windows and clamping at file boundaries.
func TestRunSearch_ContextDisplayVsWindowUnion_Differential(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg binary not installed — text plane requires it")
	}

	const body = "one\ntwo\nneedleA\nneedleB\nfive\nsix\nneedleC\neight\nnine\nten\neleven\n"
	root := writeFixtureTree(t, map[string]string{"ctx.txt": body})

	const query = `needle[AB]`
	re := regexp.MustCompile(query)
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	matchSet := map[int]bool{}
	for i, line := range lines {
		if re.MatchString(line) {
			matchSet[i+1] = true
		}
	}
	require.Equal(t, map[int]bool{3: true, 4: true}, matchSet, "reference match set sanity")

	content, _ := runSearchJSON(t, map[string]any{"query": query, "path": root, "mode": "text", "context": 1})
	got := parseDisplayLines(content, "ctx.txt")
	want := refContextDisplay(body, matchSet, 1)
	assert.Equal(t, want, got, "context display must equal the window-union reference")

	// A wider context around the distant third match clamps at the file end.
	const wide = "a\nneedleD\nb\nc\nd\ne\n"
	wideRoot := writeFixtureTree(t, map[string]string{"wide.txt": wide})
	matchSet = map[int]bool{2: true}
	content, _ = runSearchJSON(t, map[string]any{"query": `needleD`, "path": wideRoot, "mode": "text", "context": 4})
	got = parseDisplayLines(content, "wide.txt")
	want = refContextDisplay(wide, matchSet, 4)
	assert.Equal(t, want, got, "context clamps at file boundaries")
}

// naiveDetail re-derives DetailFromArgs independently: a map-based
// encoding/json decode (the tool decodes into a struct), the pattern→query
// fallback, the 40-byte truncation, and the list-mode detail.
func naiveSearchDetail(input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	str := func(key string) string {
		s, _ := m[key].(string)
		return s
	}
	q := strings.TrimSpace(str("query"))
	if q == "" {
		q = strings.TrimSpace(str("pattern"))
	}
	if q != "" {
		if len(q) > 40 {
			q = q[:40] + "..."
		}
		return fmt.Sprintf("search %q", q)
	}
	if p := strings.TrimSpace(str("path")); p != "" {
		return "list " + p
	}
	return ""
}

// oracle: naive reference implementation in this test — a map-based JSON
// decode and hand-rolled truncation/formatting compared over normal, edge,
// and hostile inputs, which the detail helper must survive without panicking.
func TestDetailFromArgs_Differential(t *testing.T) {
	long := strings.Repeat("needle", 10)   // 60 chars, past the 40-byte cut
	unicode := strings.Repeat("検索クエリ", 12) // the byte cut lands mid-rune

	cases := []struct {
		name  string
		input string
	}{
		{"query plain", `{"query":"auth flow"}`},
		{"query padded", `{"query":"   padded   "}`},
		{"query long", `{"query":"` + long + `"}`},
		{"query multibyte", `{"query":"` + unicode + `"}`},
		{"pattern alias", `{"pattern":"fallback"}`},
		{"pattern ignored when query set", `{"query":"q","pattern":"p"}`},
		{"pattern blank falls to path", `{"pattern":"   ","path":"/tmp/x"}`},
		{"non-string query", `{"query": 17}`},
		{"path only", `{"path":"internal/tools"}`},
		{"path padded", `{"path":"  /var/log  "}`},
		{"empty object", `{}`},
		{"not json", `not json at all`},
		{"json array", `[]`},
	}
	tool := SearchTool()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, naiveSearchDetail(json.RawMessage(tc.input)), tool.DetailFromArgs(json.RawMessage(tc.input)))
		})
	}
}
