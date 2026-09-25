package vector

import "errors"

var (
	// ErrNotConfigured is returned when an operation is attempted without a
	// configured embedder or store.
	ErrNotConfigured = errors.New("vector: not configured")

	// ErrEmptyEmbedding is returned when the embedding service returns no
	// vectors for a non-empty input.
	ErrEmptyEmbedding = errors.New("vector: empty embedding")

	// ErrIndexNotOpen is returned when the vector store is used before Open.
	ErrIndexNotOpen = errors.New("vector: index not open")

	// ErrUnsupportedLanguage is returned when a file extension is not mapped
	// to a chunking strategy.
	ErrUnsupportedLanguage = errors.New("vector: unsupported language")
)
