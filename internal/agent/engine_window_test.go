package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tools"
)

// stubProber records the probe call and returns a canned window (0 = not
// found), so no engine test touches the network for window resolution.
type stubProber struct {
	called  atomic.Bool
	baseURL string
	model   string
	window  int
}

func (p *stubProber) probe(_ context.Context, baseURL, model string) int {
	p.called.Store(true)
	p.baseURL = baseURL
	p.model = model
	return p.window
}

// bigOutputTool returns a ~12k-estimated-token result so a tool round grows
// the context fast enough to overflow twice in one turn.
func bigOutputTool() tools.Tool {
	return tools.Tool{
		Definition: llm.ToolDefinition{
			Name:        "bigread",
			Description: "returns a big result",
			Params:      &llm.FunctionParameters{Type: "object"},
			Readable:    true,
		},
		Run: func(context.Context, json.RawMessage) (tools.Result, error) {
			return tools.Result{Content: compactionSeed}, nil
		},
	}
}

// compactingTextServer serves: streaming requests → final text; non-streaming
// requests → a compact summary. /props and other paths 404 (probe miss).
func compactingTextServer(streamHits *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if stream, _ := body["stream"].(bool); stream {
			if streamHits != nil {
				streamHits.Add(1)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, sseTextChunk("ok"))
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": "summary"},
			}},
		})
	}))
}

func seedHistory(t *testing.T, sess *Session, turns int) {
	t.Helper()
	for i := 0; i < turns; i++ {
		require.NoError(t, sess.Append(
			llm.Message{Role: llm.RoleUser, Content: compactionSeed},
			llm.Message{Role: llm.RoleAssistant, Content: compactionSeed},
		))
	}
}

// TestLoopUnknownWindowProbesThenScales: a model with no context_window and
// no preset resolves its window from the local probe, and folding runs
// against that window from the very first turn — the growth gate fires on a
// 60k-estimate history against a probed 64K window.
func TestLoopUnknownWindowProbesThenScales(t *testing.T) {
	server := compactingTextServer(nil)
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	seedHistory(t, sess, 3) // 6 × ~12k est ≈ 72k est

	prober := &stubProber{window: 65536}
	engine, err := NewEngine(
		llm.ModelConfig{Name: "local-model", BaseURL: server.URL, APIKey: "x"},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
		WithWindowProber(prober.probe),
	)
	require.NoError(t, err)

	var compactions int
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		if err != nil {
			t.Fatalf("loop error: %v", err)
		}
		if _, ok := ev.(session.CompactionStarted); ok {
			compactions++
		}
	}
	require.True(t, prober.called.Load(), "unknown window must trigger the probe")
	require.Equal(t, server.URL, prober.baseURL)
	require.Equal(t, "local-model", prober.model)
	require.Equal(t, 65536, engine.ContextWindow(), "probed window becomes the runtime window")
	require.GreaterOrEqual(t, compactions, 1, "folding must run against the probed window")
}

// TestLoopUnknownWindowAssumesDefaultWhenProbeEmpty: when neither config,
// preset, nor probe yields a window, folding still runs against the assumed
// default — before this fallback, window 0 disabled both compaction triggers
// entirely (the "folding doesn't work on small/local models" bug).
func TestLoopUnknownWindowAssumesDefaultWhenProbeEmpty(t *testing.T) {
	server := compactingTextServer(nil)
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	seedHistory(t, sess, 3) // ~72k est, far above the 32k-assumed floor

	prober := &stubProber{window: 0}
	engine, err := NewEngine(
		llm.ModelConfig{Name: "local-model", BaseURL: server.URL, APIKey: "x"},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
		WithWindowProber(prober.probe),
	)
	require.NoError(t, err)

	var compactions int
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		if err != nil {
			t.Fatalf("loop error: %v", err)
		}
		if _, ok := ev.(session.CompactionStarted); ok {
			compactions++
		}
	}
	require.Equal(t, llm.AssumedContextWindow, engine.ContextWindow())
	require.GreaterOrEqual(t, compactions, 1, "folding must run even without a known window")
}

// TestLoopLearnsWindowFromOverflowError: a provider-stated limit in the
// overflow error is ground truth and becomes the runtime window, overriding
// the assumed default.
func TestLoopLearnsWindowFromOverflowError(t *testing.T) {
	var streamN atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if stream, _ := body["stream"].(bool); stream {
			switch streamN.Add(1) {
			case 1:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(
					`{"error":{"message":"This model's maximum context length is 65536 tokens. However, you requested 70000 tokens"}}`,
				))
			default:
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, sseTextChunk("recovered"))
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": "summary"},
			}},
		})
	}))
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	seedHistory(t, sess, 2) // ~48k est: enough that a first overflow is plausible

	prober := &stubProber{window: 0}
	engine, err := NewEngine(
		llm.ModelConfig{Name: "local-model", BaseURL: server.URL, APIKey: "x"},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
		WithWindowProber(prober.probe),
	)
	require.NoError(t, err)

	var lastErr error
	var finalText string
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		if err != nil {
			lastErr = err
			break
		}
		if e, ok := ev.(session.AssistantMessageUpdate); ok && e.Message.State == session.StateComplete {
			finalText = e.Message.FlatText()
		}
	}
	require.NoError(t, lastErr)
	require.Equal(t, "recovered", finalText)
	require.Equal(t, 65536, engine.ContextWindow(), "provider-stated limit is learned")
}

// TestLoopRecoversFromLaterOverflowsInLongTurn pins the multi-shot recovery
// fix: a turn that overflows, recovers, runs more tool rounds (accumulating
// new context), and overflows AGAIN must recover again. The old single-shot
// flag never reset, so the second overflow killed the turn — fatal for
// continuous work on a small local window.
func TestLoopRecoversFromLaterOverflowsInLongTurn(t *testing.T) {
	var streamN atomic.Int32
	var compacts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if stream, _ := body["stream"].(bool); stream {
			switch streamN.Add(1) {
			case 1, 3:
				// Tool rounds: each bigread result grows the context ~12k est.
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, sseToolCallChunk(fmt.Sprintf("call_%d", streamN.Load()), "bigread", `{}`))
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			case 2, 4:
				// llama.cpp-style numberless overflow.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"the request exceeds the available context size"}}`))
			default:
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, sseTextChunk("all done"))
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			}
			return
		}
		compacts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": "summary"},
			}},
		})
	}))
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)

	prober := &stubProber{window: 65536}
	engine, err := NewEngine(
		llm.ModelConfig{Name: "local-model", BaseURL: server.URL, APIKey: "x"},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{bigOutputTool()}),
		WithWindowProber(prober.probe),
	)
	require.NoError(t, err)

	var lastErr error
	var finalText string
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		if err != nil {
			lastErr = err
			break
		}
		if e, ok := ev.(session.AssistantMessageUpdate); ok && e.Message.State == session.StateComplete {
			finalText = e.Message.FlatText()
		}
	}
	require.NoError(t, lastErr, "a second overflow later in the turn must recover, not kill it")
	require.Equal(t, "all done", finalText)
	require.Equal(t, int32(5), streamN.Load(), "tool, overflow, tool, overflow, final")
	require.Equal(t, int32(2), compacts.Load(), "one compaction per overflow")
}

// TestSetModelReseedsWindow: switching models re-seeds the runtime window
// from the new config; an unknown window re-arms resolution.
func TestSetModelReseedsWindow(t *testing.T) {
	server := compactingTextServer(nil)
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	prober := &stubProber{window: 32768}
	engine, err := NewEngine(
		llm.ModelConfig{Name: "a", BaseURL: server.URL, APIKey: "x", ContextWindow: 65536},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
		WithWindowProber(prober.probe),
	)
	require.NoError(t, err)
	require.Equal(t, 65536, engine.ContextWindow())
	require.False(t, prober.called.Load(), "explicit window must not probe")

	engine.SetModel(llm.ModelConfig{Name: "b", BaseURL: server.URL, APIKey: "x", ContextWindow: 131072})
	require.Equal(t, 131072, engine.ContextWindow())

	// Unknown window: 0 until the next loop resolves it (probe → assumed).
	engine.SetModel(llm.ModelConfig{Name: "c", BaseURL: server.URL, APIKey: "x"})
	require.Equal(t, 0, engine.ContextWindow())
}

// TestExecutorPanicBecomesToolError: a panicking tool must not take down the
// process — it becomes a ToolError result the model can react to.
func TestExecutorPanicBecomesToolError(t *testing.T) {
	reg := tools.Registry{
		"boom": {
			Definition: llm.ToolDefinition{
				Name:        "boom",
				Description: "always panics",
				Params:      &llm.FunctionParameters{Type: "object"},
			},
			Run: func(context.Context, json.RawMessage) (tools.Result, error) {
				panic("kaboom")
			},
		},
	}
	ex := NewExecutor(reg, permission.AllowAll{}, nil, nil, nil)

	var statuses []session.ToolStatus
	msgs, stop, reason := ex.Run(t.Context(), []llm.ToolCall{{
		ID:       "c1",
		Function: llm.Function{Name: "boom", Arguments: `{}`},
	}}, func(td session.ToolData) bool {
		statuses = append(statuses, td.Run.Status)
		return true
	})

	require.False(t, stop)
	require.Equal(t, "", reason)
	require.Len(t, msgs, 1)
	require.Contains(t, msgs[0].Content, "panicked")
	require.Contains(t, msgs[0].Content, "kaboom")
	require.Equal(t, []session.ToolStatus{session.ToolInProgress, session.ToolError}, statuses,
		"the UI must see in-progress then error, never a stuck row")
}
