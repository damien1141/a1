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
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/project"
)

// turns_test.go drives the re-run surfaces (regenerate + edit) through the
// real HTTP stack against a scripted LLM, then reads the JSON transcript —
// the same projection the browser reloads. What each test pins:
//
//   - regenerate: the last assistant turn is replaced on-path (the old
//     response chain becomes an abandoned branch), the user message stays,
//     and the fresh completion lands.
//   - edit: the targeted user message is rewritten and everything after it
//     falls off the path; the turn re-runs from the edited text.

// scriptedLLMServer answers every /chat/completions with the next scripted
// text (round-robin past the script so background calls like session
// naming never wedge the turn).
func scriptedLLMServer(responses ...string) *httptest.Server {
        i := 0
        return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/chat/completions" {
                        http.NotFound(w, r)
                        return
                }
                text := "fallback"
                if i < len(responses) {
                        text = responses[i]
                }
                i++
                w.Header().Set("Content-Type", "text/event-stream")
                fl, _ := w.(http.Flusher)
                fmt.Fprintf(w, `data: {"choices":[{"delta":{"content":%q}}]}`+"\n\n", text)
                fl.Flush()
                fmt.Fprint(w, `data: {"choices":[{"finish_reason":"stop"}]}`+"\n\n")
                fl.Flush()
                fmt.Fprint(w, "data: [DONE]\n\n")
                fl.Flush()
        }))
}

// newTurnsFixture builds a hand-wired Server (the multiround_sse_test
// pattern) whose project config points at llm and disables smart session
// naming so the script is the ONLY thing the LLM is asked for.
func newTurnsFixture(t *testing.T, llm *httptest.Server) (*Server, *httptest.Server) {
        t.Helper()
        root := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n"), 0o600))
        home := t.TempDir()
        t.Setenv("HOME", home)
        t.Setenv("USERPROFILE", home)

        cfgYAML := fmt.Sprintf("models:\n  - name: fake\n    api: OpenAI\n    api_key: x\n    base_url: %s\n    context_window: 32768\nweb:\n  auto_name_sessions: false\n", llm.URL)
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
        return srv, ts
}

// transcriptRows is the flattened view the assertions work over.
type transcriptRows struct {
        users      []string
        assistants []string
}

func fetchTranscriptRows(t *testing.T, ts *httptest.Server, sessionID string) transcriptRows {
        t.Helper()
        resp, err := http.Get(ts.URL + "/api/transcript/messages?id=" + sessionID)
        require.NoError(t, err)
        defer func() { _ = resp.Body.Close() }()
        require.Equal(t, http.StatusOK, resp.StatusCode)
        var doc struct {
                Messages []struct {
                        Role string `json:"role"`
                        Text string `json:"text"`
                } `json:"messages"`
        }
        require.NoError(t, json.NewDecoder(resp.Body).Decode(&doc))
        var rows transcriptRows
        for _, m := range doc.Messages {
                switch m.Role {
                case "user":
                        rows.users = append(rows.users, m.Text)
                case "assistant":
                        rows.assistants = append(rows.assistants, m.Text)
                }
        }
        return rows
}

// waitTranscript polls until cond passes or the deadline hits — turns run
// on background goroutines, so the transcript is eventually-consistent.
func waitTranscript(t *testing.T, ts *httptest.Server, sessionID string, cond func(transcriptRows) bool) transcriptRows {
        t.Helper()
        deadline := time.Now().Add(20 * time.Second)
        var rows transcriptRows
        for time.Now().Before(deadline) {
                rows = fetchTranscriptRows(t, ts, sessionID)
                if cond(rows) {
                        return rows
                }
                time.Sleep(100 * time.Millisecond)
        }
        t.Fatalf("transcript never reached the expected state; last seen: %+v", rows)
        return rows
}

// postJSON is the small helper every turn-driving call shares.
func postJSON(t *testing.T, ts *httptest.Server, path, body string) *http.Response {
        t.Helper()
        resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(body))
        require.NoError(t, err)
        return resp
}

// startSession drives one prompt and returns the minted session id.
func startSession(t *testing.T, ts *httptest.Server, prompt string) string {
        t.Helper()
        resp := postJSON(t, ts, "/api/prompt", fmt.Sprintf(`{"prompt":%q}`, prompt))
        defer func() { _ = resp.Body.Close() }()
        require.Equal(t, http.StatusAccepted, resp.StatusCode)
        var accepted struct {
                SessionID string `json:"sessionId"`
        }
        require.NoError(t, json.NewDecoder(resp.Body).Decode(&accepted))
        require.NotEmpty(t, accepted.SessionID)
        return accepted.SessionID
}

func TestRegenerateReplacesLastTurnOnPath(t *testing.T) {
        llm := scriptedLLMServer("first answer", "second answer")
        defer llm.Close()
        _, ts := newTurnsFixture(t, llm)

        sessionID := startSession(t, ts, "the question")
        rows := waitTranscript(t, ts, sessionID, func(r transcriptRows) bool {
                return len(r.users) == 1 && len(r.assistants) == 1
        })
        assert.Equal(t, []string{"the question"}, rows.users)
        assert.Equal(t, []string{"first answer"}, rows.assistants)

        resp := postJSON(t, ts, "/api/regenerate", fmt.Sprintf(`{"sessionId":%q}`, sessionID))
        defer func() { _ = resp.Body.Close() }()
        require.Equal(t, http.StatusAccepted, resp.StatusCode)

        // The re-roll keeps the user message and swaps the on-path assistant:
        // the old answer chain is an abandoned branch, invisible to the
        // transcript the browser reloads.
        rows = waitTranscript(t, ts, sessionID, func(r transcriptRows) bool {
                return len(r.assistants) == 1 && r.assistants[0] == "second answer"
        })
        assert.Equal(t, []string{"the question"}, rows.users,
                "regenerate re-sends the same prompt as a sibling — the user row is not duplicated")
        assert.Equal(t, []string{"second answer"}, rows.assistants)
}

func TestRegenerateRefusesWhenTurnInFlight(t *testing.T) {
        // A script that never finishes: the first round hangs until the test
        // cancels it, holding the turn mutex.
        //
        // The handler DRAINS THE REQUEST BODY before parking. net/http only
        // starts the background read — the machinery that cancels r.Context()
        // when the client goes away — once the request body is consumed
        // (registerOnHitEOF in server.go). A handler that parks with the body
        // unread can never notice a closed connection, httptest.Server.Close
        // then blocks on its outstanding handler forever, and the whole
        // webserver suite dies with it.
        hanging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                _, _ = io.Copy(io.Discard, r.Body)
                <-r.Context().Done()
        }))
        // Deferred LIFO: CloseClientConnections force-drops the stream
        // connection (dropping it also cancels the parked handler via the
        // background read), then Close — which waits for active connections
        // and outstanding handlers — completes.
        defer hanging.Close()
        defer hanging.CloseClientConnections()
        _, ts := newTurnsFixture(t, hanging)

        sessionID := startSession(t, ts, "slow one")
        // Give the turn goroutine a beat to take the lock.
        time.Sleep(300 * time.Millisecond)

        resp := postJSON(t, ts, "/api/regenerate", fmt.Sprintf(`{"sessionId":%q}`, sessionID))
        assert.Equal(t, http.StatusConflict, resp.StatusCode,
                "a re-run must fail fast while a turn streams, not queue behind it")
        _ = resp.Body.Close()

        // Stop the hanging turn before teardown: the turn context is
        // deliberately detached from the prompt request (it lives until
        // /api/cancel), and an unstopped turn would keep retrying the stream
        // against the hanging server — re-dialing connections the deferred
        // close can no longer reach.
        cancel := postJSON(t, ts, "/api/cancel", fmt.Sprintf(`{"sessionId":%q}`, sessionID))
        require.Equal(t, http.StatusOK, cancel.StatusCode, "cancel must be accepted")
        _ = cancel.Body.Close()
}

func TestEditUserMessageForksBranch(t *testing.T) {
        llm := scriptedLLMServer("answer one", "answer two", "answer three")
        defer llm.Close()
        _, ts := newTurnsFixture(t, llm)

        sessionID := startSession(t, ts, "first question")
        waitTranscript(t, ts, sessionID, func(r transcriptRows) bool {
                return len(r.assistants) == 1
        })
        resp := postJSON(t, ts, "/api/prompt", fmt.Sprintf(`{"sessionId":%q,"prompt":"second question"}`, sessionID))
        defer func() { _ = resp.Body.Close() }()
        require.Equal(t, http.StatusAccepted, resp.StatusCode)
        waitTranscript(t, ts, sessionID, func(r transcriptRows) bool {
                return len(r.users) == 2 && len(r.assistants) == 2
        })

        // Edit the FIRST user message: everything after it (its answer, the
        // whole second exchange) falls off the path and the turn re-runs from
        // the edited text.
        resp = postJSON(t, ts, "/api/edit", fmt.Sprintf(`{"sessionId":%q,"userIndex":0,"prompt":"edited question"}`, sessionID))
        defer func() { _ = resp.Body.Close() }()
        require.Equal(t, http.StatusAccepted, resp.StatusCode)

        rows := waitTranscript(t, ts, sessionID, func(r transcriptRows) bool {
                return len(r.users) == 1 && len(r.assistants) == 1 &&
                        r.users[0] == "edited question" && r.assistants[0] == "answer three"
        })
        assert.Equal(t, []string{"edited question"}, rows.users,
                "the edit rewrites the targeted user message; later exchanges are off-path")
        assert.NotContains(t, rows.assistants, "answer two", "the abandoned branch's answers do not leak into the reload")

        // userIndex beyond the session's user rows is a clean 400.
        resp = postJSON(t, ts, "/api/edit", fmt.Sprintf(`{"sessionId":%q,"userIndex":9,"prompt":"nope"}`, sessionID))
        defer func() { _ = resp.Body.Close() }()
        assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestRegenerateRejectsEmptySession(t *testing.T) {
        llm := scriptedLLMServer("x")
        defer llm.Close()
        _, ts := newTurnsFixture(t, llm)

        resp := postJSON(t, ts, "/api/regenerate", `{"sessionId":""}`)
        defer func() { _ = resp.Body.Close() }()
        assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

        resp = postJSON(t, ts, "/api/edit", `{"sessionId":"","userIndex":0,"prompt":"hi"}`)
        defer func() { _ = resp.Body.Close() }()
        assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
