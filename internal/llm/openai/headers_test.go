package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// captureServer records the headers of the one request it receives.
func captureServer(t *testing.T, response string) (*httptest.Server, func() http.Header) {
	t.Helper()
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return srv, func() http.Header { return got }
}

// streamWithHeaders drains StreamChatCompletion with the given extra headers.
func streamWithHeaders(t *testing.T, srv *httptest.Server, headers map[string]string) []captured {
	t.Helper()
	var events []captured
	for ev, err := range StreamChatCompletion(t.Context(), srv.Client(), srv.URL, "k", struct{}{}, headers) {
		c := captured{ev: ev}
		if err != nil {
			c.errText = err.Error()
		}
		events = append(events, c)
	}
	return events
}

// TestStreamChatCompletionExtraHeaders pins the models[].headers wire
// contract on the OpenAI-compatible route: configured headers ride on the
// streaming request and land after the built-ins, so they can add (the
// X-Provider routing case) or override (a gateway with its own auth
// flavor) — mirroring the OpenAI SDK's extra_headers.
func TestStreamChatCompletionExtraHeaders(t *testing.T) {
	srv, headers := captureServer(t, strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"ok"}}]}`,
		`data: [DONE]`,
		``,
	}, "\n"))

	events := streamWithHeaders(t, srv, map[string]string{
		"X-Provider":    "munito",
		"X-Tenant":      "tenant-42",
		"Authorization": "Bearer special",
	})
	for _, c := range events {
		assert.Empty(t, c.errText)
	}

	require.NotNil(t, headers(), "the request must reach the server")
	got := headers()
	assert.Equal(t, "munito", got.Get("X-Provider"))
	assert.Equal(t, "tenant-42", got.Get("X-Tenant"))
	assert.Equal(t, "Bearer special", got.Get("Authorization"),
		"a configured header overrides the built-in Authorization")
	assert.Equal(t, "application/json", got.Get("Content-Type"), "untouched defaults stay")
}

// TestCompactRequestExtraHeaders pins the same contract on the
// non-streaming compaction route, which builds its request from cfg.
func TestCompactRequestExtraHeaders(t *testing.T) {
	srv, headers := captureServer(t,
		`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)

	cfg := llm.ModelConfig{
		Name:    "z-ai/glm-5.3-flash",
		APIKey:  "k",
		BaseURL: srv.URL,
		Headers: map[string]string{"X-Provider": "munito"},
	}
	res, err := CompactRequest(t.Context(), srv.Client(), cfg, NewCompactRequest(cfg.Name, "summarize", 128))
	require.NoError(t, err)
	assert.Equal(t, "ok", res.Text)

	require.NotNil(t, headers())
	assert.Equal(t, "munito", headers().Get("X-Provider"))
}

// TestStreamChatCompletionNoHeaders keeps the header-free wire: nil
// headers must not add so much as an extra routing header.
func TestStreamChatCompletionNoHeaders(t *testing.T) {
	srv, headers := captureServer(t, "data: [DONE]\n")

	events := streamWithHeaders(t, srv, nil)
	require.NotEmpty(t, events)

	require.NotNil(t, headers())
	assert.Empty(t, headers().Get("X-Provider"))
	assert.Equal(t, "Bearer k", headers().Get("Authorization"))
}
