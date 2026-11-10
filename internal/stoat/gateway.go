package stoat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/damien1141/a1/internal/ws"
)

// pingInterval is the client heartbeat. The protocol doc says ping every
// 10-30 seconds to keep the connection from being dropped; 20s sits in
// the middle, and the ws layer's idle deadline (90s) tolerates a couple
// of lost rounds before tearing down.
const pingInterval = 20 * time.Second

// Gateway is one stoat event-websocket connection: authenticate, read
// Ready, then surface Message events. Its lifetime is ONE connection —
// reconnect policy (backoff, drop-the-gap) is the bridge's business,
// exactly as the Matrix sync loop owns its own backoff.
type Gateway struct {
	url   string
	token string
	logf  func(string, ...any)

	mu     sync.Mutex
	conn   *ws.Conn
	selfID string
}

// NewGateway returns a gateway for the event URL (empty = the default).
// The version=1&format=json query is appended when absent, so a config
// override may be either the bare endpoint or a fully-formed URL.
func NewGateway(wsURL, token string, logf func(string, ...any)) *Gateway {
	if wsURL == "" {
		wsURL = DefaultWS
	}
	if !strings.Contains(wsURL, "?") {
		wsURL += "?version=1&format=json"
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Gateway{url: wsURL, token: token, logf: logf}
}

// MessageEvent is one channel message, pre-filtered: system messages
// and other bots never surface (a bridge that answers bots is a loop
// machine). The gateway's own echo is filtered by the bridge, which
// knows the channel; here it is just a message.
type MessageEvent struct {
	ID      string
	Channel string
	Author  string
	Content string
}

// SelfID returns the bot's own user id ("" before Ready).
func (g *Gateway) SelfID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.selfID
}

// Run connects, authenticates, and dispatches events until the
// connection dies or ctx is done. onMessage is called on the Run
// goroutine with Run's own ctx; messages arriving before Ready are
// dropped (bonfire guarantees Ready first — a message before it is a
// protocol surprise worth refusing, not working around).
func (g *Gateway) Run(ctx context.Context, onMessage func(context.Context, MessageEvent)) error {
	conn, err := ws.Dial(ctx, g.url, nil, nil)
	if err != nil {
		return fmt.Errorf("stoat: gateway: %w", err)
	}
	defer func() { _ = conn.Close() }()
	g.mu.Lock()
	g.conn = conn
	g.mu.Unlock()

	// Authenticate as a message, not a query parameter: tokens in URLs
	// leak into logs (the same ruling as Matrix's Bearer-vs-query).
	if err := g.send(ctx, struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}{"Authenticate", g.token}); err != nil {
		return fmt.Errorf("stoat: gateway: authenticate: %w", err)
	}

	stopPing := make(chan struct{})
	defer close(stopPing)
	go g.pingLoop(ctx, stopPing)

	ready := false
	for {
		data, err := conn.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("stoat: gateway: %w", err)
		}
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &head); err != nil {
			g.logf("stoat: gateway: decode: %v", err)
			continue
		}
		switch head.Type {
		case "Authenticated":
			// The handshake's echo; Ready is the one that matters.
		case "Ready":
			if err := g.adoptReady(data); err != nil {
				return err
			}
			ready = true
		case "Message":
			if !ready {
				continue
			}
			if ev, ok := parseMessage(data); ok {
				onMessage(ctx, ev)
			}
		case "Bulk":
			// Several events at once: each item is its own event.
			var bulk struct {
				Events []json.RawMessage `json:"v"`
			}
			if json.Unmarshal(data, &bulk) != nil {
				continue
			}
			for _, raw := range bulk.Events {
				var h struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(raw, &h) != nil || h.Type != "Message" || !ready {
					continue
				}
				if ev, ok := parseMessage(raw); ok {
					onMessage(ctx, ev)
				}
			}
		case "Ping":
			// The server's liveness probe: echo the timestamp back.
			var ping struct {
				Data int64 `json:"data"`
			}
			_ = json.Unmarshal(data, &ping)
			_ = g.send(ctx, struct {
				Type string `json:"type"`
				Data int64  `json:"data"`
			}{"Pong", ping.Data})
		case "Pong":
			// Our heartbeat's answer; the ws idle deadline is the judge.
		case "Error":
			var e struct {
				Data *struct {
					Type string `json:"type"`
				} `json:"data"`
			}
			_ = json.Unmarshal(data, &e)
			if e.Data != nil && e.Data.Type != "" {
				return fmt.Errorf("stoat: gateway: server error: %s", e.Data.Type)
			}
			return fmt.Errorf("stoat: gateway: server error")
		}
	}
}

// adoptReady finds the bot's own user (relationship "User" — the
// platform's marker for "this one is you") and pins the echo filter to
// it. Without a self id the bridge cannot suppress its own sends, so
// its absence is fatal, not cosmetic.
func (g *Gateway) adoptReady(data []byte) error {
	var ready struct {
		Users []struct {
			ID           string `json:"_id"`
			Relationship string `json:"relationship"`
		} `json:"users"`
	}
	if err := json.Unmarshal(data, &ready); err != nil {
		return fmt.Errorf("stoat: gateway: decode ready: %w", err)
	}
	for _, u := range ready.Users {
		if u.Relationship == "User" && u.ID != "" {
			g.mu.Lock()
			g.selfID = u.ID
			g.mu.Unlock()
			return nil
		}
	}
	return fmt.Errorf("stoat: gateway: ready carried no self user — cannot suppress echo loops")
}

// parseMessage filters one Message event down to prompt material:
// system messages never surface (they are joins and leaves, not text),
// and other bots never surface — their events carry the embedded user
// object with bot information, which is exactly the marker to skip on.
func parseMessage(data []byte) (MessageEvent, bool) {
	var m struct {
		ID      string          `json:"_id"`
		Channel string          `json:"channel"`
		Author  string          `json:"author"`
		Content string          `json:"content"`
		System  json.RawMessage `json:"system"`
		User    *struct {
			Bot json.RawMessage `json:"bot"`
		} `json:"user"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return MessageEvent{}, false
	}
	if m.System != nil {
		return MessageEvent{}, false
	}
	if m.User != nil && m.User.Bot != nil {
		return MessageEvent{}, false
	}
	if m.Content == "" || m.Channel == "" {
		return MessageEvent{}, false
	}
	return MessageEvent{ID: m.ID, Channel: m.Channel, Author: m.Author, Content: m.Content}, true
}

// BeginTyping and EndTyping drive the channel's typing indicator over
// the event socket (the platform has no REST route for it). No-ops when
// the connection is down — a lost indicator never fails a turn.
func (g *Gateway) BeginTyping(channel string) { g.sendTyping("BeginTyping", channel) }

func (g *Gateway) EndTyping(channel string) { g.sendTyping("EndTyping", channel) }

func (g *Gateway) sendTyping(typ, channel string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = g.send(ctx, struct {
		Type    string `json:"type"`
		Channel string `json:"channel"`
	}{typ, channel})
}

// pingLoop keeps the server from dropping the connection: one Ping
// every pingInterval until the connection or ctx dies.
func (g *Gateway) pingLoop(ctx context.Context, stop <-chan struct{}) {
	tick := time.NewTicker(pingInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-tick.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_ = g.send(pctx, struct {
				Type string `json:"type"`
				Data int64  `json:"data"`
			}{"Ping", time.Now().UnixMilli()})
			cancel()
		}
	}
}

// send writes one JSON message on the live connection.
func (g *Gateway) send(ctx context.Context, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("stoat: gateway: encode: %w", err)
	}
	g.mu.Lock()
	conn := g.conn
	g.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("stoat: gateway: no connection")
	}
	return conn.Send(data)
}
