package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tools"
)

// engine_cache_stability_test.go — the prompt-cache contract.
//
// Providers bill caching by the serialized byte: a cache read (0.1x input
// price) requires the request's prefix to match a previously written prefix
// exactly, and any byte that changes re-bills everything after it as a fresh
// cache write (1.25–2x). This file pins the properties that keep an agentic
// session on the read path:
//
//   - the system block is byte-identical across every request (no clock, no
//     weather, no memory bundle churning inside it);
//   - history is append-only on the wire: each request's messages begin with
//     the previous request's messages, element for element, and the delta is
//     exactly the new turn's bytes;
//   - the runtime rider (clock et al.) rides as the tail's leading bytes —
//     fresh per turn, frozen thereafter;
//   - the tail breakpoint (cache_control) rides the last message of every
//     request, so each round writes only its delta forward.
//
// The old shape failed all four: the per-round clock was merged into the
// cached system block, so no request ever matched a previous prefix and the
// whole context re-billed at write price on every round.

// anthropicSSE writes one Anthropic Messages-API SSE data event.
func anthropicSSE(w io.Writer, payload string) {
	_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
}

// anthropicTextEvents streams a plain text completion.
func anthropicTextEvents(w io.Writer, text string) {
	anthropicSSE(w, `{"type":"message_start","message":{"usage":{"input_tokens":42,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`)
	anthropicSSE(w, `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`)
	anthropicSSE(w, fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`, mustJSONString(text)))
	anthropicSSE(w, `{"type":"content_block_stop","index":0}`)
	anthropicSSE(w, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`)
}

// anthropicToolUseEvents streams one tool_use block.
func anthropicToolUseEvents(w io.Writer, id, name, args string) {
	anthropicSSE(w, `{"type":"message_start","message":{"usage":{"input_tokens":42,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`)
	anthropicSSE(w, fmt.Sprintf(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":%s,"name":%s}}`, mustJSONString(id), mustJSONString(name)))
	anthropicSSE(w, fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":%s}}`, mustJSONString(args)))
	anthropicSSE(w, `{"type":"content_block_stop","index":0}`)
	anthropicSSE(w, `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`)
}

func mustJSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// capturedAnthropicRequest is one request body as the server saw it.
type capturedAnthropicRequest struct {
	System   json.RawMessage   `json:"system"`
	Messages []json.RawMessage `json:"messages"`
	Tools    []struct {
		Name         string `json:"name"`
		CacheControl *struct {
			Type string `json:"type"`
		} `json:"cache_control"`
	} `json:"tools"`
}

// stripCacheControl removes cache_control markers from a parsed JSON value.
// The marker is write-directive metadata — it tells the provider where to
// ESTABLISH a checkpoint, and the prefix matcher excludes it from identity
// (which is exactly why the moving-tail pattern can read a prefix that was
// written with the marker on what is now mid-history, and why requests may
// carry at most four of them). Byte-stability of the CONTENT is the
// contract; the marker rides only the current tail.
func stripCacheControl(v any) any {
	switch t := v.(type) {
	case map[string]any:
		delete(t, "cache_control")
		for k, vv := range t {
			t[k] = stripCacheControl(vv)
		}
		return t
	case []any:
		for i, vv := range t {
			t[i] = stripCacheControl(vv)
		}
		return t
	default:
		return v
	}
}

// normalizedJSON re-serializes raw JSON with cache_control markers stripped,
// for prefix-equality comparisons.
func normalizedJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal(raw, &v))
	b, err := json.Marshal(stripCacheControl(v))
	require.NoError(t, err)
	return string(b)
}

// TestAnthropicRequestsAreCacheStable drives two full turns (turn one with a
// tool round) against an Anthropic-wire fake and asserts the byte-stability
// contract on every captured request.
func TestAnthropicRequestsAreCacheStable(t *testing.T) {
	var mu sync.Mutex
	var bodies []capturedAnthropicRequest

	n := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		data, _ := io.ReadAll(r.Body)
		var req capturedAnthropicRequest
		require.NoError(t, json.Unmarshal(data, &req))
		mu.Lock()
		bodies = append(bodies, req)
		n++
		round := n
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		switch round {
		case 1: // turn 1, round 1: call the tool
			anthropicToolUseEvents(w, "toolu_01", "bash", `{}`)
		case 2: // turn 1, round 2: finish with text
			anthropicTextEvents(w, "done one")
		case 3: // turn 2, round 1: plain answer
			anthropicTextEvents(w, "done two")
		default:
			anthropicTextEvents(w, "unexpected")
		}
	}))
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		llm.ModelConfig{
			Name:          "claude-fake",
			API:           llm.Anthropic,
			BaseURL:       server.URL,
			APIKey:        "x",
			ContextWindow: 200000,
		},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{stubBashTool(func() string { return "tool output" })}),
	)
	require.NoError(t, err)

	require.NoError(t, drainLoop(t, engine, "first prompt"))
	require.NoError(t, drainLoop(t, engine, "second prompt"))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 3, "turn 1 ran two rounds (tool + finish), turn 2 one")

	// Contract 1 — one system block, byte-identical everywhere, cache-pinned.
	for i := range bodies {
		require.NotEmpty(t, bodies[i].System, "every request carries the system block")
		assert.JSONEq(t, string(bodies[0].System), string(bodies[i].System),
			"the system block must be byte-identical across all requests — any churn re-bills the whole context as a cache write")
	}
	require.Contains(t, string(bodies[0].System), "cache_control",
		"the system block carries the cache breakpoint")
	assert.NotContains(t, string(bodies[0].System), "Current date and time",
		"the clock must never ride the system block")

	// Contract 2 — no system-role messages anywhere: runtime context rides
	// the persisted rider instead of hoisted system text.
	for i := range bodies {
		for _, raw := range bodies[i].Messages {
			assert.NotContains(t, string(raw), `"role":"system"`,
				"request %d: system-role messages get merged into the cached system block and bust it", i+1)
		}
	}

	// Contract 3 — append-only history within the turn: round 2 replays
	// round 1's messages verbatim and appends exactly the tool exchange.
	// Comparison is modulo cache_control: the marker rode round 1's tail and
	// moved on — the content underneath is what the cache matches.
	r1, r2, t2 := bodies[0], bodies[1], bodies[2]
	require.Greater(t, len(r2.Messages), len(r1.Messages))
	for i := range r1.Messages {
		assert.Equal(t, normalizedJSON(t, r1.Messages[i]), normalizedJSON(t, r2.Messages[i]),
			"round 2 must replay round 1's message %d byte-identically (the cache-read prefix)", i)
	}
	assert.Len(t, r2.Messages, len(r1.Messages)+2,
		"the within-turn delta is exactly the assistant tool_use + the tool result")
	assert.Contains(t, string(r2.Messages[len(r2.Messages)-1]), "tool_result")

	// Contract 4 — append-only history across turns: turn 2 replays turn 1's
	// final request verbatim and appends the final assistant and the new
	// prompt. This turn has no fresh memories and no job notices, so no
	// rider rides at all — the empty-rider skip keeps zero-content bytes
	// out of the cache-write path.
	require.Greater(t, len(t2.Messages), len(r2.Messages))
	for i := range r2.Messages {
		assert.Equal(t, normalizedJSON(t, r2.Messages[i]), normalizedJSON(t, t2.Messages[i]),
			"turn 2 must replay turn 1's message %d byte-identically (the cache-read prefix)", i)
	}
	require.Len(t, t2.Messages, len(r2.Messages)+2,
		"the cross-turn delta is exactly final assistant + new user message")
	penultimate := string(t2.Messages[len(t2.Messages)-2])
	assert.Contains(t, penultimate, `"role":"assistant"`,
		"with no rider, the message before the new prompt is turn 1's final assistant")
	last := string(t2.Messages[len(t2.Messages)-1])
	assert.Contains(t, last, "second prompt")

	// Contract 5 — the tail breakpoint rides the last message of every
	// request: each round writes only its own delta forward.
	for i := range bodies {
		msgs := bodies[i].Messages
		assert.Contains(t, string(msgs[len(msgs)-1]), "cache_control",
			"request %d: the last message must carry the cache_control pin", i+1)
	}

	// Contract 6 — the tools block is cache-pinned on its last definition.
	for i := range bodies {
		require.NotEmpty(t, bodies[i].Tools, "request %d advertises tools", i+1)
		assert.NotNil(t, bodies[i].Tools[len(bodies[i].Tools)-1].CacheControl,
			"request %d: the last tool definition carries the cache breakpoint", i+1)
	}

	// Contract 7 — no clock anywhere. The clock is a fetch engine now, so
	// no request message and no transcript row may carry the stamp the old
	// rider used to inject.
	for i := range bodies {
		for _, raw := range bodies[i].Messages {
			assert.NotContains(t, string(raw), "Current date and time",
				"request %d: the clock must be fetched, never injected", i+1)
		}
	}
	snap := session.ReplaySnapshot(engine.Session().AllEntries(), nil)
	var sawClock bool
	for _, m := range snap.Messages {
		if strings.Contains(m.FlatText(), "Current date and time") {
			sawClock = true
		}
	}
	assert.False(t, sawClock, "runtime riders must not surface as transcript rows")
}
