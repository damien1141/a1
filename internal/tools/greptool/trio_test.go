package greptool

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// requireRG skips when rg is not resolvable, mirroring the guard the sibling
// tests apply.
func requireRG(t *testing.T) {
	t.Helper()
	if _, err := resolveRipgrepPath(); err != nil {
		t.Skip("rg not available:", err)
	}
}

func writeGrepTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		fp := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(fp), 0o755), "fixture dir for %s", rel)
		require.NoError(t, os.WriteFile(fp, []byte(content), 0o644), "fixture file %s", rel)
	}
	return root
}

// refNormalize is the CRLF/CR → LF normalization re-derived with
// strings.ReplaceAll instead of the util package's helpers.
func refNormalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// rgGlobMatch covers the glob shapes the tool documents (e.g. *_test.go): rg
// matches globs against paths relative to the search root, and its '*' crosses
// separators, so "*.go" also matches "sub/c.go".
func rgGlobMatch(glob, rel string) bool {
	var sb strings.Builder
	sb.WriteString("^")
	for _, r := range glob {
		switch r {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteString(".")
		default:
			sb.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	sb.WriteString("$")
	return regexp.MustCompile(sb.String()).MatchString(rel)
}

// refGrepScan re-derives the display with the standard library alone:
// filepath.WalkDir enumerates regular files (hidden ones included, matching
// rg --hidden), Go regexp or strings.Contains decides per line, the glob
// filters, and every matching line gets its own context window — the same
// per-match block emission the renderer documents, duplicates included.
func refGrepScan(
	t *testing.T,
	root, query string,
	literal, ignoreCase bool,
	glob string,
	ctxN int,
) map[string][]string {
	t.Helper()
	var re *regexp.Regexp
	if !literal {
		expr := query
		if ignoreCase {
			expr = "(?i)" + expr
		}
		re = regexp.MustCompile(expr)
	}
	needle := strings.ToLower(query)

	out := map[string][]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if glob != "" && !rgGlobMatch(glob, rel) {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		lines := strings.Split(refNormalize(string(data)), "\n")
		for i, line := range lines {
			var hit bool
			switch {
			case literal && ignoreCase:
				hit = strings.Contains(strings.ToLower(line), needle)
			case literal:
				hit = strings.Contains(line, query)
			default:
				hit = re.MatchString(line) // ignoreCase is baked into (?i)
			}
			if !hit {
				continue
			}
			ln := i + 1
			start, end := ln, ln
			if ctxN > 0 {
				start = max(1, ln-ctxN)
				end = min(len(lines), ln+ctxN)
			}
			for cur := start; cur <= end; cur++ {
				text := strings.ReplaceAll(lines[cur-1], "\r", "")
				if r := []rune(text); len(r) > grepMaxLineRunes {
					text = string(r[:grepMaxLineRunes]) + grepTruncatedSuffix
				}
				prefix := "  "
				if cur == ln {
					prefix = ">>"
				}
				out[rel] = append(out[rel], fmt.Sprintf("%s:%s%4d|%s", rel, prefix, cur, text))
			}
		}
		return nil
	})
	require.NoError(t, err)
	return out
}

// parseGrepBlocks extracts the raw display lines per file from the tool's
// output, keyed by the same slash-relative paths the reference produces. A
// blank line starts the notices block, so it ends the current file's block.
func parseGrepBlocks(content string) map[string][]string {
	out := map[string][]string{}
	cur := ""
	for _, line := range strings.Split(content, "\n") {
		switch {
		case strings.HasPrefix(line, "@file "):
			cur = strings.TrimPrefix(line, "@file ")
		case line == "":
			cur = ""
		case cur != "":
			out[cur] = append(out[cur], line)
		}
	}
	return out
}

func runGrepJSON(t *testing.T, in grepInput) (content string, detail string) {
	t.Helper()
	requireRG(t)
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	res, err := runGrep(t.Context(), raw)
	require.NoError(t, err)
	return res.Content, res.Detail
}

// oracle: stdlib equivalence — Go regexp over a filepath.WalkDir walk of the
// same fixture tree (hidden files included, like rg --hidden) re-derives the
// rendered display line for line: match marks, context windows with their
// seam duplicates, line numbers past the %4d padding width, the 500-rune
// cut, literal mode, ignoreCase, and the glob filter.
func TestRunGrep_DisplayVsRegexpWalk_Differential(t *testing.T) {
	requireRG(t)

	bigLines := make([]string, 1005)
	for i := range bigLines {
		bigLines[i] = "filler"
	}
	bigLines[1002] = "needleBIG"

	tree := map[string]string{
		"alpha.go":           "package alpha\n\nfunc Use() {\n\t_ = needle42\n\t_ = needle7\n}\n",
		"sub/beta.txt":       "plain line\nNEEDLE9 stays uppercase\nneedle0 here\n",
		".hidden/gamma.md":   "hidden needle99\nnothing else\n",
		"delta.go":           "no matches at all\n",
		"sub/deeper/eps.txt": "needle5 deep\nneedle6 deeper\nneedle7 deepest\n",
		"lit.txt":            "has x+y literal\nhas xy regex-only\nhas xxy regex-only\n",
		"crlf.txt":           "needleR\r\nplain\r\n",
		"nonl.txt":           "last needleN",
		"big.txt":            strings.Join(bigLines, "\n") + "\n",
	}
	root := writeGrepTree(t, tree)
	t.Chdir(root)

	t.Run("regex across hidden and nested files", func(t *testing.T) {
		want := refGrepScan(t, root, `needle[0-9]+`, false, false, "", 0)
		require.Len(t, want, 4, "reference must find four files")
		require.Contains(t, want, ".hidden/gamma.md", "reference includes hidden files")

		content, detail := runGrepJSON(t, grepInput{Pattern: `needle[0-9]+`, Path: root})
		assert.Equal(t, want, parseGrepBlocks(content), "display must equal the regexp+WalkDir reference")
		assert.Empty(t, detail, "live-run detail stays empty; the count is the row stat")
		assert.NotContains(t, content, "limit reached")
		assert.NotContains(t, content, "truncated")
	})

	t.Run("ignoreCase with glob filter", func(t *testing.T) {
		want := refGrepScan(t, root, "NEEDLE9", false, true, "*.txt", 0)
		require.Equal(t, map[string][]string{
			"sub/beta.txt": {"sub/beta.txt:>>   2|NEEDLE9 stays uppercase"},
		}, want, "reference sanity: one case-folded hit under a subdir glob match")

		content, _ := runGrepJSON(t, grepInput{Pattern: "NEEDLE9", Path: root, IgnoreCase: true, Glob: "*.txt"})
		assert.Equal(t, want, parseGrepBlocks(content))
	})

	t.Run("literal query with regex metacharacters", func(t *testing.T) {
		// As a regex "x+y" would also match "xy"/"xxy" — only the literal
		// reading may match the "x+y" line itself.
		want := refGrepScan(t, root, "x+y", true, false, "", 0)
		require.Equal(t, map[string][]string{
			"lit.txt": {"lit.txt:>>   1|has x+y literal"},
		}, want, "literal reference sanity")

		content, _ := runGrepJSON(t, grepInput{Pattern: "x+y", Path: root, Literal: true})
		assert.Equal(t, want, parseGrepBlocks(content))
	})

	t.Run("context windows keep seam duplicates", func(t *testing.T) {
		ctxRoot := writeGrepTree(t, map[string]string{
			"ctx.txt": "one\ntwo\nneedleA\nneedleB\nfive\nsix\nneedleC\neight\nnine\nten\neleven\n",
		})
		t.Chdir(ctxRoot)
		want := refGrepScan(t, ctxRoot, `needle[AB]`, false, false, "", 1)
		ctxWant, ok := want["ctx.txt"]
		require.True(t, ok)
		// Adjacent matches at lines 3 and 4 re-render lines 3 and 4: the
		// per-match block emission is the contract, not a merged union, and a
		// seam line is a ">>" match only inside its own block.
		require.Equal(t, []string{
			"ctx.txt:     2|two",
			"ctx.txt:>>   3|needleA",
			"ctx.txt:     4|needleB",
			"ctx.txt:     3|needleA",
			"ctx.txt:>>   4|needleB",
			"ctx.txt:     5|five",
		}, ctxWant, "reference sanity: seam lines duplicated")

		content, _ := runGrepJSON(t, grepInput{Pattern: `needle[AB]`, Path: ctxRoot, Context: 1})
		assert.Equal(t, want, parseGrepBlocks(content))
	})

	t.Run("wide context clamps at file boundaries", func(t *testing.T) {
		wideRoot := writeGrepTree(t, map[string]string{"wide.txt": "a\nneedleD\nb\nc\nd\ne\n"})
		t.Chdir(wideRoot)
		want := refGrepScan(t, wideRoot, `needleD`, false, false, "", 4)
		require.Len(t, want["wide.txt"], 6, "reference sanity: whole file window")

		content, _ := runGrepJSON(t, grepInput{Pattern: `needleD`, Path: wideRoot, Context: 4})
		assert.Equal(t, want, parseGrepBlocks(content))
	})

	t.Run("lines past the %4d width and the 500-rune cut", func(t *testing.T) {
		longASCII := "needleT " + strings.Repeat("z", 600)
		longCJK := "needleU " + strings.Repeat("語", 600)
		bigRoot := writeGrepTree(t, map[string]string{
			"big.txt":  strings.Join(bigLines, "\n") + "\n",
			"long.txt": longASCII + "\n",
			"cjk.txt":  longCJK + "\n",
		})
		t.Chdir(bigRoot)
		want := refGrepScan(t, bigRoot, `needle(BIG|T|U)`, false, false, "", 0)
		require.Len(t, want, 3, "reference sanity")
		require.Contains(t, want["big.txt"][0], "big.txt:>>1003|", "4-digit line numbers lose padding")
		require.Contains(t, want["long.txt"][0], grepTruncatedSuffix, "ASCII long line is cut")
		require.Contains(t, want["cjk.txt"][0], grepTruncatedSuffix, "CJK long line is cut")

		content, _ := runGrepJSON(t, grepInput{Pattern: `needle(BIG|T|U)`, Path: bigRoot})
		assert.Equal(t, want, parseGrepBlocks(content))
		assert.Contains(t, content, "Some lines truncated to 500 chars. Use read to see full lines")
	})

	t.Run("crlf and missing trailing newline", func(t *testing.T) {
		want := refGrepScan(t, root, `needle[RN]`, false, false, "", 0)
		require.Len(t, want, 2, "reference sanity: crlf.txt and nonl.txt")

		content, _ := runGrepJSON(t, grepInput{Pattern: `needle[RN]`, Path: root})
		assert.Equal(t, want, parseGrepBlocks(content))
	})

	t.Run("no matches", func(t *testing.T) {
		want := refGrepScan(t, root, "absentqqq", false, false, "", 0)
		require.Empty(t, want)

		content, detail := runGrepJSON(t, grepInput{Pattern: "absentqqq", Path: root})
		assert.Equal(t, "No matches found", content)
		assert.Equal(t, "0 matches", detail)
	})
}

// oracle: stdlib equivalence — with a single-file fixture the event order is
// deterministic (one searcher walks the file in line order), so a limit below
// the reference's match count must render exactly the first N reference
// blocks and carry the limit notice.
func TestRunGrep_MatchLimitVsReference_Differential(t *testing.T) {
	requireRG(t)

	root := writeGrepTree(t, map[string]string{"lim.txt": strings.Repeat("needleL\n", 5)})
	t.Chdir(root)
	want := refGrepScan(t, root, "needleL", false, false, "", 0)
	require.Len(t, want["lim.txt"], 5, "reference sanity: five matching lines")

	content, _ := runGrepJSON(t, grepInput{Pattern: "needleL", Path: root, Limit: 2})
	blocks := parseGrepBlocks(content)
	require.Len(t, blocks, 1)
	assert.Equal(t, want["lim.txt"][:2], blocks["lim.txt"], "only the first two events may render")
	assert.Contains(t, content, "[2 matches limit reached. Use limit=4 for more, or refine pattern]")
}
