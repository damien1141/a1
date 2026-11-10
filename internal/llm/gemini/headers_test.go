package gemini

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// TestCompactRequestExtraHeaders pins the models[].headers contract on the
// Gemini route: configured headers ride on the request and land after the
// built-ins, so gateway routing keys flow through unchanged.
func TestCompactRequestExtraHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`))
	}))
	t.Cleanup(srv.Close)

	cfg := llm.ModelConfig{
		Name:    "gemini-2.5",
		APIKey:  "k",
		BaseURL: srv.URL,
		Headers: map[string]string{"X-Provider": "munito"},
	}
	res, err := CompactRequest(t.Context(), srv.Client(), cfg, &GeminiRequest{})
	require.NoError(t, err)
	assert.Equal(t, "ok", res.Text)

	require.NotNil(t, got)
	assert.Equal(t, "munito", got.Get("X-Provider"))
	assert.Equal(t, "application/json", got.Get("Content-Type"), "untouched defaults stay")
}

// TestCompactRequestNoHeaders keeps the header-free wire byte-identical.
func TestCompactRequestNoHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`))
	}))
	t.Cleanup(srv.Close)

	cfg := llm.ModelConfig{Name: "gemini-2.5", APIKey: "k", BaseURL: srv.URL}
	_, err := CompactRequest(t.Context(), srv.Client(), cfg, &GeminiRequest{})
	require.NoError(t, err)

	require.NotNil(t, got)
	assert.Empty(t, got.Get("X-Provider"))
}
