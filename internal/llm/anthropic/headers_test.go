package anthropic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// TestMessagesRequestExtraHeaders pins the models[].headers contract on the
// Anthropic route: configured headers ride on every Messages request and
// land after the built-ins, so a gateway can receive its routing keys — or
// its own Anthropic-Version flavor — without touching other models.
func TestMessagesRequestExtraHeaders(t *testing.T) {
	cfg := llm.ModelConfig{
		Name:    "claude-sonnet-4-20250514",
		APIKey:  "k",
		BaseURL: "https://api.anthropic.com",
		Headers: map[string]string{
			"X-Provider":        "munito",
			"Anthropic-Version": "2024-01-01",
		},
	}
	req, err := newMessagesHTTPRequest(t.Context(), cfg, []byte(`{}`), true, "")
	require.NoError(t, err)

	assert.Equal(t, "munito", req.Header.Get("X-Provider"))
	assert.Equal(t, "2024-01-01", req.Header.Get("Anthropic-Version"),
		"a configured header overrides the built-in version pin")
	assert.Equal(t, "k", req.Header.Get("X-Api-Key"), "untouched defaults stay")
}

// TestMessagesRequestNoHeaders keeps the header-free wire byte-identical.
func TestMessagesRequestNoHeaders(t *testing.T) {
	cfg := llm.ModelConfig{Name: "claude-sonnet-4-20250514", APIKey: "k", BaseURL: "https://api.anthropic.com"}
	req, err := newMessagesHTTPRequest(t.Context(), cfg, []byte(`{}`), false, "")
	require.NoError(t, err)

	assert.Empty(t, req.Header.Get("X-Provider"))
	assert.Equal(t, apiVersion, req.Header.Get("Anthropic-Version"))
	require.NotEmpty(t, req.Header)
}
