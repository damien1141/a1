package vector

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChunkText(t *testing.T) {
	text := "line1\nline2\nline3\nline4\nline5"
	chunks := ChunkText(text, 1, 3, 2)
	require.Len(t, chunks, 2)
	assert.Equal(t, 1, chunks[0].StartLine)
	assert.Equal(t, 4, chunks[0].EndLine)
	assert.Equal(t, "line1\nline2\nline3", chunks[0].Text)
	assert.Equal(t, 3, chunks[1].StartLine)
	assert.Equal(t, 6, chunks[1].EndLine)
	assert.Equal(t, "line3\nline4\nline5", chunks[1].Text)
}

func TestCosineSimilarity(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	assert.InDelta(t, 0.0, cosineSimilarity(a, b), 1e-6)

	a = []float32{1, 1}
	b = []float32{1, 1}
	assert.InDelta(t, 1.0, cosineSimilarity(a, b), 1e-6)
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vec.json")

	store, err := OpenStore(path)
	require.NoError(t, err)

	err = store.UpsertChunks(context.Background(), []Chunk{
		{Path: "a.go", StartLine: 1, EndLine: 10, Text: "foo", Embedding: []float32{1, 0}},
		{Path: "b.go", StartLine: 5, EndLine: 15, Text: "bar", Embedding: []float32{0, 1}},
	})
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = OpenStore(path)
	require.NoError(t, err)
	defer store.Close()

	assert.Equal(t, 2, store.Len())
	results, err := store.Search(context.Background(), []float32{1, 0}, 2)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "a.go", results[0].Path)
}

func TestChunkFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("a\nb\nc\nd\ne\n"), 0o644))

	results, err := ChunkFiles(context.Background(), dir, 2, 2)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Len(t, results[0].Chunks, 3)
	assert.Equal(t, "main.go", results[0].Path)
}
