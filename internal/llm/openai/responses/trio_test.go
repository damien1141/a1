package responses

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

// ---------- negative ----------

// streamStep is one yielded stream step as the consumer actually sees it: the
// event plus the error text when the step carried one. Unlike
// runResponsesStream this preserves the raw shape instead of normalizing iter
// errors into typed events.
type streamStep struct {
	ev      llm.StreamEvent
	errText string
}

// A connection that dies mid-body is terminal: the transport failure is
// yielded as the last step, with no Done event for the half-delivered answer.
// The shape matches the other providers: the event itself is error-typed (for
// consumers branching on ev.Type) and the iter error carries the same failure
// — the chat-completions path pins the identical contract.
func TestStreamConnectionDiedMidFlight_Negative(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Promise more bytes than will ever arrive, so the client read fails
		// instead of seeing a clean EOF.
		w.Header().Set("Content-Length", "4096")
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"))
	}))
	t.Cleanup(srv.Close)

	cfg := llm.ModelConfig{Name: "gpt-5", BaseURL: srv.URL, APIKey: "k"}
	req := BuildRequest(cfg, "", []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, nil)

	var steps []streamStep
	for ev, err := range Stream(t.Context(), srv.Client(), cfg, req) {
		s := streamStep{ev: ev}
		if err != nil {
			s.errText = err.Error()
		}
		steps = append(steps, s)
	}
	require.NotEmpty(t, steps)

	last := steps[len(steps)-1]
	require.NotEmpty(t, last.errText, "the dropped connection must surface as an error")
	assert.Contains(t, last.errText, "SSE stream error", "scanner failures are wrapped for context")
	assert.Equal(t, llm.StreamEventTypeError, last.ev.Type, "the error step is an error-typed event, matching anthropic and gemini")
	assert.Equal(t, last.errText, last.ev.Err, "the event text and the iter error describe the same failure")
	for _, s := range steps[:len(steps)-1] {
		assert.Empty(t, s.errText)
		assert.NotEqual(t, llm.StreamEventTypeDone, s.ev.Type, "no Done event may stand next to a mid-stream failure")
	}
	assert.Equal(t, llm.StreamEventTypeDelta, steps[0].ev.Type)
}

// Provider-signaled deaths ("error" / "response.failed" events the API sends
// mid-stream) are the same class of failure as a dropped connection: the
// terminal step is error-typed so ev.Type consumers see it, the iter error
// carries the provider's message, and no Done event stands next to it.
func TestStreamProviderSignaledError_Negative(t *testing.T) {
	cases := []struct {
		name    string
		sse     string
		wantMsg string
	}{
		{
			name:    "error event with code and message",
			sse:     "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\ndata: {\"type\":\"error\",\"code\":\"rate_limited\",\"message\":\"too many requests\"}\n\n",
			wantMsg: "Error Code rate_limited: too many requests",
		},
		{
			name:    "error event without code",
			sse:     "data: {\"type\":\"error\",\"message\":\"boom\"}\n\n",
			wantMsg: "boom",
		},
		{
			name:    "response.failed with error details",
			sse:     "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\",\"message\":\"upstream died\"}}}\n\n",
			wantMsg: "server_error: upstream died",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(tc.sse))
			}))
			t.Cleanup(srv.Close)

			cfg := llm.ModelConfig{Name: "gpt-5", BaseURL: srv.URL, APIKey: "k"}
			req := BuildRequest(cfg, "", []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, nil)

			var steps []streamStep
			for ev, err := range Stream(t.Context(), srv.Client(), cfg, req) {
				s := streamStep{ev: ev}
				if err != nil {
					s.errText = err.Error()
				}
				steps = append(steps, s)
			}
			require.NotEmpty(t, steps)

			last := steps[len(steps)-1]
			require.NotEmpty(t, last.errText, "the provider error must surface as an iter error")
			assert.Equal(t, tc.wantMsg, last.errText)
			assert.Equal(t, llm.StreamEventTypeError, last.ev.Type, "the terminal step is error-typed, matching anthropic and gemini")
			assert.Equal(t, tc.wantMsg, last.ev.Err, "the event text and the iter error describe the same failure")
			for _, s := range steps[:len(steps)-1] {
				assert.Empty(t, s.errText)
				assert.NotEqual(t, llm.StreamEventTypeDone, s.ev.Type, "no Done event may stand next to a provider error")
			}
		})
	}
}

// ---------- differential ----------

// runResponsesStream drains processStream into a slice, mirroring the gemini
// package's test helper.
func runResponsesStream(sse string) []llm.StreamEvent {
	var events []llm.StreamEvent
	processStream(strings.NewReader(sse), func(ev llm.StreamEvent, err error) bool {
		if err != nil {
			events = append(events, llm.StreamEvent{Type: llm.StreamEventTypeError, Err: err.Error()})
			return false
		}
		events = append(events, ev)
		return true
	})
	return events
}

// oracle: hand-written golden literal — the Responses create body BuildRequest
// produces (developer role for thinking models, mid-conversation system text
// folded up front, data-URL images, call_id suffix stripping, tool schemas) is
// compared against JSON written by hand from the Responses API shapes.
func TestBuildRequest_WireBodyVsGoldenLiteral_Differential(t *testing.T) {
	cfg := llm.ModelConfig{Name: "gpt-5-test", Think: llm.ThinkConfig{Enabled: true, Mode: llm.Medium}}
	tools := []llm.ToolDefinition{
		{
			Name:        "bash",
			Description: "run a command",
			Params: &llm.FunctionParameters{
				Type:       "object",
				Properties: llm.Object{"cmd": map[string]any{"type": "string"}},
				Required:   []string{"cmd"},
			},
		},
		{Name: "ping", Description: "ping the host"}, // nil params stay off the wire
	}
	messages := []llm.Message{
		// Message index 0: user with an image.
		{Role: llm.RoleUser, Content: "hi", Images: []llm.Image{{Data: "QUJD", MimeType: "image/png"}}},
		// Message index 1: mid-conversation system text, folded into the leading item.
		{Role: llm.RoleSystem, Content: "session memory"},
		// Message index 2: assistant text + tool call with a "call_id|item_id" composite.
		{
			Role:    llm.RoleAssistant,
			Content: "running it",
			ToolCalls: []llm.ToolCall{{
				ID:       "call_1|fc_9",
				Type:     "function",
				Function: llm.Function{Name: "bash", Arguments: `{"cmd":"ls"}`},
			}},
		},
		// Message index 3: the tool result.
		{Role: llm.RoleTool, ToolCallID: "call_1|fc_9", Content: "file one\nfile two"},
	}

	body, err := json.Marshal(BuildRequest(cfg, "be brief", messages, tools))
	require.NoError(t, err)

	const golden = `{
                "model": "gpt-5-test",
                "input": [
                        {"role": "developer", "content": "be brief\n\nsession memory"},
                        {"role": "user", "content": [
                                {"type": "input_text", "text": "hi"},
                                {"type": "input_image", "detail": "auto", "image_url": "data:image/png;base64,QUJD"}
                        ]},
                        {"type": "message", "role": "assistant", "status": "completed", "id": "msg_phi_2", "content": [
                                {"type": "output_text", "text": "running it"}
                        ]},
                        {"type": "function_call", "call_id": "call_1", "name": "bash", "arguments": "{\"cmd\":\"ls\"}"},
                        {"type": "function_call_output", "call_id": "call_1", "output": "file one\nfile two"}
                ],
                "tools": [
                        {"type": "function", "name": "bash", "description": "run a command", "parameters": {"type": "object", "properties": {"cmd": {"type": "string"}}, "required": ["cmd"]}, "strict": false},
                        {"type": "function", "name": "ping", "description": "ping the host", "strict": false}
                ],
                "stream": true,
                "store": false,
                "reasoning": {"effort": "medium", "summary": "auto"}
        }`
	assert.JSONEq(t, golden, string(body), "wire body must equal the hand-written Responses shape")
}

type naiveResponsesTurn struct {
	text      string
	reasoning string
	toolCalls []llm.ToolCall
	usage     llm.Usage
}

// naiveResponsesStream re-derives the final turn from an SSE corpus with the
// standard library alone: hand-split lines, generic map decodes, and plain
// accumulation of the documented event semantics. Deliberately independent of
// processStream's typed switch.
func naiveResponsesStream(t *testing.T, sse string) naiveResponsesTurn {
	t.Helper()
	var turn naiveResponsesTurn
	active := -1
	for _, line := range strings.Split(sse, "\n") {
		payload, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		payload = strings.TrimSpace(payload)
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue // same tolerance as the provider: junk lines are skipped
		}
		typ, _ := ev["type"].(string)
		delta, _ := ev["delta"].(string)
		switch typ {
		case "response.output_text.delta", "response.refusal.delta":
			if delta != "" {
				turn.text += delta
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			if delta != "" {
				turn.reasoning += delta
			}
		case "response.reasoning_summary_part.done":
			turn.reasoning += "\n\n"
		case "response.output_item.added":
			item, _ := ev["item"].(map[string]any)
			if item == nil || item["type"] != "function_call" {
				continue
			}
			id, _ := item["call_id"].(string)
			if itemID, _ := item["id"].(string); itemID != "" {
				id += "|" + itemID
			}
			name, _ := item["name"].(string)
			args, _ := item["arguments"].(string)
			tc := llm.ToolCall{
				Index:    len(turn.toolCalls),
				ID:       id,
				Type:     "function",
				Function: llm.Function{Name: name, Arguments: args},
			}
			turn.toolCalls = append(turn.toolCalls, tc)
			active = tc.Index
		case "response.function_call_arguments.delta":
			if active >= 0 && active < len(turn.toolCalls) && delta != "" {
				turn.toolCalls[active].Function.Arguments += delta
			}
		case "response.function_call_arguments.done":
			if args, _ := ev["arguments"].(string); args != "" && active >= 0 && active < len(turn.toolCalls) {
				turn.toolCalls[active].Function.Arguments = args
			}
		case "response.completed":
			resp, _ := ev["response"].(map[string]any)
			if resp == nil {
				continue
			}
			u, _ := resp["usage"].(map[string]any)
			if u == nil {
				continue
			}
			num := func(key string) int {
				f, _ := u[key].(float64)
				return int(f)
			}
			cached := 0
			if d, _ := u["input_tokens_details"].(map[string]any); d != nil {
				f, _ := d["cached_tokens"].(float64)
				cached = int(f)
			}
			turn.usage = llm.Usage{
				PromptTokens:     max(num("input_tokens")-cached, 0),
				CompletionTokens: num("output_tokens"),
				TotalTokens:      num("total_tokens"),
			}
			if cached > 0 {
				turn.usage.PromptTokensDetails = &llm.PromptTokensDetails{CachedTokens: cached}
			}
		}
	}
	turn.reasoning = strings.TrimSpace(turn.reasoning)
	return turn
}

// oracle: naive reference implementation in this test — the event-stream
// corpus is re-decoded with strings.Split + encoding/json into generic maps,
// and the accumulated text, reasoning, tool calls, and usage buckets are
// compared against processStream's typed parse of the same corpus.
func TestProcessStream_VsNaiveJSONDecode_Differential(t *testing.T) {
	corpus := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"he"}`,
		``,
		`event: ping`,
		`data: {"type":"response.reasoning_text.delta","delta":"thinking "}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"llo"}`,
		``,
		`data: {"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"bash","arguments":""}}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","delta":"{\"cmd\""}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","delta":":\"ls"}`,
		``,
		`data: {"type":"response.reasoning_summary_part.done"}`,
		``,
		`data: {"type":"response.function_call_arguments.done","arguments":"{\"cmd\":\"ls\"}"}`,
		``,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15,"input_tokens_details":{"cached_tokens":2}}}}`,
		``,
		`data: [DONE]`,
		``,
		`data: {"type":"response.output_text.delta","delta":""}`,
		``,
		`data: {"type":"unknown.event","x":1}`,
		``,
	}, "\n")

	want := naiveResponsesStream(t, corpus)
	require.Equal(t, "hello", want.text, "naive reference sanity: accumulated text")
	require.Equal(t, "thinking", want.reasoning, "naive reference sanity: reasoning is trimmed")
	require.Len(t, want.toolCalls, 1, "naive reference sanity: one tool call")
	require.Equal(t, 8, want.usage.PromptTokens, "naive reference sanity: uncached input tokens")

	var (
		deltaText      strings.Builder
		deltaReasoning strings.Builder
		done           *llm.StreamEvent
	)
	for _, ev := range runResponsesStream(corpus) {
		require.NotEqual(t, llm.StreamEventTypeError, ev.Type, "stream error: %s", ev.Err)
		switch ev.Type {
		case llm.StreamEventTypeDelta:
			deltaText.WriteString(ev.Delta.Content)
			deltaReasoning.WriteString(ev.Delta.ReasoningContent)
		case llm.StreamEventTypeDone:
			done = &ev
		}
	}
	require.NotNil(t, done, "stream must end with a done event")
	final := done.Final
	require.NotNil(t, final)

	assert.Equal(t, want.text, final.Content, "final text must equal the naive decode")
	assert.Equal(t, want.reasoning, final.ReasoningContent, "final reasoning must equal the naive decode")
	assert.Equal(t, want.toolCalls, final.ToolCalls, "final tool calls must equal the naive decode")
	assert.Equal(t, want.usage, final.Usage, "usage buckets must equal the naive decode")
	assert.Equal(t, want.text, deltaText.String(), "content deltas must concatenate to the same text")
	// The trailing summary separator is trimmed for the final message but the
	// deltas stream it verbatim, so only the trimmed prefix is comparable.
	assert.True(t, strings.HasPrefix(deltaReasoning.String(), want.reasoning), "reasoning deltas must carry the naive decode's text")
}
