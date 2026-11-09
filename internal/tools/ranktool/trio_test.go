package ranktool

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// naiveSkipDirs mirrors the traversal contract: these directory names are
// pruned wherever they appear.
var naiveSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, ".tox": true, "dist": true, "build": true,
}

// naiveMatchSegs mirrors the glob contract for multi-segment patterns
// without reusing production matchSegments: an explicit backtracking scan
// where "**" eats zero or more slash-separated segments and every other
// segment is a filepath.Match on exactly one path segment.
func naiveMatchSegs(t *testing.T, pat, p []string) bool {
	t.Helper()
	if len(pat) == 0 {
		return len(p) == 0
	}
	if pat[0] == "**" {
		for skip := 0; skip <= len(p); skip++ {
			if naiveMatchSegs(t, pat[1:], p[skip:]) {
				return true
			}
		}
		return false
	}
	if len(p) == 0 {
		return false
	}
	ok, err := filepath.Match(pat[0], p[0])
	require.NoError(t, err)
	return ok && naiveMatchSegs(t, pat[1:], p[1:])
}

// naiveGlob re-derives the documented matching rules: single-segment
// patterns (no "/" and no "**") match base names; anything else matches the
// slash-relative path with "**" wildcards.
func naiveGlob(t *testing.T, glob, rel, base string) bool {
	t.Helper()
	if !strings.Contains(glob, "/") && !strings.Contains(glob, "**") {
		ok, err := filepath.Match(glob, base)
		require.NoError(t, err)
		return ok
	}
	return naiveMatchSegs(t, strings.Split(glob, "/"), strings.Split(rel, "/"))
}

// naiveCandidates re-walks the tree with os.ReadDir recursion instead of
// filepath.Walk and returns the slash-relative paths that survive the skip
// list and the glob.
func naiveCandidates(t *testing.T, root, glob string) []string {
	t.Helper()
	var out []string
	var walk func(dir, rel string)
	walk = func(dir, rel string) {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			name := e.Name()
			childRel := name
			if rel != "" {
				childRel = rel + "/" + name
			}
			if e.IsDir() {
				if naiveSkipDirs[name] {
					continue
				}
				walk(filepath.Join(dir, name), childRel)
				continue
			}
			if naiveGlob(t, glob, childRel, name) {
				out = append(out, childRel)
			}
		}
	}
	walk(root, "")
	return out
}

// naiveScore recomputes the documented heuristics from first principles:
// 10·ln(size+1) for size, max(0, 30−5·depth) for shallowness, the file-type
// penalty table, floored at zero. Sizes come from the fixture definition,
// not from the filesystem the tool walked.
func naiveScore(size, depth int, base string) (float64, []string) {
	score := 0.0
	var reasons []string
	if size > 0 {
		score += math.Log(float64(size)+1) * 10
		if size > 10000 {
			reasons = append(reasons, fmt.Sprintf("large file (%.1f KB)", float64(size)/1024))
		}
	}
	depthScore := 30 - depth*5
	if depthScore < 0 {
		depthScore = 0
	}
	score += float64(depthScore)
	if depth == 0 {
		reasons = append(reasons, "root level")
	}
	ext := strings.ToLower(filepath.Ext(base))
	penalty := 0.0
	kind := ""
	switch {
	case strings.HasSuffix(base, "_test.go"), strings.HasSuffix(base, "_test.py"),
		strings.HasSuffix(base, "_test.rs"), strings.HasSuffix(base, "_test.js"),
		strings.HasSuffix(base, "_test.ts"), strings.HasSuffix(base, ".test.go"),
		strings.HasSuffix(base, ".spec.js"), strings.HasSuffix(base, ".spec.ts"):
		penalty, kind = -40, "test file"
	case ext == ".md":
		penalty, kind = -30, "documentation"
	case ext == ".json" || ext == ".yaml" || ext == ".yml" || ext == ".toml" ||
		ext == ".ini" || ext == ".cfg" || base == "Makefile" || base == "Dockerfile":
		penalty, kind = -20, "config"
	case ext == ".mod" || ext == ".sum" || base == "go.sum" || base == "package-lock.json" ||
		base == "yarn.lock" || base == "pnpm-lock.yaml":
		penalty, kind = -50, "lockfile"
	case ext == ".gitignore" || ext == ".editorconfig" || ext == ".env":
		penalty, kind = -60, "dotfile"
	}
	if kind != "" {
		score += penalty
		reasons = append(reasons, kind)
	}
	if score < 0 {
		score = 0
	}
	return score, reasons
}

// rankFixture builds a tree with known byte sizes (so scores are known), a
// pruned vendor directory, and no two equal scores — the production sort is
// not stable, so ties would make the order implementation-defined.
func rankFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for rel, size := range rankFixtureSizes() {
		writeRankFile(t, root, rel, strings.Repeat("x", size))
	}
	// Big enough to outscore everything if the skip list were ignored.
	writeRankFile(t, root, "vendor/skipped.go", strings.Repeat("y", 500))
	return root
}

func rankFixtureSizes() map[string]int {
	return map[string]int{
		"main.go":            120,
		"README.md":          150,
		"api_test.go":        90,
		"go.mod":             60,
		".env":               3,
		"sub/util.go":        130,
		"sub/deep/data.json": 30,
	}
}

func writeRankFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
}

func runRankIn(t *testing.T, root, glob string, limit int) string {
	t.Helper()
	t.Chdir(root)
	raw, err := json.Marshal(rankInput{Path: ".", Glob: glob, Limit: limit})
	require.NoError(t, err)
	out, err := runRank(t.Context(), raw)
	require.NoError(t, err)
	return out.Content
}

// naiveRank renders the full expected output: candidate collection, scoring,
// descending order, the limit cut, and the "score\tpath — reasons" lines.
func naiveRank(t *testing.T, root, glob string, limit int) string {
	t.Helper()
	sizes := rankFixtureSizes()
	type row struct {
		rel     string
		score   float64
		reasons []string
	}
	var rows []row
	for _, rel := range naiveCandidates(t, root, glob) {
		score, reasons := naiveScore(sizes[rel], strings.Count(rel, "/"), filepath.Base(rel))
		rows = append(rows, row{rel: rel, score: score, reasons: reasons})
	}
	for i := 0; i < len(rows); i++ {
		for j := i + 1; j < len(rows); j++ {
			require.NotEqual(t, rows[i].score, rows[j].score,
				"fixture rows %s and %s tie — the fixture must keep scores distinct", rows[i].rel, rows[j].rel)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].score > rows[j].score })
	if len(rows) > limit {
		rows = rows[:limit]
	}
	var b strings.Builder
	for _, r := range rows {
		reason := ""
		if len(r.reasons) > 0 {
			reason = " — " + strings.Join(r.reasons, ", ")
		}
		fmt.Fprintf(&b, "%.1f\t%s%s\n", r.score, r.rel, reason)
	}
	return b.String()
}

// oracle: naive reference — an os.ReadDir traversal (not filepath.Walk), the
// documented scoring heuristics recomputed from the fixture's known byte
// sizes, and an independent descending sort, compared line by line against
// the tool's rendered output.
func TestRunRank_MatchesNaiveRanker_Differential(t *testing.T) {
	root := rankFixture(t)
	assert.Equal(t, naiveRank(t, root, "*", 50), runRankIn(t, root, "*", 50))
}

// oracle: the same naive reference, now driving the glob filter and the
// limit cut independently of the tool's walker.
func TestRunRank_GlobAndLimit_Differential(t *testing.T) {
	root := rankFixture(t)
	t.Run("go files only", func(t *testing.T) {
		assert.Equal(t, naiveRank(t, root, "*.go", 50), runRankIn(t, root, "*.go", 50))
	})
	t.Run("limit three", func(t *testing.T) {
		assert.Equal(t, naiveRank(t, root, "*", 3), runRankIn(t, root, "*", 3))
	})
	t.Run("limit one", func(t *testing.T) {
		assert.Equal(t, naiveRank(t, root, "*", 1), runRankIn(t, root, "*", 1))
	})
}

// rankPaths extracts the ranked relative paths (the tab-separated second
// column) from rendered rank output, in ranked order.
func rankPaths(t *testing.T, content string) []string {
	t.Helper()
	var paths []string
	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		require.Len(t, parts, 2, "rank line must be score<TAB>path: %q", line)
		// The path column carries an optional " — reasons" suffix; paths
		// never contain that separator.
		paths = append(paths, strings.SplitN(parts[1], " — ", 2)[0])
	}
	return paths
}

// oracle: golden literals — hand-written expected file lists for the
// documented segment globs over the nested fixture tree, each cross-checked
// against the extended naive reference. Base-name matching could never
// return these, so the literals pin the "**" semantics: "**/*.go" reaches
// every depth, "sub/*.go" only direct children, "sub/**" the whole subtree.
func TestRunRank_SegmentGlobs_Differential(t *testing.T) {
	root := rankFixture(t)
	cases := []struct {
		name string
		glob string
		want []string
	}{
		{name: "double star go files at every depth", glob: "**/*.go", want: []string{"main.go", "sub/util.go", "api_test.go"}},
		{name: "direct children only", glob: "sub/*.go", want: []string{"sub/util.go"}},
		{name: "whole subtree", glob: "sub/**", want: []string{"sub/util.go", "sub/deep/data.json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rankPaths(t, runRankIn(t, root, tc.glob, 50))
			assert.Equal(t, tc.want, got)
			// The extended naive oracle must agree with the literals.
			assert.Equal(t, naiveRank(t, root, tc.glob, 50), runRankIn(t, root, tc.glob, 50))
		})
	}
}
