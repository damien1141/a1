package webserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/project"
)

// sessions_test.go pins DELETE /api/session: the file layout loses the
// jsonl, the pin list loses the id, the engine cache is evicted, and an
// unknown id is a clean 404 rather than a silent "success".

// newSessionsFixture builds the same hand-wired server as the turns tests
// (no LLM needed — sessions here are written directly to disk).
func newSessionsFixture(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cfgYAML := "models:\n  - name: test-model\n    api: openai\n    api_key: test-key\n    context_window: 128000\n    default: true\n"
	cfgDir := filepath.Join(home, ".a1")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o600))

	proj, err := project.Discover(root)
	require.NoError(t, err)
	require.NoError(t, proj.LoadConfig())

	srv := &Server{
		proj:  proj,
		modes: newModeState(),
		pins:  newPinnedSessions(t.TempDir()),
	}
	srv.mu.Lock()
	srv.hub = newHub()
	srv.engines = map[string]*liveEngine{}
	srv.pending = map[string]chan permission.AskResult{}
	srv.mu.Unlock()
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return srv, ts, proj.SessionDir()
}

// writeSessionFile creates a minimal persisted session (header + one user
// message) the way the manager would have, and returns its path.
func writeSessionFile(t *testing.T, dir, id, prompt string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, fmt.Sprintf("20250101T00-00-00_%s.jsonl", id))
	lines := fmt.Sprintf("{\"type\":\"EntrySession\",\"id\":%q,\"timestamp\":\"2025-01-01T00:00:00\",\"cwd\":\"/tmp\"}\n"+
		"{\"type\":\"EntryMessage\",\"id\":\"e1\",\"parentID\":null,\"timestamp\":\"2025-01-01T00:00:00Z\","+
		"\"message\":{\"role\":\"user\",\"content\":%q}}\n", id, prompt)
	require.NoError(t, os.WriteFile(path, []byte(lines), 0o600))
	return path
}

// listSessionIDs scrapes the id attribute off the sidebar fragment — the
// same document the browser parses.
func listSessionIDs(t *testing.T, ts *httptest.Server) map[string]bool {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/sessions")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	out := map[string]bool{}
	for _, part := range strings.Split(string(body), `data-session-id="`) {
		if idx := strings.IndexByte(part, '"'); idx > 0 {
			out[part[:idx]] = true
		}
	}
	return out
}

func deleteSessionCall(t *testing.T, ts *httptest.Server, id string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/session?id="+id, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func TestDeleteSessionRemovesFilePinAndEngine(t *testing.T) {
	srv, ts, sessionDir := newSessionsFixture(t)
	path := writeSessionFile(t, sessionDir, "sess_delete_me", "hello there")

	// Pin it so the unpin path is exercised too.
	resp, err := http.Post(ts.URL+"/api/pin", "application/json",
		strings.NewReader(`{"sessionId":"sess_delete_me"}`))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Load it into the engine cache via the transcript endpoint (liveFor).
	get, err := http.Get(ts.URL + "/api/transcript/messages?id=sess_delete_me")
	require.NoError(t, err)
	require.NoError(t, get.Body.Close())
	require.Equal(t, http.StatusOK, get.StatusCode)
	srv.mu.Lock()
	_, cached := srv.engines["sess_delete_me"]
	srv.mu.Unlock()
	require.True(t, cached, "transcript load cached the engine")

	// DELETE.
	del := deleteSessionCall(t, ts, "sess_delete_me")
	defer func() { _ = del.Body.Close() }()
	body, err := io.ReadAll(del.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, del.StatusCode, string(body))

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "jsonl removed from disk")

	srv.mu.Lock()
	_, cached = srv.engines["sess_delete_me"]
	srv.mu.Unlock()
	assert.False(t, cached, "live engine evicted from the cache")

	// Pin list: GET /api/pins no longer contains the id.
	pinsResp, err := http.Get(ts.URL + "/api/pins")
	require.NoError(t, err)
	defer func() { _ = pinsResp.Body.Close() }()
	var pins struct {
		Pins []string `json:"pins"`
	}
	require.NoError(t, json.NewDecoder(pinsResp.Body).Decode(&pins))
	assert.NotContains(t, pins.Pins, "sess_delete_me", "delete unpins the session")

	// The listing no longer shows the row.
	assert.False(t, listSessionIDs(t, ts)["sess_delete_me"], "sidebar listing drops the deleted session")
}

func TestDeleteSessionUnknownIDIs404(t *testing.T) {
	_, ts, _ := newSessionsFixture(t)
	resp := deleteSessionCall(t, ts, "never_existed")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode,
		"a typo'd id must not report a destructive success")
}

func TestDeleteSessionRequiresID(t *testing.T) {
	_, ts, _ := newSessionsFixture(t)
	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/session", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
