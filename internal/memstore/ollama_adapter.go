package memstore

import (
	"context"
	"strings"
	"sync"

	"github.com/damien1141/a1/internal/vector"
)

// OllamaAdapter wraps the existing vector.OllamaEmbedder to satisfy
// the memstore.Embedder interface. OllamaEmbedder.EmbedText takes a
// single string; the adapter batches by calling it once per text.
// At the trace-store scale (1-5 texts per call) the per-call overhead
// is negligible — the network round-trip dominates anyway.
//
// Model() returns the configured embedding model name.
// Dim() is discovered on first embed and cached (Ollama returns
// whatever dimension the model produces; we don't know until we ask).
type OllamaAdapter struct {
	embedder *vector.OllamaEmbedder
	model    string
	dimMu    sync.Once
	dimVal   int
	dimErr   error
}

// NewOllamaAdapter wraps an OllamaEmbedder for use as a memstore.Embedder.
// The model name is passed separately because OllamaEmbedder doesn't
// expose it (it's set at construction time, not stored as a field).
func NewOllamaAdapter(embedder *vector.OllamaEmbedder, model string) *OllamaAdapter {
	return &OllamaAdapter{embedder: embedder, model: model}
}

func (a *OllamaAdapter) Model() string { return a.model }

func (a *OllamaAdapter) Dim() int {
	a.dimMu.Do(func() {
		// Embed a probe to discover the dimension. The vector is
		// discarded — we only care about len(v).
		v, err := a.embedder.EmbedText(context.Background(), "dimension probe")
		if err != nil || len(v) == 0 {
			a.dimErr = err
			a.dimVal = 0
			return
		}
		a.dimVal = len(v)
	})
	return a.dimVal
}

func (a *OllamaAdapter) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		v, err := a.embedder.EmbedText(ctx, text)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// TryOllamaFromConfig attempts to create an OllamaAdapter from the
// project config's SemanticSearch settings. Returns nil (not an error)
// when Ollama isn't configured or the embedding model is empty — the
// caller falls back to FakeEmbedder in that case.
//
// This is the production embedder path: the config's
// semantic_search.embedding_model (typically "nomic-embed-text") and
// semantic_search.ollama_base_url (default 127.0.0.1:11434) drive the
// memory system's embeddings. When the user hasn't configured semantic
// search, the memory system falls back to FakeEmbedder — the
// activation math still works, just with weaker semantic matching.
func TryOllamaFromConfig(ollamaBaseURL, embeddingModel string) *OllamaAdapter {
	embeddingModel = strings.TrimSpace(embeddingModel)
	if embeddingModel == "" {
		return nil
	}
	embedder, err := vector.NewOllamaEmbedder(ollamaBaseURL, embeddingModel)
	if err != nil {
		return nil
	}
	return NewOllamaAdapter(embedder, embeddingModel)
}
