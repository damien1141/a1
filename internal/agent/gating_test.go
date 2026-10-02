package agent

import (
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

// sseUsageChunk is like sseTextChunk but also reports provider usage so the
// engine's compaction gate reads a non-zero context size.
func sseUsageChunk(text string, totalTokens int) string {
	payload, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"delta":         map[string]any{"content": text},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":     totalTokens,
			"completion_tokens": 1,
			"total_tokens":      totalTokens + 1,
		},
	})
	if err != nil {
		panic(err)
	}
	return "data: " + string(payload) + "\n\n"
}

// TestLoopGrowthGateFiresOnce verifies the billion-context growth gate (§3.4)
// at the engine level: a turn that crosses the floor fraction and growth
// threshold triggers exactly one compaction, and a subsequent turn whose
// context grows only a little does NOT re-trigger it.
func TestLoopGrowthGateFiresOnce(t *testing.T) {
	var turn atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if stream, _ := body["stream"].(bool); stream {
			n := turn.Add(1)
			tokens := 50000
			if n == 2 {
				tokens = 10000
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, sseUsageChunk("ok", tokens))
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
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	require.NoError(t, sess.Append(
		llm.Message{Role: llm.RoleUser, Content: compactionSeed, Usage: llm.Usage{TotalTokens: 12000}},
		llm.Message{Role: llm.RoleAssistant, Content: compactionSeed, Usage: llm.Usage{TotalTokens: 24000}},
		llm.Message{Role: llm.RoleUser, Content: compactionSeed, Usage: llm.Usage{TotalTokens: 36000}},
	))

	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 100_000},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
	)
	require.NoError(t, err)

	var compactCount int
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		if err != nil {
			break
		}
		if _, ok := ev.(session.CompactionStarted); ok {
			compactCount++
		}
	}
	require.NoError(t, err)
	require.Equal(t, 1, compactCount, "growth gate should fire once, not on every turn")
}

// TestLoopGrowthGateDoesNotFireBelowFloor confirms the gate is not a no-op:
// a turn whose usage is below the floor fraction must not compact, even when
// the compactor endpoint is reachable.
func TestLoopGrowthGateDoesNotFireBelowFloor(t *testing.T) {
	var compactCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if stream, _ := body["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, sseUsageChunk("ok", 10000))
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		compactCalls.Add(1)
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
	require.NoError(t, sess.Append(
		llm.Message{Role: llm.RoleUser, Content: compactionSeed, Usage: llm.Usage{TotalTokens: 12000}},
		llm.Message{Role: llm.RoleAssistant, Content: compactionSeed, Usage: llm.Usage{TotalTokens: 24000}},
	))

	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 100_000},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
	)
	require.NoError(t, err)

	var compactCount int
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		if err != nil {
			break
		}
		if _, ok := ev.(session.CompactionStarted); ok {
			compactCount++
		}
	}
	require.NoError(t, err)
	require.Equal(t, 0, compactCount, "usage below floor fraction must not compact")
}
