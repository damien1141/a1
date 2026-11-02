package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Indexer manages the lifecycle of the vector index: building, updating, and
// querying.
type Indexer struct {
	embedder *OllamaEmbedder
	store    *Store
	root     string
	window   int
	stride   int
}

// IndexerConfig controls indexer behavior.
type IndexerConfig struct {
	Embedder   *OllamaEmbedder
	Store      *Store
	Root       string
	WindowSize int
	Stride     int
}

// NewIndexer creates an indexer. A nil config yields a no-op indexer.
func NewIndexer(cfg IndexerConfig) *Indexer {
	if cfg.Embedder == nil || cfg.Store == nil {
		return nil
	}
	return &Indexer{
		embedder: cfg.Embedder,
		store:    cfg.Store,
		root:     strings.TrimRight(cfg.Root, string(filepath.Separator)),
		window:   cfg.WindowSize,
		stride:   cfg.Stride,
	}
}

// Reindex walks the workspace, embeds every chunk, and replaces the store.
func (idx *Indexer) Reindex(ctx context.Context) (int, error) {
	if idx == nil {
		return 0, ErrNotConfigured
	}
	if err := idx.ensureEmpty(); err != nil {
		return 0, err
	}

	results, err := ChunkFiles(ctx, idx.root, idx.window, idx.stride)
	if err != nil {
		return 0, err
	}

	var total int
	for _, res := range results {
		n, err := idx.indexResult(ctx, res)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (idx *Indexer) ensureEmpty() error {
	if err := idx.store.Close(); err != nil {
		return err
	}
	f, err := os.Create(idx.store.Path())
	if err != nil {
		return err
	}
	return f.Close()
}

func (idx *Indexer) indexResult(ctx context.Context, res ChunkedFileResult) (int, error) {
	if len(res.Chunks) == 0 {
		return 0, nil
	}
	// Embed in small batches to avoid overwhelming Ollama.
	const batch = 16
	var wg sync.WaitGroup
	sem := make(chan struct{}, batch)
	errCh := make(chan error, len(res.Chunks))
	var mu sync.Mutex
	var embedded int

	for i := range res.Chunks {
		wg.Add(1)
		sem <- struct{}{}
		go func(chunk *Chunk) {
			defer wg.Done()
			defer func() { <-sem }()
			emb, err := idx.embedder.EmbedText(ctx, chunk.Text)
			if err != nil {
				errCh <- fmt.Errorf("embed %s:%d-%d: %w", res.Path, chunk.StartLine, chunk.EndLine, err)
				return
			}
			mu.Lock()
			chunk.Embedding = emb
			embedded++
			mu.Unlock()
		}(&res.Chunks[i])
	}
	wg.Wait()
	close(errCh)

	var firstErr error
	for err := range errCh {
		if firstErr == nil {
			firstErr = err
		}
		_ = err
	}
	if firstErr != nil {
		return embedded, firstErr
	}

	if err := idx.store.UpsertChunks(ctx, res.Chunks); err != nil {
		return embedded, err
	}
	return embedded, nil
}

// Query returns the top-k chunks matching the query text.
func (idx *Indexer) Query(ctx context.Context, query string, k int) ([]Chunk, error) {
	if idx == nil {
		return nil, ErrNotConfigured
	}
	emb, err := idx.embedder.EmbedText(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	return idx.store.Search(ctx, emb, k)
}

// Stats returns a snapshot of the index state for diagnostics.
type Stats struct {
	StorePath string
	Chunks    int
	Root      string
	Window    int
	Stride    int
}

// Stats reports current index statistics.
func (idx *Indexer) Stats() Stats {
	if idx == nil {
		return Stats{}
	}
	return Stats{
		StorePath: idx.store.Path(),
		Chunks:    idx.store.Len(),
		Root:      idx.root,
		Window:    idx.window,
		Stride:    idx.stride,
	}
}

// Close releases resources held by the indexer.
func (idx *Indexer) Close() error {
	if idx == nil {
		return nil
	}
	return idx.store.Close()
}

// Copy copies the current store contents to w. Useful for previewing search
// results without mutating the index.
func CopyStore(w io.Writer, store *Store) error {
	if store == nil {
		return ErrNotConfigured
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(store.chunks)
}
