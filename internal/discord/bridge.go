package discord

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Config is the discord: config section, bridge-side. It mirrors
// project.DiscordConfig field-for-field; the package keeps its own copy so
// the bridge (and its tests) do not depend on the project package.
type Config struct {
	Token   string
	Channel string
	API     string
	Gateway string
}

// defaultAskWindow sits 30s under the webserver's 10-minute permission-ask
// timeout, so the channel hears "window closed" from the bridge just
// before the server-side denial lands — never after. (Same ruling as the
// other bridges, same number.)
const defaultAskWindow = 9*time.Minute + 30*time.Second

// Connect validates the config against the live API: /users/@me proves
// the token and yields the bot's own id, then one channel fetch proves
// the bot can see the channel and the channel is a bridgeable kind.
// There is no join step — guild membership is granted by inviting the
// bot, not requested here.
func Connect(ctx context.Context, cfg Config, logf func(string, ...any)) (*Client, *Gateway, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if cfg.Token == "" {
		return nil, nil, errors.New("discord: token is not configured")
	}
	if cfg.Channel == "" {
		return nil, nil, errors.New("discord: channel is not configured")
	}
	c := NewClient(cfg.API, cfg.Token, nil)
	selfID, err := c.Me(ctx)
	if err != nil {
		return nil, nil, err
	}
	ch, err := c.Channel(ctx, cfg.Channel)
	if err != nil {
		return nil, nil, err
	}
	logf("discord: bridging channel %s (type %d) as bot %s", ch.ID, ch.Type, selfID)
	g := NewGateway(cfg.Gateway, cfg.Token, logf)
	return c, g, nil
}

// Options configures [NewBridge].
type Options struct {
	// Client is the authenticated REST client (from Connect).
	Client *Client
	// Gateway is the event-websocket half (from Connect); the bridge
	// re-runs it across reconnects, so one instance serves for the
	// process lifetime.
	Gateway *Gateway
	// Channel is the bridged channel id.
	Channel string
	// LoopbackURL is the in-process webserver's base URL — the bridge is
	// a loopback client of the chat API, never a new network surface.
	LoopbackURL string
	// State carries the persisted session id; nil disables persistence.
	State *State
	// Logger receives the bridge's stderr lines; nil discards them.
	Logger func(format string, args ...any)
	// AskWindow bounds how long a channel-side y/n approval is waited
	// for. Zero = defaultAskWindow.
	AskWindow time.Duration
}

// Bridge runs one Discord channel ↔ one a1 session.
//
// Concurrency: four goroutines. The gateway loop (Run's caller
// goroutine) consumes channel events and never blocks on the agent —
// prompts go through a queued worker, because POST /api/prompt
// deliberately waits out an in-flight turn (the webserver serializes
// turns server-side), and a gateway loop stuck in that wait could not
// read the "y" that would free it. The SSE consumer reads the loopback
// event stream; the typing refresher keeps the channel's indicator
// alive while turns run.
type Bridge struct {
	client   *Client
	gateway  *Gateway
	channel  string
	loopback string
	state    *State
	logf     func(string, ...any)
	askWin   time.Duration

	// sseHTTP has NO timeout: the event stream is one long-lived response
	// (keepalives every 20s). apiHTTP bounds the request/response calls.
	sseHTTP *http.Client
	apiHTTP *http.Client

	// prompts serializes channel messages into engine turns, decoupling
	// the gateway loop from the webserver's per-session turn mutex.
	prompts chan string

	mu         sync.Mutex
	session    string          // the channel's current loopback session ("" = next message starts one)
	driven     map[string]bool // sessions this bridge prompted (SSE filter)
	seen       map[string]int  // transcript length per session (turn-diff anchor)
	pendingAsk *pendingAsk     // outstanding channel-side approval
	cancelled  map[string]bool // sessions with a just-issued !stop
	inFlight   int             // turns posted but not finished (typing flag)
}

// pendingAsk is one approval awaiting a channel-side y/n.
type pendingAsk struct {
	id    string
	timer *time.Timer // expiry notice; stopped when answered
}

// NewBridge validates the wiring and returns a bridge ready to Run.
func NewBridge(opts Options) (*Bridge, error) {
	if opts.Client == nil {
		return nil, errors.New("discord: bridge: client is required")
	}
	if opts.Gateway == nil {
		return nil, errors.New("discord: bridge: gateway is required")
	}
	if strings.TrimSpace(opts.Channel) == "" {
		return nil, errors.New("discord: bridge: channel is required")
	}
	if strings.TrimSpace(opts.LoopbackURL) == "" {
		return nil, errors.New("discord: bridge: loopback URL is required")
	}
	logf := opts.Logger
	if logf == nil {
		logf = func(string, ...any) {}
	}
	askWin := opts.AskWindow
	if askWin <= 0 {
		askWin = defaultAskWindow
	}
	b := &Bridge{
		client:    opts.Client,
		gateway:   opts.Gateway,
		channel:   opts.Channel,
		loopback:  strings.TrimRight(opts.LoopbackURL, "/"),
		state:     opts.State,
		logf:      logf,
		askWin:    askWin,
		sseHTTP:   &http.Client{},
		apiHTTP:   &http.Client{Timeout: 60 * time.Second},
		prompts:   make(chan string, 256),
		driven:    map[string]bool{},
		seen:      map[string]int{},
		cancelled: map[string]bool{},
	}
	if opts.State != nil {
		b.session = opts.State.SessionID
	}
	return b, nil
}

// Run blocks until ctx is done, bridging the channel.
func (b *Bridge) Run(ctx context.Context) error {
	if err := b.adoptStoredSession(ctx); err != nil {
		return err
	}
	go b.consumeEvents(ctx)
	go b.typingLoop(ctx)
	go b.promptLoop(ctx)
	return b.gatewayLoop(ctx)
}

// adoptStoredSession validates the persisted session id (the transcript
// must still exist) and anchors the turn-diff cursor at its end, so the
// first post-restart reply carries only new messages.
func (b *Bridge) adoptStoredSession(ctx context.Context) error {
	b.mu.Lock()
	sid := b.session
	b.mu.Unlock()
	if sid == "" {
		return nil
	}
	msgs, err := b.transcript(ctx, sid)
	if err != nil {
		// A deleted session is a fresh start, not a boot failure.
		b.logf("discord: stored session %s unavailable (%v) — starting fresh", sid, err)
		b.mu.Lock()
		b.session = ""
		b.mu.Unlock()
		b.state.SessionID = ""
		_ = b.state.Save()
		return nil
	}
	b.mu.Lock()
	b.driven[sid] = true
	b.seen[sid] = len(msgs)
	b.mu.Unlock()
	b.logf("discord: resumed session %s (%d messages)", sid, len(msgs))
	return nil
}

// ---- discord side: the gateway loop ----

// gatewayLoop keeps one gateway connection alive for the process
// lifetime, reconnecting after drops. Messages that arrived while
// disconnected are gone — history is context, not prompts (the same
// ruling as the other bridges: a three-hour-old "fix the bug" firing on
// reconnect would be worse). A disallowed-intent close (4014) lands here
// with its reason in the error text, which is the operator's cue to
// flip the MESSAGE CONTENT intent on in the Developer Portal.
func (b *Bridge) gatewayLoop(ctx context.Context) error {
	backoff := time.Second
	for {
		connectedAt := time.Now()
		err := b.gateway.Run(ctx, b.onDiscordMessage)
		if ctx.Err() != nil {
			return nil
		}
		// A connection that survived a while earns a reset; a fast loop
		// backs off to 30s so a hard-down gateway is not hammered.
		if time.Since(connectedAt) > time.Minute {
			backoff = time.Second
		} else if backoff < 30*time.Second {
			backoff *= 2
		}
		b.logf("discord: gateway: %v (reconnecting in %s)", err, backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
	}
}

// onDiscordMessage is one channel event. The gateway has already
// dropped other bots and empty content; here the bridge drops other
// channels, its own echo, and the commands, and queues the rest as
// prompts.
func (b *Bridge) onDiscordMessage(ctx context.Context, ev MessageEvent) {
	if ev.ChannelID != b.channel {
		return
	}
	if ev.AuthorID == b.gateway.SelfID() {
		// The bridge's own sends arrive as echo; acting on them would
		// loop the reply straight into a new prompt.
		return
	}
	text := strings.TrimSpace(ev.Content)
	if text == "" {
		return
	}
	switch text {
	case "!stop":
		b.stopTurn(ctx)
		return
	case "!new":
		b.newSession(ctx)
		return
	}
	// An approval answer outranks a prompt — but only while an approval
	// is actually outstanding; a bare "y" with nothing pending is a
	// prompt.
	if approve, isAnswer := yesNo(text); isAnswer && b.answerAsk(ctx, approve) {
		return
	}
	select {
	case b.prompts <- text:
	case <-ctx.Done():
	}
}

// yesNo maps the chat-side approval vocabulary.
func yesNo(s string) (approve, ok bool) {
	switch strings.ToLower(s) {
	case "y", "yes":
		return true, true
	case "n", "no":
		return false, true
	}
	return false, false
}

// stopTurn cancels the current session's in-flight turn (!stop).
func (b *Bridge) stopTurn(ctx context.Context) {
	b.mu.Lock()
	sid := b.session
	b.mu.Unlock()
	if sid == "" {
		b.notify(ctx, "nothing to stop.")
		return
	}
	if err := b.callLoopback(ctx, http.MethodPost, "/api/cancel", map[string]string{"sessionId": sid}, nil); err != nil {
		b.notify(ctx, "bridge: stop failed: "+err.Error())
		return
	}
	b.mu.Lock()
	b.cancelled[sid] = true
	b.mu.Unlock()
}

// newSession detaches the channel from its conversation (!new): the
// current turn, if any, still finishes into the old session and its
// reply arrives; the next message starts a fresh one.
func (b *Bridge) newSession(ctx context.Context) {
	b.mu.Lock()
	b.session = ""
	b.mu.Unlock()
	if b.state != nil {
		b.state.SessionID = ""
		_ = b.state.Save()
	}
	b.notify(ctx, "new session — the next message starts a fresh conversation.")
}

// ---- discord side: sends ----

// notify posts one of the bridge's own lines. Discord has no notice
// msgtype — the lines carry the "bridge:" voice the other bridges'
// notices do, just without the grey.
func (b *Bridge) notify(ctx context.Context, text string) {
	if err := b.client.SendMessage(ctx, b.channel, text); err != nil {
		b.logf("discord: notify: %v", err)
	}
}

// typingLoop keeps the channel's indicator alive while turns run.
// Discord's typing indicator lives ~10s and has NO end-typing route —
// it simply expires — so the refresh tick (7s) is the whole mechanism:
// while turns fly, the POST /channels/{id}/typing beats keep it lit;
// when they stop, it dies on its own.
func (b *Bridge) typingLoop(ctx context.Context) {
	tick := time.NewTicker(7 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if b.turnsInFlight() > 0 {
				tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				if err := b.client.Typing(tctx, b.channel); err != nil {
					b.logf("discord: typing: %v", err)
				}
				cancel()
			}
		}
	}
}

func (b *Bridge) turnsInFlight() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inFlight
}

// ---- Loopback side: the prompt worker ----

// promptLoop drains the prompt queue one message at a time. POST /api/prompt
// blocks while a turn is running (the webserver serializes turns per
// session), which is exactly the pacing this worker exists to absorb — the
// gateway loop stays free to read approvals.
func (b *Bridge) promptLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case text := <-b.prompts:
			b.prompt(ctx, text)
		}
	}
}

// prompt posts one channel message as a user turn on the loopback chat API.
func (b *Bridge) prompt(ctx context.Context, text string) {
	b.mu.Lock()
	sid := b.session
	b.mu.Unlock()

	var resp struct {
		SessionID string `json:"sessionId"`
	}
	if err := b.callLoopback(ctx, http.MethodPost, "/api/prompt",
		map[string]string{"sessionId": sid, "prompt": text}, &resp); err != nil {
		b.notify(ctx, "bridge: could not send the prompt: "+err.Error())
		return
	}
	if resp.SessionID == "" {
		b.notify(ctx, "bridge: the chat server returned no session id")
		return
	}
	b.mu.Lock()
	b.session = resp.SessionID
	b.driven[resp.SessionID] = true
	b.inFlight++
	b.mu.Unlock()
	if b.state != nil {
		b.state.SessionID = resp.SessionID
		if err := b.state.Save(); err != nil {
			b.logf("discord: %v", err)
		}
	}
	b.logf("discord: prompt on session %s", resp.SessionID)
	// The indicator lights immediately; the 7s refresh keeps it alive.
	tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	if err := b.client.Typing(tctx, b.channel); err != nil {
		b.logf("discord: typing: %v", err)
	}
	cancel()
}

// answerAsk resolves an outstanding approval with the channel's y/n. It
// reports whether an approval was actually pending (false = treat the
// text as a prompt).
func (b *Bridge) answerAsk(ctx context.Context, approve bool) bool {
	b.mu.Lock()
	ask := b.pendingAsk
	b.pendingAsk = nil
	b.mu.Unlock()
	if ask == nil {
		return false
	}
	if ask.timer != nil {
		ask.timer.Stop()
	}
	req := map[string]any{"id": ask.id, "approved": approve, "feedback": ""}
	if err := b.callLoopback(ctx, http.MethodPost, "/api/ask", req, nil); err != nil {
		b.notify(ctx, "bridge: answering the approval failed: "+err.Error())
		return true
	}
	if approve {
		b.notify(ctx, "approved.")
	} else {
		b.notify(ctx, "denied.")
	}
	return true
}

// expireAsk fires when the approval window lapses: the bridge stops
// listening for y/n, and the webserver's own ask timeout (30s later)
// does the actual denying. The channel hears why nothing happened.
func (b *Bridge) expireAsk(id string) {
	b.mu.Lock()
	ask := b.pendingAsk
	if ask == nil || ask.id != id {
		b.mu.Unlock()
		return
	}
	b.pendingAsk = nil
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b.notify(ctx, "no answer in the approval window — denied.")
}

// ---- Loopback side: the SSE consumer ----

// askInfo is one permission prompt raised mid-turn (the ask SSE event).
type askInfo struct {
	ID     string `json:"id"`
	Tool   string `json:"tool"`
	Reason string `json:"reason"`
}

// sseEvent is the slice of the webserver's serverEvent the bridge reads.
// Deltas and tool runs are ignored by design: the reply is read from the
// transcript (the authoritative record) when the turn finishes, so a
// dropped SSE connection mid-turn cannot eat the answer.
type sseEvent struct {
	Kind      string   `json:"kind"`
	SessionID string   `json:"sessionId"`
	Ask       *askInfo `json:"ask,omitempty"`
	Status    *struct {
		Text string `json:"text"`
	} `json:"status,omitempty"`
	Failed bool `json:"failed,omitempty"`
}

// consumeEvents keeps an SSE subscription to the loopback event stream
// alive for the process lifetime, reconnecting after drops. The stream
// has no replay buffer (by design, same as the browser): a reconnect
// mid-turn may miss transient statuses, but done/failed — the only
// events that matter here — always arrive at turn end, after any
// reconnect.
func (b *Bridge) consumeEvents(ctx context.Context) {
	for ctx.Err() == nil {
		if err := b.consumeOnce(ctx); err != nil && ctx.Err() == nil {
			b.logf("discord: sse: %v (reconnecting)", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

func (b *Bridge) consumeOnce(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.loopback+"/api/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := b.sseHTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("event stream: status %d", resp.StatusCode)
	}

	// Delta events carry the whole accumulated message per round; a long
	// turn can produce multi-megabyte data lines, so the scanner ceiling
	// is generous even though the bridge never decodes them.
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var name string
	var data []string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if name != "" && len(data) > 0 {
				b.dispatchSSE(ctx, name, strings.Join(data, "\n"))
			}
			name, data = "", nil
		case strings.HasPrefix(line, ":"):
			// keepalive comment
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	return sc.Err()
}

func (b *Bridge) dispatchSSE(ctx context.Context, name, payload string) {
	var ev sseEvent
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		b.logf("discord: sse: decode %s event: %v", name, err)
		return
	}
	b.mu.Lock()
	driven := b.driven[ev.SessionID]
	b.mu.Unlock()
	if !driven {
		return
	}
	switch name {
	case "ask":
		if ev.Ask != nil {
			b.onAsk(ctx, *ev.Ask)
		}
	case "done":
		b.finishTurn(ctx, ev.SessionID)
	case "status":
		// A failed turn never emits done (the webserver publishes the
		// error status and returns), so this is the turn's only end
		// signal.
		if ev.Status != nil && ev.Failed {
			b.onTurnError(ctx, ev.SessionID, ev.Status.Text)
		}
	}
}

// onAsk surfaces a permission prompt into the channel and opens the y/n
// window. The pending state is set BEFORE the notice leaves, so a fast
// "y" can never beat the bridge's own bookkeeping.
func (b *Bridge) onAsk(ctx context.Context, ask askInfo) {
	b.mu.Lock()
	if old := b.pendingAsk; old != nil && old.timer != nil {
		old.timer.Stop()
	}
	b.pendingAsk = nil
	b.mu.Unlock()

	line := "approval needed: " + ask.Tool
	if ask.Reason != "" {
		line += " — " + ask.Reason
	}
	line += "\nreply y or n"
	timer := time.AfterFunc(b.askWin, func() { b.expireAsk(ask.ID) })
	b.mu.Lock()
	b.pendingAsk = &pendingAsk{id: ask.ID, timer: timer}
	b.mu.Unlock()
	b.notify(ctx, line)
}

// finishTurn reads the turn's reply off the transcript and sends it to
// the channel. The diff anchor is the transcript length at the last
// turn (or at adoption), so multi-round turns, system riders, and
// compaction rows in between are all just "messages after the anchor" —
// only assistant text is extracted, in order.
func (b *Bridge) finishTurn(ctx context.Context, sessionID string) {
	b.mu.Lock()
	if b.inFlight > 0 {
		b.inFlight--
	}
	b.mu.Unlock()

	msgs, err := b.transcript(ctx, sessionID)
	if err != nil {
		b.notify(ctx, "bridge: could not read the reply: "+err.Error())
		return
	}
	b.mu.Lock()
	from := b.seen[sessionID]
	if from > len(msgs) {
		// The session store shrank (rewind/branch edit from the GUI) —
		// re-anchor at the top rather than reading out of range.
		from = 0
	}
	b.seen[sessionID] = len(msgs)
	b.mu.Unlock()

	var texts []string
	for _, m := range msgs[from:] {
		if m.Role == "assistant" && strings.TrimSpace(m.Text) != "" {
			texts = append(texts, m.Text)
		}
	}
	if len(texts) == 0 {
		// A turn that only ran tools (or was cancelled mid-round) has
		// nothing to say; the status line already told the story.
		return
	}
	reply := strings.Join(texts, "\n\n")
	for _, chunk := range chunkMessage(reply) {
		if err := b.client.SendMessage(ctx, b.channel, chunk); err != nil {
			b.logf("discord: send reply: %v", err)
			return
		}
	}
}

// onTurnError reports a failed turn to the channel. A !stop shows as a
// clean "stopped." instead of the raw context-cancelled error the
// webserver publishes for it.
func (b *Bridge) onTurnError(ctx context.Context, sessionID, text string) {
	b.mu.Lock()
	if b.inFlight > 0 {
		b.inFlight--
	}
	stopped := b.cancelled[sessionID]
	delete(b.cancelled, sessionID)
	b.mu.Unlock()
	if stopped && strings.Contains(text, "context canceled") {
		b.notify(ctx, "stopped.")
		return
	}
	b.notify(ctx, text)
}

// transcriptMessage is one row of GET /api/transcript/messages.
type transcriptMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

func (b *Bridge) transcript(ctx context.Context, sessionID string) ([]transcriptMessage, error) {
	var resp struct {
		Messages []transcriptMessage `json:"messages"`
	}
	if err := b.callLoopback(ctx, http.MethodGet, "/api/transcript/messages?id="+url.QueryEscape(sessionID), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Messages, nil
}

// callLoopback performs one JSON round trip against the in-process
// webserver. Non-2xx responses decode the {"error": …} body the chat API
// writes, so failures read as sentences, not status codes.
func (b *Bridge) callLoopback(ctx context.Context, method, path string, reqBody, respOut any) error {
	var reader io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("discord: encode request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.loopback+path, reader)
	if err != nil {
		return err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.apiHTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("discord: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	if respOut == nil {
		return nil
	}
	if err := json.Unmarshal(data, respOut); err != nil {
		return fmt.Errorf("discord: decode response: %w", err)
	}
	return nil
}
