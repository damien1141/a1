package webserver

import (
        "bufio"
        "encoding/json"
        "fmt"
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

// This file drives the real web stack — HTTP handler, SSE hub, engine loop —
// against a scripted LLM server, then parses the browser-visible SSE stream.
// It pins the end-to-end fix for the "webui eats what the model says" bug:
// a turn that streams text, calls a tool, then streams more text must emit
// deltas tagged with TWO different round ids so the client renders two
// bubbles instead of overwriting the first round's text.

// multiRoundLLMServer scripts a two-round conversation over OpenAI-style SSE:
//   request 1 → text chunk ("Checking the file now."), then a tool call
//   request 2 → text chunk ("The file looks good.")
// It writes no [DONE] before the tool call's finish_reason, matching how
// real providers end a tool round.
func multiRoundLLMServer() *httptest.Server {
        requests := 0
        return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/chat/completions" {
                        http.NotFound(w, r)
                        return
                }
                requests++
                w.Header().Set("Content-Type", "text/event-stream")
                fl, _ := w.(http.Flusher)

                if requests == 1 {
                        // Round 1: say something, then call the tool.
                        fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"Checking the file now."}}]}`+"\n\n")
                        fl.Flush()
                        fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"count","arguments":"{}"}}]}}]}`+"\n\n")
                        fl.Flush()
                        fmt.Fprint(w, `data: {"choices":[{"finish_reason":"tool_calls"}]}`+"\n\n")
                        fl.Flush()
                } else {
                        // Round 2: the post-tool answer.
                        fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"The file looks good."}}]}`+"\n\n")
                        fl.Flush()
                        fmt.Fprint(w, `data: {"choices":[{"finish_reason":"stop"}]}`+"\n\n")
                        fl.Flush()
                }
                fmt.Fprint(w, "data: [DONE]\n\n")
                fl.Flush()
        }))
}

// readSSE collects every event off one SSE stream until the done event or a
// deadline. Returns raw event payloads grouped by event name.
func readSSE(t *testing.T, ts *httptest.Server, sessionID string) (deltas []string, tools []string, done bool) {
        t.Helper()
        client := &http.Client{Timeout: 30 * time.Second}
        req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/events", http.NoBody)
        require.NoError(t, err)
        resp, err := client.Do(req)
        if err != nil {
                t.Fatalf("subscribe to /api/events: %v", err)
        }
        defer func() { _ = resp.Body.Close() }()

        // Give the turn a moment to finish before we read (events buffer in
        // the hub; the SSE stream replays nothing, so the turn must run AFTER
        // the subscription — the test drives the prompt after a beat).
        _ = sessionID
        deadline := time.Now().Add(25 * time.Second)
        br := bufio.NewReader(resp.Body)
        for time.Now().Before(deadline) {
                line, err := br.ReadString('\n')
                if err != nil {
                        return
                }
                line = strings.TrimRight(line, "\r\n")
                if !strings.HasPrefix(line, "data: ") {
                        continue
                }
                payload := strings.TrimPrefix(line, "data: ")
                if payload == "" || payload == "[DONE]" {
                        continue
                }
                var ev struct {
                        Kind  string `json:"kind"`
                        Delta *struct {
                                Text  string `json:"text"`
                                MsgID string `json:"msgId"`
                        } `json:"delta"`
                        Tool *struct {
                                Name  string `json:"name"`
                                MsgID string `json:"msgId"`
                        } `json:"tool"`
                }
                if err := json.Unmarshal([]byte(payload), &ev); err != nil {
                        continue
                }
                switch {
                case ev.Delta != nil && ev.Kind == "delta":
                        deltas = append(deltas, ev.Delta.MsgID)
                case ev.Tool != nil && ev.Kind == "tool":
                        tools = append(tools, ev.Tool.MsgID)
                case ev.Kind == "done":
                        done = true
                        return
                }
        }
        return
}

// TestMultiRoundTurnThroughSSE is the end-to-end pin of the fix: the browser
// sees round-1 text, the tool row, and round-2 text tagged with distinct
// round ids — never a second text over the first round's id.
func TestMultiRoundTurnThroughSSE(t *testing.T) {
        llm := multiRoundLLMServer()
        defer llm.Close()

        // Project rooted at a temp dir whose config points at the mock LLM.
        root := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n"), 0o600))
        home := t.TempDir()
        t.Setenv("HOME", home) // keep ~/.a1 inside the sandbox of the test
        _ = root

        cfgYaml := fmt.Sprintf("models:\n  - name: fake\n    api: OpenAI\n    api_key: x\n    base_url: %s\n    context_window: 32768\n", llm.URL)
        cfgDir := filepath.Join(home, ".a1")
        require.NoError(t, os.MkdirAll(cfgDir, 0o755))
        require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYaml), 0o600))

        proj, err := project.Discover(root)
        require.NoError(t, err)
        require.NoError(t, proj.LoadConfig())

        srv := &Server{
                proj:         proj,
                modes:        newModeState(),
                pins:         newPinnedSessions(t.TempDir()),
                personaStore: nil,
        }
        srv.mu.Lock()
        srv.hub = newHub()
        srv.engines = map[string]*liveEngine{}
        srv.pending = map[string]chan permission.AskResult{}
        srv.mu.Unlock()
        ts := httptest.NewServer(srv.routes())
        defer ts.Close()

        // Subscribe FIRST (live events don't replay), then drive the turn.
        type result struct {
                deltas []string
                tools  []string
                done   bool
        }
        resCh := make(chan result, 1)
        go func() {
                d, tl, dn := readSSE(t, ts, "")
                resCh <- result{deltas: d, tools: tl, done: dn}
        }()
        time.Sleep(300 * time.Millisecond) // let the subscription land

        client := &http.Client{Timeout: 10 * time.Second}
        resp, err := client.Post(ts.URL+"/api/prompt", "application/json",
                strings.NewReader(`{"prompt":"check the file"}`))
        require.NoError(t, err)
        defer func() { _ = resp.Body.Close() }()
        require.Equal(t, http.StatusAccepted, resp.StatusCode)
        var accepted struct {
                SessionID string `json:"sessionId"`
        }
        require.NoError(t, json.NewDecoder(resp.Body).Decode(&accepted))
        require.NotEmpty(t, accepted.SessionID)

        select {
        case res := <-resCh:
                // Round 1's deltas (several per round: streaming chunks + the
                // complete event), then the tool, then round 2's deltas —
                // with a DIFFERENT round id on the post-tool text. Same-id
                // overwrite is the bug this pins.
                require.NotEmpty(t, res.deltas, "no deltas observed")
                r1, r2 := res.deltas[0], res.deltas[len(res.deltas)-1]
                assert.NotEqual(t, r1, r2,
                        "round-2 text must carry a different msgId than round-1 — same id means the browser overwrites round 1's text")
                for _, id := range res.deltas {
                        assert.True(t, id == r1 || id == r2,
                                "every delta belongs to one of the two rounds; got %q", id)
                }
                require.NotEmpty(t, res.tools, "no tool events observed")
                assert.Equal(t, r1, res.tools[0],
                        "the tool row must be tagged with the round id of the text that preceded it")
                assert.True(t, res.done, "turn did not finish (done event missing)")
        case <-time.After(30 * time.Second):
                t.Fatal("SSE stream never completed")
        }
}
