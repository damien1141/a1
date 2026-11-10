package stoat

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/ws"
)

// ---- the fake stoat event gateway (a server-side websocket) ----

// fakeGateway is the event half of the platform: it upgrades one
// websocket, answers Authenticate with Authenticated + Ready, and pushes
// whatever the test queues. Client frames (Ping, BeginTyping,
// EndTyping) are recorded for assertions.
type fakeGateway struct {
	srv  *httptest.Server
	push chan string // server→client events, queued by the test

	mu        sync.Mutex
	frames    []string // client→server frames as raw JSON
	selfUser  string   // the id Ready carries as relationship "User"
	noSelf    bool     // Ready without a self user (the fatal case)
	holdReady bool     // answer Authenticate but withhold Ready (the drop window)
	conn      net.Conn

	connected chan struct{} // closed when the first connection is live
}

func (g *fakeGateway) url() string { return "ws" + strings.TrimPrefix(g.srv.URL, "http") }

func newFakeGateway(t *testing.T, selfUser string) *fakeGateway {
	t.Helper()
	g := &fakeGateway{
		push:      make(chan string, 64),
		selfUser:  selfUser,
		connected: make(chan struct{}),
	}
	once := sync.Once{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		hj, ok := rw.(http.Hijacker)
		if !ok {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		conn, brw, err := hj.Hijack()
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + ws.GUID))
		accept := base64.StdEncoding.EncodeToString(sum[:])
		resp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
		if _, err := conn.Write([]byte(resp)); err != nil {
			return
		}
		_ = brw.Flush()

		g.mu.Lock()
		g.conn = conn
		g.mu.Unlock()
		once.Do(func() { close(g.connected) })
		g.serve(conn, bufio.NewReader(brw.Reader))
	})
	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	t.Cleanup(func() { g.mu.Lock(); conn := g.conn; g.mu.Unlock(); _ = conn.Close() })
	return g
}

// serve is the fake's event loop: one goroutine reads client frames,
// another drains the push queue. Authentication is answered inline.
func (g *fakeGateway) serve(conn net.Conn, br *bufio.Reader) {
	go func() {
		for ev := range g.push {
			if err := writeSrvText(conn, []byte(ev)); err != nil {
				return
			}
		}
	}()
	for {
		frame, err := readClientFrame(br)
		if err != nil {
			return
		}
		g.mu.Lock()
		g.frames = append(g.frames, string(frame))
		self, noSelf, hold := g.selfUser, g.noSelf, g.holdReady
		g.mu.Unlock()

		var head struct {
			Type  string `json:"type"`
			Token string `json:"token"`
		}
		_ = json.Unmarshal(frame, &head)
		switch head.Type {
		case "Authenticate":
			_ = writeSrvText(conn, []byte(`{"type":"Authenticated"}`))
			if hold {
				continue // Ready withheld: the drop-window test drives it later
			}
			_ = writeSrvText(conn, []byte(g.readyBody(self, noSelf)))
		case "Ping":
			// The fake never answers Pong; the bridge tolerates that
			// because the ws idle deadline is what judges liveness.
		}
	}
}

// readyBody builds the Ready payload: one stranger plus (unless noSelf)
// the bot itself, marked relationship "User".
func (g *fakeGateway) readyBody(self string, noSelf bool) string {
	if noSelf {
		return `{"type":"Ready","users":[{"_id":"someone","relationship":"None"}]}`
	}
	return `{"type":"Ready","users":[{"_id":"someone","relationship":"None"},` +
		`{"_id":"` + self + `","relationship":"User"}]}`
}

// pushEvent queues one server→client event.
func (g *fakeGateway) pushEvent(t *testing.T, ev string) {
	t.Helper()
	select {
	case g.push <- ev:
	case <-time.After(3 * time.Second):
		t.Error("push queue full — the gateway stopped consuming")
	}
}

func (g *fakeGateway) clientFrames() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.frames...)
}

// typingFrames returns the BeginTyping/EndTyping channel targets in order.
func (g *fakeGateway) typingFrames() []string {
	var out []string
	for _, f := range g.clientFrames() {
		var ev struct {
			Type    string `json:"type"`
			Channel string `json:"channel"`
		}
		if json.Unmarshal([]byte(f), &ev) == nil && strings.HasSuffix(ev.Type, "Typing") {
			out = append(out, ev.Type+":"+ev.Channel)
		}
	}
	return out
}

// ---- server-side frame I/O (the ws package's test oracle, slimmed) ----

// readClientFrame reads one masked client frame and returns its
// unmasked payload.
func readClientFrame(br *bufio.Reader) ([]byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(br, h[:]); err != nil {
		return nil, err
	}
	length := int64(h[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return nil, err
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return nil, err
		}
		length = int64(binary.BigEndian.Uint64(ext[:]))
	}
	var key [4]byte
	if h[1]&0x80 != 0 {
		if _, err := io.ReadFull(br, key[:]); err != nil {
			return nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(br, payload); err != nil {
		return nil, err
	}
	if h[1]&0x80 != 0 {
		for i := range payload {
			payload[i] ^= key[i%4]
		}
	}
	return payload, nil
}

// writeSrvText writes one unmasked server text frame.
func writeSrvText(conn net.Conn, payload []byte) error {
	var hdr [4]byte
	hdr[0] = 0x80 | 0x1 // FIN + text
	pos := 2
	switch {
	case len(payload) < 126:
		hdr[1] = byte(len(payload))
	default:
		hdr[1] = 126
		binary.BigEndian.PutUint16(hdr[2:4], uint16(len(payload)))
		pos = 4
	}
	if _, err := conn.Write(hdr[:pos]); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

// ---- the fake loopback chat API (same shape as the matrix bridge's) ----

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

func (fl *fakeLoopback) recordedCancels() []string {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	return append([]string(nil), fl.cancels...)
}

func (fl *fakeLoopback) setTranscript(msgs ...transcriptMessage) {
	fl.mu.Lock()
	fl.transcript = msgs
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

func (fl *fakeLoopback) failTranscript(status int, body string) {
	fl.mu.Lock()
	fl.transcriptErr = struct {
		status int
		body   string
	}{status, body}
	fl.mu.Unlock()
}

// ---- event fixture builders ----

func msgEvent(id, channel, author, content string) string {
	return `{"type":"Message","_id":"` + id + `","channel":"` + channel + `","author":"` + author + `","content":` + quote(content) + `}`
}

func systemEvent(channel string) string {
	return `{"type":"Message","_id":"sys","channel":"` + channel + `","author":"someone","content":"x","system":{"type":"channel_left"}}`
}

func botEvent(channel, content string) string {
	return `{"type":"Message","_id":"b1","channel":"` + channel + `","author":"otherbot","content":` + quote(content) + `,"user":{"_id":"otherbot","bot":{"owner":"x"}}}`
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func sseFrame(name, data string) string {
	return "event: " + name + "\ndata: " + data + "\n\n"
}

// ---- the bridge test harness ----

// stoatTest bundles one running bridge with its three fakes.
type stoatTest struct {
	api *fakeAPI
	gw  *fakeGateway
	fl  *fakeLoopback
	b   *Bridge
	ctx context.Context
}

// startBridge boots the fakes and a bridge over them. The gateway's
// Ready self user is "bridge-bot"; channel "chan-1" is the bridged one.
// setup, when given, runs BEFORE the bridge: adoption at Run reads the
// loopback's transcript, so a test resuming a stored session must seed
// it (or its failure) here, not after.
func startBridge(t *testing.T, mutate func(*Options), setup ...func(fl *fakeLoopback)) *stoatTest {
	t.Helper()
	api := newFakeAPI(t)
	gw := newFakeGateway(t, "bridge-bot")
	fl := newFakeLoopback(t)
	for _, s := range setup {
		s(fl)
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	opts := Options{
		Client:      NewClient(api.srv.URL, "tok-test", nil),
		Gateway:     NewGateway(gw.url(), "tok-test", nil),
		Channel:     "chan-1",
		LoopbackURL: fl.srv.URL,
		State:       LoadState(statePath),
		Logger:      func(string, ...any) {},
	}
	if mutate != nil {
		mutate(&opts)
	}

	b, err := NewBridge(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st := &stoatTest{api: api, gw: gw, fl: fl, b: b, ctx: ctx}

	go func() { _ = b.Run(ctx) }()

	// Wait for the gateway connection to be live before pushing events.
	select {
	case <-gw.connected:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway never connected")
	}
	// And for the bridge's SSE subscription to be up: the first prompt
	// flushes through /api/prompt, and the done frame must find the
	// consumer attached. The consumer reconnects every second, so a
	// small wait is enough; Require the SSE to be attached by watching
	// for the gateway's Authenticate frame instead (bridge boot order:
	// SSE consumer starts before the gateway loop runs).
	require.Eventually(t, func() bool {
		for _, f := range gw.clientFrames() {
			if strings.Contains(f, "Authenticate") {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond, "bridge never authenticated")
	return st
}

// message pushes one channel message through the event stream.
func (st *stoatTest) message(t *testing.T, id, author, content string) {
	t.Helper()
	st.gw.pushEvent(t, msgEvent(id, "chan-1", author, content))
}

// waitPrompt blocks until the loopback side has recorded a prompt AND
// the bridge has committed it — BeginTyping is prompt()'s last call, so
// an SSE frame pushed after this returns cannot race the driven-session
// bookkeeping (the same role waitTyping plays in the matrix tests).
func (st *stoatTest) waitPrompt(t *testing.T) promptCall {
	t.Helper()
	require.Eventually(t, func() bool {
		return len(st.fl.recordedPrompts()) > 0
	}, 3*time.Second, 25*time.Millisecond, "the message never became a prompt")
	require.Eventually(t, func() bool {
		for _, f := range st.gw.typingFrames() {
			if f == "BeginTyping:chan-1" {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond, "the prompt path never committed (no BeginTyping)")
	return st.fl.recordedPrompts()[0]
}

// ---- the bridge trio ----

func TestBridgeMessageBecomesPromptAndReplyArrives(t *testing.T) {
	st := startBridge(t, nil)

	st.message(t, "m1", "alice", "fix the bug")
	prompt := st.waitPrompt(t)
	assert.Equal(t, "fix the bug", prompt.Prompt)
	assert.Empty(t, prompt.SessionID, "a fresh bridge requests a new session")

	st.fl.setTranscript(
		transcriptMessage{Role: "user", Text: "fix the bug"},
		transcriptMessage{Role: "assistant", Text: "on it."},
	)
	st.fl.push <- sseFrame("done", `{"kind":"done","sessionId":"sess-1"}`)

	require.Eventually(t, func() bool {
		for _, m := range st.api.sent() {
			if m.Content == "on it." {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond, "the reply never arrived in the channel")
}

func TestBridgeReplyChunksLongAnswers(t *testing.T) {
	st := startBridge(t, nil)

	st.message(t, "m1", "alice", "write a lot")
	st.waitPrompt(t)

	long := strings.Repeat("é", 2100) + "\n" + strings.Repeat("z", 100)
	st.fl.setTranscript(transcriptMessage{Role: "assistant", Text: long})
	st.fl.push <- sseFrame("done", `{"kind":"done","sessionId":"sess-1"}`)

	require.Eventually(t, func() bool {
		return len(st.api.sent()) == 2
	}, 3*time.Second, 25*time.Millisecond, "the reply must arrive as two chunks")
	chunks := st.api.sent()
	for _, c := range chunks {
		assert.LessOrEqual(t, runeLen(c.Content), maxMessageChars, "every chunk fits the wire limit")
	}
	// Byte-exact round trip: the chunks are the reply, not a rendering of it.
	assert.Equal(t, long, chunks[0].Content+chunks[1].Content)
}

func TestBridgeAskApprovesOverChat(t *testing.T) {
	st := startBridge(t, nil)

	st.message(t, "m1", "alice", "run the tests")
	st.waitPrompt(t)

	st.fl.push <- sseFrame("ask", `{"kind":"ask","sessionId":"sess-1","ask":{"id":"a1","tool":"bash","reason":"run pytest"}}`)
	require.Eventually(t, func() bool {
		for _, m := range st.api.sent() {
			if strings.Contains(m.Content, "approval needed: bash") {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond, "the approval never surfaced")

	st.message(t, "m2", "alice", "y")

	require.Eventually(t, func() bool {
		asks := st.fl.recordedAsks()
		return len(asks) == 1 && asks[0].ID == "a1" && asks[0].Approved
	}, 3*time.Second, 25*time.Millisecond, "the y never answered the approval")
	require.Eventually(t, func() bool {
		for _, m := range st.api.sent() {
			if m.Content == "approved." {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond)
}

func TestBridgeAskDeniesOverChat(t *testing.T) {
	st := startBridge(t, nil)

	st.message(t, "m1", "alice", "run the tests")
	st.waitPrompt(t)

	st.fl.push <- sseFrame("ask", `{"kind":"ask","sessionId":"sess-1","ask":{"id":"a1","tool":"bash","reason":"rm -rf"}}`)
	require.Eventually(t, func() bool {
		for _, m := range st.api.sent() {
			if strings.Contains(m.Content, "approval needed: bash") {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond, "the approval never surfaced")

	st.message(t, "m2", "alice", "n")

	require.Eventually(t, func() bool {
		asks := st.fl.recordedAsks()
		return len(asks) == 1 && !asks[0].Approved
	}, 3*time.Second, 25*time.Millisecond)
}

func TestBridgeAskExpiryNotifiesWithoutAnswering(t *testing.T) {
	st := startBridge(t, func(o *Options) { o.AskWindow = 150 * time.Millisecond })

	st.message(t, "m1", "alice", "go")
	st.waitPrompt(t)

	st.fl.push <- sseFrame("ask", `{"kind":"ask","sessionId":"sess-1","ask":{"id":"a1","tool":"bash","reason":"x"}}`)
	require.Eventually(t, func() bool {
		for _, m := range st.api.sent() {
			if strings.Contains(m.Content, "approval needed") {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond)

	require.Eventually(t, func() bool {
		for _, m := range st.api.sent() {
			if strings.Contains(m.Content, "no answer in the approval window") {
				return true
			}
		}
		return false
	}, 3*time.Second, 50*time.Millisecond, "the expiry notice never arrived")
	// The bridge itself never answers: the webserver's timeout denies.
	assert.Empty(t, st.fl.recordedAsks())
}

func TestBridgePromptFailurePostsNotice(t *testing.T) {
	st := startBridge(t, nil)
	st.fl.failPrompt(http.StatusInternalServerError, "model not configured")

	st.message(t, "m1", "alice", "hello")

	require.Eventually(t, func() bool {
		for _, m := range st.api.sent() {
			if strings.Contains(m.Content, "could not send the prompt") {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond)
}

func TestBridgeFailedTurnPostsError(t *testing.T) {
	st := startBridge(t, nil)

	st.message(t, "m1", "alice", "go")
	st.waitPrompt(t)

	st.fl.push <- sseFrame("status", `{"kind":"status","sessionId":"sess-1","status":{"text":"turn failed: model overloaded"},"failed":true}`)

	require.Eventually(t, func() bool {
		for _, m := range st.api.sent() {
			if m.Content == "turn failed: model overloaded" {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond)
}

func TestBridgeStopCancelsAndReportsStopped(t *testing.T) {
	st := startBridge(t, nil)

	st.message(t, "m1", "alice", "go")
	st.waitPrompt(t)

	st.message(t, "m2", "alice", "!stop")

	require.Eventually(t, func() bool {
		return len(st.fl.recordedCancels()) == 1
	}, 3*time.Second, 25*time.Millisecond, "!stop never cancelled")

	st.fl.push <- sseFrame("status", `{"kind":"status","sessionId":"sess-1","status":{"text":"turn failed: context canceled"},"failed":true}`)
	require.Eventually(t, func() bool {
		for _, m := range st.api.sent() {
			if m.Content == "stopped." {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond, "a cancelled turn reads as stopped, not as an error")
}

func TestBridgeNewSessionResetsTheNextPrompt(t *testing.T) {
	st := startBridge(t, nil)

	st.message(t, "m1", "alice", "one")
	st.waitPrompt(t)

	st.message(t, "m2", "alice", "!new")
	require.Eventually(t, func() bool {
		for _, m := range st.api.sent() {
			if strings.Contains(m.Content, "new session") {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond)

	st.message(t, "m3", "alice", "two")
	require.Eventually(t, func() bool {
		return len(st.fl.recordedPrompts()) == 2
	}, 3*time.Second, 25*time.Millisecond)
	// The fresh prompt posts with no session id.
	assert.Empty(t, st.fl.recordedPrompts()[1].SessionID)
}

func TestBridgeIgnoresOtherChannelsEchoSystemBotsAndBareY(t *testing.T) {
	st := startBridge(t, nil)

	st.gw.pushEvent(t, msgEvent("x1", "chan-other", "alice", "not this channel"))
	st.gw.pushEvent(t, msgEvent("x2", "chan-1", "bridge-bot", "the bridge's own echo"))
	st.gw.pushEvent(t, systemEvent("chan-1"))
	st.gw.pushEvent(t, botEvent("chan-1", "beep boop"))
	st.gw.pushEvent(t, msgEvent("x3", "chan-1", "alice", "y")) // no pending approval

	require.Eventually(t, func() bool {
		return len(st.fl.recordedPrompts()) == 1
	}, 3*time.Second, 25*time.Millisecond, "only the bare y (a prompt) crosses")
	assert.Equal(t, "y", st.fl.recordedPrompts()[0].Prompt)
	assert.Empty(t, st.fl.recordedAsks())
}

func TestBridgeResumesStoredSessionAndDiffsCorrectly(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	state := LoadState(statePath)
	state.SessionID = "sess-old"
	require.NoError(t, state.Save())

	st := startBridge(t, func(o *Options) { o.State = state }, func(fl *fakeLoopback) {
		fl.setTranscript(
			transcriptMessage{Role: "user", Text: "old question"},
			transcriptMessage{Role: "assistant", Text: "old answer"},
		)
	})

	// The adoption fetch anchors at the transcript's end.
	require.Eventually(t, func() bool {
		for _, id := range st.fl.fetchIDs() {
			if id == "sess-old" {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond, "the stored session was never adopted")

	st.message(t, "m1", "alice", "and now?")
	require.Eventually(t, func() bool {
		prompts := st.fl.recordedPrompts()
		return len(prompts) == 1 && prompts[0].SessionID == "sess-old"
	}, 3*time.Second, 25*time.Millisecond, "the prompt continues the stored session")

	// The next reply carries only the NEW assistant text.
	st.fl.setTranscript(
		transcriptMessage{Role: "user", Text: "old question"},
		transcriptMessage{Role: "assistant", Text: "old answer"},
		transcriptMessage{Role: "user", Text: "and now?"},
		transcriptMessage{Role: "assistant", Text: "fresh answer"},
	)
	st.fl.push <- sseFrame("done", `{"kind":"done","sessionId":"sess-old"}`)
	require.Eventually(t, func() bool {
		for _, m := range st.api.sent() {
			if m.Content == "fresh answer" {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond, "the diff anchor kept the old answer out")
}

func TestBridgeStoredSessionUnavailableStartsFresh(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	state := LoadState(statePath)
	state.SessionID = "sess-gone"
	require.NoError(t, state.Save())

	st := startBridge(t, func(o *Options) { o.State = state }, func(fl *fakeLoopback) {
		fl.failTranscript(http.StatusNotFound, "no such session")
	})

	st.message(t, "m1", "alice", "hi")
	require.Eventually(t, func() bool {
		prompts := st.fl.recordedPrompts()
		return len(prompts) == 1 && prompts[0].SessionID == ""
	}, 3*time.Second, 25*time.Millisecond, "a deleted session means a fresh start, not a boot failure")
}

func TestBridgeTypingFollowsTurns(t *testing.T) {
	st := startBridge(t, nil)

	st.message(t, "m1", "alice", "go")
	st.waitPrompt(t)

	// prompt() begins typing immediately.
	require.Eventually(t, func() bool {
		for _, f := range st.gw.typingFrames() {
			if f == "BeginTyping:chan-1" {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond, "posting a prompt must begin typing")

	st.fl.push <- sseFrame("done", `{"kind":"done","sessionId":"sess-1"}`)
	require.Eventually(t, func() bool {
		for _, f := range st.gw.typingFrames() {
			if f == "EndTyping:chan-1" {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "a finished turn must end typing (the 3s refresh tick may lag)")
}

func TestBridgeSSEFilterIgnoresUndrivenSessions(t *testing.T) {
	st := startBridge(t, nil)

	// A session the bridge never prompted (GUI-driven, say) publishes
	// events; the bridge has no business replying to it.
	st.fl.push <- sseFrame("done", `{"kind":"done","sessionId":"sess-elsewhere"}`)

	st.message(t, "m1", "alice", "mine")
	st.waitPrompt(t)

	require.Eventually(t, func() bool {
		return len(st.api.sent()) == 0
	}, 500*time.Millisecond, 25*time.Millisecond, "undriven sessions produce no sends")
}

// ---- Connect ----

func TestConnectValidatesTokenChannelAndType(t *testing.T) {
	api := newFakeAPI(t)

	_, _, err := Connect(context.Background(), Config{Token: "tok-test", Channel: "chan-1", API: api.srv.URL}, nil)
	require.NoError(t, err)

	_, _, err = Connect(context.Background(), Config{Token: "tok-wrong", Channel: "chan-1", API: api.srv.URL}, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "Unauthorized")

	_, _, err = Connect(context.Background(), Config{Token: "tok-test", Channel: "chan-missing", API: api.srv.URL}, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "NotFound")

	_, _, err = Connect(context.Background(), Config{Token: "", Channel: "chan-1"}, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "token")

	_, _, err = Connect(context.Background(), Config{Token: "tok-test", Channel: ""}, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "channel")
}

// ---- the gateway trio (against the bridge's own Gateway) ----

func TestGatewayAuthenticatesAndSurfacesMessages(t *testing.T) {
	gw := newFakeGateway(t, "bridge-bot")
	g := NewGateway(gw.url(), "tok-test", nil)

	got := make(chan MessageEvent, 8)
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { done <- g.Run(ctx, func(_ context.Context, ev MessageEvent) { got <- ev }) }()

	select {
	case <-gw.connected:
	case <-time.After(3 * time.Second):
		t.Fatal("never connected")
	}
	require.Eventually(t, func() bool {
		for _, f := range gw.clientFrames() {
			if strings.Contains(f, `"Authenticate"`) && strings.Contains(f, "tok-test") {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond, "the token authenticates as a message, not a query")
	require.Eventually(t, func() bool { return g.SelfID() == "bridge-bot" }, 3*time.Second, 25*time.Millisecond)

	gw.pushEvent(t, msgEvent("m1", "chan-1", "alice", "hello"))
	select {
	case ev := <-got:
		assert.Equal(t, "alice", ev.Author)
		assert.Equal(t, "hello", ev.Content)
	case <-time.After(3 * time.Second):
		t.Fatal("the message never surfaced")
	}

	// Bulk carries several events; non-Message entries are ignored.
	gw.pushEvent(t, `{"type":"Bulk","v":[{"type":"ChannelStartTyping","id":"chan-1","user":"alice"},`+
		msgEvent("m2", "chan-1", "bob", "from bulk")+`]}`)
	select {
	case ev := <-got:
		assert.Equal(t, "from bulk", ev.Content)
	case <-time.After(3 * time.Second):
		t.Fatal("the bulked message never surfaced")
	}

	cancel()
	require.NoError(t, <-done)
}

func TestGatewayRefusesReadyWithoutSelf(t *testing.T) {
	gw := newFakeGateway(t, "bridge-bot")
	gw.mu.Lock()
	gw.noSelf = true
	gw.mu.Unlock()

	g := NewGateway(gw.url(), "tok-test", nil)
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { done <- g.Run(ctx, func(context.Context, MessageEvent) {}) }()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.ErrorContains(t, err, "no self user")
	case <-time.After(3 * time.Second):
		t.Fatal("the missing self user must be fatal")
	}
	cancel()
}

func TestGatewayServerErrorIsFatal(t *testing.T) {
	gw := newFakeGateway(t, "bridge-bot")
	g := NewGateway(gw.url(), "tok-test", nil)
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { done <- g.Run(ctx, func(context.Context, MessageEvent) {}) }()

	select {
	case <-gw.connected:
	case <-time.After(3 * time.Second):
		t.Fatal("never connected")
	}
	gw.pushEvent(t, `{"type":"Error","data":{"type":"InvalidSession"}}`)

	select {
	case err := <-done:
		require.Error(t, err)
		assert.ErrorContains(t, err, "InvalidSession")
	case <-time.After(3 * time.Second):
		t.Fatal("the Error event must end the connection")
	}
	cancel()
}

func TestGatewayDropsMessagesBeforeReady(t *testing.T) {
	gw := newFakeGateway(t, "bridge-bot")
	gw.mu.Lock()
	gw.holdReady = true
	gw.mu.Unlock()

	g := NewGateway(gw.url(), "tok-test", nil)
	got := make(chan MessageEvent, 8)
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { done <- g.Run(ctx, func(_ context.Context, ev MessageEvent) { got <- ev }) }()

	select {
	case <-gw.connected:
	case <-time.After(3 * time.Second):
		t.Fatal("never connected")
	}
	require.Eventually(t, func() bool {
		for _, f := range gw.clientFrames() {
			if strings.Contains(f, "Authenticate") {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond, "authentication never left")

	// A message with no Ready behind it: dropped, not prompted on.
	gw.pushEvent(t, msgEvent("early", "chan-1", "alice", "too early"))
	select {
	case ev := <-got:
		t.Fatalf("a pre-Ready message surfaced: %+v", ev)
	case <-time.After(300 * time.Millisecond):
	}

	// Ready arrives; the next message surfaces.
	gw.pushEvent(t, gw.readyBody("bridge-bot", false))
	require.Eventually(t, func() bool { return g.SelfID() == "bridge-bot" }, 3*time.Second, 25*time.Millisecond)
	gw.pushEvent(t, msgEvent("ontime", "chan-1", "alice", "right on time"))
	select {
	case ev := <-got:
		assert.Equal(t, "right on time", ev.Content)
	case <-time.After(3 * time.Second):
		t.Fatal("the post-Ready message never surfaced")
	}
	cancel()
	require.NoError(t, <-done)
}

// runeLen is the chunker's unit, named for the test assertions.
func runeLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// fetchIDs snapshots the loopback's transcript fetches.
func (fl *fakeLoopback) fetchIDs() []string {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	return append([]string(nil), fl.fetches...)
}
