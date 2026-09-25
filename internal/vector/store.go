package vector

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Chunk struct {
	ID        int       `json:"id"`
	Path      string    `json:"path"`
	StartLine int       `json:"start_line"`
	EndLine   int       `json:"end_line"`
	Text      string    `json:"text"`
	Embedding []float32 `json:"embedding,omitempty"`
}

// Store persists and queries code chunks by embedding similarity.
type Store struct {
	path   string
	chunks []Chunk
	nextID int
	mu     sync.RWMutex
	opened bool
}

// OpenStore opens or creates a vector store at the given path.
func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create vector store dir: %w", err)
	}
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	s.opened = true
	return s, nil
}

// Close flushes any pending writes.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.opened {
		return nil
	}
	s.opened = false
	return s.persist()
}

func (s *Store) load() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.chunks = nil
			s.nextID = 1
			return nil
		}
		return fmt.Errorf("open vector store: %w", err)
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&s.chunks); err != nil {
		if err == io.EOF {
			s.chunks = nil
			s.nextID = 1
			return nil
		}
		return fmt.Errorf("decode vector store: %w", err)
	}
	if len(s.chunks) > 0 {
		maxID := 0
		for _, c := range s.chunks {
			if c.ID > maxID {
				maxID = c.ID
			}
		}
		s.nextID = maxID + 1
	} else {
		s.nextID = 1
	}
	return nil
}

func (s *Store) persist() error {
	f, err := os.CreateTemp(filepath.Dir(s.path), "vec-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp vector store: %w", err)
	}
	tmpPath := f.Name()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s.chunks); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("encode vector store: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp vector store: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename vector store: %w", err)
	}
	return nil
}

// UpsertChunks adds or replaces chunks in the store.
func (s *Store) UpsertChunks(ctx context.Context, chunks []Chunk) error {
	if s == nil {
		return ErrNotConfigured
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range chunks {
		chunks[i].ID = s.nextID
		s.nextID++
		s.chunks = append(s.chunks, chunks[i])
	}
	return s.persist()
}

// Search returns the top-k chunks most similar to the query embedding.
func (s *Store) Search(ctx context.Context, query []float32, k int) ([]Chunk, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	if len(query) == 0 {
		return nil, fmt.Errorf("vector: empty query embedding")
	}
	if k <= 0 {
		k = 10
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	type scored struct {
		chunk Chunk
		score float64
	}
	scoredChunks := make([]scored, 0, len(s.chunks))
	for i := range s.chunks {
		chunk := s.chunks[i]
		if len(chunk.Embedding) == 0 {
			continue
		}
		score := cosineSimilarity(query, chunk.Embedding)
		scoredChunks = append(scoredChunks, scored{chunk: chunk, score: score})
	}

	// Partial sort: keep top-k by score descending.
	for i := 0; i < len(scoredChunks) && i < k; i++ {
		maxIdx := i
		for j := i + 1; j < len(scoredChunks); j++ {
			if scoredChunks[j].score > scoredChunks[maxIdx].score {
				maxIdx = j
			}
		}
		if maxIdx != i {
			scoredChunks[i], scoredChunks[maxIdx] = scoredChunks[maxIdx], scoredChunks[i]
		}
	}
	if len(scoredChunks) > k {
		scoredChunks = scoredChunks[:k]
	}

	out := make([]Chunk, len(scoredChunks))
	for i, sc := range scoredChunks {
		out[i] = sc.chunk
	}
	return out, nil
}

// Len returns the number of indexed chunks.
func (s *Store) Len() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.chunks)
}

// Path returns the backing file path.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// ChunkText splits text into overlapping windows of approximately windowSize
// lines with the given stride.
func ChunkText(text string, startLine, windowSize, stride int) []Chunk {
	if stride <= 0 {
		stride = windowSize
	}
	if stride > windowSize {
		stride = windowSize
	}
	lines := splitLines(text)
	if len(lines) == 0 {
		return nil
	}
	var chunks []Chunk
	id := 1
	for offset := 0; offset < len(lines); offset += stride {
		end := offset + windowSize
		if end > len(lines) {
			end = len(lines)
		}
		chunks = append(chunks, Chunk{
			ID:        id,
			StartLine: startLine + offset,
			EndLine:   startLine + offset + (end - offset),
			Text:      strings.Join(lines[offset:end], "\n"),
		})
		id++
		if end == len(lines) {
			break
		}
	}
	return chunks
}

// ChunkedFileResult holds the chunks produced from a single file.
type ChunkedFileResult struct {
	Path   string
	Chunks []Chunk
}

// ChunkFiles splits every readable file under root into text windows.
// Extensions without a text detection fallback are skipped.
func ChunkFiles(ctx context.Context, root string, windowSize, stride int) ([]ChunkedFileResult, error) {
	if windowSize <= 0 {
		windowSize = 50
	}
	if stride <= 0 {
		stride = windowSize
	}

	var results []ChunkedFileResult
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !isTextFile(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel := strings.TrimPrefix(path, root)
		if strings.HasPrefix(rel, string(filepath.Separator)) {
			rel = rel[1:]
		}
		chunks := ChunkText(string(data), 1, windowSize, stride)
		if len(chunks) > 0 {
			for i := range chunks {
				chunks[i].Path = rel
			}
			results = append(results, ChunkedFileResult{Path: rel, Chunks: chunks})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	return results, nil
}

func splitLines(text string) []string {
	var out []string
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		out = append(out, scanner.Text())
	}
	return out
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) != len(a) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		fa := float64(a[i])
		fb := float64(b[i])
		dot += fa * fb
		na += fa * fa
		nb += fb * fb
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func isTextFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".md", ".txt", ".yaml", ".yml", ".json", ".toml", ".mod", ".sum", ".sh", ".bash", ".zsh", ".fish", ".lua", ".py", ".rs", ".js", ".ts", ".tsx", ".jsx", ".rb", ".java", ".c", ".cpp", ".h", ".hpp", ".cs", ".php", ".html", ".css", ".proto", ".graphql", ".gql", ".sql", ".tf", ".tmpl", ".tpl", ".dockerfile", ".makefile", ".gitignore", ".env", ".editorconfig":
		return true
	}
	return false
}
