package nplusonetool

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

// naiveLoopLine re-derives the loop heuristic by tokenization: a line is a
// loop when the word "for" appears as a field (the implementation substring
// matches "for " / "for{"). Fixture lines are chosen so both detectors agree.
func naiveLoopLine(line string) bool {
	for _, f := range strings.Fields(line) {
		if f == "for" {
			return true
		}
	}
	return false
}

// naiveQuery re-derives queryRe as substring tests on the lowercased line —
// every alternative in the production regexp is a literal (the db.* ORM group
// included), so the two detectors are equivalent for any input.
func naiveQueryLine(lower string) bool {
	for _, s := range []string{
		"db.query", "db.exec", "sql.query", "gorm.find", "gorm.first", "gorm.where",
		".raw(", ".scan(", ".select(", ".create(", ".save(", ".update(", ".delete(",
		"db.find", "db.first", "db.where", "db.take", "db.last", "db.raw",
		"db.create", "db.save", "db.update", "db.delete",
	} {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// naiveQueryType mirrors the documented type precedence: query, exec, gorm
// (find/first/where), raw, then unknown.
func naiveQueryType(lower string) string {
	switch {
	case strings.Contains(lower, "query"):
		return "query"
	case strings.Contains(lower, "exec"):
		return "exec"
	case strings.Contains(lower, "find"), strings.Contains(lower, "first"), strings.Contains(lower, "where"):
		return "gorm"
	case strings.Contains(lower, "raw"):
		return "raw"
	}
	return "unknown"
}

// naiveBlockEnd finds the line closing the block: the first later line whose
// left-trimmed text starts with "}", or the last line when there is none.
func naiveBlockEnd(lines []string, start int) int {
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimLeft(lines[i], " \t"), "}") {
			return i
		}
	}
	return len(lines) - 1
}

func naiveSkipDirs(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", ".tox", "dist", "build":
		return true
	}
	return false
}

// naiveDetect re-derives detectNPlusOne over filepath.WalkDir: one hit per
// loop line (the first query line inside its block), file-major order, limit
// applied at file granularity then hard-trimmed.
func naiveDetect(t *testing.T, root, glob string, limit int) []string {
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
		for i, line := range lines {
			if !naiveLoopLine(line) {
				continue
			}
			for j := i + 1; j <= naiveBlockEnd(lines, i) && j < len(lines); j++ {
				lower := strings.ToLower(lines[j])
				if naiveQueryLine(lower) {
					hits = append(hits, fmt.Sprintf("%s:%d\t%d\t%s\t%s",
						filepath.ToSlash(rel), i+1, j+1, naiveQueryType(lower), strings.TrimSpace(lines[j])))
					break
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

// writeNplusoneFixture builds a tree with: a plain query-in-loop, a gorm
// Find, idiomatic db.Find / db.Take loops (receiver named db — the shapes the
// gorm.*-only regexp missed), an unknown-type Scan, a "for" in a comment
// followed by a call before the closing brace, nested loops sharing one
// query line, a query outside any loop (never reported), two queries in one
// loop (only the first reported), an unclosed loop at EOF, and vendor/ +
// prose exclusions.
func writeNplusoneFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"sub", "top", "vendor"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, d), 0o755))
	}

	writeFile(t, root, "a_multi.go", `package multi

func two(db *sql.DB) {
        for i := 0; i < 3; i++ {
                _ = db.Query("one")
                _ = db.Exec("two")
        }
}
`)
	// gormdb.go pins the idiomatic ORM shapes: the receiver is named db, so
	// only the db.* alternation (not the historic gorm.* entries) sees them.
	writeFile(t, root, "gormdb.go", `package gormdb

func idiomatic(db *DB, users []User) {
        for _, u := range users {
                db.Find(&rec, u.ID)
        }
}

func takeLoop(db *DB, ids []int) {
        for _, id := range ids {
                db.Take(&rec, id)
        }
}
`)
	writeFile(t, root, "notes.txt", "for x := range xs { db.Query } \n")
	writeFile(t, root, filepath.Join("sub", "queries.go"), `package queries

import "database/sql"

func loadAll(db *sql.DB, users []User) {
        for _, u := range users {
                row := db.QueryRow("SELECT * FROM users WHERE id = ?", u.ID)
                _ = row
        }
}

func gormLoop(repo *Repo, ids []int) {
        for i := 0; i < len(ids); i++ {
                out := client.Gorm.Find(&rec)
                _ = out
        }
}

func scanLoop(rows *sql.Rows) {
        for rows.Next() {
                err := rows.Scan(&id)
                _ = err
        }
}

// wait for the migration to settle
func settle(db *sql.DB) {
        _ = db.Exec("SETTLE")
}
`)
	writeFile(t, root, filepath.Join("top", "outer.go"), `package outer

func nested(items []Item, db *sql.DB) {
        for _, it := range items {
                for _, sub := range it.Parts {
                        _ = db.Exec("INSERT", sub)
                }
        }
}

func clean(db *sql.DB) {
        _ = db.Query("SELECT 1")
}
`)
	writeFile(t, root, "z_unclosed.go", "package unclosed\n\nfunc pending(db *sql.DB) {\n\tfor _, x := range xs {\n\t\t_ = db.Exec(\"x\")")
	writeFile(t, root, filepath.Join("vendor", "skip.go"), `package vendored

func hidden(db *sql.DB) {
        for i := 0; i < 2; i++ {
                _ = db.Query("x")
        }
}
`)
	return root
}

func runNplusoneOn(t *testing.T, glob string, limit int) (string, string) {
	t.Helper()
	raw, err := json.Marshal(nplusoneInput{Path: ".", Glob: glob, Limit: limit})
	require.NoError(t, err)
	out, err := runNplusone(t.Context(), raw)
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

// oracle: naive reference implementation in this test — tokenized loop
// detection plus substring query detection (every regexp alternative is a
// literal) over a hand-written fixture, walked with filepath.WalkDir, must
// find the same sites: loop and query line numbers, type classification,
// first-query-only per loop, nested-loop double reporting, and the
// comment-"for" quirk.
func TestRunNplusone_MatchesNaiveScan_Differential(t *testing.T) {
	root := writeNplusoneFixture(t)
	t.Chdir(root)

	got, detail := runNplusoneOn(t, "*.go", 100)
	want := naiveDetect(t, root, "*.go", 100)
	require.Len(t, want, 10, "ten hits across the fixture")
	assert.Equal(t, renderNaiveHits(want), got)
	assert.Equal(t, "10 potential N+1 patterns", detail)

	// Idiomatic GORM inside loops: db.Find classifies as gorm, db.Take as
	// unknown (only find/first/where map to gorm today).
	assert.Contains(t, got, "gormdb.go:4\t5\tgorm\tdb.Find(&rec, u.ID)")
	assert.Contains(t, got, "gormdb.go:10\t11\tunknown\tdb.Take(&rec, id)")

	// The nested loop reports the shared query line under both loop lines.
	assert.Contains(t, got, "top/outer.go:4\t6\texec\t_ = db.Exec(\"INSERT\", sub)")
	assert.Contains(t, got, "top/outer.go:5\t6\texec\t_ = db.Exec(\"INSERT\", sub)")
	// Only the first query in a_multi's loop is reported.
	assert.Contains(t, got, "a_multi.go:4\t5\tquery\t_ = db.Query(\"one\")")
	assert.NotContains(t, got, "db.Exec(\"two\")")
	// A query outside any loop is never reported.
	assert.NotContains(t, got, "SELECT 1")
	// The unclosed loop falls back to the last line as its block end.
	assert.Contains(t, got, "z_unclosed.go:4\t5\texec\t_ = db.Exec(\"x\")")
	// vendor/ and non-.go files are not scanned.
	assert.NotContains(t, got, "vendor/skip.go")
	assert.NotContains(t, got, "notes.txt")
}

// oracle: same naive reference with a spent budget — counting stops after
// the file that crosses the limit, the hit list is hard-trimmed to the limit,
// and a base-name glob restricts the walk identically.
func TestRunNplusone_LimitAndGlobMatchNaiveCount_Differential(t *testing.T) {
	root := writeNplusoneFixture(t)
	t.Chdir(root)

	t.Run("limit trims mid-file", func(t *testing.T) {
		got, detail := runNplusoneOn(t, "*.go", 3)
		want := naiveDetect(t, root, "*.go", 3)
		require.Len(t, want, 3)
		assert.Equal(t, renderNaiveHits(want), got)
		assert.Equal(t, "3 potential N+1 patterns", detail)
		assert.NotContains(t, got, "top/outer.go")
	})

	t.Run("base-name glob restricts the walk", func(t *testing.T) {
		got, detail := runNplusoneOn(t, "outer*", 100)
		want := naiveDetect(t, root, "outer*", 100)
		require.Len(t, want, 2, "only outer.go matches the glob")
		assert.Equal(t, renderNaiveHits(want), got)
		assert.Equal(t, "2 potential N+1 patterns", detail)
	})
}
