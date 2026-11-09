package anthropic

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// ---------- negative ----------

// brokenSSEReader feeds a valid SSE prefix, then fails the way a dropped
// connection does: a read error instead of a clean EOF.
type brokenSSEReader struct {
	data []byte
}

var errBrokenStream = errors.New("boom: connection reset by peer")

func (r *brokenSSEReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, errBrokenStream
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// Unparseable event payloads (truncated JSON, wrong field types, unknown event
// names) must be skipped without failing the turn: the stream still completes
// with a Done event built from whatever valid events did parse.
func TestProcessStreamMalformedEventsSkipped_Negative(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":12,"cache_read_input_tokens":9}}}`,
		`data: {this is not json`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"bro`,
		`data: {"type":"never_seen_this_event","payload":{"whatever":1}}`,
		`data: {"type":"content_block_delta","index":"zero","delta":{"type":"text_delta","text":"type errors skip too"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"orphan\":"}}`,
		`data: {"type":"content_block_stop","index":7}`,
		`data: `,
		`data:`,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		`data: {"type":"message_delta","usage":{"output_tokens":3}}`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	events := processForTest(sse)
	require.NotEmpty(t, events)

	var text strings.Builder
	var done *llm.StreamEvent
	for i, ev := range events {
		assert.NotEqual(t, llm.StreamEventTypeError, ev.Type,
			"event %d: malformed payloads must not become stream errors: %s", i, ev.Err)
		switch ev.Type {
		case llm.StreamEventTypeDelta:
			text.WriteString(ev.Delta.Content)
			assert.Empty(t, ev.Delta.ToolCalls, "orphan input_json_delta must not emit a tool call delta")
		case llm.StreamEventTypeDone:
			done = &events[i]
		}
	}

	require.NotNil(t, done, "the stream must still complete after unparseable events")
	assert.Equal(t, "ok", text.String(), "only the parseable text delta survives")
	msg := done.Final
	require.NotNil(t, msg)
	assert.Equal(t, "ok", msg.Content)
	assert.Empty(t, msg.ToolCalls, "an input_json_delta with no tool_use start must not invent a call")
	assert.Equal(t, 12, msg.Usage.PromptTokens, "usage from the parseable message_start stands")
	assert.Equal(t, 3, msg.Usage.CompletionTokens)
	assert.Equal(t, 24, msg.Usage.TotalTokens, "12 prompt + 9 cache-read + 3 output")
}

// A read error mid-stream is terminal: the parser surfaces the transport
// failure and never emits a Done event for a half-delivered answer.
func TestProcessStreamReaderErrorMidFlight_Negative(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial an"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"swer"}}`,
		"",
	}, "\n")

	var events []llm.StreamEvent
	processStream(&brokenSSEReader{data: []byte(sse)}, func(ev llm.StreamEvent, err error) bool {
		if err != nil {
			events = append(events, llm.StreamEvent{Type: llm.StreamEventTypeError, Err: err.Error()})
			return false
		}
		events = append(events, ev)
		return true
	})

	require.Len(t, events, 3, "two deltas then the error, nothing else")
	assert.Equal(t, llm.StreamEventTypeDelta, events[0].Type)
	assert.Equal(t, llm.StreamEventTypeDelta, events[1].Type)
	assert.Equal(t, llm.StreamEventTypeError, events[2].Type, "the read failure becomes the terminal event")
	assert.Contains(t, events[2].Err, "SSE stream error", "scanner failures are wrapped for context")
	assert.Contains(t, events[2].Err, "boom: connection reset by peer", "the underlying transport error survives")
}

// Missing usage fields degrade to zeros, not errors: a stream without
// message_start, a message_delta without output_tokens, and a message_start
// with only cache buckets must all still complete.
func TestProcessStreamUsageMissingFields_Negative(t *testing.T) {
	t.Run("no usage events at all", func(t *testing.T) {
		events := processForTest(strings.Join([]string{
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
			`data: {"type":"message_stop"}`,
			"",
		}, "\n"))
		done := findDone(t, events)
		assert.Equal(t, 0, done.Final.Usage.PromptTokens)
		assert.Equal(t, 0, done.Final.Usage.CompletionTokens)
		assert.Equal(t, 0, done.Final.Usage.TotalTokens)
		assert.Nil(t, done.Final.Usage.PromptTokensDetails, "no cache buckets reported means no details struct")
		assert.Equal(t, 0, done.Final.Usage.CachedTokens())
	})

	t.Run("message_delta without usage", func(t *testing.T) {
		events := processForTest(strings.Join([]string{
			`data: {"type":"message_start","message":{"usage":{"input_tokens":4}}}`,
			`data: {"type":"message_delta","stop_reason":"end_turn"}`,
			"",
		}, "\n"))
		done := findDone(t, events)
		assert.Equal(t, 4, done.Final.Usage.PromptTokens)
		assert.Equal(t, 0, done.Final.Usage.CompletionTokens)
		assert.Equal(t, 4, done.Final.Usage.TotalTokens)
	})

	t.Run("cache buckets without input_tokens", func(t *testing.T) {
		events := processForTest(strings.Join([]string{
			`data: {"type":"message_start","message":{"usage":{"cache_read_input_tokens":5,"cache_creation_input_tokens":2}}}`,
			`data: {"type":"message_delta","usage":{"output_tokens":3}}`,
			"",
		}, "\n"))
		done := findDone(t, events)
		assert.Equal(t, 0, done.Final.Usage.PromptTokens, "missing input_tokens reads as zero, not as the cache total")
		assert.Equal(t, 5, done.Final.Usage.CachedTokens())
		assert.Equal(t, 2, done.Final.Usage.CacheWriteTokens())
		assert.Equal(t, 3, done.Final.Usage.CompletionTokens)
		assert.Equal(t, 10, done.Final.Usage.TotalTokens, "disjoint buckets: 0 + 5 + 2 + 3")
	})
}

// tool_use input must be a JSON object. Invalid JSON and valid-but-non-object
// JSON both degrade to "{}" so the request stays parseable; hostile tool-call
// IDs are normalized to the [a-zA-Z0-9_-] alphabet Anthropic accepts.
func TestBuildRequestHostileToolPayloads_Negative(t *testing.T) {
	cfg := llm.ModelConfig{Name: "claude-sonnet-4", APIKey: "k"}
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "run"},
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				{ID: "ok", Function: llm.Function{Name: "read", Arguments: `{"path":"a.go"}`}},
				{ID: "empty", Function: llm.Function{Name: "read", Arguments: ``}},
				{ID: "blank", Function: llm.Function{Name: "read", Arguments: "   "}},
				{ID: "garbage", Function: llm.Function{Name: "read", Arguments: `{"path": "tru`}},
				{ID: "number", Function: llm.Function{Name: "read", Arguments: `42`}},
				{ID: "string", Function: llm.Function{Name: "read", Arguments: `"just a string"`}},
				{ID: "array", Function: llm.Function{Name: "read", Arguments: `[1,2]`}},
			},
		},
	}

	req := BuildRequest(cfg, "", messages, nil)
	require.Len(t, req.Messages, 2)
	blocks, ok := req.Messages[1].Content.([]anthropicContentBlock)
	require.True(t, ok)
	require.Len(t, blocks, 7)

	assert.Equal(t, `{"path":"a.go"}`, string(blocks[0].Input), "a valid object rides verbatim")
	for i, id := range []string{"empty", "blank", "garbage", "number", "string", "array"} {
		assert.Equal(t, `{}`, string(blocks[i+1].Input), "arguments for %s must degrade to an empty object", id)
	}

	// Hostile IDs: characters outside [a-zA-Z0-9_-] become underscores, and an
	// over-long ID is cut to the 64-byte bound.
	hostile := []llm.Message{
		{Role: llm.RoleUser, Content: "run"},
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				{ID: "tool/call:01 spaced", Function: llm.Function{Name: "read", Arguments: `{}`}},
				{ID: strings.Repeat("x", 100), Function: llm.Function{Name: "read", Arguments: `{}`}},
			},
		},
	}
	hreq := BuildRequest(cfg, "", hostile, nil)
	hblocks, ok := hreq.Messages[1].Content.([]anthropicContentBlock)
	require.True(t, ok)
	require.Len(t, hblocks, 2)
	assert.Equal(t, "tool_call_01_spaced", hblocks[0].ID)
	assert.Equal(t, strings.Repeat("x", 64), hblocks[1].ID, "IDs longer than 64 bytes are truncated")

	// The repaired request must serialize: the whole point of the fallback.
	body, err := json.Marshal(hreq)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"input":{}`)
}

// Whitespace-only system content must never reach the wire: the system block
// exists to carry the cache_control breakpoint, and pinning it on blank content
// is a cache write on nothing (the openai builder skips the same messages).
// Real system text keeps byte-identical bytes — cache prefixes bill drift.
func TestBuildRequestWhitespaceSystemSkipped_Negative(t *testing.T) {
	cfg := llm.ModelConfig{Name: "claude-sonnet-4", APIKey: "k"}

	t.Run("whitespace-only system message with empty param", func(t *testing.T) {
		req := BuildRequest(cfg, "", []llm.Message{
			{Role: llm.RoleSystem, Content: " \n\t"},
			{Role: llm.RoleUser, Content: "hi"},
		}, nil)
		assert.Nil(t, req.System, "no system block: nothing to pin a cache breakpoint on")
	})

	t.Run("whitespace-only system param with no system messages", func(t *testing.T) {
		req := BuildRequest(cfg, " \n\t", []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, nil)
		assert.Nil(t, req.System, "the trim gate holds for the param too")
	})

	t.Run("real system message plus a whitespace-only one", func(t *testing.T) {
		want := BuildRequest(cfg, "", []llm.Message{
			{Role: llm.RoleSystem, Content: "extra rules"},
			{Role: llm.RoleUser, Content: "hi"},
		}, nil)
		got := BuildRequest(cfg, "", []llm.Message{
			{Role: llm.RoleSystem, Content: "extra rules"},
			{Role: llm.RoleSystem, Content: "  "},
			{Role: llm.RoleUser, Content: "hi"},
		}, nil)
		require.NotNil(t, got.System)
		require.Len(t, got.System, 1)
		assert.Equal(t, "extra rules", got.System[0].Text, "the blank message leaves no trace: no join byte, no content")
		require.NotNil(t, got.System[0].CacheControl, "the real content still carries the cache pin")

		wantBody, err := json.Marshal(want)
		require.NoError(t, err)
		gotBody, err := json.Marshal(got)
		require.NoError(t, err)
		assert.Equal(t, string(wantBody), string(gotBody), "byte-identical to the real message alone")
	})

	t.Run("real system param plus a whitespace-only message", func(t *testing.T) {
		req := BuildRequest(cfg, "base prompt", []llm.Message{
			{Role: llm.RoleSystem, Content: " \n\t"},
			{Role: llm.RoleUser, Content: "hi"},
		}, nil)
		require.NotNil(t, req.System)
		require.Len(t, req.System, 1)
		assert.Equal(t, "base prompt", req.System[0].Text, "no trailing join byte after the skipped message")
	})

	// The param + real-message join ("base prompt\nextra rules") is pinned
	// byte-exact by TestBuildRequestGoldenJSON_Differential below.
}

// Compact's failure modes: a non-200 with a JSON error envelope is unwrapped
// into an APIError, an empty content array is an explicit error, and a
// non-object body is a decode error — never a silent zero-value result.
func TestCompactErrorPaths_Negative(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		check   func(t *testing.T, res llm.CompactResult, err error)
	}{
		{
			name: "400 with json envelope",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"max_tokens is required"}}`))
			},
			check: func(t *testing.T, _ llm.CompactResult, err error) {
				require.Error(t, err)
				var apiErr *llm.APIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, 400, apiErr.Status)
				assert.Equal(t, "anthropic", apiErr.Provider)
				assert.Equal(t, "max_tokens is required", apiErr.Message, "the envelope message is unwrapped, not the raw JSON")
			},
		},
		{
			name: "200 with empty content",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"content":[],"stop_reason":"end_turn"}`))
			},
			check: func(t *testing.T, res llm.CompactResult, err error) {
				require.Error(t, err)
				assert.EqualError(t, err, "anthropic API error: empty response")
				assert.Empty(t, res.Text)
			},
		},
		{
			name: "200 with non-object body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`[]`))
			},
			check: func(t *testing.T, _ llm.CompactResult, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "cannot unmarshal array", "a malformed body is a decode error")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			res, err := Compact(
				t.Context(),
				srv.Client(),
				llm.ModelConfig{Name: "claude-sonnet-4", APIKey: "k", BaseURL: srv.URL},
				llm.CompactRequest{Prompt: "summarize"},
			)
			tt.check(t, res, err)
		})
	}
}

// ---------- differential ----------

// oracle: hand-written known-good JSON literal — the full Messages API body
// for a system + user + tool_use + tool_result round with one tool, written
// out byte for byte from the documented wire shape (field names, block types,
// cache_control placement). Key order is part of the contract because prefix
// caching bills byte drift.
func TestBuildRequestGoldenJSON_Differential(t *testing.T) {
	cfg := llm.ModelConfig{Name: "claude-sonnet-4", APIKey: "k"}
	tools := []llm.ToolDefinition{{
		Name:        "read",
		Description: "read a file",
		Params: &llm.FunctionParameters{
			Type:       "object",
			Properties: llm.Object{"path": llm.Object{"type": "string"}},
			Required:   []string{"path"},
		},
	}}

	t.Run("system messages merge into the system array", func(t *testing.T) {
		req := BuildRequest(cfg, "base prompt", []llm.Message{
			{Role: llm.RoleSystem, Content: "extra rules"},
			{Role: llm.RoleUser, Content: "hi"},
		}, nil)
		body, err := json.Marshal(req)
		require.NoError(t, err)
		assert.Equal(t, `{"model":"claude-sonnet-4","max_tokens":4096,`+
			`"system":[{"type":"text","text":"base prompt\nextra rules",`+
			`"cache_control":{"type":"ephemeral","ttl":"1h"}}],`+
			`"messages":[{"role":"user","content":[{"type":"text","text":"hi",`+
			`"cache_control":{"type":"ephemeral","ttl":"1h"}}]}],`+
			`"stream":true}`, string(body)) //nolint:testifylint // byte-exact golden; key order is cache-critical
	})

	t.Run("tool round with tail cache pin", func(t *testing.T) {
		req := BuildRequest(cfg, "You are terse.", []llm.Message{
			{Role: llm.RoleUser, Content: "hi"},
			{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{ID: "call_01", Function: llm.Function{Name: "read", Arguments: `{"path":"a.go"}`}},
				},
			},
			{Role: llm.RoleTool, ToolCallID: "call_01", Content: "ok"},
		}, tools)
		body, err := json.Marshal(req)
		require.NoError(t, err)
		assert.Equal(t, `{"model":"claude-sonnet-4","max_tokens":4096,`+
			`"system":[{"type":"text","text":"You are terse.","cache_control":{"type":"ephemeral","ttl":"1h"}}],`+
			`"messages":[`+
			`{"role":"user","content":[{"type":"text","text":"hi"}]},`+
			`{"role":"assistant","content":[{"type":"tool_use","id":"call_01","name":"read","input":{"path":"a.go"}}]},`+
			`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_01","content":"ok",`+
			`"cache_control":{"type":"ephemeral","ttl":"1h"}}]}`+
			`],"stream":true,`+
			`"tools":[{"name":"read","description":"read a file",`+
			`"input_schema":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]},`+
			`"cache_control":{"type":"ephemeral","ttl":"1h"}}]}`, string(body)) //nolint:testifylint // byte-exact golden
	})
}

// refAnthropicResult is what a naive reader of the Anthropic SSE contract
// expects from a stream: concatenated text and thinking, tool calls committed
// at content_block_stop with their fragmented arguments, and the disjoint
// usage buckets from message_start + message_delta.
type refAnthropicResult struct {
	textDeltas      []string
	thinkDeltas     []string
	toolArgProgress []string // arguments-so-far at each input_json_delta
	content         string
	reasoning       string
	tools           []llm.ToolCall
	prompt          int
	completion      int
	cacheRead       int
	cacheWrite      int
}

// refAnthropicStream re-derives the stream with a plain bufio line scanner
// and map-based encoding/json decoding — no typed envelopes, no shared code
// with the production parser.
func refAnthropicStream(t *testing.T, sse string) refAnthropicResult {
	t.Helper()
	var out refAnthropicResult
	var curName, curID string
	curIndex := -1
	haveTool := false
	var args strings.Builder

	for _, line := range strings.Split(sse, "\n") {
		payload, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		switch ev["type"] {
		case "message_start":
			usage := ev["message"].(map[string]any)["usage"].(map[string]any)
			out.prompt = int(usage["input_tokens"].(float64))
			out.cacheRead = int(usage["cache_read_input_tokens"].(float64))
			out.cacheWrite = int(usage["cache_creation_input_tokens"].(float64))
		case "content_block_start":
			block := ev["content_block"].(map[string]any)
			if block["type"] == "tool_use" {
				haveTool = true
				curIndex = int(ev["index"].(float64))
				curID = block["id"].(string)
				curName = block["name"].(string)
				args.Reset()
			}
		case "content_block_delta":
			delta := ev["delta"].(map[string]any)
			switch delta["type"] {
			case "text_delta":
				out.textDeltas = append(out.textDeltas, delta["text"].(string))
				out.content += delta["text"].(string)
			case "thinking_delta":
				out.thinkDeltas = append(out.thinkDeltas, delta["thinking"].(string))
				out.reasoning += delta["thinking"].(string)
			case "input_json_delta":
				if !haveTool {
					continue
				}
				args.WriteString(delta["partial_json"].(string))
				out.toolArgProgress = append(out.toolArgProgress, args.String())
			}
		case "content_block_stop":
			if haveTool && int(ev["index"].(float64)) == curIndex {
				out.tools = append(out.tools, llm.ToolCall{
					Index: curIndex, ID: curID, Type: "function",
					Function: llm.Function{Name: curName, Arguments: args.String()},
				})
				haveTool = false
				args.Reset()
			}
		case "message_delta":
			if usage, ok := ev["usage"].(map[string]any); ok {
				out.completion = int(usage["output_tokens"].(float64))
			}
		}
	}
	return out
}

// oracle: naive reference implementation in this test — the same SSE corpus
// is fed through a bufio line scanner and map-based encoding/json decoding,
// and the resulting text, reasoning, tool calls, argument-fragment progress,
// and usage buckets are compared event for event against processStream.
func TestProcessStreamVsNaiveSSEReference_Differential(t *testing.T) {
	sse := strings.Join([]string{
		`: keep-alive comment`,
		`event: message_start`,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":100,` +
			`"cache_read_input_tokens":40,"cache_creation_input_tokens":10}}}`,
		``,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"consider "}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"the options"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hel"}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"lo."}}`,
		`data: {"type":"content_block_stop","index":1}`,
		``,
		`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_a","name":"read"}}`,
		`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
		`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"a.go\"}"}}`,
		`data: {"type":"content_block_stop","index":2}`,
		``,
		`data: {"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_b","name":"grep"}}`,
		`data: {"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{\"q\":\"x\"}"}}`,
		`data: {"type":"content_block_stop","index":3}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":55}}`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	want := refAnthropicStream(t, sse)

	var textDeltas, thinkDeltas, toolArgProgress []string
	var done *llm.StreamEvent
	for _, ev := range processForTest(sse) {
		require.NotEqual(t, llm.StreamEventTypeError, ev.Type, "stream error: %s", ev.Err)
		switch ev.Type {
		case llm.StreamEventTypeDelta:
			if ev.Delta.Content != "" {
				textDeltas = append(textDeltas, ev.Delta.Content)
			}
			if ev.Delta.ReasoningContent != "" {
				thinkDeltas = append(thinkDeltas, ev.Delta.ReasoningContent)
			}
			for _, tc := range ev.Delta.ToolCalls {
				toolArgProgress = append(toolArgProgress, tc.Function.Arguments)
			}
		case llm.StreamEventTypeDone:
			done = &ev
		}
	}
	require.NotNil(t, done, "the reference corpus describes a complete turn")

	msg := done.Final
	require.NotNil(t, msg)
	assert.Equal(t, want.content, msg.Content, "final text")
	assert.Equal(t, want.reasoning, msg.ReasoningContent, "final reasoning")
	assert.Equal(t, want.textDeltas, textDeltas, "text deltas, in order")
	assert.Equal(t, want.thinkDeltas, thinkDeltas, "thinking deltas, in order")
	assert.Equal(t, want.toolArgProgress, toolArgProgress, "tool arguments accumulate fragment by fragment")
	assert.Equal(t, want.tools, msg.ToolCalls, "tool calls commit at content_block_stop with full arguments")

	assert.Equal(t, want.prompt, msg.Usage.PromptTokens, "prompt tokens")
	assert.Equal(t, want.completion, msg.Usage.CompletionTokens, "completion tokens")
	assert.Equal(t, want.cacheRead, msg.Usage.CachedTokens(), "cache-read bucket")
	assert.Equal(t, want.cacheWrite, msg.Usage.CacheWriteTokens(), "cache-write bucket")
	assert.Equal(t, want.prompt+want.completion+want.cacheRead+want.cacheWrite, msg.Usage.TotalTokens,
		"total is the sum of the disjoint buckets")
}

// findDone returns the Done event of a completed stream.
func findDone(t *testing.T, events []llm.StreamEvent) llm.StreamEvent {
	t.Helper()
	for _, ev := range events {
		if ev.Type == llm.StreamEventTypeDone {
			require.NotNil(t, ev.Final)
			return ev
		}
	}
	t.Fatal("no Done event in stream")
	return llm.StreamEvent{}
}
