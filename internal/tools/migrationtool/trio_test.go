package migrationtool

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// naiveRule is one migration rule expressed as a substring test on the
// lowercased line — no regexp anywhere, unlike the production patterns.
type naiveRule struct {
	match   func(lower string) bool
	message string
}

func naiveRules() []naiveRule {
	isWord := func(s string) bool {
		if s == "" {
			return false
		}
		for _, r := range s {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
				return false
			}
		}
		return true
	}
	containsAny := func(subs ...string) func(string) bool {
		return func(lower string) bool {
			for _, s := range subs {
				if strings.Contains(lower, s) {
					return true
				}
			}
			return false
		}
	}

	return []naiveRule{
		{containsAny("ioutil.readfile"), "ioutil.ReadFile is deprecated; use os.ReadFile (Go 1.16+)"},
		{containsAny("ioutil.writefile"), "ioutil.WriteFile is deprecated; use os.WriteFile (Go 1.16+)"},
		{containsAny("ioutil.readall"), "ioutil.ReadAll is deprecated; use io.ReadAll (Go 1.16+)"},
		{containsAny("ioutil.nopcloser"), "ioutil.NopCloser is deprecated; use io.NopCloser (Go 1.16+)"},
		{containsAny("golang.org/x/net/context"), "golang.org/x/net/context is deprecated; use context package (Go 1.7+)"},
		{containsAny("context.todo()"), "context.TODO() suggests incomplete error handling; consider context.Background() or context.WithCancel"},
		{containsAny("http.get(", "http.post(", "http.do("), "http.Client.Get/Post/Do without timeout; wrap in context.WithTimeout"},
		// The sql.Rows.Scan rule is paren-aware and has no faithful substring
		// mirror, so the fixture deliberately contains no such line and the
		// naive table omits it. Rule order otherwise mirrors the documented
		// pattern table, which is the hit order the renderer exposes.
		{func(lower string) bool {
			fields := strings.Fields(strings.TrimRight(lower, " \t\v\f\r"))
			if len(fields) != 2 || fields[0] != "defer" {
				return false
			}
			recv, ok := strings.CutSuffix(fields[1], ".close()")
			return ok && isWord(recv)
		}, "defer Close() without error check; consider checking Close() error"},
	}
}

// naiveSkipDirs is the documented walk rule, hand-written: common VCS and
// dependency directories are never scanned.
func naiveSkipDirs(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", ".tox", "dist", "build":
		return true
	}
	return false
}

// naiveMigrate re-derives scanMigrationTargets with filepath.WalkDir and the
// substring rule table: pattern-major hit order within a file, file-major
// across files, limit applied at file granularity then hard-trimmed.
func naiveMigrate(t *testing.T, root, glob string, limit int) []string {
	t.Helper()
	var hits []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if naiveSkipDirs(filepath.Base(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		matched, merr := filepath.Match(glob, filepath.Base(path))
		if merr != nil || !matched || !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		lines := strings.Split(string(b), "\n")
		rel, rerr := filepath.Rel(root, path)
		require.NoError(t, rerr)
		for _, rule := range naiveRules() {
			for i, line := range lines {
				if rule.match(strings.ToLower(line)) {
					hits = append(hits, fmt.Sprintf("%s:%d\t%s", filepath.ToSlash(rel), i+1, rule.message))
				}
			}
		}
		if len(hits) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	require.NoError(t, err)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// writeMigrationFixture builds a tree exercising every naively expressible
// rule plus the exclusions: vendor/ is skipped wholesale, prose files are
// ignored even when the glob would match, mixed-case APIs still hit, and the
// last file has no trailing newline.
func writeMigrationFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "vendor"), 0o755))

	writeFile(t, root, "a.go", `package fixture

import (
	"io/ioutil"
	"golang.org/x/net/context"
)

// see also ioutil.ReadAll in the old docs
func load() {
	data, err := ioutil.ReadFile("x.txt")
	_ = data
	_ = err
}

func save() {
	_ = ioutil.WriteFile("y.txt", nil, 0o644)
}
`)
	writeFile(t, root, "case.go", `package mixedcase

func loud() {
	_, _ = IOUTIL.ReadFile("upper")
}
`)
	writeFile(t, root, "notes.txt", "ioutil.ReadFile mentioned in prose\nhttp.Get( in prose\n")
	writeFile(t, root, filepath.Join("sub", "b.go"), `package sub

func all(r io.Reader) {
	_, _ = ioutil.ReadAll(r)
	_ = http.Get("http://example")
}

func todo() {
	_ = context.TODO()
}

func done(f *os.File) {
	defer f.Close()
}
`)
	writeFile(t, root, "trailing.go", "package trailing\n\nfunc last() {\n\t_ = ioutil.ReadFile(\"z\")")
	writeFile(t, root, filepath.Join("vendor", "skip.go"), `package vendored

func hidden() {
	_, _ = ioutil.ReadAll(nil)
}
`)
	return root
}

func runMigrateOn(t *testing.T, glob string, limit int) (string, string) {
	t.Helper()
	raw, err := json.Marshal(migrationInput{Path: ".", Glob: glob, Limit: limit})
	require.NoError(t, err)
	out, err := runMigration(t.Context(), raw)
	require.NoError(t, err)
	return out.Content, out.Detail
}

func renderNaiveHits(hits []string) string {
	var sb strings.Builder
	for _, h := range hits {
		sb.WriteString(h)
		sb.WriteString("\n")
	}
	return sb.String()
}

// oracle: naive reference implementation in this test — a substring rule
// table (no regexp) over a hand-written fixture, walked with filepath.WalkDir
// under a hand-written skip rule, must produce the same hit lines, order, and
// count as the regexp-based scanner.
func TestRunMigration_MatchesNaiveScan_Differential(t *testing.T) {
	root := writeMigrationFixture(t)
	t.Chdir(root)

	got, detail := runMigrateOn(t, "*.go", 100)
	want := naiveMigrate(t, root, "*.go", 100)
	require.Len(t, want, 10, "ten hits across the fixture")
	assert.Equal(t, renderNaiveHits(want), got)
	assert.Equal(t, "10 migration targets", detail)

	// The exclusions the naive walk also computes: vendor/ and non-.go files.
	for _, banned := range []string{"vendor/skip.go", "notes.txt", "IOUTIL.ReadFile"} {
		assert.NotContains(t, got, banned)
	}
	// Mixed case still hits, and the comment mention counts too.
	assert.Contains(t, got, "case.go:4\tioutil.ReadFile is deprecated")
	assert.Contains(t, got, "a.go:8\tioutil.ReadAll is deprecated")
}

// oracle: same naive reference with a spent budget — counting stops after the
// file that crosses the limit and the hit list is hard-trimmed to exactly the
// limit, and a narrower glob restricts the walk the same way.
func TestRunMigration_LimitAndGlobMatchNaiveCount_Differential(t *testing.T) {
	root := writeMigrationFixture(t)
	t.Chdir(root)

	t.Run("limit trims mid-file", func(t *testing.T) {
		got, detail := runMigrateOn(t, "*.go", 6)
		want := naiveMigrate(t, root, "*.go", 6)
		require.Len(t, want, 6)
		assert.Equal(t, renderNaiveHits(want), got)
		assert.Equal(t, "6 migration targets", detail)
		// a.go's four hits plus case.go's one fit; sub/b.go is cut at six.
		assert.NotContains(t, got, "sub/b.go:9")
	})

	t.Run("narrow glob restricts the walk", func(t *testing.T) {
		got, detail := runMigrateOn(t, "a*.go", 100)
		want := naiveMigrate(t, root, "a*.go", 100)
		require.Len(t, want, 4, "only a.go matches the glob")
		assert.Equal(t, renderNaiveHits(want), got)
		assert.Equal(t, "4 migration targets", detail)
	})

	t.Run("star glob still requires the .go suffix", func(t *testing.T) {
		got, detail := runMigrateOn(t, "*", 100)
		want := naiveMigrate(t, root, "*", 100)
		assert.Equal(t, renderNaiveHits(want), got)
		assert.Equal(t, "10 migration targets", detail)
		assert.NotContains(t, got, "notes.txt")
	})
}
