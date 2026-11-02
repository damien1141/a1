package graph

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite" // driver name "sqlite"; pure Go, FTS5 included
)

// openTestDB gives each test its own throwaway SQLite file so the cache
// tests never touch ~/.a1/a1.db. Same DSN shape as the vault (WAL +
// busy timeout) so lock behavior matches production.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite",
		"file:"+filepath.Join(t.TempDir(), "graph.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// writeFileAt writes content and pins the mtime. Pinning matters: a
// rewrite can land in the same mtime nanosecond on coarse filesystems,
// and the cache's freshness key is exactly (mtime, size).
func writeFileAt(t *testing.T, path, content string, modTime time.Time) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	require.NoError(t, os.Chtimes(path, modTime, modTime))
}

func rowCount(t *testing.T, db *sql.DB, project string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM graph_files WHERE project = ?`, project).Scan(&n))
	return n
}

func TestOpenGraphCachedParsesOnlyChangedFiles(t *testing.T) {
	ws := t.TempDir()
	db := openTestDB(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	writeFileAt(t, filepath.Join(ws, "a.go"), "package a\nimport \"fmt\"\n", base)
	writeFileAt(t, filepath.Join(ws, "b.go"), "package b\nimport \"os\"\n", base)

	// First open parses everything and stores one row per file.
	cachedParseCalls.Store(0)
	g, err := OpenGraphCached(ctx, ws, db, false)
	require.NoError(t, err)
	assert.Equal(t, 2, g.Len())
	assert.Equal(t, []string{"fmt"}, g.Imports("a.go"))
	assert.Equal(t, []string{"os"}, g.Imports("b.go"))
	assert.Equal(t, int64(2), cachedParseCalls.Load())
	assert.Equal(t, 2, rowCount(t, db, ws))

	// Second open hits the cache for both files: zero parses, same graph.
	cachedParseCalls.Store(0)
	g, err = OpenGraphCached(ctx, ws, db, false)
	require.NoError(t, err)
	assert.Equal(t, 2, g.Len())
	assert.Equal(t, int64(0), cachedParseCalls.Load(), "unchanged files must not be re-parsed")

	// Change a.go (new import, new mtime): only a.go re-parses; the edge
	// set updates; b.go comes from its cached row.
	writeFileAt(t, filepath.Join(ws, "a.go"), "package a\nimport \"strings\"\n", base.Add(time.Second))
	cachedParseCalls.Store(0)
	g, err = OpenGraphCached(ctx, ws, db, false)
	require.NoError(t, err)
	assert.Equal(t, 2, g.Len())
	assert.Equal(t, []string{"strings"}, g.Imports("a.go"))
	assert.Equal(t, []string{"os"}, g.Imports("b.go"))
	assert.Equal(t, []string{"a.go"}, g.ImportedBy("strings"))
	assert.Equal(t, int64(1), cachedParseCalls.Load(), "only the changed file should re-parse")
}

func TestOpenGraphCachedDeletesVanishedFiles(t *testing.T) {
	ws := t.TempDir()
	db := openTestDB(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	writeFileAt(t, filepath.Join(ws, "a.go"), "package a\nimport \"fmt\"\n", base)
	writeFileAt(t, filepath.Join(ws, "b.go"), "package b\nimport \"os\"\n", base)
	_, err := OpenGraphCached(ctx, ws, db, false)
	require.NoError(t, err)
	require.Equal(t, 2, rowCount(t, db, ws))

	require.NoError(t, os.Remove(filepath.Join(ws, "b.go")))
	g, err := OpenGraphCached(ctx, ws, db, false)
	require.NoError(t, err)
	assert.Equal(t, 1, g.Len())
	assert.Empty(t, g.Imports("b.go"))
	assert.Equal(t, 1, rowCount(t, db, ws), "the vanished file's row must be deleted")

	// New files on later opens are picked up (they simply have no row).
	writeFileAt(t, filepath.Join(ws, "c.go"), "package c\nimport \"a\"\n", base.Add(2*time.Second))
	g, err = OpenGraphCached(ctx, ws, db, false)
	require.NoError(t, err)
	assert.Equal(t, 2, g.Len())
	assert.Equal(t, []string{"c.go"}, g.ImportedBy("a"))
}

func TestOpenGraphCachedRescanForcesFullRebuild(t *testing.T) {
	ws := t.TempDir()
	db := openTestDB(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	writeFileAt(t, filepath.Join(ws, "a.go"), "package a\nimport \"fmt\"\n", base)
	writeFileAt(t, filepath.Join(ws, "b.go"), "package b\nimport \"os\"\n", base)
	_, err := OpenGraphCached(ctx, ws, db, false)
	require.NoError(t, err)

	// rescan re-parses every file even though nothing changed on disk.
	cachedParseCalls.Store(0)
	g, err := OpenGraphCached(ctx, ws, db, true)
	require.NoError(t, err)
	assert.Equal(t, 2, g.Len())
	assert.Equal(t, int64(2), cachedParseCalls.Load())
	assert.Equal(t, 2, rowCount(t, db, ws))
}

func TestOpenGraphCachedNilDBMatchesOpenGraph(t *testing.T) {
	ws := t.TempDir()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	writeFileAt(t, filepath.Join(ws, "a.go"), "package a\nimport \"fmt\"\n", base)
	writeFileAt(t, filepath.Join(ws, "b.go"), "package b\nimport \"os\"\n", base)

	g, err := OpenGraphCached(context.Background(), ws, nil, false)
	require.NoError(t, err)
	assert.Equal(t, 2, g.Len())
	assert.Equal(t, []string{"fmt"}, g.Imports("a.go"))
	assert.Equal(t, []string{"os"}, g.Imports("b.go"))
}

func TestOpenGraphCachedProjectIsolation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// Two workspaces sharing one vault file, each with a same-named file
	// importing something different. Rows must not leak between projects.
	ws1 := t.TempDir()
	writeFileAt(t, filepath.Join(ws1, "a.go"), "package a\nimport \"fmt\"\n", base)
	ws2 := t.TempDir()
	writeFileAt(t, filepath.Join(ws2, "a.go"), "package a\nimport \"os\"\n", base)

	g1, err := OpenGraphCached(ctx, ws1, db, false)
	require.NoError(t, err)
	g2, err := OpenGraphCached(ctx, ws2, db, false)
	require.NoError(t, err)

	assert.Equal(t, []string{"fmt"}, g1.Imports("a.go"))
	assert.Equal(t, []string{"os"}, g2.Imports("a.go"))
	assert.Equal(t, 1, rowCount(t, db, ws1))
	assert.Equal(t, 1, rowCount(t, db, ws2))
}

func TestOpenGraphCachedUnparseableFileDropsRow(t *testing.T) {
	ws := t.TempDir()
	db := openTestDB(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// A valid file first, so a row exists to go stale.
	writeFileAt(t, filepath.Join(ws, "broken.go"), "package a\nimport \"fmt\"\n", base)
	_, err := OpenGraphCached(ctx, ws, db, false)
	require.NoError(t, err)
	require.Equal(t, 1, rowCount(t, db, ws))

	// Then break it mid-edit style. OpenGraph omits unparseable files from
	// the graph; the cache must both omit it and drop the row so the stale
	// import list can't come back on a later open. (ImportsOnly-mode Go
	// parsing tolerates junk after the import block, so the break has to be
	// structural: a missing package clause fails the parse outright.)
	writeFileAt(t, filepath.Join(ws, "broken.go"), "func broken() {\n", base.Add(time.Second))
	g, err := OpenGraphCached(ctx, ws, db, false)
	require.NoError(t, err)
	assert.Equal(t, 0, g.Len())
	assert.Equal(t, 0, rowCount(t, db, ws))

	// Repairing it restores the edge — a dropped row is not permanent.
	writeFileAt(t, filepath.Join(ws, "broken.go"), "package a\nimport \"strings\"\n", base.Add(2*time.Second))
	g, err = OpenGraphCached(ctx, ws, db, false)
	require.NoError(t, err)
	assert.Equal(t, 1, g.Len())
	assert.Equal(t, []string{"strings"}, g.Imports("broken.go"))
}

func TestOpenGraphCachedSkipsIgnoredDirs(t *testing.T) {
	ws := t.TempDir()
	db := openTestDB(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	writeFileAt(t, filepath.Join(ws, "vendor", "v.go"), "package v\n", base)
	writeFileAt(t, filepath.Join(ws, "node_modules", "n.go"), "package n\n", base)
	writeFileAt(t, filepath.Join(ws, "main.go"), "package main\n", base)

	g, err := OpenGraphCached(context.Background(), ws, db, false)
	require.NoError(t, err)
	assert.Equal(t, 1, g.Len(), "vendor/ and node_modules/ must stay excluded from the cached walk")
	assert.Equal(t, 1, rowCount(t, db, ws))
}
