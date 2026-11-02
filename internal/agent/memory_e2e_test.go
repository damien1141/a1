package agent

import (
        "context"
        "net/http"
        "net/http/httptest"
        "path/filepath"
        "strings"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/memstore"
        "github.com/damien1141/a1/internal/persona"
)

// setupMemoryE2E wires a real engine against a real persona store + a real
// SQLite semantic memory store (FakeEmbedder keeps vectors deterministic),
// so the full inlet/outlet loop can be exercised without an Ollama server.
func setupMemoryE2E(t *testing.T) (*Engine, *persona.Store, *memstore.Store) {
        t.Helper()
        root := t.TempDir()
        pstore := persona.NewStore(filepath.Join(root, "personas"))

        p, err := pstore.Create("solo", "solo persona", "")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(p.ID))

        emb := memstore.FakeEmbedder{DimCount: 64}
        sess, err := NewSession(WithPersonaStore(pstore))
        require.NoError(t, err)
        eng, err := NewEngine(llm.ModelConfig{Name: "base-model"}, sess, WithEmbedder(emb))
        require.NoError(t, err)

        // Same path the inlet/outlet use — the store is cached per persona on
        // the pstore, so this handle IS the one the pipeline writes through.
        semStore, err := pstore.SemanticStore(p, emb, memstore.DefaultParams())
        require.NoError(t, err)
        return eng, pstore, semStore
}

// newConsolidationEngine builds a second engine over the same persona
// store whose LLM client points at the given fake endpoint — the outlet
// uses engine.client.Compact for its one-shot consolidation call.
func newConsolidationEngine(t *testing.T, pstore *persona.Store, baseURL string) *Engine {
        t.Helper()
        sess, err := NewSession(WithPersonaStore(pstore))
        require.NoError(t, err)
        eng, err := NewEngine(
                llm.ModelConfig{Name: "consolidator", BaseURL: baseURL, APIKey: "test"},
                sess,
                WithEmbedder(memstore.FakeEmbedder{DimCount: 64}),
        )
        require.NoError(t, err)
        return eng
}

// TestMemoryOutletConsolidatesAndInletInjects is the end-to-end proof the
// automatic memory loop works: a turn's content flows through the outlet
// pipeline (fake LLM answers with a CREATE op), the op lands in the
// persona's SQLite store, and the inlet retrieves it for the next turn.
func TestMemoryOutletConsolidatesAndInletInjects(t *testing.T) {
        _, pstore, semStore := setupMemoryE2E(t)

        // Fake OpenAI-compatible endpoint: the consolidation LLM call returns
        // one CREATE op — exactly what a local model would answer for a
        // "my name is Solis" turn.
        const memoryContent = "The user's name is Solis and they build Go agent harnesses."
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                require.Equal(t, "/chat/completions", r.URL.Path)
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"ops\":[{\"operation\":\"CREATE\",\"content\":\"` + memoryContent + `\",\"kind\":\"fact\",\"scope\":\"user\"}]}"},"finish_reason":"stop"}]}`))
        }))
        defer srv.Close()

        eng2 := newConsolidationEngine(t, pstore, srv.URL)

        // 1. Outlet: consolidate a personal turn. Runs synchronously enough —
        //    WaitForOutstandingOutlets blocks until the goroutine finishes.
        eng2.consolidateSemanticMemories(
                "hey, my name is Solis and I build Go agent harnesses",
                "Nice to meet you, Solis — noted.",
                []string{"hey, my name is Solis and I build Go agent harnesses"},
        )
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        require.NoError(t, eng2.WaitForOutstandingOutlets(ctx))

        // 2. The memory must now exist in the persona's semantic store.
        traces, err := semStore.Retrieve(ctx, "Solis Go agent harnesses", memstore.ModeCompanion, memstore.Cues{}, time.Now())
        require.NoError(t, err)
        require.NotEmpty(t, traces, "the CREATE op must have written to the persona's store")
        found := false
        for _, tr := range traces {
                if strings.Contains(tr.Content, "Solis") {
                        found = true
                }
        }
        assert.True(t, found, "retrieved memories should contain the Solis fact, got %+v", traces)
        // 3. Inlet: the next turn's injection must surface the stored fact.
        msg := eng2.injectSemanticMemories(ctx, "what do I do again? Solis things")
        require.NotNil(t, msg, "a stored personal fact must inject on a related prompt")
        assert.Contains(t, msg.Content, "Solis")
        assert.Contains(t, msg.Content, "memories about the user",
                "injection must use the third-person 'a memory about the user' framing")

        // 4. Persona scoping: the memory lives in THIS persona's bank only.
        other, err := pstore.Create("other", "other persona", "")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(other.ID))
        otherMsg := eng2.injectSemanticMemories(ctx, "what do I do again? Solis things")
        assert.Nil(t, otherMsg,
                "a different persona's bank is empty — nothing may inject across personas")
}

// TestMemoryOutletSkipsNonPersonal verifies the skip detector keeps code/
// URL turns out of the memory pipeline entirely: the fake LLM must never
// be called.
func TestMemoryOutletSkipsNonPersonal(t *testing.T) {
        eng, _, _ := setupMemoryE2E(t)

        called := false
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                called = true
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"ops\":[]}"},"finish_reason":"stop"}]}`))
        }))
        defer srv.Close()

        eng2 := newConsolidationEngine(t, eng.personaStore(), srv.URL)

        // A pure technical exchange — long tokens (hashes) on both sides, no
        // personal content. The detector's long_token pattern must trip for
        // both the concatenated turn and the assistant reply alone.
        hash := strings.Repeat("a1b2c3d4", 15) // 120 chars of hex-ish blob
        eng2.consolidateSemanticMemories(
                "the artifact digest is "+hash,
                "verified — the digest matches: "+hash,
                []string{"the artifact digest is " + hash},
        )
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        require.NoError(t, eng2.WaitForOutstandingOutlets(ctx))

        assert.False(t, called,
                "non-personal (code + URL) turns must not reach the consolidation LLM")
}

// TestMemoryInletNilWithoutEmbedder verifies the inlet degrades silently
// when no embedder is configured: no injection, no panic.
func TestMemoryInletNilWithoutEmbedder(t *testing.T) {
        root := t.TempDir()
        pstore := persona.NewStore(filepath.Join(root, "personas"))
        p, err := pstore.Create("solo", "solo persona", "")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(p.ID))

        sess, err := NewSession(WithPersonaStore(pstore))
        require.NoError(t, err)
        eng, err := NewEngine(llm.ModelConfig{Name: "base"}, sess) // no embedder
        require.NoError(t, err)

        msg := eng.injectSemanticMemories(context.Background(), "what is my name?")
        assert.Nil(t, msg, "without an embedder the inlet must quietly return nil")
}
