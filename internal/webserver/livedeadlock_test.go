package webserver

import (
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "sync"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/persona"
)

// The bug report: the persona menu shows no personas and old sessions fail
// to load. Root cause was a self-deadlock in liveFor — it took s.mu and then
// called engineOptions, which reads modeStore() → s.mu again. The first
// transcript load (or first prompt) wedged s.mu forever, so every later
// /api/personas, /api/sessions, and /api/state request hung: the browser
// rendered an empty persona menu and never-populated sessions.
//
// These tests pin the two user-visible symptoms so the deadlock cannot be
// reintroduced without a red build.

// newLiveTestServer builds a Server the way Start does, rooted at a real
// (temp) project with a resumable session file and a two-persona registry,
// which is the minimum needed to reproduce the reported breakage. The
// fixture HOME keeps the config load and the session/persona files inside
// the test sandbox instead of the developer's real ~/.a1. The returned
// httptest.Server is closed on test cleanup.
func newLiveTestServer(t *testing.T) (*Server, *httptest.Server, string) {
        t.Helper()

        proj := newFixtureProject(t)

        // A minimal but valid session file: header plus one user turn, which
        // is what WithResumeID requires to accept the id as resumable.
        sessionDir := proj.SessionDir()
        require.NoError(t, os.MkdirAll(sessionDir, 0o755))
        const sessionID = "b7c3d4e5f60718293a4b5c6d7e8f9012"
        require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "probe_"+sessionID+".jsonl"), []byte(
                `{"type":"EntrySession","id":"`+sessionID+`","timestamp":"2026-10-29T10-00-00","cwd":"`+t.TempDir()+`"}`+"\n"+
                        `{"type":"EntryMessage","id":"m1","parentID":null,"timestamp":"2026-10-29T10:00:01-05:00",`+
                        `"message":{"role":"user","content":"hello"}}`+"\n"), 0o600))

        // Persona registry with one non-default persona so the menu has real
        // content to list.
        personasDir := proj.Global().PersonasDir()
        require.NoError(t, os.MkdirAll(personasDir, 0o755))
        reg := map[string]any{
                "active": "work",
                "personas": []map[string]any{
                        {
                                "id":      persona.DefaultPersonaID,
                                "name":    "a1",
                                "emoji":   "🧭",
                                "hue":     200,
                                "builtin": true,
                                "created": "2026-09-01T00:00:00Z",
                                "updated": "2026-09-01T00:00:00Z",
                        },
                        {
                                "id":      "work",
                                "name":    "Work",
                                "tagline": "Focused coder",
                                "emoji":   "⚙️",
                                "hue":     258,
                                "created": "2026-09-02T00:00:00Z",
                                "updated": "2026-09-02T00:00:00Z",
                        },
                },
        }
        data, err := json.Marshal(reg)
        require.NoError(t, err)
        require.NoError(t, os.WriteFile(filepath.Join(personasDir, "personas.json"), data, 0o600))

        srv := &Server{
                proj:         proj,
                modes:        newModeState(),
                pins:         newPinnedSessions(t.TempDir()),
                personaStore: persona.NewStore(personasDir),
        }
        // Prime the registries the way Start does, so the zero-value
        // fallback is not what is under test.
        srv.mu.Lock()
        srv.hub = newHub()
        srv.engines = map[string]*liveEngine{}
        srv.pending = map[string]chan permission.AskResult{}
        srv.mu.Unlock()
        ts := httptest.NewServer(srv.routes())
        t.Cleanup(ts.Close)
        return srv, ts, sessionID
}

// getJSON issues a GET against a real httptest.Server with a hard client
// timeout, so a wedged handler FAILS the test instead of hanging the suite.
// This is the whole point: the shipped bug presented as infinite fetches,
// which a plain mux.ServeHTTP call would happily wait out forever (the
// deadlock is in-process, so ServeHTTP would just block with no escape).
func getJSON(t *testing.T, ts *httptest.Server, path string, timeout time.Duration) (*http.Response, string) {
        t.Helper()
        client := &http.Client{Timeout: timeout}
        resp, err := client.Get(ts.URL + path)
        if err != nil {
                t.Fatalf("GET %s did not answer within %s (deadlock? want live handler): %v", path, timeout, err)
        }
        t.Cleanup(func() { _ = resp.Body.Close() })
        buf := make([]byte, 64*1024)
        n, _ := resp.Body.Read(buf)
        return resp, string(buf[:n])
}

// TestPersonaMenuSurvivesSessionLoad pins the exact reported symptom: after a
// transcript load (the first path that builds an engine through liveFor),
// the persona menu's backing request must still answer with the full
// registry. Pre-fix, liveFor deadlocked on s.mu during the transcript call
// and /api/personas hung forever.
func TestPersonaMenuSurvivesSessionLoad(t *testing.T) {
        srv, ts, sessionID := newLiveTestServer(t)
        _ = srv

        // Sanity: before any session load, the registry lists both personas.
        resp, body := getJSON(t, ts, "/api/personas", 5*time.Second)
        require.Equal(t, http.StatusOK, resp.StatusCode, body)
        var list personaListResponse
        require.NoError(t, json.Unmarshal([]byte(body), &list))
        require.Len(t, list.Personas, 2)

        // Load the old session (the user's "old session don't load" action).
        resp, body = getJSON(t, ts, "/api/transcript/messages?id="+sessionID, 10*time.Second)
        require.Equal(t, http.StatusOK, resp.StatusCode, body)

        // Now the persona menu request must still answer — pre-fix it hung.
        resp, body = getJSON(t, ts, "/api/personas", 5*time.Second)
        require.Equal(t, http.StatusOK, resp.StatusCode, body)
        require.NoError(t, json.Unmarshal([]byte(body), &list))
        assert.Len(t, list.Personas, 2, "persona registry must still list both personas after a session load")
        assert.Equal(t, "work", list.Active)
}

// TestSessionsEndpointSurvivesSessionLoad pins the other half of the report:
// the sidebar's /api/sessions must keep answering after a transcript load.
func TestSessionsEndpointSurvivesSessionLoad(t *testing.T) {
        _, ts, sessionID := newLiveTestServer(t)

        resp, body := getJSON(t, ts, "/api/transcript/messages?id="+sessionID, 10*time.Second)
        require.Equal(t, http.StatusOK, resp.StatusCode, body)

        resp, _ = getJSON(t, ts, "/api/sessions", 5*time.Second)
        assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestStateEndpointSurvivesSessionLoad covers /api/state: the footer and the
// persona chip ride on it, and its stateStore()/modeStore() calls were also
// wedged pre-fix.
func TestStateEndpointSurvivesSessionLoad(t *testing.T) {
        _, ts, sessionID := newLiveTestServer(t)

        resp, body := getJSON(t, ts, "/api/transcript/messages?id="+sessionID, 10*time.Second)
        require.Equal(t, http.StatusOK, resp.StatusCode, body)

        resp, _ = getJSON(t, ts, "/api/state", 5*time.Second)
        assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestLiveForConcurrentLoadAndPersonaList drives transcript loads and persona
// listings concurrently — the browser fires both on page load — to catch lock
// ordering regressions the sequential tests might miss.
func TestLiveForConcurrentLoadAndPersonaList(t *testing.T) {
        _, ts, sessionID := newLiveTestServer(t)

        const workers = 4
        stop := make(chan struct{})
        var wg sync.WaitGroup
        for range workers {
                wg.Go(func() {
                        client := &http.Client{Timeout: 10 * time.Second}
                        for {
                                select {
                                case <-stop:
                                        return
                                default:
                                }
                                resp, err := client.Get(ts.URL + "/api/transcript/messages?id=" + sessionID)
                                if err != nil {
                                        t.Errorf("transcript load: %v", err)
                                        return
                                }
                                _ = resp.Body.Close()
                                if resp.StatusCode != http.StatusOK {
                                        t.Errorf("transcript load: status %d", resp.StatusCode)
                                        return
                                }
                                resp, err = client.Get(ts.URL + "/api/personas")
                                if err != nil {
                                        t.Errorf("personas: %v", err)
                                        return
                                }
                                _ = resp.Body.Close()
                                if resp.StatusCode != http.StatusOK {
                                        t.Errorf("personas: status %d", resp.StatusCode)
                                        return
                                }
                        }
                })
        }
        time.Sleep(2 * time.Second)
        close(stop)

        done := make(chan struct{})
        go func() { wg.Wait(); close(done) }()
        select {
        case <-done:
        case <-time.After(20 * time.Second):
                t.Fatal("concurrent transcript + persona requests deadlocked")
        }
}
