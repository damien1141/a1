package configserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigModelHeadersRoundTrip pins the editor surface for
// models[].headers: GET surfaces the map, a POST with an explicit object
// writes it, an explicit empty object clears it, and a payload that omits
// the key entirely (a stale page from before the field existed) carries the
// file's headers — the apiKeys rule, applied to the new field.
func TestConfigModelHeadersRoundTrip(t *testing.T) {
	path := writeTestConfig(t, `models:
  - name: main
    api_key: sk-main
    headers:
      X-Provider: munito
    default: true
  - name: fresh
    api_key: sk-fresh
`)
	h := &Handler{ConfigPath: path}

	// GET surfaces headers so the editor can render them.
	get := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	get.Host = "127.0.0.1:8080"
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, get)
	require.Equal(t, http.StatusOK, getRec.Code)
	var doc struct {
		Models []struct {
			Name    string            `json:"name"`
			Headers map[string]string `json:"headers"`
		} `json:"models"`
	}
	require.NoError(t, json.NewDecoder(getRec.Body).Decode(&doc))
	require.Len(t, doc.Models, 2)
	assert.Equal(t, map[string]string{"X-Provider": "munito"}, doc.Models[0].Headers)
	assert.Nil(t, doc.Models[1].Headers, "a model without headers stays empty")

	// POST with an explicit map writes it; a renamed model arrives without
	// headers and starts clean.
	post := func(models []map[string]any) {
		t.Helper()
		body, err := json.Marshal(map[string]any{"models": models})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://127.0.0.1:8080")
		req.Host = "127.0.0.1:8080"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	read := func() string {
		t.Helper()
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		return string(data)
	}

	post([]map[string]any{
		{"name": "main", "apiKey": "sk-main", "headers": map[string]string{"X-Provider": "nano-gpt", "X-Tenant": "acme"}},
		{"name": "brand-new", "apiKey": "sk-new"},
	})
	text := read()
	assert.Contains(t, text, "nano-gpt", "an explicit headers map lands")
	assert.Contains(t, text, "acme")
	assert.NotContains(t, text, "munito", "the old value is replaced")

	// An explicit empty object clears ({} decodes non-nil, so the carry
	// does not resurrect the file's headers).
	post([]map[string]any{
		{"name": "main", "apiKey": "sk-main", "headers": map[string]string{}},
	})
	assert.NotContains(t, read(), "X-Provider", "an explicit empty object clears the headers")

	// A payload that omits the key entirely carries the file's headers —
	// the stale-editor-page case.
	path2 := writeTestConfig(t, `models:
  - name: main
    api_key: sk-main
    headers:
      X-Provider: munito
    default: true
`)
	h2 := &Handler{ConfigPath: path2}
	body, err := json.Marshal(map[string]any{
		"models": []map[string]any{{"name": "main", "apiKey": "sk-main-2"}},
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	h2.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	data, err := os.ReadFile(path2)
	require.NoError(t, err)
	assert.Contains(t, string(data), "munito", "an omitted headers key carries the file's headers")
	assert.Contains(t, string(data), "sk-main-2", "the edited api_key still lands")
}
