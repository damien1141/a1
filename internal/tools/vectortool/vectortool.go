package vectortool

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/vector"
)

const (
	vectorDefaultLimit = 10
)

var vectorDescription = `Semantic code search using local embeddings.

Requires semantic search to be enabled in config with a running Ollama instance.
Returns matching code chunks as @file path headers with similarity scores.`

// VectorTool returns the semantic search tool definition + handler.
func VectorTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "vector_search",
			Description: vectorDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"query": llm.Object{
						"type":        "string",
						"description": "Natural language search query. Example: \"how is authentication implemented\"",
					},
					"path": llm.Object{
						"type":        "string",
						"description": "Directory to search. Example: ./internal",
					},
					"limit": llm.Object{
						"type": "integer",
						"description": fmt.Sprintf(
							"Maximum results. Example: 20 (default: %d)",
							vectorDefaultLimit,
						),
					},
					"reindex": llm.Object{
						"type":        "boolean",
						"description": "Force rebuild the index before searching.",
					},
				},
				Required: []string{"query"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in vectorInput
			_ = json.Unmarshal(input, &in)
			q := strings.TrimSpace(in.Query)
			if q == "" {
				return "vector_search"
			}
			return fmt.Sprintf("vector_search %q", q)
		},
		Run: runVectorSearch,
	}
}

type vectorInput struct {
	Query   string `json:"query"`
	Path    string `json:"path,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Reindex bool   `json:"reindex,omitempty"`
}

func runVectorSearch(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in vectorInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse vector_search arguments: %w", err)
	}

	query := strings.TrimSpace(in.Query)
	if query == "" {
		return tooldef.Result{}, fmt.Errorf("query is required: provide a natural language search phrase")
	}

	limit := in.Limit
	if limit <= 0 {
		limit = vectorDefaultLimit
	}

	searchRel := strings.TrimSpace(in.Path)
	if searchRel == "" {
		searchRel = "."
	}
	searchPath, err := tooldef.ResolveToCwd(ctx, searchRel)
	if err != nil {
		return tooldef.Result{}, err
	}

	proj := project.GetDefaultProject()
	cfg := proj.Config()
	if cfg == nil {
		return tooldef.Result{}, fmt.Errorf("vector_search: project config not loaded")
	}
	ss := cfg.SemanticSearch
	if !ss.Enabled {
		return tooldef.Result{}, fmt.Errorf("vector_search is disabled; enable semantic_search in config")
	}
	if ss.EmbeddingModel == "" {
		return tooldef.Result{}, fmt.Errorf("vector_search: embedding_model is not configured")
	}

	embedder, err := vector.NewOllamaEmbedder(ss.OllamaBaseURL, ss.EmbeddingModel)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("create embedder: %w", err)
	}

	var chunks []vector.Chunk
	if v := session.DefaultVault(); v != nil && v.DB() != nil {
		// Vault-backed store: chunks live as project-scoped rows in
		// ~/.a1/a1.db, re-embedding only files whose (mtime, size)
		// changed, with hybrid dense+FTS5 retrieval. The project key is
		// the resolved search path so two workspaces sharing a vault
		// keep separate indexes.
		vs, err := vector.OpenVaultStore(vector.VaultStoreConfig{
			DB:         v.DB(),
			Project:    searchPath,
			Root:       searchPath,
			WindowSize: 50,
			Stride:     25,
			Embedder:   embedder,
			Model:      ss.EmbeddingModel,
		})
		if err != nil {
			return tooldef.Result{}, fmt.Errorf("open vector store: %w", err)
		}
		n, err := vs.Len(ctx)
		if err != nil {
			return tooldef.Result{}, fmt.Errorf("vector store len: %w", err)
		}
		// Same trigger as before, but "empty" is now per project + model
		// instead of one global JSON file.
		if in.Reindex || n == 0 {
			if _, err := vs.Reindex(ctx, in.Reindex); err != nil {
				return tooldef.Result{}, fmt.Errorf("reindex failed: %w", err)
			}
		}
		chunks, err = vs.Search(ctx, query, limit)
		if err != nil {
			return tooldef.Result{}, fmt.Errorf("search failed: %w", err)
		}
	} else {
		// Degraded mode (no vault handle — e.g. a harness that booted
		// without one): the legacy JSON brute-force store, unchanged.
		storePath := filepath.Join(proj.Global().Root(), "semantic_index.json")
		store, err := vector.OpenStore(storePath)
		if err != nil {
			return tooldef.Result{}, fmt.Errorf("open vector store: %w", err)
		}
		defer store.Close()

		indexer := vector.NewIndexer(vector.IndexerConfig{
			Embedder:   embedder,
			Store:      store,
			Root:       searchPath,
			WindowSize: 50,
			Stride:     25,
		})

		if in.Reindex || store.Len() == 0 {
			if _, err := indexer.Reindex(ctx); err != nil {
				return tooldef.Result{}, fmt.Errorf("reindex failed: %w", err)
			}
		}

		chunks, err = indexer.Query(ctx, query, limit)
		if err != nil {
			return tooldef.Result{}, fmt.Errorf("search failed: %w", err)
		}
	}

	if len(chunks) == 0 {
		return tooldef.Result{Content: "No matches found", Detail: "0 results", Output: "No matches found"}, nil
	}

	content := renderVectorResults(ctx, chunks)
	detail := fmt.Sprintf("%d vector_search results", len(chunks))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func renderVectorResults(ctx context.Context, chunks []vector.Chunk) string {
	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString(fmt.Sprintf("@file %s\n", c.Path))
		sb.WriteString(fmt.Sprintf("// lines %d-%d\n", c.StartLine, c.EndLine))
		sb.WriteString(c.Text)
		sb.WriteString("\n\n")
	}
	return sb.String()
}
