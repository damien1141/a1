// cache.go is the vault-backed graph cache (TODO Phase-3 "incremental
// indexes"): OpenGraph re-parses every source file on every call, which
// made each `search`/`graph`/`testimpact`/`batch` call an O(workspace)
// parse walk. The cache keeps one row per parseable file in the vault
// (graph_files), keyed by the absolute workspace root, invalidated by
// (mtime, size) — the same elision key readcache.go uses for reads.
//
// The parse result (not the file content) is what gets cached: the graph
// only ever consumes the import list, so rows stay tiny and the walk stays
// stat-only for unchanged files.
package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// graphFilesSchema is created lazily on the vault handle, mirroring the
// memstore precedent (Bet 08): subsystems own their tables inside ~/.a1/a1.db.
// imports is a JSON array of raw import strings — the exact values that go
// into g.edges, so reverse lookups keyed by raw imports keep working.
const graphFilesSchema = `
CREATE TABLE IF NOT EXISTS graph_files (
  project TEXT NOT NULL,
  path    TEXT NOT NULL,
  mtime   INTEGER NOT NULL,
  size    INTEGER NOT NULL,
  imports TEXT NOT NULL,
  PRIMARY KEY(project, path)
);
CREATE INDEX IF NOT EXISTS idx_graph_files_project ON graph_files(project);
`

// cachedParseCalls counts parseImports invocations made by OpenGraphCached.
// It exists so tests can prove the second open parses nothing — behavior
// alone can't distinguish "cache hit" from "re-parse produced equal output".
var cachedParseCalls atomic.Int64

// skipDirName reports whether a directory is excluded from indexing: the
// same list OpenGraph has always skipped, factored out so the cache walk
// and both OpenGraph variants cannot drift apart.
func skipDirName(name string) bool {
	return name == "vendor" || name == ".git" || name == "node_modules" || name == "target" ||
		name == "dist" || name == "build"
}

// OpenGraphCached opens the dependency graph for root. When db is non-nil
// the parse results are cached in graph_files keyed by (project, path) and
// invalidated by (mtime, size); only new or changed files are parsed, rows
// for vanished files are dropped, and all row updates land in one
// transaction. A nil db degrades to exactly OpenGraph's behavior (tests,
// or a surface that booted without the vault). rescan forces a full
// rebuild: the project's rows are deleted before the walk so every
// parseable file is re-parsed and re-stored.
func OpenGraphCached(ctx context.Context, root string, db *sql.DB, rescan bool) (*Graph, error) {
	if db == nil {
		return OpenGraph(ctx, root)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// The project key must be stable across processes and surfaces, so a
	// relative root (e.g. "." from testimpact) is resolved once here.
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("graph cache: resolve root: %w", err)
	}
	if _, err := db.ExecContext(ctx, graphFilesSchema); err != nil {
		return nil, fmt.Errorf("graph cache schema: %w", err)
	}
	if rescan {
		if _, err := db.ExecContext(ctx, `DELETE FROM graph_files WHERE project = ?`, absRoot); err != nil {
			return nil, fmt.Errorf("graph cache rescan: %w", err)
		}
	}

	// Snapshot the cached rows up front; the walk then decides per file
	// whether the row is still fresh.
	type fileRow struct {
		mtime   int64
		size    int64
		imports []string
	}
	cached := make(map[string]fileRow)
	rows, err := db.QueryContext(ctx, `SELECT path, mtime, size, imports FROM graph_files WHERE project = ?`, absRoot)
	if err != nil {
		return nil, fmt.Errorf("graph cache load: %w", err)
	}
	for rows.Next() {
		var rel, impJSON string
		var fr fileRow
		if err := rows.Scan(&rel, &fr.mtime, &fr.size, &impJSON); err != nil {
			rows.Close()
			return nil, fmt.Errorf("graph cache load: %w", err)
		}
		if err := json.Unmarshal([]byte(impJSON), &fr.imports); err != nil {
			// A row we can't decode is a stale row; dropping it forces a
			// re-parse rather than poisoning the graph with nothing.
			cached[rel] = fileRow{mtime: -1}
			continue
		}
		cached[rel] = fr
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("graph cache load: %w", err)
	}
	rows.Close()

	// delta is one row update from the walk. drop marks a file that became
	// unparseable (mid-edit syntax error): its row is deleted so the stale
	// import list can't resurrect it on later opens.
	type delta struct {
		rel     string
		mtime   int64
		size    int64
		imports []string
		drop    bool
	}
	var (
		deltas []delta
		seen   = make(map[string]struct{})
	)
	if err := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirName(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		rel := strings.TrimPrefix(path, absRoot)
		rel = strings.TrimPrefix(rel, string(filepath.Separator))
		if rel == "" || !parseablePath(rel) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		seen[rel] = struct{}{}
		if row, ok := cached[rel]; ok && row.mtime == info.ModTime().UnixNano() && row.size == info.Size() {
			return nil // cache hit: row already holds the imports
		}
		cachedParseCalls.Add(1)
		imports, ok := parseImports(path, rel)
		if !ok {
			deltas = append(deltas, delta{rel: rel, drop: true})
			return nil
		}
		deltas = append(deltas, delta{
			rel: rel, mtime: info.ModTime().UnixNano(), size: info.Size(), imports: imports,
		})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("graph cache walk: %w", err)
	}

	// Rows for files that vanished since the last open (deleted, moved out
	// of the workspace, or now inside a skipped directory).
	var vanished []string
	for rel := range cached {
		if _, ok := seen[rel]; !ok {
			vanished = append(vanished, rel)
		}
	}

	if len(deltas) > 0 || len(vanished) > 0 {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("graph cache tx: %w", err)
		}
		upsert, err := tx.PrepareContext(ctx, `
                INSERT INTO graph_files (project, path, mtime, size, imports) VALUES (?, ?, ?, ?, ?)
                ON CONFLICT(project, path) DO UPDATE SET
                  mtime=excluded.mtime, size=excluded.size, imports=excluded.imports`)
		if err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("graph cache tx: %w", err)
		}
		for _, d := range deltas {
			if d.drop {
				if _, err := tx.ExecContext(ctx,
					`DELETE FROM graph_files WHERE project = ? AND path = ?`, absRoot, d.rel); err != nil {
					upsert.Close()
					_ = tx.Rollback()
					return nil, fmt.Errorf("graph cache delete %s: %w", d.rel, err)
				}
				continue
			}
			// nil imports must encode as [], not null, so decode yields an
			// empty (present) edge list like OpenGraph's edges[rel] = nil.
			if d.imports == nil {
				d.imports = []string{}
			}
			impJSON, err := json.Marshal(d.imports)
			if err != nil {
				upsert.Close()
				_ = tx.Rollback()
				return nil, fmt.Errorf("graph cache encode %s: %w", d.rel, err)
			}
			if _, err := upsert.ExecContext(ctx, absRoot, d.rel, d.mtime, d.size, string(impJSON)); err != nil {
				upsert.Close()
				_ = tx.Rollback()
				return nil, fmt.Errorf("graph cache upsert %s: %w", d.rel, err)
			}
		}
		upsert.Close()
		for _, rel := range vanished {
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM graph_files WHERE project = ? AND path = ?`, absRoot, rel); err != nil {
				_ = tx.Rollback()
				return nil, fmt.Errorf("graph cache delete %s: %w", rel, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("graph cache commit: %w", err)
		}
	}

	// Build the in-memory graph from the post-tx rows. Re-selecting (rather
	// than merging maps in memory) guarantees the graph reflects exactly
	// what the next process will load, fresh parses included. path order
	// keeps edge lists as deterministic as the old WalkDir order.
	g := &Graph{
		edges:   make(map[string][]string),
		reverse: make(map[string][]string),
		root:    absRoot,
	}
	rows, err = db.QueryContext(ctx, `SELECT path, imports FROM graph_files WHERE project = ? ORDER BY path`, absRoot)
	if err != nil {
		return nil, fmt.Errorf("graph cache readback: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rel, impJSON string
		if err := rows.Scan(&rel, &impJSON); err != nil {
			return nil, fmt.Errorf("graph cache readback: %w", err)
		}
		var imports []string
		if err := json.Unmarshal([]byte(impJSON), &imports); err != nil {
			return nil, fmt.Errorf("graph cache readback %s: %w", rel, err)
		}
		g.edges[rel] = imports
		for _, dst := range imports {
			g.reverse[dst] = append(g.reverse[dst], rel)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("graph cache readback: %w", err)
	}
	return g, nil
}
