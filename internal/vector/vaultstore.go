// vaultstore.go is the SQLite-backed semantic store (TODO Phase-3
// "incremental indexes"). The JSON Store rewrites every chunk it owns on
// each upsert and searches by brute-force cosine over one global file;
// this store keeps per-project rows in the vault (~/.a1/a1.db) with
// mtime-keyed incremental re-embedding and hybrid retrieval.
//
// The vector index stays inside SQLite on purpose (no sqlite-vec, no new
// daemon): embeddings are stored normalized so cosine similarity is a
// plain dot product, and lexical recall comes from an external-content
// FTS5 index over the same rows. The two legs are fused with reciprocal
// rank fusion — the pattern memstore already uses for memories.
package vector

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// TextEmbedder is the single-text embedding contract the vault store
// needs. *OllamaEmbedder satisfies it; tests substitute deterministic
// fakes so nothing here touches the network.
type TextEmbedder interface {
	EmbedText(ctx context.Context, text string) ([]float32, error)
}

// vecSchema is created lazily on the vault handle (the memstore Bet-08
// precedent: subsystems own their tables inside the shared db file).
// vec_chunks holds one row per chunk with a normalized embedding BLOB;
// vec_chunks_fts mirrors the text column for keyword recall; vec_files
// holds the per-file (mtime, size) staleness key so re-indexing skips
// unchanged files entirely.
const vecSchema = `
CREATE TABLE IF NOT EXISTS vec_chunks (
  id          INTEGER PRIMARY KEY,
  project     TEXT NOT NULL,
  path        TEXT NOT NULL,
  chunk_seq   INTEGER NOT NULL,
  start_line  INTEGER NOT NULL,
  end_line    INTEGER NOT NULL,
  text        TEXT NOT NULL,
  embedding   BLOB NOT NULL,
  embed_model TEXT NOT NULL,
  dim         INTEGER NOT NULL,
  mtime       INTEGER NOT NULL,
  size        INTEGER NOT NULL,
  UNIQUE(project, path, chunk_seq)
);
CREATE INDEX IF NOT EXISTS idx_vec_chunks_project ON vec_chunks(project, embed_model);

CREATE VIRTUAL TABLE IF NOT EXISTS vec_chunks_fts USING fts5(
  text, content='vec_chunks', content_rowid='id'
);
CREATE TRIGGER IF NOT EXISTS vec_chunks_ai AFTER INSERT ON vec_chunks BEGIN
  INSERT INTO vec_chunks_fts(rowid, text) VALUES (new.id, new.text);
END;
CREATE TRIGGER IF NOT EXISTS vec_chunks_ad AFTER DELETE ON vec_chunks BEGIN
  INSERT INTO vec_chunks_fts(vec_chunks_fts, rowid, text)
  VALUES ('delete', old.id, old.text);
END;
CREATE TRIGGER IF NOT EXISTS vec_chunks_au AFTER UPDATE ON vec_chunks BEGIN
  INSERT INTO vec_chunks_fts(vec_chunks_fts, rowid, text)
  VALUES ('delete', old.id, old.text);
  INSERT INTO vec_chunks_fts(rowid, text) VALUES (new.id, new.text);
END;

CREATE TABLE IF NOT EXISTS vec_files (
  project     TEXT NOT NULL,
  path        TEXT NOT NULL,
  mtime       INTEGER NOT NULL,
  size        INTEGER NOT NULL,
  embed_model TEXT NOT NULL,
  dim         INTEGER NOT NULL,
  PRIMARY KEY(project, path)
);
`

// embedConcurrency caps parallel EmbedText calls, matching the JSON
// indexer's batch of 16 — enough to keep Ollama busy without burying it.
const embedConcurrency = 16

// VaultStoreConfig parameterizes OpenVaultStore.
type VaultStoreConfig struct {
	DB         *sql.DB
	Project    string // absolute workspace root; scopes every row
	Root       string // directory walked on reindex (usually == Project)
	WindowSize int
	Stride     int
	Embedder   TextEmbedder
	// Model is the embedding model name. Rows stamped with a different
	// model are treated as stale so switching models re-embeds the
	// workspace instead of serving unusable vectors.
	Model string
}

// VaultStore is a project-scoped chunk store inside a SQLite handle. It
// does not own the handle (the vault does); there is nothing to Close.
type VaultStore struct {
	db       *sql.DB
	project  string
	root     string
	window   int
	stride   int
	embedder TextEmbedder
	model    string
}

// OpenVaultStore validates the config and ensures the schema exists.
func OpenVaultStore(cfg VaultStoreConfig) (*VaultStore, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("vector: vault store requires a db handle")
	}
	if strings.TrimSpace(cfg.Project) == "" {
		return nil, fmt.Errorf("vector: vault store requires a project key")
	}
	if cfg.Embedder == nil {
		return nil, fmt.Errorf("vector: vault store requires an embedder")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("vector: vault store requires an embedding model name")
	}
	root := strings.TrimSpace(cfg.Root)
	if root == "" {
		root = cfg.Project
	}
	if !filepath.IsAbs(root) {
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("vector: resolve root: %w", err)
		}
		root = abs
	}
	window := cfg.WindowSize
	if window <= 0 {
		window = 50
	}
	stride := cfg.Stride
	if stride <= 0 {
		stride = window
	}
	if _, err := cfg.DB.Exec(vecSchema); err != nil {
		return nil, fmt.Errorf("vector: vault store schema: %w", err)
	}
	return &VaultStore{
		db:       cfg.DB,
		project:  filepath.Clean(cfg.Project),
		root:     filepath.Clean(root),
		window:   window,
		stride:   stride,
		embedder: cfg.Embedder,
		model:    cfg.Model,
	}, nil
}

// Reindex brings the project's chunks up to date with the workspace. The
// walk is stat-only: files whose (mtime, size, embed_model) match their
// vec_files row are skipped, changed or new files are re-chunked and
// re-embedded (their old chunks replaced in one tx), and vanished files
// have their rows removed. force wipes the project first — the tool-level
// `reindex` escape hatch for when the index is somehow wrong.
func (s *VaultStore) Reindex(ctx context.Context, force bool) (int, error) {
	if s == nil {
		return 0, ErrNotConfigured
	}
	if _, err := s.db.ExecContext(ctx, vecSchema); err != nil {
		return 0, fmt.Errorf("vector: vault store schema: %w", err)
	}
	if force {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return 0, fmt.Errorf("vector: wipe: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM vec_chunks WHERE project = ?`, s.project); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("vector: wipe chunks: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM vec_files WHERE project = ?`, s.project); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("vector: wipe files: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("vector: wipe: %w", err)
		}
	}

	// Staleness snapshot: one row per already-embedded file.
	type fileRow struct {
		mtime int64
		size  int64
		model string
		dim   int
	}
	files := make(map[string]fileRow)
	rows, err := s.db.QueryContext(ctx,
		`SELECT path, mtime, size, embed_model, dim FROM vec_files WHERE project = ?`, s.project)
	if err != nil {
		return 0, fmt.Errorf("vector: load file rows: %w", err)
	}
	for rows.Next() {
		var rel string
		var fr fileRow
		if err := rows.Scan(&rel, &fr.mtime, &fr.size, &fr.model, &fr.dim); err != nil {
			rows.Close()
			return 0, fmt.Errorf("vector: load file rows: %w", err)
		}
		files[rel] = fr
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("vector: load file rows: %w", err)
	}
	rows.Close()

	var (
		seen  = make(map[string]struct{})
		stale []string // absolute paths needing a re-embed
	)
	if err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipIndexDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if !isTextFile(path) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel := strings.TrimPrefix(path, s.root)
		rel = strings.TrimPrefix(rel, string(filepath.Separator))
		if rel == "" {
			return nil
		}
		seen[rel] = struct{}{}
		if fr, ok := files[rel]; ok &&
			fr.mtime == info.ModTime().UnixNano() && fr.size == info.Size() && fr.model == s.model {
			return nil
		}
		stale = append(stale, path)
		return nil
	}); err != nil {
		return 0, fmt.Errorf("vector: walk %s: %w", s.root, err)
	}

	// Vanished files: drop chunks and file rows in one tx.
	var vanished []string
	for rel := range files {
		if _, ok := seen[rel]; !ok {
			vanished = append(vanished, rel)
		}
	}
	if len(vanished) > 0 {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return 0, fmt.Errorf("vector: drop vanished: %w", err)
		}
		for _, rel := range vanished {
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM vec_chunks WHERE project = ? AND path = ?`, s.project, rel); err != nil {
				_ = tx.Rollback()
				return 0, fmt.Errorf("vector: drop vanished chunks %s: %w", rel, err)
			}
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM vec_files WHERE project = ? AND path = ?`, s.project, rel); err != nil {
				_ = tx.Rollback()
				return 0, fmt.Errorf("vector: drop vanished file %s: %w", rel, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("vector: drop vanished: %w", err)
		}
	}

	var total int
	for _, abs := range stale {
		n, err := s.reembedFile(ctx, abs)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// reembedFile replaces one file's chunks: chunk, embed with bounded
// concurrency, then swap the rows in a single transaction so a crash
// mid-reindex leaves either the old or the new chunk set, never neither.
func (s *VaultStore) reembedFile(ctx context.Context, abs string) (int, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return 0, fmt.Errorf("vector: stat %s: %w", abs, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return 0, fmt.Errorf("vector: read %s: %w", abs, err)
	}
	rel := strings.TrimPrefix(abs, s.root)
	rel = strings.TrimPrefix(rel, string(filepath.Separator))

	chunks := ChunkText(string(data), 1, s.window, s.stride)
	for i := range chunks {
		chunks[i].Path = rel
	}

	var (
		wg    sync.WaitGroup
		sem   = make(chan struct{}, embedConcurrency)
		errCh = make(chan error, len(chunks))
		mu    sync.Mutex
		dim   int
	)
	for i := range chunks {
		wg.Add(1)
		sem <- struct{}{}
		go func(chunk *Chunk) {
			defer wg.Done()
			defer func() { <-sem }()
			emb, err := s.embedder.EmbedText(ctx, chunk.Text)
			if err != nil {
				errCh <- fmt.Errorf("vector: embed %s:%d-%d: %w",
					rel, chunk.StartLine, chunk.EndLine, err)
				return
			}
			mu.Lock()
			chunk.Embedding = normalizeVec(emb)
			if dim == 0 {
				dim = len(emb)
			}
			mu.Unlock()
		}(&chunks[i])
	}
	wg.Wait()
	close(errCh)
	if firstErr := <-errCh; firstErr != nil {
		return 0, firstErr
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("vector: reembed %s: %w", rel, err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM vec_chunks WHERE project = ? AND path = ?`, s.project, rel); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("vector: reembed %s: %w", rel, err)
	}
	insertChunk := `
                INSERT INTO vec_chunks
                  (project, path, chunk_seq, start_line, end_line,
                   text, embedding, embed_model, dim, mtime, size)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for i, c := range chunks {
		if _, err := tx.ExecContext(ctx, insertChunk,
			s.project, rel, i+1, c.StartLine, c.EndLine, c.Text,
			encodeVec(c.Embedding), s.model, len(c.Embedding),
			info.ModTime().UnixNano(), info.Size()); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("vector: reembed %s: %w", rel, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
                INSERT INTO vec_files (project, path, mtime, size, embed_model, dim) VALUES (?, ?, ?, ?, ?, ?)
                ON CONFLICT(project, path) DO UPDATE SET
                  mtime=excluded.mtime, size=excluded.size, embed_model=excluded.embed_model, dim=excluded.dim`,
		s.project, rel, info.ModTime().UnixNano(), info.Size(), s.model, dim); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("vector: reembed %s: %w", rel, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("vector: reembed %s: %w", rel, err)
	}
	return len(chunks), nil
}

// Len reports the chunk count for this project under the current model.
// The tool-level reindex trigger uses it: "empty for this project"
// replaces the old global-file-empty check, so two workspaces sharing a
// vault no longer reindex each other on every other call.
func (s *VaultStore) Len(ctx context.Context) (int, error) {
	if s == nil {
		return 0, ErrNotConfigured
	}
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM vec_chunks WHERE project = ? AND embed_model = ?`,
		s.project, s.model).Scan(&n); err != nil {
		return 0, fmt.Errorf("vector: count chunks: %w", err)
	}
	return n, nil
}

// Search returns the top-k chunks for the query using hybrid retrieval:
// a dot-product leg over normalized embeddings plus an FTS5 keyword leg,
// fused with reciprocal rank fusion. Chunks from other models are
// excluded from the vector leg (their dot product would be noise, not
// similarity); the keyword leg is model-independent.
func (s *VaultStore) Search(ctx context.Context, query string, k int) ([]Chunk, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	if k <= 0 {
		k = 10
	}
	emb, err := s.embedder.EmbedText(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("vector: embed query: %w", err)
	}
	if len(emb) == 0 {
		return nil, fmt.Errorf("vector: empty query embedding")
	}
	q := normalizeVec(emb)

	vecIDs, err := s.vecLeg(ctx, q, k)
	if err != nil {
		return nil, err
	}
	ftsIDs, err := s.ftsLeg(ctx, query, k)
	if err != nil {
		return nil, err
	}
	ids := rrfFuse(k, vecIDs, ftsIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	return s.loadChunks(ctx, ids)
}

// vecLeg is the dense leg: decode every chunk embedding for this project
// and model, score by dot product (vectors are normalized at write, so
// this is cosine similarity), keep the top-k.
func (s *VaultStore) vecLeg(ctx context.Context, q []float32, k int) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, embedding, dim FROM vec_chunks WHERE project = ? AND embed_model = ?`, s.project, s.model)
	if err != nil {
		return nil, fmt.Errorf("vector: vec leg: %w", err)
	}
	defer rows.Close()
	type hit struct {
		id int64
		d  float64
	}
	var hits []hit
	for rows.Next() {
		var id, dim int64
		var blob []byte
		if err := rows.Scan(&id, &blob, &dim); err != nil {
			return nil, fmt.Errorf("vector: vec leg: %w", err)
		}
		if int(dim) != len(q) {
			// Defensive: a dim change under the same model name (model
			// re-pulled with different dimensions) yields garbage dots.
			continue
		}
		hits = append(hits, hit{id: id, d: dot(q, decodeVec(blob))})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vector: vec leg: %w", err)
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].d != hits[j].d {
			return hits[i].d > hits[j].d
		}
		return hits[i].id < hits[j].id
	})
	if len(hits) > k {
		hits = hits[:k]
	}
	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.id
	}
	return ids, nil
}

// ftsTok extracts the token alphabet FTS5 queries are built from.
var ftsTok = regexp.MustCompile(`[A-Za-z0-9_]{2,}`)

// ftsQueryString sanitizes a natural-language query into an FTS5 MATCH
// expression: lowercase [A-Za-z0-9_]{2,} tokens, de-duplicated, quoted,
// OR-joined. Punctuation and operators in the user's query can never
// reach the parser, so no query can crash the leg. (Copied from memstore
// rather than imported — the packages stay independent on purpose.)
func ftsQueryString(q string) string {
	seen := map[string]bool{}
	var parts []string
	for _, t := range ftsTok.FindAllString(strings.ToLower(q), -1) {
		if !seen[t] {
			seen[t] = true
			parts = append(parts, `"`+t+`"`)
		}
	}
	return strings.Join(parts, " OR ")
}

// ftsLeg is the sparse leg. The join back to vec_chunks scopes the
// global FTS rowid space to this project — without it, another
// workspace's chunks could crowd this one out of the top-k.
func (s *VaultStore) ftsLeg(ctx context.Context, query string, k int) ([]int64, error) {
	qs := ftsQueryString(query)
	if qs == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
                SELECT c.id FROM vec_chunks AS c
                JOIN vec_chunks_fts AS f ON f.rowid = c.id
                WHERE vec_chunks_fts MATCH ? AND c.project = ?
                ORDER BY rank LIMIT ?`, qs, s.project, k)
	if err != nil {
		return nil, fmt.Errorf("vector: fts leg: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("vector: fts leg: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// rrfFuse combines ranked id lists with reciprocal rank fusion,
// 1/(60+rank+1) per leg (memstore's constant; the absolute value matters
// less than both legs sharing it). Ties break by id for determinism.
func rrfFuse(m int, legs ...[]int64) []int64 {
	score := map[int64]float64{}
	for _, leg := range legs {
		for r, id := range leg {
			score[id] += 1.0 / float64(60+r+1)
		}
	}
	ids := make([]int64, 0, len(score))
	for id := range score {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if score[ids[i]] != score[ids[j]] {
			return score[ids[i]] > score[ids[j]]
		}
		return ids[i] < ids[j]
	})
	if len(ids) > m {
		ids = ids[:m]
	}
	return ids
}

// loadChunks fetches the fused ids' content in fused order.
func (s *VaultStore) loadChunks(ctx context.Context, ids []int64) ([]Chunk, error) {
	ph := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, s.project)
	for i, id := range ids {
		ph[i] = "?"
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `
                SELECT id, path, start_line, end_line, text FROM vec_chunks
                WHERE project = ? AND id IN (`+strings.Join(ph, ",")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("vector: load chunks: %w", err)
	}
	defer rows.Close()
	byID := make(map[int64]Chunk, len(ids))
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.Path, &c.StartLine, &c.EndLine, &c.Text); err != nil {
			return nil, fmt.Errorf("vector: load chunks: %w", err)
		}
		byID[int64(c.ID)] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vector: load chunks: %w", err)
	}
	out := make([]Chunk, 0, len(ids))
	for _, id := range ids {
		if c, ok := byID[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// skipIndexDir reports whether a directory is excluded from chunk
// indexing — the same list the graph walk skips. The JSON ChunkFiles walk
// never skipped these, so .git and node_modules landed in the index as
// opaque chunks; both walks now share this list.
func skipIndexDir(name string) bool {
	return name == "vendor" || name == ".git" || name == "node_modules" || name == "target" ||
		name == "dist" || name == "build"
}

// normalizeVec scales v to unit length in place so stored dot products
// are cosine similarities.
func normalizeVec(v []float32) []float32 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	if s == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(s))
	for i := range v {
		v[i] *= inv
	}
	return v
}

// encodeVec packs v as little-endian float32s (the memstore BLOB format).
func encodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

// decodeVec unpacks a little-endian float32 BLOB.
func decodeVec(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

// dot returns the inner product, truncated to the shorter operand.
func dot(a, b []float32) float64 {
	var s float64
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}
