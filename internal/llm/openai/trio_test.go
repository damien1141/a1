package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// ---------- helpers ----------

// sseServer serves one fixed event-stream body.
func sseServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// captured is one yielded stream step: the event plus the error text when the
// step carried one.
type captured struct {
	ev      llm.StreamEvent
	errText string
}

// collectStream drains StreamChatCompletion against srv.
func collectStream(t *testing.T, srv *httptest.Server) []captured {
	t.Helper()
	var events []captured
	for ev, err := range StreamChatCompletion(t.Context(), srv.Client(), srv.URL, "k", struct{}{}) {
		c := captured{ev: ev}
		if err != nil {
			c.errText = err.Error()
		}
		events = append(events, c)
	}
	return events
}

// findFinal returns the Final message of a completed stream.
func findFinal(t *testing.T, events []captured) *llm.Message {
	t.Helper()
	for _, c := range events {
		if c.errText != "" {
			continue
		}
		if c.ev.Type == llm.StreamEventTypeDone {
			require.NotNil(t, c.ev.Final)
			return c.ev.Final
		}
	}
	t.Fatal("no Done event in stream")
	return nil
}

// ---------- negative ----------

// Unparseable chunks are skipped, literal tabs inside chunk JSON are read as
// spaces (the gateway-mangling workaround), and everything after [DONE] is
// ignored — the stream still completes with a Done event.
func TestStreamMalformedChunksSkipped_Negative(t *testing.T) {
	srv := sseServer(t, strings.Join([]string{
		`data: {broken json`,
		``,
		`data: {"choices":[{"delta":{"content":"ok"}}]}`,
		``,
		`data: {"choices":[{"delta":`,
		``,
		"data: {\"choices\":[{\"delta\":{\"content\":\"a\tb\"}}]}",
		``,
		`data: [DONE]`,
		``,
		`data: {"choices":[{"delta":{"content":"after done ignored"}}]}`,
		``,
	}, "\n"))

	events := collectStream(t, srv)
	require.NotEmpty(t, events)

	var contents []string
	for _, c := range events {
		assert.Empty(t, c.errText, "malformed chunks must not fail the turn: %s", c.errText)
		if c.ev.Type == llm.StreamEventTypeDelta {
			contents = append(contents, c.ev.Delta.Content)
		}
	}

	assert.Equal(t, []string{"ok", "a b"}, contents, "garbage skipped, tab mangled to a space")
	msg := findFinal(t, events)
	assert.Equal(t, "oka b", msg.Content)
	assert.Empty(t, msg.ToolCalls)
	assert.Equal(t, llm.RoleAssistant, msg.Role)
}

// Missing usage degrades to zeros: no usage chunk, a null usage chunk, and a
// usage chunk with only completion tokens all complete without errors.
func TestStreamUsageMissingFields_Negative(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		check func(t *testing.T, u llm.Usage)
	}{
		{
			name: "no usage chunk at all",
			body: strings.Join([]string{
				`data: {"choices":[{"delta":{"content":"hi"}}]}`,
				`data: [DONE]`,
				``,
			}, "\n"),
			check: func(t *testing.T, u llm.Usage) {
				assert.Equal(t, 0, u.PromptTokens)
				assert.Equal(t, 0, u.CompletionTokens)
				assert.Equal(t, 0, u.TotalTokens)
				assert.Nil(t, u.PromptTokensDetails)
			},
		},
		{
			name: "explicit null usage",
			body: strings.Join([]string{
				`data: {"choices":[{"delta":{"content":"hi"}}],"usage":null}`,
				`data: [DONE]`,
				``,
			}, "\n"),
			check: func(t *testing.T, u llm.Usage) {
				assert.Equal(t, 0, u.PromptTokens)
				assert.Equal(t, 0, u.TotalTokens)
			},
		},
		{
			name: "usage without prompt_tokens",
			body: strings.Join([]string{
				`data: {"choices":[],"usage":{"completion_tokens":9}}`,
				`data: [DONE]`,
				``,
			}, "\n"),
			check: func(t *testing.T, u llm.Usage) {
				assert.Equal(t, 0, u.PromptTokens, "missing prompt_tokens reads as zero")
				assert.Equal(t, 9, u.CompletionTokens)
				assert.Equal(t, 9, u.TotalTokens)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := collectStream(t, sseServer(t, tt.body))
			for _, c := range events {
				assert.Empty(t, c.errText, "usage gaps must not fail the turn: %s", c.errText)
			}
			tt.check(t, findFinal(t, events).Usage)
		})
	}
}

// A connection that dies mid-body is terminal: the transport failure is
// yielded as the last step, with no Done event for the half-delivered answer.
// The shape matches the other providers: the event itself is error-typed (for
// consumers branching on ev.Type) and the iter error carries the same failure.
func TestStreamConnectionDiedMidFlight_Negative(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Promise more bytes than will ever arrive, so the client read fails
		// instead of seeing a clean EOF.
		w.Header().Set("Content-Length", "4096")
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))
	}))
	t.Cleanup(srv.Close)

	events := collectStream(t, srv)
	require.NotEmpty(t, events)

	last := events[len(events)-1]
	require.NotEmpty(t, last.errText, "the dropped connection must surface as an error")
	assert.Contains(t, last.errText, "SSE stream error", "scanner failures are wrapped for context")
	assert.Equal(t, llm.StreamEventTypeError, last.ev.Type, "the error step is an error-typed event, matching anthropic and gemini")
	assert.Equal(t, last.errText, last.ev.Err, "the event text and the iter error describe the same failure")
	for _, c := range events[:len(events)-1] {
		assert.Empty(t, c.errText)
		assert.NotEqual(t, llm.StreamEventTypeDone, c.ev.Type, "no Done event may stand next to a mid-stream failure")
	}
	assert.Equal(t, llm.StreamEventTypeDelta, events[0].ev.Type)
}

// Compact's failure modes: a non-2xx body is unwrapped into an APIError, an
// empty choices array is an explicit error, and a non-JSON body is a decode
// error — never a silent zero-value result.
func TestCompactRequestErrorPaths_Negative(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		check   func(t *testing.T, res llm.CompactResult, err error)
	}{
		{
			name: "401 with plain text body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("unauthorized\n"))
			},
			check: func(t *testing.T, _ llm.CompactResult, err error) {
				require.Error(t, err)
				var apiErr *llm.APIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, 401, apiErr.Status)
				assert.Equal(t, "unauthorized", apiErr.Message, "the trimmed raw body is the fallback message")
				assert.EqualError(t, err, "LLM API error (401): unauthorized")
			},
		},
		{
			name: "200 with empty choices",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"choices":[]}`))
			},
			check: func(t *testing.T, res llm.CompactResult, err error) {
				require.Error(t, err)
				assert.EqualError(t, err, "LLM API error: empty choices")
				assert.Empty(t, res.Text)
			},
		},
		{
			name: "200 with non-JSON body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`not json`))
			},
			check: func(t *testing.T, _ llm.CompactResult, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid character", "a malformed body is a decode error")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			res, err := CompactRequest(
				t.Context(),
				srv.Client(),
				llm.ModelConfig{Name: "gpt-4o-mini", APIKey: "k", BaseURL: srv.URL},
				NewCompactRequest("gpt-4o-mini", "summarize", 0),
			)
			tt.check(t, res, err)
		})
	}
}

// ---------- differential ----------

// oracle: hand-written known-good JSON literal — the chat-completions body
// for system merge, a tool round, and an image attachment, written byte for
// byte from the documented wire shape. Content stays a plain string unless
// images force the parts array (whose maps marshal with sorted keys).
func TestBuildRequestGoldenJSON_Differential(t *testing.T) {
	readTool := llm.ToolDefinition{
		Name:        "read",
		Description: "read a file",
		Params: &llm.FunctionParameters{
			Type:       "object",
			Properties: llm.Object{"path": llm.Object{"type": "string"}},
			Required:   []string{"path"},
		},
	}

	t.Run("tool round", func(t *testing.T) {
		cfg := llm.ModelConfig{Name: "gpt-4o-mini", APIKey: "k"}
		req := BuildRequest(cfg, "You are terse.", []llm.Message{
			{Role: llm.RoleUser, Content: "hi"},
			{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{ID: "call_1", Type: "function", Function: llm.Function{Name: "read", Arguments: `{"path":"a.go"}`}},
				},
			},
			{Role: llm.RoleTool, ToolCallID: "call_1", Content: "ok"},
		}, []llm.ToolDefinition{readTool})
		body, err := json.Marshal(req)
		require.NoError(t, err)
		assert.Equal(t, `{"model":"gpt-4o-mini",`+
			`"messages":[{"role":"system","content":"You are terse."},`+
			`{"role":"user","content":"hi"},`+
			`{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function",`+
			`"function":{"name":"read","arguments":"{\"path\":\"a.go\"}"}}]},`+
			`{"role":"tool","content":"ok","tool_call_id":"call_1"}],`+
			`"tools":[{"type":"function","function":{"name":"read","description":"read a file",`+
			`"parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}],`+
			`"stream":true,"stream_options":{"include_usage":true}}`, string(body)) //nolint:testifylint // byte-exact golden
	})

	t.Run("mid-history system messages merge up front", func(t *testing.T) {
		cfg := llm.ModelConfig{Name: "gpt-4o-mini", APIKey: "k"}
		req := BuildRequest(cfg, "base", []llm.Message{
			{Role: llm.RoleUser, Content: "hi"},
			{Role: llm.RoleSystem, Content: "extra rules"},
			{Role: llm.RoleUser, Content: "go"},
		}, nil)
		body, err := json.Marshal(req)
		require.NoError(t, err)
		assert.Equal(t, `{"model":"gpt-4o-mini",`+
			`"messages":[{"role":"system","content":"base\n\nextra rules"},`+
			`{"role":"user","content":"hi"},{"role":"user","content":"go"}],`+
			`"stream":true,"stream_options":{"include_usage":true}}`, string(body)) //nolint:testifylint // byte-exact golden
	})

	t.Run("image attachment becomes a data-url parts array", func(t *testing.T) {
		cfg := llm.ModelConfig{Name: "gpt-4o-mini", APIKey: "k"}
		req := BuildRequest(cfg, "", []llm.Message{
			{
				Role:    llm.RoleUser,
				Content: "see",
				Images:  []llm.Image{{Data: "QUJD", MimeType: "image/png"}},
			},
		}, nil)
		body, err := json.Marshal(req)
		require.NoError(t, err)
		assert.Equal(t, `{"model":"gpt-4o-mini",`+
			`"messages":[{"role":"user","content":[`+
			`{"text":"see","type":"text"},`+
			`{"image_url":{"url":"data:image/png;base64,QUJD"},"type":"image_url"}]}],`+
			`"stream":true,"stream_options":{"include_usage":true}}`, string(body)) //nolint:testifylint // byte-exact golden
	})

	t.Run("think mode maps to reasoning_effort", func(t *testing.T) {
		cfg := llm.ModelConfig{Name: "gpt-4o-mini", Think: llm.ThinkConfig{Enabled: true, Mode: llm.High}}
		req := BuildRequest(cfg, "", []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, nil)
		body, err := json.Marshal(req)
		require.NoError(t, err)
		assert.Equal(t, `{"model":"gpt-4o-mini",`+
			`"messages":[{"role":"user","content":"hi"}],`+
			`"stream":true,"stream_options":{"include_usage":true},"reasoning_effort":"high"}`,
			string(body)) //nolint:testifylint // byte-exact golden
	})
}

// refToolFrag is one emitted tool-call fragment as the naive reader sees it.
type refToolFrag struct {
	index int
	id    string
	name  string
	args  string
}

// refOpenAIResult is what a naive reader of the chat-completions streaming
// contract expects: concatenated deltas, tool fragments merged by index into
// complete calls, and the last usage chunk split into disjoint buckets.
type refOpenAIResult struct {
	role      string
	content   string
	reasoning string
	tools     []llm.ToolCall

	contentDeltas []string
	reasonDeltas  []string
	toolFrags     []refToolFrag

	prompt, completion, cacheRead, cacheWrite int
}

// refOpenAIStream re-derives the stream with a plain line scanner and
// map-based encoding/json decoding — no typed chunks, no shared code with the
// production parser or accumulator.
func refOpenAIStream(t *testing.T, sse string) refOpenAIResult {
	t.Helper()
	var out refOpenAIResult
	type slot struct {
		id, name, args string
	}
	slots := map[int]*slot{}
	maxIdx := -1

	for _, line := range strings.Split(sse, "\n") {
		payload, ok := strings.CutPrefix(line, "data: ")
		if !ok || payload == "" {
			continue
		}
		if payload == "[DONE]" {
			break
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if usage, ok := chunk["usage"].(map[string]any); ok {
			get := func(key string) int {
				if v, ok := usage[key].(float64); ok {
					return int(v)
				}
				return 0
			}
			cacheRead := get("prompt_cache_hit_tokens")
			cacheWrite := 0
			if d, ok := usage["prompt_tokens_details"].(map[string]any); ok {
				if v, ok := d["cached_tokens"].(float64); ok && int(v) > cacheRead {
					cacheRead = int(v)
				}
				if v, ok := d["cache_write_tokens"].(float64); ok {
					cacheWrite = int(v)
				}
			}
			prompt := get("prompt_tokens") - cacheRead - cacheWrite
			if prompt < 0 {
				prompt = 0
			}
			out.prompt, out.completion = prompt, get("completion_tokens")
			out.cacheRead, out.cacheWrite = cacheRead, cacheWrite
		}
		choices, ok := chunk["choices"].([]any)
		if !ok || len(choices) == 0 {
			continue
		}
		delta, _ := choices[0].(map[string]any)["delta"].(map[string]any)
		if delta == nil {
			continue
		}
		if r, ok := delta["role"].(string); ok && r != "" {
			out.role = r
		}
		if c, ok := delta["content"].(string); ok && c != "" {
			out.contentDeltas = append(out.contentDeltas, c)
			out.content += c
		}
		if r, ok := delta["reasoning_content"].(string); ok && r != "" {
			out.reasonDeltas = append(out.reasonDeltas, r)
			out.reasoning += r
		}
		if tcs, ok := delta["tool_calls"].([]any); ok {
			for _, raw := range tcs {
				tc, _ := raw.(map[string]any)
				idx := 0
				if v, ok := tc["index"].(float64); ok {
					idx = int(v)
				}
				s := slots[idx]
				if s == nil {
					s = &slot{}
					slots[idx] = s
				}
				if idx > maxIdx {
					maxIdx = idx
				}
				frag := refToolFrag{index: idx}
				if id, ok := tc["id"].(string); ok && id != "" {
					s.id = id
					frag.id = id
				}
				fn, _ := tc["function"].(map[string]any)
				if fn != nil {
					if n, ok := fn["name"].(string); ok && n != "" {
						s.name = n
						frag.name = n
					}
					if a, ok := fn["arguments"].(string); ok {
						s.args += a
						frag.args = a
					}
				}
				out.toolFrags = append(out.toolFrags, frag)
			}
		}
	}
	for i := 0; i <= maxIdx; i++ {
		if s, ok := slots[i]; ok {
			out.tools = append(out.tools, llm.ToolCall{
				Index: i, ID: s.id, Type: "function",
				Function: llm.Function{Name: s.name, Arguments: s.args},
			})
		}
	}
	return out
}

// oracle: naive reference implementation in this test — the same SSE corpus
// is fed through a line scanner and map-based encoding/json decoding, and the
// resulting deltas, tool calls, and usage buckets are compared against
// StreamChatCompletion.
func TestStreamVsNaiveSSEReference_Differential(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant"}}]}`,
		``,
		`data: {"choices":[{"delta":{"reasoning_content":"hmm"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"Hel"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"lo"}}]}`,
		``,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function",` +
			`"function":{"name":"read","arguments":"{\"pa"}}]}}]}`,
		``,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a.go\"}"}}]}}]}`,
		``,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2",` +
			`"function":{"name":"grep","arguments":"{}"}}]}}]}`,
		``,
		`data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":9,` +
			`"prompt_tokens_details":{"cached_tokens":40}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	want := refOpenAIStream(t, sse)

	events := collectStream(t, sseServer(t, sse))
	msg := findFinal(t, events)

	var contentDeltas, reasonDeltas []string
	var toolFrags []refToolFrag
	for _, c := range events {
		require.Empty(t, c.errText, "stream error: %s", c.errText)
		if c.ev.Type != llm.StreamEventTypeDelta {
			continue
		}
		if c.ev.Delta.Content != "" {
			contentDeltas = append(contentDeltas, c.ev.Delta.Content)
		}
		if c.ev.Delta.ReasoningContent != "" {
			reasonDeltas = append(reasonDeltas, c.ev.Delta.ReasoningContent)
		}
		for _, tc := range c.ev.Delta.ToolCalls {
			toolFrags = append(toolFrags, refToolFrag{
				index: tc.Index, id: tc.ID,
				name: tc.Function.Name, args: tc.Function.Arguments,
			})
		}
	}

	assert.Equal(t, llm.Role(want.role), msg.Role, "role from the role delta")
	assert.Equal(t, want.content, msg.Content, "final text")
	assert.Equal(t, want.reasoning, msg.ReasoningContent, "final reasoning")
	assert.Equal(t, want.contentDeltas, contentDeltas, "content deltas, in order")
	assert.Equal(t, want.reasonDeltas, reasonDeltas, "reasoning deltas, in order")
	assert.Equal(t, want.toolFrags, toolFrags, "emitted fragments carry the raw piece, not the accumulation")
	assert.Equal(t, want.tools, msg.ToolCalls, "fragments merge by index into complete calls")

	assert.Equal(t, want.prompt, msg.Usage.PromptTokens, "prompt tokens net of cache reads")
	assert.Equal(t, want.completion, msg.Usage.CompletionTokens)
	assert.Equal(t, want.cacheRead, msg.Usage.CachedTokens())
	assert.Equal(t, want.cacheWrite, msg.Usage.CacheWriteTokens())
	assert.Equal(t, want.prompt+want.completion+want.cacheRead+want.cacheWrite, msg.Usage.TotalTokens)
}
