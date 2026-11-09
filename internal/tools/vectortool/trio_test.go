package vectortool

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/vector"
)

// installProject writes a temp-HOME config.yaml (models plus the given
// semantic_search YAML) and installs it as the default project — the same
// seam bashtool/configvalidatortool tests use to pin config reads.
func installProject(t *testing.T, semanticYAML string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfgDir := filepath.Join(home, ".a1")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	cfg := "models:\n  - name: test-model\n    api_key: sk-test\n" + semanticYAML
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfg), 0o644))
	proj, err := project.Discover("")
	require.NoError(t, err)
	require.NoError(t, proj.LoadConfig())
	project.SetDefaultProject(proj)
	t.Cleanup(func() { project.SetDefaultProject(nil) })
}

// semanticYAML enables semantic search against the given Ollama base URL.
func semanticYAML(baseURL string) string {
	return fmt.Sprintf("semantic_search:\n  enabled: true\n  ollama_base_url: %s\n  embedding_model: test-embed\n", baseURL)
}

// fakeOllamaEmbedder answers POST /api/embed with a fixed vector, standing in
// for a local Ollama on loopback only — the same pattern internal/agent tests
// use to fake HTTP backends; no external network is touched.
func fakeOllamaEmbedder(t *testing.T, vec []float32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{vec}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// seedIndex pre-writes the legacy JSON store the tool opens when no vault
// handle is installed (the default in tests), so a search runs against a
// known corpus without a reindex.
func seedIndex(t *testing.T, chunks []vector.Chunk) {
	t.Helper()
	home := os.Getenv("HOME")
	require.NotEmpty(t, home, "call installProject before seedIndex")
	raw, err := json.Marshal(chunks)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".a1", "semantic_index.json"), raw, 0o644))
}

func runVectorTool(t *testing.T, args map[string]any) (tooldef.Result, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	return VectorTool().Run(t.Context(), raw)
}

func runVectorRaw(t *testing.T, raw string) (tooldef.Result, error) {
	t.Helper()
	return VectorTool().Run(t.Context(), json.RawMessage(raw))
}

// ---------- positive ----------

func TestVectorTool_Positive(t *testing.T) {
	tool := VectorTool()
	assert.Equal(t, "vector_search", tool.Definition.Name)
	assert.True(t, tool.Definition.Readable)
	assert.Equal(t, []string{"query"}, tool.Definition.Params.Required)
	for _, p := range []string{"query", "path", "limit", "reindex"} {
		assert.Contains(t, tool.Definition.Params.Properties, p, "param %s must be declared", p)
	}
	require.NotNil(t, tool.Run)

	assert.Equal(t, `vector_search "how auth works"`,
		tool.DetailFromArgs(json.RawMessage(`{"query":"how auth works"}`)))
	assert.Equal(t, "vector_search", tool.DetailFromArgs(json.RawMessage(`{}`)))
	assert.Equal(t, "vector_search", tool.DetailFromArgs(json.RawMessage(`{"query":"   "}`)))
}

func TestRunVectorSearch_Positive(t *testing.T) {
	srv := fakeOllamaEmbedder(t, []float32{0.6, 0.8})
	installProject(t, semanticYAML(srv.URL))

	// Seeded in a deliberately non-ranked order: gamma, alpha, beta.
	seedIndex(t, []vector.Chunk{
		{ID: 1, Path: "gamma.go", StartLine: 20, EndLine: 30, Text: "gamma code", Embedding: []float32{0, 1}},
		{ID: 2, Path: "alpha.go", StartLine: 1, EndLine: 9, Text: "alpha code", Embedding: []float32{1, 0}},
		{ID: 3, Path: "beta.go", StartLine: 5, EndLine: 9, Text: "beta code", Embedding: []float32{0.6, 0.8}},
	})

	res, err := runVectorTool(t, map[string]any{"query": "anything", "path": t.TempDir(), "limit": 2})
	require.NoError(t, err)

	// beta is the exact match (cos 1.0 to the fixed query vector), gamma
	// second (cos 0.8); limit 2 drops alpha (cos 0.6).
	assert.True(t, strings.HasPrefix(res.Content, "@file beta.go\n// lines 5-9\nbeta code\n\n"), res.Content)
	assert.Contains(t, res.Content, "@file gamma.go\n")
	assert.NotContains(t, res.Content, "alpha.go")
	assert.Equal(t, "2 vector_search results", res.Detail)
	assert.Equal(t, res.Content, res.Output)
}

func TestRunVectorSearch_Reindex_Positive(t *testing.T) {
	srv := fakeOllamaEmbedder(t, []float32{0.6, 0.8})
	installProject(t, semanticYAML(srv.URL))

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0o644))

	// No seeded index: the empty store triggers a reindex of the search
	// path, and the query then matches the freshly embedded chunk — the
	// fake embedder returns the same vector for chunk and query (cos 1.0).
	res, err := runVectorTool(t, map[string]any{"query": "main function", "path": root})
	require.NoError(t, err)
	assert.Contains(t, res.Content, "@file main.go\n// lines 1-4\n")
	assert.Contains(t, res.Content, "func main() {}")
	assert.Equal(t, "1 vector_search results", res.Detail)
}

// ---------- negative ----------

func TestRunVectorSearch_Negative(t *testing.T) {
	t.Run("malformed json", func(t *testing.T) {
		_, err := runVectorRaw(t, `{"query"`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse vector_search arguments")
	})

	t.Run("missing query", func(t *testing.T) {
		_, err := runVectorRaw(t, `{}`)
		require.Error(t, err)
		assert.EqualError(t, err, "query is required: provide a natural language search phrase")
	})

	t.Run("blank query", func(t *testing.T) {
		_, err := runVectorRaw(t, `{"query": "   "}`)
		require.Error(t, err)
		assert.EqualError(t, err, "query is required: provide a natural language search phrase")
	})

	t.Run("config not loaded", func(t *testing.T) {
		// Discovered but never LoadConfig-ed: Config() is nil.
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		proj, err := project.Discover("")
		require.NoError(t, err)
		project.SetDefaultProject(proj)
		t.Cleanup(func() { project.SetDefaultProject(nil) })

		_, err = runVectorRaw(t, `{"query": "anything"}`)
		require.Error(t, err)
		assert.EqualError(t, err, "vector_search: project config not loaded")
	})

	t.Run("semantic search disabled", func(t *testing.T) {
		installProject(t, "semantic_search:\n  enabled: false\n")
		_, err := runVectorRaw(t, `{"query": "anything"}`)
		require.Error(t, err)
		assert.EqualError(t, err, "vector_search is disabled; enable semantic_search in config")
	})

	t.Run("embedding model missing", func(t *testing.T) {
		installProject(t, "semantic_search:\n  enabled: true\n")
		_, err := runVectorRaw(t, `{"query": "anything"}`)
		require.Error(t, err)
		assert.EqualError(t, err, "vector_search: embedding_model is not configured")
	})

	t.Run("unparseable ollama base url", func(t *testing.T) {
		installProject(t, "semantic_search:\n  enabled: true\n  ollama_base_url: http://[::1\n  embedding_model: test-embed\n")
		_, err := runVectorRaw(t, `{"query": "anything"}`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create embedder:")
	})
}

// ---------- differential ----------

// oracle: brute-force cosine scan — plain float64 loops in this test over the
// seeded chunk vectors and the fake embedder's fixed query vector; the tool's
// reported @file ordering must match the naive ranking, ties excluded by
// construction so any correct top-k order is unambiguous.
func TestRunVectorSearch_Ordering_Differential(t *testing.T) {
	const d0, d1 = 0.6, 0.8 // the fake embedder's fixed query vector, |q| = 1
	chunks := []vector.Chunk{
		{ID: 1, Path: "zero.go", StartLine: 1, EndLine: 2, Text: "zero", Embedding: []float32{0, 0}},
		{ID: 2, Path: "orth.go", StartLine: 1, EndLine: 2, Text: "orth", Embedding: []float32{1, 0}},
		{ID: 3, Path: "same.go", StartLine: 1, EndLine: 2, Text: "same", Embedding: []float32{d0, d1}},
		{ID: 4, Path: "axis.go", StartLine: 1, EndLine: 2, Text: "axis", Embedding: []float32{0, 1}},
		{ID: 5, Path: "opposed.go", StartLine: 1, EndLine: 2, Text: "opposed", Embedding: []float32{-1, 0}},
	}

	srv := fakeOllamaEmbedder(t, []float32{d0, d1})
	installProject(t, semanticYAML(srv.URL))
	seedIndex(t, chunks)

	res, err := runVectorTool(t, map[string]any{"query": "ranking probe", "path": t.TempDir()})
	require.NoError(t, err)

	// Naive reference: cosine similarity by hand over every chunk.
	type scored struct {
		path  string
		score float64
	}
	q := []float64{d0, d1}
	want := make([]scored, 0, len(chunks))
	for _, c := range chunks {
		var dot, na, nb float64
		for i := range c.Embedding {
			fi := float64(c.Embedding[i])
			dot += fi * q[i]
			na += fi * fi
			nb += q[i] * q[i]
		}
		var cos float64
		if na != 0 && nb != 0 {
			cos = dot / (math.Sqrt(na) * math.Sqrt(nb))
		}
		want = append(want, scored{path: c.Path, score: cos})
	}
	sort.Slice(want, func(i, j int) bool {
		if want[i].score != want[j].score {
			return want[i].score > want[j].score
		}
		return want[i].path < want[j].path
	})
	var wantOrder []string
	for _, w := range want {
		wantOrder = append(wantOrder, w.path)
	}

	// The tool's reported order, extracted from the @file headers.
	var gotOrder []string
	for _, m := range regexp.MustCompile(`@file (\S+)\n`).FindAllStringSubmatch(res.Content, -1) {
		gotOrder = append(gotOrder, m[1])
	}
	assert.Equal(t, wantOrder, gotOrder)
	assert.Len(t, gotOrder, len(chunks), "default limit 10 keeps the whole corpus")
}
