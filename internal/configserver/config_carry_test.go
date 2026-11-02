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

// TestConfigSaveCarriesChecksAndSchedules pins the round-trip protection
// for the two config sections that have no editor form: an editor save
// (which posts only its own fields) must not wipe config-defined evidence
// checks or Clock schedules from config.yaml.
func TestConfigSaveCarriesChecksAndSchedules(t *testing.T) {
	path := writeTestConfig(t, `models:
  - name: gpt-4o
    api_key: sk-test
    default: true
checks:
  - name: lint
    command: "make lint"
  - name: health
    command: "curl -s localhost:3000/health"
    pattern: '"status":"ok"'
schedules:
  - id: nightly-report
    prompt: summarize today
    cron: "0 21 * * *"
`)
	h := &Handler{ConfigPath: path}

	// The editor payload carries models only — checks/schedules/tools all
	// ride on the file's existing values.
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
	text := string(data)
	assert.Contains(t, text, "name: lint", "evidence checks survive an editor save")
	assert.Contains(t, text, "command: make lint", "check commands round-trip")
	assert.Contains(t, text, "pattern", "check patterns round-trip")
	assert.Contains(t, text, "nightly-report", "schedules survive an editor save")
	assert.Contains(t, text, "0 21 * * *", "cron expressions round-trip")

	// GET surfaces both sections so the client can keep passing them back.
	get := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	get.Host = "127.0.0.1:8080"
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, get)
	require.Equal(t, http.StatusOK, getRec.Code)
	var doc struct {
		Checks []map[string]any `json:"checks"`
		Schedules []map[string]any `json:"schedules"`
	}
	require.NoError(t, json.NewDecoder(getRec.Body).Decode(&doc))
	require.Len(t, doc.Checks, 2)
	require.Len(t, doc.Schedules, 1)
}
