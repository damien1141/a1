// Package discord bridges one Discord channel onto one a1 session.
//
// The third chat surface, built on the same loopback-client ruling the
// Matrix and stoat bridges locked: `a1 discord` boots the webserver
// in-process on an ephemeral loopback port and drives the agent through
// the same chat API + SSE stream the web GUI uses. Nothing about the
// agent's network posture changes; the only new connections are the
// bridge's outbound gateway websocket and REST calls.
//
// This file is the REST half, and it carries the one ruling the roadmap
// note locked before any code: Discord's rate limits are aggressive, so
// the send path PACES. Buckets are per route (identified by the
// X-RateLimit-Bucket header when the server deigns to send one, else the
// route path), paced BEFORE requests from the previous response's
// X-RateLimit-Remaining / X-RateLimit-Reset-After, and a 429 in spite of
// pacing is slept out per its retry_after body exactly once. Global 429s
// take a process-wide lockout — Discord's global bucket says "stop
// everything", and the honest response is to stop everything.
//
// One prerequisite no code can supply: the MESSAGE CONTENT intent is
// privileged and must be enabled in the Developer Portal's bot settings.
// Without it, MESSAGE_CREATE events arrive with empty content and the
// bridge sees nothing — the gateway's close code 4014 is the louder
// failure mode (disallowed intents), but the silent one is why the doc
// repeats it.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultAPI and DefaultGateway are the production endpoints; both are
// config-overridable so tests (and proxies) can point elsewhere.
const (
	DefaultAPI     = "https://discord.com/api/v10"
	DefaultGateway = "wss://gateway.discord.gg"
)

// gatewayQuery is appended to a configured gateway URL that carries no
// query of its own: protocol v10, JSON encoding — the blessed spelling.
const gatewayQuery = "v=10&encoding=json"

// Client is a Discord REST session (one bot token).
type Client struct {
	base  string
	token string
	http  *http.Client

	mu          sync.Mutex
	buckets     map[string]*bucketState
	globalUntil time.Time
}

type bucketState struct {
	remaining int
	resetAt   time.Time
}

// NewClient returns a client for the API base URL (empty = the default)
// with the given bot token.
func NewClient(base, token string, httpClient *http.Client) *Client {
	if base == "" {
		base = DefaultAPI
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{
		base:    strings.TrimRight(base, "/"),
		token:   token,
		http:    httpClient,
		buckets: map[string]*bucketState{},
	}
}

// Me is the token handshake: GET /users/@me proves the token is live and
// returns the bot's own user id — the echo filter's other half.
func (c *Client) Me(ctx context.Context) (id string, err error) {
	var me struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Bot      bool   `json:"bot"`
	}
	if err := c.do(ctx, http.MethodGet, "/users/@me", nil, &me); err != nil {
		return "", err
	}
	if me.ID == "" {
		return "", fmt.Errorf("discord: /users/@me returned no id")
	}
	return me.ID, nil
}

// ChannelInfo is the slice of GET /channels/{id} the bridge reads.
type ChannelInfo struct {
	ID   string `json:"id"`
	Type int    `json:"type"`
	Name string `json:"name"`
}

// bridgeableChannelTypes: guild text channels (0), DMs (1), and
// announcement channels (5) carry bridged text. Voice (2), categories
// (4), threads (11+), forums, and stage are refused at startup — the
// messages either do not exist there or do not belong to a bridge.
var bridgeableChannelTypes = map[int]bool{
	0: true, // GUILD_TEXT
	1: true, // DIRECT_MESSAGE
	5: true, // GUILD_ANNOUNCEMENT
}

// Channel fetches one channel — the startup validation: a 200 proves the
// token is live AND the bot can see the channel (it is in the guild with
// VIEW_CHANNEL, or the DM is open); the type check refuses the
// unbridgeable kinds up front instead of dropping their messages
// forever.
func (c *Client) Channel(ctx context.Context, id string) (ChannelInfo, error) {
	var ch ChannelInfo
	if err := c.do(ctx, http.MethodGet, "/channels/"+url.PathEscape(id), nil, &ch); err != nil {
		return ChannelInfo{}, err
	}
	if ch.ID == "" {
		return ChannelInfo{}, fmt.Errorf("discord: channel %s: server returned no id", id)
	}
	if !bridgeableChannelTypes[ch.Type] {
		return ChannelInfo{}, fmt.Errorf("discord: channel %s is type %d — bridge a text channel, announcement channel, or DM", id, ch.Type)
	}
	return ch, nil
}

// SendMessage posts one message. No idempotency key exists on this API
// (Discord's is not a header); the bucket pacer is the whole discipline.
func (c *Client) SendMessage(ctx context.Context, channel, content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("discord: send: content is empty")
	}
	route := "/channels/" + url.PathEscape(channel) + "/messages"
	for attempt := 0; ; attempt++ {
		if err := c.pace(ctx, route); err != nil {
			return err
		}
		req := struct {
			Content string `json:"content"`
		}{Content: content}
		resp, err := c.raw(ctx, http.MethodPost, route, &req)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			// Every 429 teaches the limiter — even the one that ends
			// this call; the next request benefits either way. Only the
			// RETRY is bounded.
			wait, global := retryAfter(resp)
			_ = resp.Body.Close()
			if global {
				c.setGlobalLockout(wait)
			} else {
				c.updateBucket(route, 0, wait)
			}
			if attempt > 0 {
				return fmt.Errorf("discord: send: rate limited (retry_after %s)", wait)
			}
			if err := sleep(ctx, wait); err != nil {
				return err
			}
			continue
		}
		defer func() { _ = resp.Body.Close() }()
		c.record(resp, route)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("discord: send: %w", responseError(resp))
		}
		return nil
	}
}

// Typing triggers the channel's typing indicator (~10s of life on
// Discord's side; there is no end-typing route — the indicator simply
// expires, so the bridge refreshes it on a shorter tick and lets it die
// when turns finish). It paces like every other request: the global
// lockout says stop everything, and typing is something.
func (c *Client) Typing(ctx context.Context, channel string) error {
	route := "/channels/" + url.PathEscape(channel) + "/typing"
	if err := c.pace(ctx, route); err != nil {
		return err
	}
	resp, err := c.raw(ctx, http.MethodPost, route, struct{}{})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	c.record(resp, route)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord: typing: %w", responseError(resp))
	}
	return nil
}

// pace waits until every applicable limiter allows a request: the
// global lockout first (it says stop everything), then the route's
// bucket (a stale bucket with a past reset is free capacity).
func (c *Client) pace(ctx context.Context, route string) error {
	for {
		c.mu.Lock()
		wait := time.Duration(0)
		if until := c.globalUntil; time.Now().Before(until) {
			wait = time.Until(until)
		} else if st := c.buckets[route]; st != nil && st.remaining <= 0 && time.Now().Before(st.resetAt) {
			wait = time.Until(st.resetAt)
		}
		c.mu.Unlock()
		if wait == 0 {
			return nil
		}
		if err := sleep(ctx, wait); err != nil {
			return err
		}
	}
}

// record folds one response's rate-limit headers into the route's
// bucket state. Keyed by ROUTE, not by the X-RateLimit-Bucket header:
// the bucket id is only known after a response, while pacing must
// decide before the next one — and the routes that share a bucket
// (send/edit/delete on one channel) are exactly the verbs this bridge
// never mixes. Discord's Reset-After is in SECONDS (floating) — the
// one unit difference from stoat's milliseconds, and a trap worth
// pinning in tests.
func (c *Client) record(resp *http.Response, route string) {
	remaining, err1 := strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining"))
	resetAfter, err2 := strconv.ParseFloat(resp.Header.Get("X-RateLimit-Reset-After"), 64)
	if err1 != nil || err2 != nil || resetAfter < 0 {
		return
	}
	c.updateBucket(route, remaining, time.Duration(resetAfter*float64(time.Second)))
}

func (c *Client) updateBucket(key string, remaining int, resetIn time.Duration) {
	if key == "" || resetIn < 0 {
		resetIn = 0
	}
	c.mu.Lock()
	c.buckets[key] = &bucketState{remaining: remaining, resetAt: time.Now().Add(resetIn)}
	c.mu.Unlock()
}

func (c *Client) setGlobalLockout(wait time.Duration) {
	c.mu.Lock()
	if until := time.Now().Add(wait); until.After(c.globalUntil) {
		c.globalUntil = until
	}
	c.mu.Unlock()
}

// retryAfter reads a 429 body: {"global": bool, "retry_after": SECONDS
// (float)}. A missing or malformed field falls back to a conservative
// one-second wait — wrong in the safe direction.
func retryAfter(resp *http.Response) (wait time.Duration, global bool) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return time.Second, false
	}
	var body struct {
		Global     bool    `json:"global"`
		RetryAfter float64 `json:"retry_after"`
	}
	if json.Unmarshal(data, &body) != nil || body.RetryAfter <= 0 {
		return time.Second, body.Global
	}
	return time.Duration(body.RetryAfter * float64(time.Second)), body.Global
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// apiError is the slice of Discord's error body worth surfacing: the
// message is a sentence ("Missing Permissions"), not a bare status.
type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// responseError renders one non-2xx response as a sentence.
func responseError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var e apiError
	if json.Unmarshal(data, &e) == nil && e.Message != "" {
		return fmt.Errorf("%d %s", resp.StatusCode, e.Message)
	}
	return fmt.Errorf("unexpected status %d", resp.StatusCode)
}

// raw performs one authenticated request and returns the raw response
// (the caller records rate-limit headers and closes the body).
func (c *Client) raw(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("discord: encode request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("discord: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bot "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discord: %s %s: %w", method, path, err)
	}
	return resp, nil
}

// do is the JSON round trip for the calls that carry no send-path
// discipline (the boot-time validations happen once).
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	resp, err := c.raw(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("discord: %s %s: read response: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e apiError
		if json.Unmarshal(data, &e) == nil && e.Message != "" {
			return fmt.Errorf("discord: %s %s: %d %s", method, path, resp.StatusCode, e.Message)
		}
		return fmt.Errorf("discord: %s %s: unexpected status %d", method, path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("discord: %s %s: decode response: %w", method, path, err)
	}
	return nil
}
