// Package stoat bridges one stoat.chat channel onto one a1 session.
//
// Stoat is the Revolt lineage (the platform rebranded; the wire did
// not): a REST API at api.stoat.chat speaking X-Bot-Token, and an event
// websocket at wss://stoat.chat/events speaking protocol v1 JSON. The
// design copies the Matrix bridge ruling for ruling — `a1 stoat` boots
// the webserver in-process on an ephemeral loopback port, and the
// bridge drives the agent through the same chat API + SSE stream the
// web GUI uses (POST /api/prompt, GET /api/events, POST /api/ask).
// Nothing about the agent's network posture changes; the only new
// connections are the bridge's outbound websocket and REST calls.
//
// The channel is the trust boundary — keep it private. Other bots do
// not get a turn (their messages arrive with embedded bot information
// and are skipped): a bridge that answers bots is a loop machine.
//
// This file is the REST half: message sends with the two disciplines
// stoat's fixed-window rate limiter demands — pace BEFORE the request
// (the messaging bucket is 10 sends per 10 seconds per channel) and
// honor the 429 body's retry_after when the window says otherwise.
package stoat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
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

// DefaultAPI and DefaultWS are the production endpoints; both are
// config-overridable so tests (and self-hosters) can point elsewhere.
const (
	DefaultAPI = "https://api.stoat.chat"
	DefaultWS  = "wss://stoat.chat/events"
)

// Client is a stoat REST session (one bot token).
type Client struct {
	base  string
	token string
	http  *http.Client

	// buckets paces sends per rate-limit bucket. Stoat's limiter is a
	// fixed window: a bucket holds {remaining, resetAt} updated from
	// response headers, and a send waits out the window instead of
	// collecting 429s.
	mu      sync.Mutex
	buckets map[string]*bucketState
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

// ChannelInfo is the slice of GET /channels/{id} the bridge reads.
type ChannelInfo struct {
	ID   string `json:"_id"`
	Type string `json:"channel_type"`
	Name string `json:"name"`
}

// bridgeableChannelTypes are the channel kinds that carry bridged text.
// SavedMessages is a private scratchpad, VoiceChannel has no message
// surface worth bridging.
var bridgeableChannelTypes = map[string]bool{
	"TextChannel":   true,
	"Group":         true,
	"DirectMessage": true,
}

// Channel fetches one channel — the Connect handshake. A 200 proves the
// token is live AND the bot can see the channel; the type check refuses
// the unbridgeable kinds up front instead of dropping their messages
// forever.
func (c *Client) Channel(ctx context.Context, id string) (ChannelInfo, error) {
	var ch ChannelInfo
	if err := c.do(ctx, http.MethodGet, "/channels/"+url.PathEscape(id), nil, &ch); err != nil {
		return ChannelInfo{}, err
	}
	if ch.ID == "" {
		return ChannelInfo{}, fmt.Errorf("stoat: channel %s: server returned no id", id)
	}
	if !bridgeableChannelTypes[ch.Type] {
		return ChannelInfo{}, fmt.Errorf("stoat: channel %s is a %s — bridge a text channel, group, or DM", id, ch.Type)
	}
	return ch, nil
}

// SendMessage posts one message. Idempotency-Key rides every send: a
// retry after a flaky network must not double-post, and the field the
// API used for this (nonce) is documented as replaced by this header.
func (c *Client) SendMessage(ctx context.Context, channel, content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("stoat: send: content is empty")
	}
	bucket := "messaging:" + channel
	// Pace, then send; a 429 in spite of pacing (the window can shift
	// between our read and the server's) is slept out and retried once.
	for attempt := 0; ; attempt++ {
		if err := c.pace(ctx, bucket); err != nil {
			return err
		}
		var key [8]byte
		if _, err := rand.Read(key[:]); err != nil {
			return fmt.Errorf("stoat: idempotency key: %w", err)
		}
		req := struct {
			Content string `json:"content"`
		}{Content: content}
		resp, err := c.raw(ctx, http.MethodPost, "/channels/"+url.PathEscape(channel)+"/messages", &req,
			map[string]string{"Idempotency-Key": hex.EncodeToString(key[:])})
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			wait := retryAfter(resp)
			_ = resp.Body.Close()
			c.updateBucket(bucket, 0, wait)
			if err := sleep(ctx, wait); err != nil {
				return err
			}
			continue
		}
		defer func() { _ = resp.Body.Close() }()
		c.record(bucket, resp)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("stoat: send: %w", responseError(resp))
		}
		return nil
	}
}

// pace waits until the named bucket has capacity (or its window has
// rolled over — a stale bucket with a past reset is free capacity).
func (c *Client) pace(ctx context.Context, bucket string) error {
	for {
		c.mu.Lock()
		st := c.buckets[bucket]
		if st == nil || st.remaining > 0 || !time.Now().Before(st.resetAt) {
			c.mu.Unlock()
			return nil
		}
		wait := time.Until(st.resetAt)
		c.mu.Unlock()
		if err := sleep(ctx, wait); err != nil {
			return err
		}
	}
}

// record folds one response's rate-limit headers into the bucket state.
// The bucket id from X-RateLimit-Bucket is authoritative when present;
// the fallback key is ours.
func (c *Client) record(bucket string, resp *http.Response) {
	if id := resp.Header.Get("X-RateLimit-Bucket"); id != "" {
		bucket = id
	}
	remaining, err1 := strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining"))
	resetAfter, err2 := strconv.Atoi(resp.Header.Get("X-RateLimit-Reset-After"))
	if err1 != nil || err2 != nil {
		return
	}
	c.updateBucket(bucket, remaining, time.Duration(resetAfter)*time.Millisecond)
}

func (c *Client) updateBucket(bucket string, remaining int, resetIn time.Duration) {
	if resetIn < 0 {
		resetIn = 0
	}
	c.mu.Lock()
	c.buckets[bucket] = &bucketState{remaining: remaining, resetAt: time.Now().Add(resetIn)}
	c.mu.Unlock()
}

// retryAfter reads a 429 body: {"retry_after": milliseconds}. A missing
// or malformed field falls back to a conservative one-second wait —
// wrong in the safe direction.
func retryAfter(resp *http.Response) time.Duration {
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return time.Second
	}
	var body struct {
		RetryAfter int64 `json:"retry_after"`
	}
	if json.Unmarshal(data, &body) != nil || body.RetryAfter <= 0 {
		return time.Second
	}
	return time.Duration(body.RetryAfter) * time.Millisecond
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

// apiError is the platform's error body: {"type": "…"}. The type is the
// actionable part ("Unauthorized" says more than a bare 401).
type apiError struct {
	Type string `json:"type"`
}

// responseError renders one non-2xx response as a sentence.
func responseError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var e apiError
	if json.Unmarshal(data, &e) == nil && e.Type != "" {
		return fmt.Errorf("%d %s", resp.StatusCode, e.Type)
	}
	if len(data) > 0 && len(string(data)) < 200 {
		return fmt.Errorf("%d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return fmt.Errorf("unexpected status %d", resp.StatusCode)
}

// raw performs one authenticated request and returns the raw response
// (the caller records rate-limit headers and closes the body).
func (c *Client) raw(ctx context.Context, method, path string, body any, headers map[string]string) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("stoat: encode request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("stoat: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Bot-Token", c.token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stoat: %s %s: %w", method, path, err)
	}
	return resp, nil
}

// do is the JSON round trip for the calls that carry no rate-limit
// discipline of their own (the channel fetch happens once at boot).
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	resp, err := c.raw(ctx, method, path, body, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("stoat: %s %s: read response: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e apiError
		if json.Unmarshal(data, &e) == nil && e.Type != "" {
			return fmt.Errorf("stoat: %s %s: %d %s", method, path, resp.StatusCode, e.Type)
		}
		return fmt.Errorf("stoat: %s %s: unexpected status %d", method, path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("stoat: %s %s: decode response: %w", method, path, err)
	}
	return nil
}
