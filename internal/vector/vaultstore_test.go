package vector

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite" // driver name "sqlite"; pure Go, FTS5 included
)

// fakeEmbedder produces deterministic token-hash vectors (the memstore
// FakeEmbedder idea): texts that share tokens land near each other, so
// tests can exercise the dense leg without a network. It counts calls so
// tests can prove the incremental walk skips unchanged files.
type fakeEmbedder struct {
	dim int

	mu    sync.Mutex
	calls int
}

func (f *fakeEmbedder) EmbedText(_ context.Context, text string) ([]float32, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	v := make([]float32, f.dim)
	for _, tok := range strings.Fields(strings.ToLower(text)) {
		h := uint32(2166136261)
		for _, c := range tok {
			h = (h ^ uint32(c)) * 16777619
		}
		v[h%uint32(f.dim)] += 1
		v[(h>>8)%uint32(f.dim)] += 0.5
	}
	return v, nil
}

func (f *fakeEmbedder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func openVecTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite",
		"file:"+filepath.Join(t.TempDir(), "vec.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func writeVecFileAt(t *testing.T, path, content string, modTime time.Time) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	require.NoError(t, os.Chtimes(path, modTime, modTime))
}

func openTestStore(t *testing.T, db *sql.DB, project string, emb *fakeEmbedder) *VaultStore {
	t.Helper()
	s, err := OpenVaultStore(VaultStoreConfig{
		DB: db, Project: project, Root: project,
		WindowSize: 50, Stride: 25,
		Embedder: emb, Model: "fake-v1",
	})
	require.NoError(t, err)
	return s
}

func chunkCount(t *testing.T, db *sql.DB, project string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM vec_chunks WHERE project = ?`, project).Scan(&n))
	return n
}

func TestVaultStoreIncrementalSync(t *testing.T) {
	ws := t.TempDir()
	db := openVecTestDB(t)
	emb := &fakeEmbedder{dim: 32}
	s := openTestStore(t, db, ws, emb)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	writeVecFileAt(t, filepath.Join(ws, "alpha.md"), "alpha beta gamma\n", base)
	writeVecFileAt(t, filepath.Join(ws, "beta.go"), "package beta\nimport \"os\"\n", base)

	n, err := s.Reindex(ctx, false)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, 2, chunkCount(t, db, ws))
	firstCalls := emb.callCount()
	require.Equal(t, 2, firstCalls)

	// Unchanged workspace: the walk is stat-only and nothing re-embeds.
	n, err = s.Reindex(ctx, false)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "unchanged files must not be re-embedded")
	assert.Equal(t, firstCalls, emb.callCount())

	// Change one file (content + pinned mtime): only it re-embeds, and
	// its old chunks are replaced rather than accumulated.
	writeVecFileAt(t, filepath.Join(ws, "alpha.md"), "delta epsilon zeta\n", base.Add(time.Second))
	n, err = s.Reindex(ctx, false)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, firstCalls+1, emb.callCount())
	assert.Equal(t, 2, chunkCount(t, db, ws))

	// The new content is what ranks first now (both chunks still come
	// back within top-k — like the JSON store, there is no minimum
	// score floor).
	hits, err := s.Search(ctx, "delta epsilon", 5)
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	assert.Equal(t, "alpha.md", hits[0].Path)
	assert.Equal(t, "delta epsilon zeta", hits[0].Text)

	// Delete a file: its chunks and file row go away.
	require.NoError(t, os.Remove(filepath.Join(ws, "beta.go")))
	n, err = s.Reindex(ctx, false)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, 1, chunkCount(t, db, ws))
	var fileRows int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM vec_files WHERE project = ?`, ws).Scan(&fileRows))
	assert.Equal(t, 1, fileRows)
}

func TestVaultStoreForceWipesProject(t *testing.T) {
	ws := t.TempDir()
	db := openVecTestDB(t)
	emb := &fakeEmbedder{dim: 32}
	s := openTestStore(t, db, ws, emb)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	writeVecFileAt(t, filepath.Join(ws, "a.md"), "one two\n", base)
	writeVecFileAt(t, filepath.Join(ws, "b.md"), "three four\n", base)
	_, err := s.Reindex(ctx, false)
	require.NoError(t, err)
	calls := emb.callCount()

	// force re-embeds everything even though nothing changed on disk.
	n, err := s.Reindex(ctx, true)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, calls+2, emb.callCount())
	assert.Equal(t, 2, chunkCount(t, db, ws))
}

func TestVaultStoreProjectIsolation(t *testing.T) {
	db := openVecTestDB(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	ws1 := t.TempDir()
	writeVecFileAt(t, filepath.Join(ws1, "notes.md"), "quantum flux capacitor\n", base)
	ws2 := t.TempDir()
	writeVecFileAt(t, filepath.Join(ws2, "notes.md"), "medieval castle siege\n", base)

	s1 := openTestStore(t, db, ws1, &fakeEmbedder{dim: 32})
	s2 := openTestStore(t, db, ws2, &fakeEmbedder{dim: 32})
	_, err := s1.Reindex(ctx, false)
	require.NoError(t, err)
	_, err = s2.Reindex(ctx, false)
	require.NoError(t, err)

	// Same relative path in both projects: search must stay scoped.
	hits, err := s1.Search(ctx, "flux capacitor", 5)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, "notes.md", hits[0].Path)
	assert.Equal(t, "quantum flux capacitor", hits[0].Text, "project 2's chunk must not leak in")

	hits, err = s2.Search(ctx, "castle siege", 5)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, "medieval castle siege", hits[0].Text)

	// Len is per project + model.
	n, err := s1.Len(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestVaultStoreModelChangeReembeds(t *testing.T) {
	ws := t.TempDir()
	db := openVecTestDB(t)
	emb := &fakeEmbedder{dim: 32}
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	writeVecFileAt(t, filepath.Join(ws, "a.md"), "one two three\n", base)
	writeVecFileAt(t, filepath.Join(ws, "b.md"), "four five six\n", base)

	s1 := openTestStore(t, db, ws, emb)
	ctx := context.Background()
	_, err := s1.Reindex(ctx, false)
	require.NoError(t, err)
	calls := emb.callCount()

	// A different model under the same project: no usable chunks, and a
	// reindex must re-embed despite unchanged (mtime, size).
	s2, err := OpenVaultStore(VaultStoreConfig{
		DB: db, Project: ws, Root: ws, WindowSize: 50, Stride: 25,
		Embedder: emb, Model: "fake-v2",
	})
	require.NoError(t, err)
	n, err := s2.Len(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "chunks under the old model must not count for the new one")
	n, err = s2.Reindex(ctx, false)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, calls+2, emb.callCount(), "model change must invalidate the staleness key")

	// Old-model chunks were replaced, not accumulated.
	assert.Equal(t, 2, chunkCount(t, db, ws))
	hits, err := s2.Search(ctx, "one two", 5)
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	assert.Equal(t, "a.md", hits[0].Path)
}

func TestVaultStoreFTSSanitisation(t *testing.T) {
	ws := t.TempDir()
	db := openVecTestDB(t)
	emb := &fakeEmbedder{dim: 32}
	s := openTestStore(t, db, ws, emb)
	ctx := context.Background()
	writeVecFileAt(t, filepath.Join(ws, "err.md"),
		"error handling middleware retries\n", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	_, err := s.Reindex(ctx, false)
	require.NoError(t, err)

	// Punctuation, quotes, and FTS operators in the query must never
	// reach the MATCH parser — only sanitized quoted tokens do.
	hits, err := s.Search(ctx, `error!! (handling) "middleware"; DROP --`, 5)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, "err.md", hits[0].Path)

	// No [A-Za-z0-9_]{2,} tokens at all: the FTS leg bows out quietly
	// and the dense leg still answers.
	hits, err = s.Search(ctx, "!! ., ;", 5)
	require.NoError(t, err)
	assert.Len(t, hits, 1)

	// The sanitizer itself: dedupe, quote, OR-join.
	assert.Equal(t, `"go" OR "fmt"`, ftsQueryString("go (fmt! go"))
}

func TestVaultStoreHybridFusion(t *testing.T) {
	ws := t.TempDir()
	db := openVecTestDB(t)
	emb := &fakeEmbedder{dim: 64}
	s := openTestStore(t, db, ws, emb)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// target.md carries both a rare keyword and semantically loaded
	// tokens; noise.md is long enough to produce several chunks of fluff
	// so neither leg has an empty field to win on.
	writeVecFileAt(t, filepath.Join(ws, "target.md"),
		"zyzomys retries with exponential backoff\n", base)
	writeVecFileAt(t, filepath.Join(ws, "noise.md"),
		strings.Repeat("lorem ipsum dolor sit amet\n", 12), base)
	_, err := s.Reindex(ctx, false)
	require.NoError(t, err)

	// Keyword-shaped query: the rare token only exists in target.md.
	hits, err := s.Search(ctx, "zyzomys", 5)
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	assert.Equal(t, "target.md", hits[0].Path)

	// Semantically shaped query: shared tokens with target.md.
	hits, err = s.Search(ctx, "retries exponential backoff", 5)
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	assert.Equal(t, "target.md", hits[0].Path)

	// Result shape: fused chunks carry path + line range + text for the
	// tool's @file renderer.
	for _, h := range hits {
		assert.NotEmpty(t, h.Path)
		assert.True(t, h.StartLine >= 1)
		assert.True(t, h.EndLine >= h.StartLine)
		assert.NotEmpty(t, h.Text)
	}
}

func TestVaultStoreSkipsIgnoredDirs(t *testing.T) {
	ws := t.TempDir()
	db := openVecTestDB(t)
	emb := &fakeEmbedder{dim: 32}
	s := openTestStore(t, db, ws, emb)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	writeVecFileAt(t, filepath.Join(ws, ".git", "config"), "[core]\nrepositoryformatversion = 0\n", base)
	writeVecFileAt(t, filepath.Join(ws, "node_modules", "dep.js"), "module.exports = {}\n", base)
	writeVecFileAt(t, filepath.Join(ws, "vendor", "v.go"), "package v\n", base)
	writeVecFileAt(t, filepath.Join(ws, "main.go"), "package main\nfunc main() {}\n", base)

	n, err := s.Reindex(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "only main.go may be chunked; .git/node_modules/vendor stay out")
	assert.Equal(t, 1, chunkCount(t, db, ws))
}

func TestVaultStoreNilAndConfigValidation(t *testing.T) {
	ctx := context.Background()

	var nilStore *VaultStore
	_, err := nilStore.Reindex(ctx, false)
	assert.ErrorIs(t, err, ErrNotConfigured)
	_, err = nilStore.Search(ctx, "q", 5)
	assert.ErrorIs(t, err, ErrNotConfigured)
	_, err = nilStore.Len(ctx)
	assert.ErrorIs(t, err, ErrNotConfigured)

	_, err = OpenVaultStore(VaultStoreConfig{}) // no db, no embedder, no model
	assert.Error(t, err)

	db := openVecTestDB(t)
	_, err = OpenVaultStore(VaultStoreConfig{DB: db, Project: "/ws", Embedder: &fakeEmbedder{dim: 8}})
	assert.Error(t, err, "missing model name must be rejected")
}

func TestRRFFusionPrefersDualLegHits(t *testing.T) {
	// A chunk ranked modestly in both legs must outrank one ranked first
	// in a single leg — that is the entire point of RRF.
	both := []int64{7, 9} // ranks 0,1
	one := []int64{3, 7}  // ranks 0,1
	got := rrfFuse(3, both, one)
	require.Len(t, got, 3)
	assert.Equal(t, int64(7), got[0], "id 7 (rank 1 + rank 1) beats id 3 (rank 0 only)")
	assert.Equal(t, int64(3), got[1])
}
