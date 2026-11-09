package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/session"
)

// TestCredentialPoolSurvivesClientRebinds pins the engine-level property the
// whole design hangs on: pool state lives on the Engine, not the Client.
// SetJobs, scheduler attach, and every surface model switch rebuild the
// client through rebindTools — a rate-limited or auth-failed key's cooldown
// must survive those rebuilds, or the first rebind would hand a known-bad
// key straight back to the provider.
func TestCredentialPoolSurvivesClientRebinds(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		keys = append(keys, key)
		mu.Unlock()

		if key == "k1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseTextChunk("done") + "data: [DONE]\n\n"))
	}))
	defer server.Close()

	newEngine := func() *Engine {
		sess, err := NewSession(WithCwd(t.TempDir()))
		require.NoError(t, err)
		engine, err := NewEngine(
			llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "k1", APIKeys: []string{"k1", "k2"}},
			sess,
			WithGate(permission.AllowAll{}),
		)
		require.NoError(t, err)
		return engine
	}

	runTurn := func(t *testing.T, engine *Engine) string {
		t.Helper()
		var finalText string
		var lastErr error
		for ev, err := range engine.Loop(t.Context(), "hello", LoopOpts{}) {
			if err != nil {
				lastErr = err
				break
			}
			if update, ok := ev.(session.AssistantMessageUpdate); ok && update.Message.State == session.StateComplete {
				finalText = update.Message.FlatText()
			}
		}
		require.NoError(t, lastErr, "the 401 rotation is invisible: the turn must succeed")
		return finalText
	}

	engine := newEngine()
	require.Equal(t, "done", runTurn(t, engine))

	// The rebind: SetJobs rebuilds the client. The pool it attaches must
	// be the same one that just quarantined k1.
	engine.SetJobs(nil)
	require.Equal(t, "done", runTurn(t, engine))

	// A second rebind via the model-switch surface (same key set): the
	// pool follows the keys, not the client or the model name.
	cfg := engine.modelCfg
	cfg.Name = "fake-2"
	engine.SetModel(cfg)
	require.Equal(t, "done", runTurn(t, engine))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"k1", "k2", "k2", "k2"}, keys,
		"one 401 total: every rebinded client starts on the proven-good key")
}

// TestPoolForSharesByKeySet pins the registry semantics directly: pools are
// keyed by the key set, so configs sharing keys share a pool (and its
// cooldowns), configs with different keys get different pools, and a
// single-key config gets no pool at all.
func TestPoolForSharesByKeySet(t *testing.T) {
	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:1", APIKey: "k1", APIKeys: []string{"k1", "k2"}},
		sess,
		WithGate(permission.AllowAll{}),
	)
	require.NoError(t, err)

	// Same set, different order and duplication: one pool.
	a := engine.poolFor(llm.ModelConfig{APIKey: "k1", APIKeys: []string{"k1", "k2"}})
	b := engine.poolFor(llm.ModelConfig{APIKey: "k2", APIKeys: []string{"k2", "k1", "k1"}})
	require.Same(t, a, b, "pools key by set membership, not order or entry shape")

	// A diary-style config (APIKey + others via APIKeys) shares too.
	c := engine.poolFor(llm.ModelConfig{APIKey: "k1", APIKeys: []string{"k2"}})
	require.Same(t, a, c)

	// Different keys: a different pool.
	d := engine.poolFor(llm.ModelConfig{APIKey: "k9", APIKeys: []string{"k8"}})
	require.NotSame(t, a, d)

	// Single key: no pool — the pre-pool world.
	require.Nil(t, engine.poolFor(llm.ModelConfig{APIKey: "only"}))
	require.Nil(t, engine.poolFor(llm.ModelConfig{}))
}
