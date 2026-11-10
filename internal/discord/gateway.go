package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/damien1141/a1/internal/ws"
)

// intents is the gateway subscription: guild messages (1<<9), direct
// messages (1<<12), and the PRIVILEGED message content intent (1<<15).
// The last one must also be flipped on in the Developer Portal — the
// gateway refuses the connection (close code 4014) when it is not, and
// the refusal text surfaces in the bridge's log for exactly that
// reason.
const intents = 1<<9 | 1<<12 | 1<<15

// Gateway is one Discord gateway v10 connection: HELLO → IDENTIFY →
// READY, then MESSAGE_CREATE dispatches. Its lifetime is ONE connection
// — reconnect policy (backoff, drop-the-gap) is the bridge's business.
// There is no RESUME in v1: a reconnected bridge identifies fresh and
// the messages that arrived during the gap are gone, the same
// "history is context, not prompts" ruling the other bridges set.
type Gateway struct {
	url   string
	token string
	logf  func(string, ...any)

	mu     sync.Mutex
	conn   *ws.Conn
	selfID string
	seq    atomic.Int64 // the last dispatch sequence number (heartbeats echo it)
}

// NewGateway returns a gateway for the gateway URL (empty = the
// default). The v=10&encoding=json query is appended when the URL
// carries no version of its own, so a config override may be the bare
// host or a fully-formed URL.
func NewGateway(gatewayURL, token string, logf func(string, ...any)) *Gateway {
	if gatewayURL == "" {
		gatewayURL = DefaultGateway
	}
	if !strings.Contains(gatewayURL, "v=") {
		join := "?"
		if strings.Contains(gatewayURL, "?") {
			join = "&"
		}
		gatewayURL += join + gatewayQuery
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Gateway{url: gatewayURL, token: token, logf: logf}
}

// MessageEvent is one channel message, pre-filtered: other bots never
// surface (Discord marks them author.bot — a bridge that answers bots
// is a loop machine), and empty content never surfaces (embed-only
// messages, or the missing MESSAGE CONTENT intent's silent failure).
type MessageEvent struct {
	ID        string
	ChannelID string
	AuthorID  string
	Content   string
}

// SelfID returns the bot's own user id ("" before READY).
func (g *Gateway) SelfID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.selfID
}

// The gateway op codes (v10).
const (
	opDispatch     = 0 // t = event name, s = seq, d = payload
	opHeartbeat    = 1 // client→server: {"op":1,"d":<seq|null>}
	opIdentify     = 2
	opReconnect    = 7  // server: reconnect now
	opInvalidSess  = 9  // server: d = resumable bool
	opHello        = 10 // server: d.heartbeat_interval ms
	opHeartbeatAck = 11
)

// payload is the wire envelope every gateway frame shares.
type payload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

// Run connects, identifies, and dispatches MESSAGE_CREATE events until
// the connection dies or ctx is done. onMessage is called on the Run
// goroutine with Run's own ctx; messages arriving before READY are
// dropped (Discord guarantees READY first — a message before it is a
// protocol surprise worth refusing, not working around).
func (g *Gateway) Run(ctx context.Context, onMessage func(context.Context, MessageEvent)) error {
	conn, err := ws.Dial(ctx, g.url, nil, nil)
	if err != nil {
		return fmt.Errorf("discord: gateway: %w", err)
	}
	defer func() { _ = conn.Close() }()
	g.mu.Lock()
	g.conn = conn
	g.mu.Unlock()

	stopBeat := make(chan struct{})
	defer close(stopBeat)
	ready := false
	for {
		data, err := conn.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("discord: gateway: %w", err)
		}
		var p payload
		if err := json.Unmarshal(data, &p); err != nil {
			g.logf("discord: gateway: decode: %v", err)
			continue
		}
		if p.S != nil {
			g.seq.Store(*p.S)
		}
		switch p.Op {
		case opHello:
			// d.heartbeat_interval (ms) opens the liveness contract.
			var hello struct {
				HeartbeatInterval float64 `json:"heartbeat_interval"`
			}
			if err := json.Unmarshal(p.D, &hello); err != nil || hello.HeartbeatInterval <= 0 {
				return fmt.Errorf("discord: gateway: hello carried no heartbeat interval")
			}
			interval := time.Duration(hello.HeartbeatInterval * float64(time.Millisecond))
			go g.heartbeatLoop(ctx, interval, stopBeat)
			if err := g.identify(ctx); err != nil {
				return err
			}
		case opDispatch:
			switch p.T {
			case "READY":
				var readyEv struct {
					User struct {
						ID string `json:"id"`
					} `json:"user"`
					SessionID string `json:"session_id"`
				}
				if err := json.Unmarshal(p.D, &readyEv); err != nil || readyEv.User.ID == "" {
					return fmt.Errorf("discord: gateway: ready carried no self user — cannot suppress echo loops")
				}
				g.mu.Lock()
				g.selfID = readyEv.User.ID
				g.mu.Unlock()
				ready = true
			case "MESSAGE_CREATE":
				if !ready {
					continue
				}
				if ev, ok := parseMessage(p.D); ok {
					onMessage(ctx, ev)
				}
			default:
				// Every other dispatch — presences, typing, guild
				// churn — is ignored by design.
			}
		case opHeartbeat:
			// The server asked for a beat right now.
			_ = g.sendHeartbeat(ctx)
		case opHeartbeatAck:
			// The ws layer's idle deadline is the judge of liveness.
		case opReconnect:
			return fmt.Errorf("discord: gateway: server requested reconnect")
		case opInvalidSess:
			// v1 does not RESUME: identify fresh on the next connect.
			// The flag is logged for the operator, not acted on.
			resumable := string(p.D) == "true"
			return fmt.Errorf("discord: gateway: invalid session (resumable=%v)", resumable)
		default:
			// Unknown op codes are forward compatibility — ignored.
		}
	}
}

// identify sends the IDENTIFY payload: the bot token, the intents, and
// the connection properties (a honest client name, no browser
// masquerade).
func (g *Gateway) identify(ctx context.Context) error {
	body := struct {
		Token      string            `json:"token"`
		Intents    int               `json:"intents"`
		Properties map[string]string `json:"properties"`
	}{
		Token:   g.token,
		Intents: intents,
		Properties: map[string]string{
			"os":      "linux",
			"browser": "a1",
			"device":  "a1",
		},
	}
	return g.sendPayload(ctx, opIdentify, body)
}

// sendHeartbeat sends one beat carrying the last sequence number (null
// before any dispatch — the spec's own spelling).
func (g *Gateway) sendHeartbeat(ctx context.Context) error {
	var seq any
	if s := g.seq.Load(); s > 0 {
		seq = s
	}
	return g.sendPayload(ctx, opHeartbeat, seq)
}

// heartbeatLoop keeps the connection alive: the first beat waits a
// jittered fraction of the interval (the spec's anti-thundering-herd
// guidance), then one beat per interval until the connection or ctx
// dies. Missing ACKs are the ws idle deadline's problem, not ours.
func (g *Gateway) heartbeatLoop(ctx context.Context, interval time.Duration, stop <-chan struct{}) {
	jitter := time.Duration(rand.Float64() * float64(interval))
	select {
	case <-ctx.Done():
		return
	case <-stop:
		return
	case <-time.After(jitter):
	}
	beat := func() {
		bctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		_ = g.sendHeartbeat(bctx)
	}
	beat()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-tick.C:
			beat()
		}
	}
}

// parseMessage filters one MESSAGE_CREATE down to prompt material:
// author.bot never surfaces, empty content never surfaces.
func parseMessage(data []byte) (MessageEvent, bool) {
	var m struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
		Author    struct {
			ID  string `json:"id"`
			Bot bool   `json:"bot"`
		} `json:"author"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return MessageEvent{}, false
	}
	if m.Author.Bot {
		return MessageEvent{}, false
	}
	if strings.TrimSpace(m.Content) == "" || m.ChannelID == "" {
		return MessageEvent{}, false
	}
	return MessageEvent{ID: m.ID, ChannelID: m.ChannelID, AuthorID: m.Author.ID, Content: m.Content}, true
}

// sendPayload writes one gateway frame.
func (g *Gateway) sendPayload(ctx context.Context, op int, d any) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("discord: gateway: encode: %w", err)
	}
	return g.send(ctx, payload{Op: op, D: raw})
}

func (g *Gateway) send(ctx context.Context, p payload) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("discord: gateway: encode: %w", err)
	}
	g.mu.Lock()
	conn := g.conn
	g.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("discord: gateway: no connection")
	}
	return conn.Send(data)
}
