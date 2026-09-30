package browsertool

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBrowserProxyOnOff verifies the toggle form. "off" clears the proxy,
// "on" re-enables the last-used URL without retyping, and an explicit URL
// becomes the remembered one for the next "on".
func TestBrowserProxyOnOff(t *testing.T) {
	// Start clean.
	SetProxy("")
	// Seed a remembered URL as if the user had typed it before.
	browserMu.Lock()
	lastProxyURL = "http://127.0.0.1:8885"
	browserMu.Unlock()

	// off
	res, err := runBrowser(t.Context(), json.RawMessage(`{"action":"proxy","url":"off"}`))
	require.NoError(t, err)
	assert.Equal(t, "proxy off", res.Detail)
	assert.Equal(t, "", ProxyURL())

	// on — re-enables the remembered URL
	res, err = runBrowser(t.Context(), json.RawMessage(`{"action":"proxy","url":"on"}`))
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:8885", ProxyURL())
	assert.Contains(t, res.Content, "http://127.0.0.1:8885")

	// explicit URL becomes the new remembered one
	res, err = runBrowser(t.Context(), json.RawMessage(`{"action":"proxy","url":"http://example.com:8080"}`))
	require.NoError(t, err)
	assert.Equal(t, "http://example.com:8080", ProxyURL())

	// off then on uses the new remembered URL
	_, err = runBrowser(t.Context(), json.RawMessage(`{"action":"proxy","url":"off"}`))
	require.NoError(t, err)
	res, err = runBrowser(t.Context(), json.RawMessage(`{"action":"proxy","url":"on"}`))
	require.NoError(t, err)
	assert.Equal(t, "http://example.com:8080", ProxyURL())
	assert.Contains(t, res.Content, "http://example.com:8080")
}

// TestBrowserProxyOnWithNoURL reports clearly when there is nothing to
// re-enable, rather than silently enabling an empty proxy.
func TestBrowserProxyOnWithNoURL(t *testing.T) {
	SetProxy("")
	browserMu.Lock()
	lastProxyURL = ""
	browserMu.Unlock()

	res, err := runBrowser(t.Context(), json.RawMessage(`{"action":"proxy","url":"on"}`))
	require.NoError(t, err)
	assert.Equal(t, "", ProxyURL())
	assert.Contains(t, res.Content, "No proxy URL to enable")
	assert.Contains(t, res.Content, "browser.proxy")
}