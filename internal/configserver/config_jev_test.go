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

// postConfig drives one editor save through the handler.
func postConfig(t *testing.T, h *Handler, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestConfigJevRoundTrip pins the editor surface for the jev: section: a
// save writes the provider block to config.yaml and a GET hands it back
// in the document shape the form paints from.
func TestConfigJevRoundTrip(t *testing.T) {
	path := writeTestConfig(t, `models:
  - name: gpt-4o
    api_key: sk-test
    default: true
`)
	h := &Handler{ConfigPath: path}

	rec := postConfig(t, h, map[string]any{
		"models": []map[string]any{
			{"name": "gpt-4o", "apiKey": "sk-test", "default": true},
		},
		"jev": map[string]any{
			"provider":      "ollama",
			"model":         "qwen3:4b",
			"ollamaBaseUrl": "http://127.0.0.1:11500",
			"timeoutSec":    12,
		},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, "provider: ollama")
	assert.Contains(t, text, "model: qwen3:4b")
	assert.Contains(t, text, "ollama_base_url: http://127.0.0.1:11500")
	assert.Contains(t, text, "timeout_sec: 12")

	get := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	get.Host = "127.0.0.1:8080"
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, get)
	require.Equal(t, http.StatusOK, getRec.Code)
	var doc struct {
		Jev *struct {
			Provider      string `json:"provider"`
			Model         string `json:"model"`
			OllamaBaseURL string `json:"ollamaBaseUrl"`
			TimeoutSec    int    `json:"timeoutSec"`
		} `json:"jev"`
	}
	require.NoError(t, json.NewDecoder(getRec.Body).Decode(&doc))
	require.NotNil(t, doc.Jev)
	assert.Equal(t, "ollama", doc.Jev.Provider)
	assert.Equal(t, "qwen3:4b", doc.Jev.Model)
	assert.Equal(t, "http://127.0.0.1:11500", doc.Jev.OllamaBaseURL)
	assert.Equal(t, 12, doc.Jev.TimeoutSec)
}

// TestConfigJevProviderValidated: a typo'd provider is rejected at save
// time with a clear message; the file on disk is untouched.
func TestConfigJevProviderValidated(t *testing.T) {
	original := `models:
  - name: gpt-4o
    api_key: sk-test
    default: true
`
	path := writeTestConfig(t, original)
	h := &Handler{ConfigPath: path}

	rec := postConfig(t, h, map[string]any{
		"models": []map[string]any{
			{"name": "gpt-4o", "apiKey": "sk-test", "default": true},
		},
		"jev": map[string]any{"provider": "typo"},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "jev.provider")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, string(data), "a rejected save leaves the file untouched")
}

// TestConfigJevNegativeTimeoutRejected: timeout_sec must be positive when
// present.
func TestConfigJevNegativeTimeoutRejected(t *testing.T) {
	path := writeTestConfig(t, `models:
  - name: gpt-4o
    api_key: sk-test
    default: true
`)
	h := &Handler{ConfigPath: path}

	rec := postConfig(t, h, map[string]any{
		"models": []map[string]any{
			{"name": "gpt-4o", "apiKey": "sk-test", "default": true},
		},
		"jev": map[string]any{"provider": "api", "timeoutSec": -3},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "timeout_sec")
}

// TestConfigJevSurvivesEditorSaveFromDoc pins the editor's own carry
// behavior: a form that round-trips the document it loaded (jev comes
// from orig when the section is untouched) cannot wipe the section.
func TestConfigJevSurvivesEditorSaveFromDoc(t *testing.T) {
	path := writeTestConfig(t, `models:
  - name: gpt-4o
    api_key: sk-test
    default: true
jev:
  provider: api
  api_key: sk-jev
  model: jev-latest
`)
	h := &Handler{ConfigPath: path}

	// What the editor sends when the jev panel was never touched: the
	// section echoed back from the loaded document.
	rec := postConfig(t, h, map[string]any{
		"models": []map[string]any{
			{"name": "gpt-4o", "apiKey": "sk-test", "default": true},
		},
		"jev": map[string]any{
			"provider": "api",
			"apiKey":   "sk-jev",
			"model":    "jev-latest",
		},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "provider: api")
	assert.Contains(t, string(data), "sk-jev")
}
