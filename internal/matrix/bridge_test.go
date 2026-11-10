package matrix

import (
        "context"
        "encoding/json"
        "io"
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// ---- the fake loopback chat API ----

// fakeLoopback implements the five loopback endpoints the bridge uses, with
// recorders for what crossed the wire and failure injection knobs.
type fakeLoopback struct {
        srv  *httptest.Server
        push chan string // SSE frames

        mu         sync.Mutex
        prompts    []promptCall
        asks       []askCall
        cancels    []string
        fetches    []string // transcript GETs by session id
        promptSess string
        promptErr  struct {
                status int
                body   string
        }
        transcript    []transcriptMessage
        transcriptErr struct {
                status int
                body   string
        }
}

type promptCall struct {
        SessionID string
        Prompt    string
}

type askCall struct {
        ID       string
        Approved bool
}

func newFakeLoopback(t *testing.T) *fakeLoopback {
        t.Helper()
        fl := &fakeLoopback{push: make(chan string, 32), promptSess: "sess-1"}
        mux := http.NewServeMux()

        mux.HandleFunc("POST /api/prompt", func(w http.ResponseWriter, r *http.Request) {
                var req promptCall
                _ = json.NewDecoder(r.Body).Decode(&req)
                fl.mu.Lock()
                fl.prompts = append(fl.prompts, req)
                status := fl.promptErr.status
                body := fl.promptErr.body
                sess := fl.promptSess
                fl.mu.Unlock()
                w.Header().Set("Content-Type", "application/json")
                if status != 0 {
                        w.WriteHeader(status)
                        _ = json.NewEncoder(w).Encode(map[string]string{"error": body})
                        return
                }
                _ = json.NewEncoder(w).Encode(map[string]string{"sessionId": sess})
        })

        mux.HandleFunc("POST /api/ask", func(w http.ResponseWriter, r *http.Request) {
                var req askCall
                _ = json.NewDecoder(r.Body).Decode(&req)
                fl.mu.Lock()
                fl.asks = append(fl.asks, req)
                fl.mu.Unlock()
                _ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
        })

        mux.HandleFunc("POST /api/cancel", func(w http.ResponseWriter, r *http.Request) {
                var req map[string]string
                _ = json.NewDecoder(r.Body).Decode(&req)
                fl.mu.Lock()
                fl.cancels = append(fl.cancels, req["sessionId"])
                fl.mu.Unlock()
                _ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
        })

        mux.HandleFunc("GET /api/transcript/messages", func(w http.ResponseWriter, r *http.Request) {
                id := r.URL.Query().Get("id")
                fl.mu.Lock()
                fl.fetches = append(fl.fetches, id)
                status := fl.transcriptErr.status
                body := fl.transcriptErr.body
                msgs := append([]transcriptMessage(nil), fl.transcript...)
                fl.mu.Unlock()
                w.Header().Set("Content-Type", "application/json")
                if status != 0 {
                        w.WriteHeader(status)
                        _ = json.NewEncoder(w).Encode(map[string]string{"error": body})
                        return
                }
                _ = json.NewEncoder(w).Encode(map[string]any{"sessionId": id, "messages": msgs})
        })

        mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
                flusher, ok := w.(http.Flusher)
                if !ok {
                        w.WriteHeader(http.StatusInternalServerError)
                        return
                }
                w.Header().Set("Content-Type", "text/event-stream")
                flusher.Flush()
                for {
                        select {
                        case frame := <-fl.push:
                                _, _ = io.WriteString(w, frame)
                                flusher.Flush()
                        case <-r.Context().Done():
                                return
                        }
                }
        })

        fl.srv = httptest.NewServer(mux)
        t.Cleanup(fl.srv.Close)
        return fl
}

func (fl *fakeLoopback) recordedPrompts() []promptCall {
        fl.mu.Lock()
        defer fl.mu.Unlock()
        return append([]promptCall(nil), fl.prompts...)
}

func (fl *fakeLoopback) recordedAsks() []askCall {
        fl.mu.Lock()
        defer fl.mu.Unlock()
        return append([]askCall(nil), fl.asks...)
}

func (fl *fakeLoopback) setTranscript(msgs ...transcriptMessage) {
        fl.mu.Lock()
        fl.transcript = msgs
        fl.mu.Unlock()
}

func (fl *fakeLoopback) failTranscript(status int, body string) {
        fl.mu.Lock()
        fl.transcriptErr = struct {
                status int
                body   string
        }{status, body}
        fl.mu.Unlock()
}

func (fl *fakeLoopback) failPrompt(status int, body string) {
        fl.mu.Lock()
        fl.promptErr = struct {
                status int
                body   string
        }{status, body}
        fl.mu.Unlock()
}

// ---- sync/event fixture builders ----

// syncBody frames one incremental sync for the bridged room. The state
// events list is empty except where a test exercises the initial sync's
// encryption check.
func syncBody(next, timelineJSON, stateJSON string) string {
        return `{"next_batch":"` + next + `","rooms":{"join":{"!r:hs":{"state":{"events":[` + stateJSON + `]},"timeline":{"events":[` + timelineJSON + `]}}}}}`
}

func msgEvent(sender, body string) string {
        b, _ := json.Marshal(body)
        return `{"type":"m.room.message","sender":"` + sender + `","event_id":"$e","origin_server_ts":1,` +
                `"content":{"msgtype":"m.text","body":` + string(b) + `}}`
}

func typedEvent(sender, msgtype, body string) string {
        b, _ := json.Marshal(body)
        return `{"type":"m.room.message","sender":"` + sender + `","event_id":"$e","origin_server_ts":1,` +
                `"content":{"msgtype":"` + msgtype + `","body":` + string(b) + `}}`
}

func editEvent(sender, newBody string) string {
        b, _ := json.Marshal(newBody)
        return `{"type":"m.room.message","sender":"` + sender + `","event_id":"$e","origin_server_ts":1,` +
                `"content":{"msgtype":"m.text","body":"* ` + string(b) + `","m.new_content":{"msgtype":"m.text","body":` + string(b) + `},` +
                `"m.relates_to":{"rel_type":"m.replace","event_id":"$old"}}}`
}

func replyEvent(sender, body string) string {
        b, _ := json.Marshal(body)
        return `{"type":"m.room.message","sender":"` + sender + `","event_id":"$e","origin_server_ts":1,` +
                `"content":{"msgtype":"m.text","body":` + string(b) + `,"m.relates_to":{"m.in_reply_to":{"event_id":"$prev"}}}}`
}

func sseFrame(name, data string) string {
        return "event: " + name + "\ndata: " + data + "\n\n"
}

// bridgeTest bundles one running bridge with its two fakes.
type bridgeTest struct {
        hs     *fakeHS
        fl     *fakeLoopback
        b      *Bridge
        ctx    context.Context
        cancel context.CancelFunc
        state  *State
}

// startBridge boots the loopback fakes and a bridge over them, with the
// initial sync already queued (history is never replayed as prompts).
func startBridge(t *testing.T, mutate func(*Options)) *bridgeTest {
        t.Helper()
        hs := newFakeHS(t)
        fl := newFakeLoopback(t)

        statePath := filepath.Join(t.TempDir(), "state.json")
        opts := Options{
                Room:        "!r:hs",
                LoopbackURL: fl.srv.URL,
                State:       LoadState(statePath),
                Logger:      func(string, ...any) {},
        }
        if mutate != nil {
                mutate(&opts)
        }
        if opts.Client == nil {
                c := NewClient(hs.srv.URL, nil)
                c.token = hs.validToken
                c.userID = "@bridge:hs"
                opts.Client = c
        }

        b, err := NewBridge(opts)
        require.NoError(t, err)

        ctx, cancel := context.WithCancel(context.Background())
        bt := &bridgeTest{hs: hs, fl: fl, b: b, ctx: ctx, cancel: cancel, state: opts.State}
        t.Cleanup(cancel)

        // The initial sync: cursor only, no events.
        hs.syncQ <- syncBody("s1", "", "")
        go func() { _ = b.Run(ctx) }()
        return bt
}

// message pushes one room message through the sync stream.
func (bt *bridgeTest) message(t *testing.T, sender, body string) {
        t.Helper()
        select {
        case bt.hs.syncQ <- syncBody("s2", msgEvent(sender, body), ""):
        case <-time.After(3 * time.Second):
                t.Fatal("sync queue full — the bridge stopped consuming")
        }
}

// waitTyping blocks until the prompt path has fully committed (typing is
// the last call prompt() makes), so a done frame pushed after it cannot
// race the driven-session bookkeeping.
func (bt *bridgeTest) waitTyping(t *testing.T) {
        t.Helper()
        require.Eventually(t, func() bool {
                for _, c := range bt.hs.typingCalls() {
                        if c.Typing {
                                return true
                        }
                }
                return false
        }, 3*time.Second, 25*time.Millisecond, "typing flag never turned on")
}

// roomText collects the m.text bodies sent to the room.
func (bt *bridgeTest) roomText() []string {
        var out []string
        for _, m := range bt.hs.sentMessages() {
                if m.MsgType == "m.text" {
                        out = append(out, m.Body)
                }
        }
        return out
}

// roomNotices collects the m.notice bodies sent to the room.
func (bt *bridgeTest) roomNotices() []string {
        var out []string
        for _, m := range bt.hs.sentMessages() {
                if m.MsgType == "m.notice" {
                        out = append(out, m.Body)
                }
        }
        return out
}

// ---- the bridge trio ----

func TestBridgeMessageBecomesPromptAndReplyArrives(t *testing.T) {
        bt := startBridge(t, nil)

        bt.message(t, "@alice:hs", "fix the bug")
        require.Eventually(t, func() bool {
                p := bt.fl.recordedPrompts()
                return len(p) == 1 && p[0].Prompt == "fix the bug" && p[0].SessionID == ""
        }, 3*time.Second, 25*time.Millisecond, "the room message reached /api/prompt as a new session")

        bt.waitTyping(t)
        bt.fl.setTranscript(
                transcriptMessage{Role: "user", Text: "fix the bug"},
                transcriptMessage{Role: "assistant", Text: "Fixed."},
                transcriptMessage{Role: "assistant", Text: "All tests pass."},
        )
        bt.fl.push <- sseFrame("done", `{"kind":"done","sessionId":"sess-1"}`)

        require.Eventually(t, func() bool {
                return len(bt.roomText()) == 1
        }, 3*time.Second, 25*time.Millisecond, "the reply landed in the room")
        msgs := bt.hs.sentMessages()
        require.Len(t, msgs, 1)
        assert.Equal(t, "Fixed.\n\nAll tests pass.", msgs[0].Body, "multi-round text joins in order")
        assert.Contains(t, msgs[0].Formatted, "<p>Fixed.</p>", "the markdown renders to Matrix HTML")

        // The session persists: the next message resumes sess-1.
        bt.message(t, "@alice:hs", "thanks")
        require.Eventually(t, func() bool {
                p := bt.fl.recordedPrompts()
                return len(p) == 2 && p[1].SessionID == "sess-1"
        }, 3*time.Second, 25*time.Millisecond, "the follow-up resumes the bridged session")

        data, err := os.ReadFile(bt.state.path)
        require.NoError(t, err)
        assert.Contains(t, string(data), "sess-1", "the session id persisted to the state file")
}

func TestBridgeReplyChunksLongAnswers(t *testing.T) {
        bt := startBridge(t, nil)
        long := strings.Repeat("line of answer text\n", 600) // 11.4 kB, newline-rich

        bt.message(t, "@alice:hs", "write a lot")
        bt.waitTyping(t)
        bt.fl.setTranscript(
                transcriptMessage{Role: "user", Text: "write a lot"},
                transcriptMessage{Role: "assistant", Text: long},
        )
        bt.fl.push <- sseFrame("done", `{"kind":"done","sessionId":"sess-1"}`)

        require.Eventually(t, func() bool {
                return len(bt.roomText()) >= 2
        }, 3*time.Second, 25*time.Millisecond, "a long reply arrives as several messages")
        for i, m := range bt.hs.sentMessages() {
                assert.LessOrEqual(t, len(m.Body), maxChunkBytes, "chunk %d over the wire limit", i)
        }
        // The round trip the room depends on: concatenating the chunks is the
        // reply, byte-exact.
        require.Eventually(t, func() bool {
                return strings.Join(bt.roomText(), "") == long
        }, 3*time.Second, 25*time.Millisecond)
}

func TestBridgeReplyMessagesAreFallbackStripped(t *testing.T) {
        bt := startBridge(t, nil)

        select {
        case bt.hs.syncQ <- syncBody("s2", replyEvent("@alice:hs", "> <@bridge:hs> old answer\n\ndo it now"), ""):
        case <-time.After(3 * time.Second):
                t.Fatal("sync queue full")
        }

        require.Eventually(t, func() bool {
                p := bt.fl.recordedPrompts()
                return len(p) == 1 && p[0].Prompt == "do it now"
        }, 3*time.Second, 25*time.Millisecond, "the reply's quoted fallback never reaches the model")
}

func TestBridgeAskApprovesOverChat(t *testing.T) {
        bt := startBridge(t, nil)

        bt.message(t, "@alice:hs", "clean the tmp dir")
        bt.waitTyping(t)
        bt.fl.push <- sseFrame("ask", `{"kind":"ask","sessionId":"sess-1","ask":{"id":"ask-1","tool":"bash","reason":"rm -rf /tmp/x"}}`)

        require.Eventually(t, func() bool {
                return len(bt.roomNotices()) == 1
        }, 3*time.Second, 25*time.Millisecond, "the approval request reached the room")
        assert.Contains(t, bt.roomNotices()[0], "approval needed: bash")
        assert.Contains(t, bt.roomNotices()[0], "rm -rf /tmp/x")
        assert.Contains(t, bt.roomNotices()[0], "reply y or n")

        bt.message(t, "@alice:hs", "y")
        require.Eventually(t, func() bool {
                asks := bt.fl.recordedAsks()
                return len(asks) == 1 && asks[0].ID == "ask-1" && asks[0].Approved
        }, 3*time.Second, 25*time.Millisecond, "the y answer crossed to /api/ask")
        require.Eventually(t, func() bool {
                return len(bt.roomNotices()) == 2
        }, 3*time.Second, 25*time.Millisecond)
        assert.Equal(t, "approved.", bt.roomNotices()[1])
}

func TestBridgeAskDeniesOverChat(t *testing.T) {
        bt := startBridge(t, nil)

        bt.message(t, "@alice:hs", "clean the tmp dir")
        bt.waitTyping(t)
        bt.fl.push <- sseFrame("ask", `{"kind":"ask","sessionId":"sess-1","ask":{"id":"ask-9","tool":"bash","reason":"rm -rf /"}}`)
        require.Eventually(t, func() bool { return len(bt.roomNotices()) == 1 }, 3*time.Second, 25*time.Millisecond)

        bt.message(t, "@alice:hs", "n")
        require.Eventually(t, func() bool {
                asks := bt.fl.recordedAsks()
                return len(asks) == 1 && asks[0].ID == "ask-9" && !asks[0].Approved
        }, 3*time.Second, 25*time.Millisecond, "the n answer denies")
        require.Eventually(t, func() bool {
                return len(bt.roomNotices()) == 2
        }, 3*time.Second, 25*time.Millisecond)
        assert.Equal(t, "denied.", bt.roomNotices()[1])
}

func TestBridgeAskExpiryNotifiesWithoutAnswering(t *testing.T) {
        bt := startBridge(t, func(o *Options) { o.AskWindow = 60 * time.Millisecond })

        bt.message(t, "@alice:hs", "do the risky thing")
        bt.waitTyping(t)
        bt.fl.push <- sseFrame("ask", `{"kind":"ask","sessionId":"sess-1","ask":{"id":"ask-2","tool":"bash","reason":"risky"}}`)

        require.Eventually(t, func() bool {
                for _, n := range bt.roomNotices() {
                        if strings.Contains(n, "no answer in the approval window") {
                                return true
                        }
                }
                return false
        }, 3*time.Second, 25*time.Millisecond, "the expiry notice reached the room")

        // The window closing is bridge-side only: nothing is ever posted to
        // /api/ask — the webserver's own timeout does the denying.
        assert.Never(t, func() bool { return len(bt.fl.recordedAsks()) > 0 }, 200*time.Millisecond, 25*time.Millisecond)
}

func TestBridgePromptFailurePostsNotice(t *testing.T) {
        bt := startBridge(t, nil)
        bt.fl.failPrompt(http.StatusInternalServerError, "prompt is empty")

        bt.message(t, "@alice:hs", "hello")
        require.Eventually(t, func() bool {
                for _, n := range bt.roomNotices() {
                        if strings.Contains(n, "bridge: could not send the prompt: prompt is empty") {
                                return true
                        }
                }
                return false
        }, 3*time.Second, 25*time.Millisecond, "a failed prompt surfaces in the room")
}

func TestBridgeTranscriptFailurePostsNotice(t *testing.T) {
        bt := startBridge(t, nil)

        bt.message(t, "@alice:hs", "hello")
        bt.waitTyping(t)
        bt.fl.failTranscript(http.StatusInternalServerError, "session store unavailable")
        bt.fl.push <- sseFrame("done", `{"kind":"done","sessionId":"sess-1"}`)

        require.Eventually(t, func() bool {
                for _, n := range bt.roomNotices() {
                        if strings.Contains(n, "bridge: could not read the reply: session store unavailable") {
                                return true
                        }
                }
                return false
        }, 3*time.Second, 25*time.Millisecond, "a failed transcript read surfaces in the room")
}

func TestBridgeFailedTurnPostsError(t *testing.T) {
        bt := startBridge(t, nil)

        bt.message(t, "@alice:hs", "hello")
        bt.waitTyping(t)
        bt.fl.push <- sseFrame("status", `{"kind":"status","sessionId":"sess-1","status":{"text":"error: provider went away"},"failed":true}`)

        require.Eventually(t, func() bool {
                for _, n := range bt.roomNotices() {
                        if n == "error: provider went away" {
                                return true
                        }
                }
                return false
        }, 3*time.Second, 25*time.Millisecond, "the failed turn surfaces in the room")
}

func TestBridgeStopCancelsAndReportsStopped(t *testing.T) {
        bt := startBridge(t, nil)

        bt.message(t, "@alice:hs", "long task")
        bt.waitTyping(t)

        bt.message(t, "@alice:hs", "!stop")
        require.Eventually(t, func() bool {
                bt.fl.mu.Lock()
                defer bt.fl.mu.Unlock()
                return len(bt.fl.cancels) == 1 && bt.fl.cancels[0] == "sess-1"
        }, 3*time.Second, 25*time.Millisecond, "!stop crossed to /api/cancel")

        // The cancelled turn's failure status renders as the clean form.
        bt.fl.push <- sseFrame("status", `{"kind":"status","sessionId":"sess-1","status":{"text":"error: context canceled"},"failed":true}`)
        require.Eventually(t, func() bool {
                for _, n := range bt.roomNotices() {
                        if n == "stopped." {
                                return true
                        }
                }
                return false
        }, 3*time.Second, 25*time.Millisecond)
}

func TestBridgeNewSessionResetsTheNextPrompt(t *testing.T) {
        bt := startBridge(t, nil)

        bt.message(t, "@alice:hs", "first conversation")
        bt.waitTyping(t)

        bt.message(t, "@alice:hs", "!new")
        require.Eventually(t, func() bool {
                for _, n := range bt.roomNotices() {
                        if strings.Contains(n, "new session") {
                                return true
                        }
                }
                return false
        }, 3*time.Second, 25*time.Millisecond)

        bt.message(t, "@alice:hs", "fresh start")
        require.Eventually(t, func() bool {
                p := bt.fl.recordedPrompts()
                return len(p) == 2 && p[1].SessionID == ""
        }, 3*time.Second, 25*time.Millisecond, "the post-!new prompt starts a fresh session")
}

func TestBridgeIgnoresOwnSendsNoticesEmotesEditsAndOtherRooms(t *testing.T) {
        bt := startBridge(t, nil)

        // The bridge's own reply (echoed by the sync stream) must not loop back
        // as a prompt; notices, emotes, and edits of old messages are not
        // prompts either.
        events := strings.Join([]string{
                msgEvent("@bridge:hs", "the bridge's own answer"),
                typedEvent("@alice:hs", "m.notice", "another bot's notice"),
                typedEvent("@alice:hs", "m.emote", "waves"),
                editEvent("@alice:hs", "reworded prompt"),
        }, ",")
        select {
        case bt.hs.syncQ <- syncBody("s2", events, ""):
        case <-time.After(3 * time.Second):
                t.Fatal("sync queue full")
        }

        assert.Never(t, func() bool { return len(bt.fl.recordedPrompts()) > 0 }, 300*time.Millisecond, 25*time.Millisecond,
                "none of the ignored event shapes reached /api/prompt")

        // A message in a DIFFERENT room is not the bridge's business: the sync
        // body carries only !r:hs, so this is pinned at the client level —
        // the bridge reads resp.Rooms.Join[b.room] and nothing else.
        assert.Empty(t, bt.fl.recordedPrompts())
}

func TestBridgeRefusesEncryptedRoomAtStartup(t *testing.T) {
        hs := newFakeHS(t)
        fl := newFakeLoopback(t)

        c := NewClient(hs.srv.URL, nil)
        c.token = hs.validToken
        c.userID = "@bridge:hs"
        b, err := NewBridge(Options{Client: c, Room: "!r:hs", LoopbackURL: fl.srv.URL})
        require.NoError(t, err)

        // The initial sync carries m.room.encryption in the room state.
        hs.syncQ <- syncBody("s1", "", `{"type":"m.room.encryption","sender":"@alice:hs","content":{}}`)

        // A cancellable context: Run returns the error, but its consumer
        // goroutines hold connections open until the context dies — an
        // uncancellable one would hang the test server's Close forever.
        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()
        err = b.Run(ctx)
        require.Error(t, err)
        assert.ErrorContains(t, err, "encryption", "an encrypted room is refused loudly, not silently ignored")
}

func TestBridgeResumesStoredSessionAndDiffsCorrectly(t *testing.T) {
        // The differential spine of the reply path: the diff anchor set at
        // adoption (3 old messages) means the post-restart reply carries only
        // the NEW assistant text — never the history, never the user echo.
        hs := newFakeHS(t)
        fl := newFakeLoopback(t)
        fl.setTranscript(
                transcriptMessage{Role: "user", Text: "yesterday's question"},
                transcriptMessage{Role: "assistant", Text: "yesterday's long answer"},
                transcriptMessage{Role: "assistant", Text: "yesterday's follow-up"},
        )

        statePath := filepath.Join(t.TempDir(), "state.json")
        state := LoadState(statePath)
        state.SessionID = "old-1"
        require.NoError(t, state.Save())

        c := NewClient(hs.srv.URL, nil)
        c.token = hs.validToken
        c.userID = "@bridge:hs"
        b, err := NewBridge(Options{Client: c, Room: "!r:hs", LoopbackURL: fl.srv.URL, State: state})
        require.NoError(t, err)

        ctx, cancel := context.WithCancel(context.Background())
        t.Cleanup(cancel)
        hs.syncQ <- syncBody("s1", "", "")
        go func() { _ = b.Run(ctx) }()

        require.Eventually(t, func() bool {
                fl.mu.Lock()
                defer fl.mu.Unlock()
                return len(fl.fetches) == 1 && fl.fetches[0] == "old-1"
        }, 3*time.Second, 25*time.Millisecond, "adoption read the stored session's transcript")

        select {
        case hs.syncQ <- syncBody("s2", msgEvent("@alice:hs", "continue"), ""):
        case <-time.After(3 * time.Second):
                t.Fatal("sync queue full")
        }
        require.Eventually(t, func() bool {
                p := fl.recordedPrompts()
                return len(p) == 1 && p[0].SessionID == "old-1"
        }, 3*time.Second, 25*time.Millisecond, "the stored session is resumed, not replaced")

        fl.setTranscript(
                transcriptMessage{Role: "user", Text: "yesterday's question"},
                transcriptMessage{Role: "assistant", Text: "yesterday's long answer"},
                transcriptMessage{Role: "assistant", Text: "yesterday's follow-up"},
                transcriptMessage{Role: "user", Text: "continue"},
                transcriptMessage{Role: "assistant", Text: "Done again."},
        )
        fl.push <- sseFrame("done", `{"kind":"done","sessionId":"old-1"}`)

        require.Eventually(t, func() bool {
                texts := roomTextOf(hs)
                return len(texts) == 1 && texts[0] == "Done again."
        }, 3*time.Second, 25*time.Millisecond, "only the new turn's text crossed the anchor")
}

func roomTextOf(hs *fakeHS) []string {
        var out []string
        for _, m := range hs.sentMessages() {
                if m.MsgType == "m.text" {
                        out = append(out, m.Body)
                }
        }
        return out
}

func TestBridgeStoredSessionUnavailableStartsFresh(t *testing.T) {
        hs := newFakeHS(t)
        fl := newFakeLoopback(t)
        fl.failTranscript(http.StatusInternalServerError, "no such session")

        statePath := filepath.Join(t.TempDir(), "state.json")
        state := LoadState(statePath)
        state.SessionID = "gone"
        require.NoError(t, state.Save())

        c := NewClient(hs.srv.URL, nil)
        c.token = hs.validToken
        c.userID = "@bridge:hs"
        b, err := NewBridge(Options{Client: c, Room: "!r:hs", LoopbackURL: fl.srv.URL, State: state})
        require.NoError(t, err)

        ctx, cancel := context.WithCancel(context.Background())
        t.Cleanup(cancel)
        hs.syncQ <- syncBody("s1", "", "")
        go func() { _ = b.Run(ctx) }()

        select {
        case hs.syncQ <- syncBody("s2", msgEvent("@alice:hs", "hello"), ""):
        case <-time.After(3 * time.Second):
                t.Fatal("sync queue full")
        }
        require.Eventually(t, func() bool {
                p := fl.recordedPrompts()
                return len(p) == 1 && p[0].SessionID == ""
        }, 3*time.Second, 25*time.Millisecond, "a dead stored session degrades to a fresh start, not a boot failure")
}

// ---- Connect's trio ----

func TestConnectTokenAuthenticatesAndJoins(t *testing.T) {
        hs := newFakeHS(t)
        hs.validToken = "tok-config"
        hs.issueToken("tok-config", "@bridge:hs")
        state := LoadState(filepath.Join(t.TempDir(), "state.json"))

        c, roomID, err := Connect(context.Background(), Config{
                Homeserver:  hs.srv.URL,
                AccessToken: "tok-config",
                Room:        "#a1:hs",
        }, state, nil)
        require.NoError(t, err)
        assert.Equal(t, "@bridge:hs", c.UserID())
        assert.Equal(t, "!resolved:hs", roomID, "the alias resolved through /join")

        hs.mu.Lock()
        joins := append([]string(nil), hs.joins...)
        hs.mu.Unlock()
        require.Len(t, joins, 1)
}

func TestConnectPasswordLoginCachesToken(t *testing.T) {
        hs := newFakeHS(t)
        state := LoadState(filepath.Join(t.TempDir(), "state.json"))

        c, roomID, err := Connect(context.Background(), Config{
                Homeserver: hs.srv.URL,
                User:       "bot",
                Password:   "good",
                Room:       "!r:hs",
        }, state, nil)
        require.NoError(t, err)
        assert.Equal(t, "@bot:hs", c.UserID())
        assert.Equal(t, "tok-login", c.Token())
        assert.Equal(t, "!resolved:hs", roomID)

        reloaded := LoadState(state.path)
        assert.Equal(t, "tok-login", reloaded.AccessToken, "the granted token is cached for the next boot")
        assert.Equal(t, "@bot:hs", reloaded.UserID)
}

func TestConnectRecoversFromStaleCachedToken(t *testing.T) {
        hs := newFakeHS(t)
        state := LoadState(filepath.Join(t.TempDir(), "state.json"))
        state.AccessToken = "stale"
        require.NoError(t, state.Save())

        _, _, err := Connect(context.Background(), Config{
                Homeserver: hs.srv.URL,
                User:       "bot",
                Password:   "good",
                Room:       "!r:hs",
        }, state, nil)
        require.NoError(t, err, "a revoked cached token falls back to password login")

        reloaded := LoadState(state.path)
        assert.Equal(t, "tok-login", reloaded.AccessToken, "the stale cache was replaced")
}

func TestConnectRejectsBadTokenWithoutPasswordFallback(t *testing.T) {
        hs := newFakeHS(t)
        _, _, err := Connect(context.Background(), Config{
                Homeserver:  hs.srv.URL,
                AccessToken: "revoked",
                Room:        "!r:hs",
        }, LoadState(filepath.Join(t.TempDir(), "state.json")), nil)
        require.Error(t, err)
        assert.ErrorContains(t, err, "access token rejected")
}

func TestConnectRejectsIncompleteConfig(t *testing.T) {
        hs := newFakeHS(t)
        state := LoadState(filepath.Join(t.TempDir(), "state.json"))

        _, _, err := Connect(context.Background(), Config{}, state, nil)
        require.Error(t, err)
        assert.ErrorContains(t, err, "homeserver is not configured")

        _, _, err = Connect(context.Background(), Config{Homeserver: hs.srv.URL}, state, nil)
        require.Error(t, err)
        assert.ErrorContains(t, err, "room is not configured")

        _, _, err = Connect(context.Background(), Config{Homeserver: hs.srv.URL, Room: "not-a-room"}, state, nil)
        require.Error(t, err)
        assert.ErrorContains(t, err, "must be a !id:homeserver or #alias:homeserver")

        _, _, err = Connect(context.Background(), Config{Homeserver: hs.srv.URL, Room: "!r:hs"}, state, nil)
        require.Error(t, err)
        assert.ErrorContains(t, err, "set access_token, or user + password")
}

func TestConnectNormalizesBareHosts(t *testing.T) {
        assert.Equal(t, "https://matrix.org", normalizeHomeserver("matrix.org"))
        assert.Equal(t, "https://matrix.org", normalizeHomeserver("https://matrix.org/"))
        assert.Equal(t, "", normalizeHomeserver("  "))
        assert.Equal(t, "matrix.example.org", hostOf("https://matrix.example.org/_matrix"))
}
