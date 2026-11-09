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

// TestConfigSaveCarriesModelAPIKeys pins the per-model carry for api_keys:
// the editor form does not know the field, so a models-only save must keep
// the file's credential pool for each entry it still names — while an
// explicit empty array clears it, and a renamed (new) model starts clean.
func TestConfigSaveCarriesModelAPIKeys(t *testing.T) {
	path := writeTestConfig(t, `models:
  - name: gpt-4o
    api_key: sk-main
    api_keys:
      - sk-extra-1
      - sk-extra-2
    default: true
  - name: claude
    api_key: sk-claude
    api_keys:
      - sk-claude-backup
`)
	h := &Handler{ConfigPath: path}

	// The editor payload: same models, connection fields edited, no
	// apiKeys anywhere — exactly what the form posts.
	payload := map[string]any{
		"models": []map[string]any{
			{"name": "gpt-4o", "apiKey": "sk-main-2", "baseUrl": "https://api.openai.com/v1", "default": true},
			{"name": "claude", "apiKey": "sk-claude", "default": false},
		},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, "sk-extra-1", "the pool survives an editor save that never mentions it")
	assert.Contains(t, text, "sk-extra-2")
	assert.Contains(t, text, "sk-claude-backup", "every model's pool carries, not just the default")
	assert.Contains(t, text, "sk-main-2", "the edited api_key still lands")

	// GET surfaces the pool so a richer client can round-trip it.
	get := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	get.Host = "127.0.0.1:8080"
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, get)
	require.Equal(t, http.StatusOK, getRec.Code)
	var doc struct {
		Models []struct {
			Name    string   `json:"name"`
			APIKeys []string `json:"apiKeys"`
		} `json:"models"`
	}
	require.NoError(t, json.NewDecoder(getRec.Body).Decode(&doc))
	require.Len(t, doc.Models, 2)
	require.Equal(t, []string{"sk-extra-1", "sk-extra-2"}, doc.Models[0].APIKeys)

	// An explicit empty array clears the pool ("clear them" wins), and a
	// model the file did not have starts with none.
	payload = map[string]any{
		"models": []map[string]any{
			{"name": "gpt-4o", "apiKey": "sk-main-2", "default": true, "apiKeys": []string{}},
			{"name": "fresh", "apiKey": "sk-new", "default": false},
		},
	}
	body, err = json.Marshal(payload)
	require.NoError(t, err)
	req = httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	req.Host = "127.0.0.1:8080"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "sk-extra-1", "an explicit empty array clears the pool")
	assert.NotContains(t, string(data), "sk-claude-backup", "dropping the model drops its pool")
}
