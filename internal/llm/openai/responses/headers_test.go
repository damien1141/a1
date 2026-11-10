package responses

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequestExtraHeaders pins the models[].headers contract on the
// OpenAI Responses route: configured headers land after the built-ins so
// they can add or override.
func TestRequestExtraHeaders(t *testing.T) {
	req, err := newHTTPRequest(t.Context(), "https://example.com/v1/responses", "k", []byte(`{}`), true, map[string]string{
		"X-Provider":   "munito",
		"X-Beta-Flags": "responses-v1",
	})
	require.NoError(t, err)

	assert.Equal(t, "munito", req.Header.Get("X-Provider"))
	assert.Equal(t, "responses-v1", req.Header.Get("X-Beta-Flags"))
	assert.Equal(t, "Bearer k", req.Header.Get("Authorization"), "untouched defaults stay")
}

// TestRequestNoHeaders keeps the header-free wire byte-identical.
func TestRequestNoHeaders(t *testing.T) {
	req, err := newHTTPRequest(t.Context(), "https://example.com/v1/responses", "k", []byte(`{}`), false, nil)
	require.NoError(t, err)

	assert.Empty(t, req.Header.Get("X-Provider"))
	assert.Equal(t, "Bearer k", req.Header.Get("Authorization"))
}
