package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/llm/openai"
)

func TestClientStreamAnthropicEndToEnd(t *testing.T) {
	var gotPath, gotKey, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("X-Api-Key")
		gotVersion = r.Header.Get("Anthropic-Version")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"type":"message_start","message":{"usage":{"input_tokens":3}}}`,
			"",
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
			"",
			`data: {"type":"message_delta","usage":{"output_tokens":2}}`,
			"",
			`data: {"type":"message_stop"}`,
			"",
		}, "\n")))
	}))
	defer srv.Close()

	client := NewClient(
		llm.ModelConfig{Name: "claude-sonnet-4-20250514", BaseURL: srv.URL, APIKey: "sk-test", API: llm.Anthropic},
		Hooks{},
		nil,
		"be brief",
	)
	events := collectEvents(client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hello"}}))

	require.Equal(t, "/v1/messages", gotPath)
	require.Equal(t, "sk-test", gotKey)
	require.NotEmpty(t, gotVersion, "expected Anthropic-Version header")
	var text strings.Builder
	var done *llm.StreamEvent
	for _, ev := range events {
		require.Empty(t, ev.Err, "stream error")
		switch ev.Type {
		case llm.StreamEventTypeDelta:
			text.WriteString(ev.Delta.Content)
		case llm.StreamEventTypeDone:
			done = &ev
		}
	}
	require.Equal(t, "hi", text.String())
	require.NotNil(t, done, "unexpected stream result")
	require.Equal(t, "hi", done.Final.Content)
	require.Equal(t, 5, done.Final.Usage.TotalTokens)
}

func TestClientStreamOpenAIEndToEnd(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"choices":[{"delta":{"role":"assistant","content":"he"}}]}`,
			"",
			`data: {"choices":[{"delta":{"content":"llo"}}]}`,
			"",
			`data: {"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n")))
	}))
	defer srv.Close()

	client := NewClient(llm.ModelConfig{Name: "gpt-4o", BaseURL: srv.URL, APIKey: "sk-test"}, Hooks{}, nil, "")
	events := collectEvents(client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hello"}}))

	require.Equal(t, "/chat/completions", gotPath)
	var text strings.Builder
	var done *llm.StreamEvent
	for _, ev := range events {
		require.Empty(t, ev.Err, "stream error")
		switch ev.Type {
		case llm.StreamEventTypeDelta:
			text.WriteString(ev.Delta.Content)
		case llm.StreamEventTypeDone:
			done = &ev
		}
	}
	require.Equal(t, "hello", text.String())
	require.NotNil(t, done, "unexpected stream result")
	require.Equal(t, "hello", done.Final.Content)
	require.Equal(t, 6, done.Final.Usage.TotalTokens)
}

func TestClientStreamOpenAIResponsesEndToEnd(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"hi"}`,
			"",
			`data: {"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}}`,
			"",
		}, "\n")))
	}))
	defer srv.Close()

	client := NewClient(
		llm.ModelConfig{Name: "gpt-5", BaseURL: srv.URL, APIKey: "sk-test", API: llm.OpenAIResponses},
		Hooks{},
		nil,
		"",
	)
	events := collectEvents(client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hello"}}))

	require.Equal(t, "/responses", gotPath)
	var text strings.Builder
	var done *llm.StreamEvent
	for _, ev := range events {
		require.Empty(t, ev.Err, "stream error")
		switch ev.Type {
		case llm.StreamEventTypeDelta:
			text.WriteString(ev.Delta.Content)
		case llm.StreamEventTypeDone:
			done = &ev
		}
	}
	require.Equal(t, "hi", text.String())
	require.NotNil(t, done)
	require.Equal(t, "hi", done.Final.Content)
	require.Equal(t, 4, done.Final.Usage.TotalTokens)
}

func TestClientCompactAnthropic(t *testing.T) {
	var gotPath string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"summary here"}],"stop_reason":"end_turn"}`))
	}))
	defer srv.Close()

	client := NewClient(
		llm.ModelConfig{Name: "claude-sonnet-4-20250514", BaseURL: srv.URL, APIKey: "sk-test", API: llm.Anthropic},
		Hooks{},
		nil,
		"",
	)
	res, err := client.Compact(t.Context(), llm.CompactRequest{Prompt: "summarize", MaxTokens: 13107})
	require.NoError(t, err)
	require.Equal(t, "/v1/messages", gotPath)
	require.Equal(t, "summary here", res.Text)
	require.False(t, res.Truncated)
	require.Equal(t, 13107, jsonField(t, body, "max_tokens"), "the summary cap must reach the provider")
}

func TestClientCompactOpenAI(t *testing.T) {
	var gotPath string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(
			[]byte(`{"choices":[{"message":{"role":"assistant","content":"summary here"},"finish_reason":"stop"}]}`),
		)
	}))
	defer srv.Close()

	client := NewClient(llm.ModelConfig{Name: "gpt-4o", BaseURL: srv.URL, APIKey: "sk-test"}, Hooks{}, nil, "")
	res, err := client.Compact(t.Context(), llm.CompactRequest{Prompt: "summarize", MaxTokens: 13107})
	require.NoError(t, err)
	require.Equal(t, "/chat/completions", gotPath)
	require.Equal(t, "summary here", res.Text)
	require.False(t, res.Truncated)
	require.Equal(t, 13107, jsonField(t, body, "max_tokens"))
}

// A provider that stopped at the cap returns partial text, and the caller must
// be able to tell it apart from a finished summary.
func TestClientCompactReportsCappedOutput(t *testing.T) {
	cases := []struct {
		name string
		api  llm.RouterType
		body string
	}{
		{
			name: "anthropic",
			api:  llm.Anthropic,
			body: `{"content":[{"type":"text","text":"partial"}],"stop_reason":"max_tokens"}`,
		},
		{
			name: "openai",
			api:  llm.OpenAI,
			body: `{"choices":[{"message":{"role":"assistant","content":"partial"},"finish_reason":"length"}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			client := NewClient(
				llm.ModelConfig{Name: "m", BaseURL: srv.URL, APIKey: "sk-test", API: tc.api},
				Hooks{},
				nil,
				"",
			)
			res, err := client.Compact(t.Context(), llm.CompactRequest{Prompt: "summarize", MaxTokens: 100})
			require.NoError(t, err)
			require.True(t, res.Truncated, "capped output must be reported as truncated")
			require.Equal(t, "partial", res.Text)
		})
	}
}

// jsonField reads one numeric field out of a captured request body.
func jsonField(t *testing.T, body []byte, field string) int {
	t.Helper()
	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	n, ok := raw[field].(float64)
	require.True(t, ok, "field %q missing from the request body", field)
	return int(n)
}

type openAIExtraThinking struct{}

func (openAIExtraThinking) Before(_ context.Context, req *openai.Request, _ llm.ModelConfig) error {
	req.ExtraBody = &openai.ExtraBody{Thinking: &openai.ThinkingConfig{Type: "enabled"}}
	return nil
}

func TestClientStreamOpenAIRunsInterceptor(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := NewClient(
		llm.ModelConfig{Name: "deepseek-flash", BaseURL: srv.URL, APIKey: "sk-test"},
		Hooks{OpenAI: openAIExtraThinking{}},
		nil,
		"",
	)
	for _, err := range client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hi"}}) {
		require.NoError(t, err)
	}
	require.Contains(t, string(body), `"extra_body"`)
	require.Contains(t, string(body), `"thinking"`)
}

type rejectHook struct{}

func (rejectHook) Before(context.Context, *openai.Request, llm.ModelConfig) error {
	return errors.New("blocked by hook")
}

func TestClientStreamOpenAIInterceptorError(t *testing.T) {
	client := NewClient(
		llm.ModelConfig{Name: "gpt-4o", BaseURL: "http://127.0.0.1:9", APIKey: "sk-test"},
		Hooks{OpenAI: rejectHook{}},
		nil,
		"",
	)
	events := collectEvents(client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hi"}}))
	require.Len(t, events, 1)
	require.Equal(t, llm.StreamEventTypeError, events[0].Type)
	require.Contains(t, events[0].Err, "blocked by hook")
}

// A session can hold tool calls whose arguments arrived malformed — a stream
// cut mid-arguments leaves a truncated prefix. Replayed verbatim those are
// invalid JSON on the request and a strict gateway answers a bare 400 no
// recovery loop can diagnose. Every request must leave the client with
// parseable arguments; the session slice itself is never rewritten.
func TestClientStreamRepairsMalformedToolCallArguments(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	history := []llm.Message{
		{Role: llm.RoleUser, Content: "rewrite the readme"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "c1", Function: llm.Function{Name: "write", Arguments: `{"path": "/tmp/a"`}},  // truncated mid-stream
			{ID: "c2", Function: llm.Function{Name: "write", Arguments: `{"path": "/tmp/b"}`}}, // healthy
		}},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: "refused: malformed arguments"},
		{Role: llm.RoleTool, ToolCallID: "c2", Content: "ok"},
		{Role: llm.RoleUser, Content: "continue"},
	}

	client := NewClient(llm.ModelConfig{Name: "gpt-4o", BaseURL: srv.URL, APIKey: "sk-test"}, Hooks{}, nil, "")
	for _, err := range client.Stream(t.Context(), history) {
		require.NoError(t, err)
	}

	var req struct {
		Messages []struct {
			ToolCalls []struct {
				Function struct {
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(body, &req))
	var args []string
	for _, m := range req.Messages {
		for _, tc := range m.ToolCalls {
			args = append(args, tc.Function.Arguments)
		}
	}
	require.Len(t, args, 2, "both tool calls ride the wire")
	for _, a := range args {
		require.True(t, json.Valid([]byte(a)), "wire arguments must be valid JSON, got %q", a)
	}
	require.Equal(t, "{}", args[0], "the truncated call is repaired on the wire")
	require.Equal(t, `{"path": "/tmp/b"}`, args[1], "the healthy call rides verbatim")

	require.Equal(t, `{"path": "/tmp/a"`,
		history[1].ToolCalls[0].Function.Arguments,
		"the caller's history is never rewritten by the repair")
}

// collectEvents drains an iter.Seq2 into a slice.
func collectEvents(seq func(func(llm.StreamEvent, error) bool)) []llm.StreamEvent {
	var events []llm.StreamEvent
	for ev, err := range seq {
		if err != nil {
			events = append(events, llm.StreamEvent{Type: llm.StreamEventTypeError, Err: err.Error()})
			continue
		}
		events = append(events, ev)
	}
	return events
}

// captureTransport records the request body and answers with a canned SSE
// stream, so wire-shape tests can target the real api.openai.com host
// without any network leaving the process.
type captureTransport struct {
	body []byte
	sse  string
}

func (ct *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	ct.body = body
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(ct.sse)),
		Request:    req,
	}, nil
}

const chatSSEFixture = "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"

const responsesSSEFixture = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":3,\"output_tokens\":1,\"total_tokens\":4}}}}\n\n"

// A client with a pinned scope derives the content-addressed
// prompt_cache_key on the wire when — and only when — the endpoint is
// OpenAI's own.
func TestClientStreamSendsPromptCacheKeyOnOpenAIHost(t *testing.T) {
	ct := &captureTransport{sse: chatSSEFixture}
	toolDefs := []llm.ToolDefinition{{Name: "count", Description: "count things"}}
	client := NewClient(
		llm.ModelConfig{Name: "gpt-4o", BaseURL: "https://api.openai.com/v1", APIKey: "sk-test"},
		Hooks{},
		toolDefs,
		"be brief",
	).SetCacheScope("schedule:job-7")
	client.httpClient = &http.Client{Transport: ct}

	for _, err := range client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hi"}}) {
		require.NoError(t, err)
	}

	var req struct {
		PromptCacheKey string `json:"prompt_cache_key"`
	}
	require.NoError(t, json.Unmarshal(ct.body, &req))
	require.Equal(t, llm.PromptCacheKey("schedule:job-7", "be brief", toolDefs), req.PromptCacheKey)
}

// Every other host — strict compat endpoints, aggregators, local servers —
// keeps the exact pre-scope request bytes.
func TestClientStreamOmitsPromptCacheKeyOffOpenAIHost(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := NewClient(
		llm.ModelConfig{Name: "gpt-4o", BaseURL: srv.URL, APIKey: "sk-test"},
		Hooks{},
		nil,
		"be brief",
	).SetCacheScope("schedule:job-7")
	for _, err := range client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hi"}}) {
		require.NoError(t, err)
	}
	require.NotContains(t, string(body), "prompt_cache_key", "non-OpenAI hosts keep the pre-scope body")
}

// The chat-completions and Responses routes of one engine hash the same
// normalized tool surface, so both derive the same key and share the bucket
// across modes.
func TestClientStreamResponsesCacheKeyMatchesChatRoute(t *testing.T) {
	ct := &captureTransport{sse: responsesSSEFixture}
	toolDefs := []llm.ToolDefinition{{Name: "count", Description: "count things"}}
	client := NewClient(
		llm.ModelConfig{Name: "gpt-5", BaseURL: "https://api.openai.com/v1", APIKey: "sk-test", API: llm.OpenAIResponses},
		Hooks{},
		toolDefs,
		"be brief",
	).SetCacheScope("schedule:job-7")
	client.httpClient = &http.Client{Transport: ct}

	for _, err := range client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hi"}}) {
		require.NoError(t, err)
	}

	var req struct {
		PromptCacheKey string `json:"prompt_cache_key"`
	}
	require.NoError(t, json.Unmarshal(ct.body, &req))
	require.Equal(t, llm.PromptCacheKey("schedule:job-7", "be brief", toolDefs), req.PromptCacheKey,
		"chat and Responses routes of one engine share the bucket")
}

// No scope pinned — not even api.openai.com gets a key: the wire is exactly
// what it was before scopes existed.
func TestClientStreamNoScopeKeepsPreScopeWire(t *testing.T) {
	ct := &captureTransport{sse: chatSSEFixture}
	client := NewClient(
		llm.ModelConfig{Name: "gpt-4o", BaseURL: "https://api.openai.com/v1", APIKey: "sk-test"},
		Hooks{},
		nil,
		"be brief",
	)
	client.httpClient = &http.Client{Transport: ct}

	for _, err := range client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hi"}}) {
		require.NoError(t, err)
	}
	require.NotContains(t, string(ct.body), "prompt_cache_key", "no scope = the pre-scope wire")
}
