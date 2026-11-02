package configserver

import (
        "bytes"
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

func writeTestConfig(t *testing.T, text string) string {
        t.Helper()
        path := filepath.Join(t.TempDir(), "config.yaml")
        require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
        return path
}

func TestConfigSaveCarriesOverToolsBudget(t *testing.T) {
        path := writeTestConfig(t, `models:
  - name: gpt-4o
    api_key: sk-test
    default: true
tools:
  max_tool_calls: 25
`)
        h := &Handler{ConfigPath: path}

        // The editor page posts its form fields only — no tools block. The save
        // must carry the existing budget over instead of wiping it.
        payload := map[string]any{
                "models": []map[string]any{
                        {"name": "gpt-4o", "apiKey": "sk-test", "default": true},
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
        assert.Contains(t, string(data), "max_tool_calls: 25", "budget survives an editor save")

        // GET surfaces the doc so a future editor form can show it.
        get := httptest.NewRequest(http.MethodGet, "/api/config", nil)
        get.Host = "127.0.0.1:8080"
        getRec := httptest.NewRecorder()
        h.ServeHTTP(getRec, get)
        require.Equal(t, http.StatusOK, getRec.Code)
        assert.Contains(t, getRec.Body.String(), `"maxToolCalls":25`)
}

func TestConfigSaveExplicitToolsWins(t *testing.T) {
        path := writeTestConfig(t, `models:
  - name: gpt-4o
    api_key: sk-test
tools:
  max_tool_calls: 25
`)
        h := &Handler{ConfigPath: path}

        payload := map[string]any{
                "models": []map[string]any{
                        {"name": "gpt-4o", "apiKey": "sk-test", "default": true},
                },
                "tools": map[string]any{"maxToolCalls": 100},
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
        assert.True(t, strings.Contains(string(data), "max_tool_calls: 100"), "explicit payload value wins: %s", data)
}
