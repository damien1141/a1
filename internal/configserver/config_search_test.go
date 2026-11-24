package configserver_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/configserver"
)

const searchBaseConfig = `models:
  - name: gpt-4o
    api_key: sk-test
    base_url: http://localhost:8080
    default: true
`

// postConfig saves payload through the real handler, as the editor does.
func postConfig(t *testing.T, path string, payload map[string]any) {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	(&configserver.Handler{ConfigPath: path}).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// TestConfigSearchSurvivesUnrelatedSave_Negative pins the reported bug:
// `search:` had no counterpart in ConfigDoc, so the editor rewrote config.yaml
// from a document that never contained it. Editing ANY unrelated setting
// silently deleted a configured SearXNG URL.
func TestConfigSearchSurvivesUnrelatedSave_Negative(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(searchBaseConfig+`search:
  searxng_url: http://127.0.0.1:8888
`), 0o644))

	// The editor sends models only; search is not in the payload, so it must
	// ride on the file's existing value.
	postConfig(t, path, map[string]any{
		"models": []map[string]any{
			{"name": "gpt-4o", "apiKey": "sk-test", "default": true},
		},
	})

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(out), "searxng_url: http://127.0.0.1:8888",
		"the SearXNG URL must survive an unrelated save")
}

// TestConfigSearchWritableFromEditor_Positive: configurable means the editor
// can SET it, not merely avoid dropping it.
func TestConfigSearchWritableFromEditor_Positive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(searchBaseConfig), 0o644))

	postConfig(t, path, map[string]any{
		"models": []map[string]any{
			{"name": "gpt-4o", "apiKey": "sk-test", "default": true},
		},
		"search": map[string]any{"searxngUrl": "http://127.0.0.1:9999"},
	})

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(out), "searxng_url: http://127.0.0.1:9999",
		"the editor must be able to set the SearXNG URL")
}

// TestConfigSearchAbsentStaysAbsent_Negative: a config with no search block must
// not gain an empty one, or every save would add noise to the user's yaml.
func TestConfigSearchAbsentStaysAbsent_Negative(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(searchBaseConfig), 0o644))

	postConfig(t, path, map[string]any{
		"models": []map[string]any{
			{"name": "gpt-4o", "apiKey": "sk-test", "default": true},
		},
	})

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "search:", "an absent section must not be invented")
}
