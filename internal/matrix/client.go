// Package matrix bridges one Matrix room onto one a1 session.
//
// The design is a loopback client, not a new server surface (the roadmap
// note that locked it): `a1 matrix` boots the webserver in-process on an
// ephemeral 127.0.0.1 port, and the bridge drives the agent through the
// same chat API + SSE stream the web GUI uses — POST /api/prompt,
// GET /api/events, POST /api/ask. Nothing about the agent's network
// posture changes; the only new listener is the outbound Matrix sync
// long-poll.
//
// v1 speaks PLAINTEXT rooms only: a megolm implementation is a protocol
// unto itself and would drag the dependency budget with it, so rooms with
// encryption enabled are refused at startup instead of silently ignoring
// every message. The room is the trust boundary — keep it invite-only.
//
// This file is the wire half: a hand-rolled Matrix client-server v3 client
// over net/http + encoding/json. mautrix et al. are fine libraries, but
// the harness's dependency budget is spent on purpose elsewhere and the
// bridge needs six endpoints, not a framework.
package matrix

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
        "sync/atomic"
        "time"
)

// Client is a logged-in Matrix client-server API session. All methods are
// safe for concurrent use (the sync long-poll blocks; typing refreshes and
// message sends happen alongside it).
type Client struct {
        homeserver string
        http       *http.Client

        token  string
        userID string

        // txn serializes event transaction ids so two sends can never reuse one
        // (a reused txn id is silently dropped by homeservers as a duplicate).
        txn atomic.Uint64
}

// NewClient returns a client for the homeserver base URL. The URL is used
// verbatim; Connect (bridge.go) does the scheme/host normalization.
func NewClient(homeserver string, httpClient *http.Client) *Client {
        if httpClient == nil {
                httpClient = &http.Client{Timeout: 60 * time.Second}
        }
        return &Client{homeserver: strings.TrimRight(homeserver, "/"), http: httpClient}
}

// UserID returns the authenticated user id (empty before login/whoami).
func (c *Client) UserID() string { return c.userID }

// Token returns the access token in use (empty before login).
func (c *Client) Token() string { return c.token }

// apiError is the standard Matrix error body (errcode + error). Homeservers
// put useful, actionable text in "error"; surfacing it instead of a bare
// status code is the difference between "M_LIMIT_EXCEEDED: too many requests"
// and a mysterious 429. It is a payload, not an error implementation — the
// call sites wrap it with the route context that produced it.
type apiError struct {
        ErrCode string `json:"errcode"`
        Error   string `json:"error"`
}

func (e apiError) detail() string {
        if e.Error == "" {
                return e.ErrCode
        }
        return e.ErrCode + ": " + e.Error
}

// do performs one authenticated JSON round trip. method/path are the CS API
// route (path carries escaped ids), body is JSON-encoded when non-nil. The
// caller supplies the context so the sync long-poll can outlive the client's
// own timeout.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
        var reader io.Reader
        if body != nil {
                data, err := json.Marshal(body)
                if err != nil {
                        return fmt.Errorf("matrix: encode request: %w", err)
                }
                reader = bytes.NewReader(data)
        }
        req, err := http.NewRequestWithContext(ctx, method, c.homeserver+path, reader)
        if err != nil {
                return fmt.Errorf("matrix: build request: %w", err)
        }
        if body != nil {
                req.Header.Set("Content-Type", "application/json")
        }
        if c.token != "" {
                // Bearer is the current spec spelling; the ?access_token= query form
                // is legacy and leaks into server logs.
                req.Header.Set("Authorization", "Bearer "+c.token)
        }
        resp, err := c.http.Do(req)
        if err != nil {
                return fmt.Errorf("matrix: %s %s: %w", method, path, err)
        }
        defer func() { _ = resp.Body.Close() }()
        data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
        if err != nil {
                return fmt.Errorf("matrix: %s %s: read response: %w", method, path, err)
        }
        if resp.StatusCode < 200 || resp.StatusCode >= 300 {
                var apiErr apiError
                if json.Unmarshal(data, &apiErr) == nil && apiErr.ErrCode != "" {
                        return fmt.Errorf("matrix: %s %s: %d %s", method, path, resp.StatusCode, apiErr.detail())
                }
                return fmt.Errorf("matrix: %s %s: unexpected status %d", method, path, resp.StatusCode)
        }
        if out == nil {
                return nil
        }
        if err := json.Unmarshal(data, out); err != nil {
                return fmt.Errorf("matrix: %s %s: decode response: %w", method, path, err)
        }
        return nil
}

// Login authenticates with m.login.password and stores the granted token.
// user may be a full @id:host or a localpart. The device name shows in the
// homeserver's sessions list as the device the token belongs to.
func (c *Client) Login(ctx context.Context, user, password string) error {
        if strings.TrimSpace(user) == "" {
                return fmt.Errorf("matrix: login: user is empty")
        }
        if password == "" {
                return fmt.Errorf("matrix: login: password is empty")
        }
        var req struct {
                Type       string `json:"type"`
                Identifier struct {
                        Type string `json:"type"`
                        User string `json:"user"`
                } `json:"identifier"`
                Password                 string `json:"password"`
                InitialDeviceDisplayName string `json:"initial_device_display_name"`
        }
        req.Type = "m.login.password"
        req.Identifier.Type = "m.id.user"
        req.Identifier.User = user
        req.Password = password
        req.InitialDeviceDisplayName = "a1 bridge"
        var resp struct {
                UserID      string `json:"user_id"`
                AccessToken string `json:"access_token"`
        }
        if err := c.do(ctx, http.MethodPost, "/_matrix/client/v3/login", &req, &resp); err != nil {
                return err
        }
        if resp.AccessToken == "" || resp.UserID == "" {
                return fmt.Errorf("matrix: login: homeserver returned no token or user id")
        }
        c.token = resp.AccessToken
        c.userID = resp.UserID
        return nil
}

// Whoami validates the access token and returns the user id it belongs to.
// It is the token-path handshake: config carries only a token, so the
// account name comes from the homeserver, not the file.
func (c *Client) Whoami(ctx context.Context) (string, error) {
        var resp struct {
                UserID string `json:"user_id"`
        }
        if err := c.do(ctx, http.MethodGet, "/_matrix/client/v3/account/whoami", nil, &resp); err != nil {
                return "", err
        }
        if resp.UserID == "" {
                return "", fmt.Errorf("matrix: whoami: homeserver returned no user id")
        }
        return resp.UserID, nil
}

// Event is one room timeline event, filtered down to the fields the bridge
// reads. Everything else in a sync payload is ignored by design — the less
// of the room state the bridge parses, the less of it exists to go wrong.
type Event struct {
        Type           string `json:"type"`
        Sender         string `json:"sender"`
        EventID        string `json:"event_id"`
        OriginServerTS int64  `json:"origin_server_ts"`
        Content        struct {
                MsgType   string     `json:"msgtype"`
                Body      string     `json:"body"`
                RelatesTo *relatesTo `json:"m.relates_to"`
        } `json:"content"`
}

// relatesTo carries the m.relates_to content: rel_type m.replace marks an
// edit (the new body lives in m.new_content, and firing a prompt for a
// reworded message from three days ago would be wrong), while m.in_reply_to
// marks a reply (whose plain-text body carries a "> " quoted fallback the
// bridge strips before prompting).
type relatesTo struct {
        RelType   string `json:"rel_type"`
        EventID   string `json:"event_id"`
        InReplyTo *struct {
                EventID string `json:"event_id"`
        } `json:"m.in_reply_to"`
}

// SyncResponse is the slice of /sync the bridge consumes: the next-batch
// cursor plus the joined rooms' state and timelines.
type SyncResponse struct {
        NextBatch string `json:"next_batch"`
        Rooms     struct {
                Join map[string]struct {
                        // State carries the room's state events on the initial sync —
                        // the bridge reads exactly one thing from it: m.room.encryption,
                        // which marks the room unreadable for a plaintext client.
                        State struct {
                                Events []Event `json:"events"`
                        } `json:"state"`
                        Timeline struct {
                                Events []Event `json:"events"`
                        } `json:"timeline"`
                } `json:"join"`
        } `json:"rooms"`
}

// Sync performs one /sync call. An empty since is the INITIAL sync: the
// homeserver returns recent timeline history, which the caller must NOT
// treat as new messages — the bridge takes the cursor and drops the events,
// so messages sent while it was down stay unanswered (visible in the room,
// re-askable; a three-hour-old "fix the bug" firing on boot would be worse).
//
// timeout is the long-poll window; the request context should outlive it so
// a clean shutdown cancels the poll instead of waiting it out.
func (c *Client) Sync(ctx context.Context, since string, timeout time.Duration) (SyncResponse, error) {
        q := url.Values{}
        q.Set("timeout", strconv.Itoa(int(timeout.Milliseconds())))
        if since != "" {
                q.Set("since", since)
        }
        // The initial sync otherwise ships the full room state and a deep
        // timeline for every joined room; the bridge reads none of it.
        if since == "" {
                q.Set("filter", `{"room":{"timeline":{"limit":1}}}`)
        }
        var resp SyncResponse
        if err := c.do(ctx, http.MethodGet, "/_matrix/client/v3/sync?"+q.Encode(), nil, &resp); err != nil {
                return SyncResponse{}, err
        }
        if resp.NextBatch == "" {
                return SyncResponse{}, fmt.Errorf("matrix: sync: homeserver returned no next_batch")
        }
        return resp, nil
}

// SendMessage sends one m.text message and returns the event id. formatted
// (org.matrix.custom.html) is sent alongside the plain body when non-empty —
// clients that render HTML show code blocks and tables; the rest fall back.
func (c *Client) SendMessage(ctx context.Context, room, body, formatted string) (string, error) {
        return c.sendEvent(ctx, "m.text", room, body, formatted)
}

// SendNotice sends one m.notice — the grey system-line msgtype. The bridge
// uses it for its own meta lines (approvals, errors) so they never read as
// the agent speaking.
func (c *Client) SendNotice(ctx context.Context, room, body string) (string, error) {
        return c.sendEvent(ctx, "m.notice", room, body, "")
}

func (c *Client) sendEvent(ctx context.Context, msgtype, room, body, formatted string) (string, error) {
        if strings.TrimSpace(body) == "" {
                return "", fmt.Errorf("matrix: send: body is empty")
        }
        payload := struct {
                MsgType       string `json:"msgtype"`
                Body          string `json:"body"`
                Format        string `json:"format,omitempty"`
                FormattedBody string `json:"formatted_body,omitempty"`
        }{MsgType: msgtype, Body: body}
        if formatted != "" {
                payload.Format = "org.matrix.custom.html"
                payload.FormattedBody = formatted
        }
        // The txn id pairs a monotonic counter with the wall clock: unique across
        // restarts (clock) and within one process (counter).
        txn := fmt.Sprintf("a1-%d-%d", time.Now().UnixMilli(), c.txn.Add(1))
        path := "/_matrix/client/v3/rooms/" + url.PathEscape(room) + "/send/m.room.message/" + url.PathEscape(txn)
        var resp struct {
                EventID string `json:"event_id"`
        }
        if err := c.do(ctx, http.MethodPut, path, &payload, &resp); err != nil {
                return "", err
        }
        return resp.EventID, nil
}

// JoinRoom joins (or resolves) a room and returns the canonical room id.
// Joining an already-joined room is a no-op returning the same id, so the
// bridge can call it unconditionally at startup: it resolves #aliases and
// verifies membership in one round trip.
func (c *Client) JoinRoom(ctx context.Context, roomOrAlias string) (string, error) {
        var resp struct {
                RoomID string `json:"room_id"`
        }
        path := "/_matrix/client/v3/join/" + url.PathEscape(roomOrAlias)
        if err := c.do(ctx, http.MethodPost, path, struct{}{}, &resp); err != nil {
                return "", err
        }
        if resp.RoomID == "" {
                return "", fmt.Errorf("matrix: join %s: homeserver returned no room id", roomOrAlias)
        }
        return resp.RoomID, nil
}

// SetTyping flips the room's typing notification for the bridge user.
// The timeout bounds how long a homeserver holds the flag, so an in-flight
// turn refreshes it periodically rather than setting it once.
func (c *Client) SetTyping(ctx context.Context, room string, typing bool, timeout time.Duration) error {
        if c.userID == "" {
                return fmt.Errorf("matrix: typing: no user id (login or whoami first)")
        }
        payload := struct {
                Typing  bool `json:"typing"`
                Timeout int  `json:"timeout"`
        }{Typing: typing, Timeout: int(timeout.Milliseconds())}
        path := "/_matrix/client/v3/rooms/" + url.PathEscape(room) + "/typing/" + url.PathEscape(c.userID)
        return c.do(ctx, http.MethodPut, path, &payload, nil)
}
