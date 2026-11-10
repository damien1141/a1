package matrix

import (
        "context"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "net/http/httptest"
        "strings"
        "sync"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// fakeHS is a Matrix homeserver with exactly the surface the bridge uses.
// Handlers record what crossed the wire so tests assert on requests, not on
// side effects inferred from later calls.
type fakeHS struct {
        srv *httptest.Server

        mu      sync.Mutex
        sends   []sentMessage
        typing  []typingCall
        logins  []string
        joins   []string
        queries []string // raw query strings of the last /sync per call
        // sessions maps accepted bearer tokens to the user id they belong
        // to (whoami answers from it). Seeded with the ambient test token;
        // the login endpoint adds its grant.
        sessions map[string]string

        syncQ chan string // queued sync bodies; the handler long-polls on this

        // validToken is the ambient token bridge tests authenticate with.
        validToken string
}

// issueToken registers one accepted bearer (whoami will return uid for it).
func (hs *fakeHS) issueToken(token, uid string) {
        hs.mu.Lock()
        defer hs.mu.Unlock()
        hs.sessions[token] = uid
}

type sentMessage struct {
        Path      string // the raw PUT path (txn id included)
        MsgType   string
        Body      string
        Formatted string
}

type typingCall struct {
        Path    string
        Typing  bool
        Timeout int
}

func newFakeHS(t *testing.T) *fakeHS {
        t.Helper()
        hs := &fakeHS{
                syncQ:      make(chan string, 16),
                validToken: "tok-test",
                sessions:   map[string]string{"tok-test": "@bridge:hs"},
        }
        mux := http.NewServeMux()

        mux.HandleFunc("POST /_matrix/client/v3/login", func(w http.ResponseWriter, r *http.Request) {
                var req struct {
                        Identifier struct {
                                User string `json:"user"`
                        } `json:"identifier"`
                        Password string `json:"password"`
                }
                _ = json.NewDecoder(r.Body).Decode(&req)
                hs.mu.Lock()
                hs.logins = append(hs.logins, req.Identifier.User+"/"+req.Password)
                hs.mu.Unlock()
                w.Header().Set("Content-Type", "application/json")
                if req.Password != "good" {
                        w.WriteHeader(http.StatusForbidden)
                        _, _ = w.Write([]byte(`{"errcode":"M_FORBIDDEN","error":"Invalid password"}`))
                        return
                }
                hs.mu.Lock()
                hs.sessions["tok-login"] = "@bot:hs"
                hs.mu.Unlock()
                _, _ = w.Write([]byte(`{"user_id":"@bot:hs","access_token":"tok-login","device_id":"DEV"}`))
        })

        mux.HandleFunc("GET /_matrix/client/v3/account/whoami", func(w http.ResponseWriter, r *http.Request) {
                auth := r.Header.Get("Authorization")
                hs.mu.Lock()
                uid, ok := hs.sessions[strings.TrimPrefix(auth, "Bearer ")]
                hs.mu.Unlock()
                if !ok {
                        w.Header().Set("Content-Type", "application/json")
                        w.WriteHeader(http.StatusUnauthorized)
                        _, _ = w.Write([]byte(`{"errcode":"M_UNKNOWN_TOKEN","error":"Invalid access token"}`))
                        return
                }
                w.Header().Set("Content-Type", "application/json")
                _, _ = fmt.Fprintf(w, `{"user_id":%q,"device_id":"DEV"}`, uid)
        })

        mux.HandleFunc("GET /_matrix/client/v3/sync", func(w http.ResponseWriter, r *http.Request) {
                hs.mu.Lock()
                hs.queries = append(hs.queries, r.URL.RawQuery)
                hs.mu.Unlock()
                // A real homeserver long-polls: hold the connection until a response
                // is queued or the client goes away.
                select {
                case body := <-hs.syncQ:
                        w.Header().Set("Content-Type", "application/json")
                        _, _ = w.Write([]byte(body))
                case <-r.Context().Done():
                }
        })

        mux.HandleFunc(
                "PUT /_matrix/client/v3/rooms/{room}/send/m.room.message/{txn}",
                func(w http.ResponseWriter, r *http.Request) {
                        data, _ := io.ReadAll(r.Body)
                        var req struct {
                                MsgType       string `json:"msgtype"`
                                Body          string `json:"body"`
                                Format        string `json:"format"`
                                FormattedBody string `json:"formatted_body"`
                        }
                        _ = json.Unmarshal(data, &req)
                        hs.mu.Lock()
                        hs.sends = append(hs.sends, sentMessage{
                                Path:      r.URL.Path,
                                MsgType:   req.MsgType,
                                Body:      req.Body,
                                Formatted: req.FormattedBody,
                        })
                        hs.mu.Unlock()
                        w.Header().Set("Content-Type", "application/json")
                        _, _ = w.Write([]byte(`{"event_id":"$ev"}`))
                },
        )

        mux.HandleFunc("POST /_matrix/client/v3/join/{room}", func(w http.ResponseWriter, r *http.Request) {
                hs.mu.Lock()
                hs.joins = append(hs.joins, r.URL.EscapedPath())
                hs.mu.Unlock()
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"room_id":"!resolved:hs"}`))
        })

        mux.HandleFunc("PUT /_matrix/client/v3/rooms/{room}/typing/{user}", func(w http.ResponseWriter, r *http.Request) {
                var req struct {
                        Typing  bool `json:"typing"`
                        Timeout int  `json:"timeout"`
                }
                _ = json.NewDecoder(r.Body).Decode(&req)
                hs.mu.Lock()
                hs.typing = append(hs.typing, typingCall{Path: r.URL.EscapedPath(), Typing: req.Typing, Timeout: req.Timeout})
                hs.mu.Unlock()
                w.WriteHeader(http.StatusOK)
        })

        hs.srv = httptest.NewServer(mux)
        t.Cleanup(hs.srv.Close)
        return hs
}

func (hs *fakeHS) sentMessages() []sentMessage {
        hs.mu.Lock()
        defer hs.mu.Unlock()
        return append([]sentMessage(nil), hs.sends...)
}

func (hs *fakeHS) typingCalls() []typingCall {
        hs.mu.Lock()
        defer hs.mu.Unlock()
        return append([]typingCall(nil), hs.typing...)
}

// ---- the client trio ----

func TestClientLoginStoresTokenAndUserID(t *testing.T) {
        hs := newFakeHS(t)
        c := NewClient(hs.srv.URL, nil)

        require.NoError(t, c.Login(context.Background(), "bot", "good"))
        assert.Equal(t, "tok-login", c.Token())
        assert.Equal(t, "@bot:hs", c.UserID())

        // The granted token rides every later call as a Bearer.
        uid, err := c.Whoami(context.Background())
        require.NoError(t, err)
        assert.Equal(t, "@bot:hs", uid)
}

func TestClientLoginRejectsBadCredentials(t *testing.T) {
        hs := newFakeHS(t)
        c := NewClient(hs.srv.URL, nil)

        err := c.Login(context.Background(), "bot", "wrong")
        require.Error(t, err)
        assert.ErrorContains(t, err, "M_FORBIDDEN")
        assert.ErrorContains(t, err, "Invalid password", "the homeserver's error text surfaces")
}

func TestClientLoginRejectsEmptyInputs(t *testing.T) {
        c := NewClient("https://hs", nil)
        assert.ErrorContains(t, c.Login(context.Background(), "", "pw"), "user is empty")
        assert.ErrorContains(t, c.Login(context.Background(), "bot", ""), "password is empty")
}

func TestClientWhoamiRejectsBadToken(t *testing.T) {
        hs := newFakeHS(t)
        c := NewClient(hs.srv.URL, nil)
        c.token = "revoked"

        _, err := c.Whoami(context.Background())
        require.Error(t, err)
        assert.ErrorContains(t, err, "M_UNKNOWN_TOKEN")
}

func TestClientSyncParsesTimelineAndState(t *testing.T) {
        hs := newFakeHS(t)
        c := NewClient(hs.srv.URL, nil)
        c.token = hs.validToken

        hs.syncQ <- `{"next_batch":"s2","rooms":{"join":{"!r:hs":{
                "state":{"events":[{"type":"m.room.encryption","sender":"@alice:hs","content":{}}]},
                "timeline":{"events":[
                        {"type":"m.room.message","sender":"@alice:hs","event_id":"$1","origin_server_ts":1,
                         "content":{"msgtype":"m.text","body":"hello",
                         "m.relates_to":{"m.in_reply_to":{"event_id":"$0"}}}}
                ]}
        }}}}`

        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        resp, err := c.Sync(ctx, "s1", time.Second)
        require.NoError(t, err)
        assert.Equal(t, "s2", resp.NextBatch)

        room, ok := resp.Rooms.Join["!r:hs"]
        require.True(t, ok, "the joined room parses")
        require.Len(t, room.Timeline.Events, 1)
        ev := room.Timeline.Events[0]
        assert.Equal(t, "m.room.message", ev.Type)
        assert.Equal(t, "hello", ev.Content.Body)
        require.NotNil(t, ev.Content.RelatesTo, "m.relates_to parses")
        require.NotNil(t, ev.Content.RelatesTo.InReplyTo, "the reply marker parses")
        assert.Equal(t, "$0", ev.Content.RelatesTo.InReplyTo.EventID)
        require.Len(t, room.State.Events, 1)
        assert.Equal(t, "m.room.encryption", room.State.Events[0].Type)
}

func TestClientSyncInitialUsesFilterAndIncrementalUsesSince(t *testing.T) {
        hs := newFakeHS(t)
        c := NewClient(hs.srv.URL, nil)
        c.token = hs.validToken

        hs.syncQ <- `{"next_batch":"s1","rooms":{"join":{}}}`
        hs.syncQ <- `{"next_batch":"s2","rooms":{"join":{}}}`

        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        _, err := c.Sync(ctx, "", time.Second)
        require.NoError(t, err)
        _, err = c.Sync(ctx, "s1", time.Second)
        require.NoError(t, err)

        hs.mu.Lock()
        queries := append([]string(nil), hs.queries...)
        hs.mu.Unlock()
        require.Len(t, queries, 2)
        assert.Contains(t, queries[0], "filter=", "the initial sync asks for a trimmed timeline")
        assert.NotContains(t, queries[0], "since=", "the initial sync carries no cursor")
        assert.Contains(t, queries[1], "since=s1")
        assert.Contains(t, queries[1], "timeout=1000", "the long-poll window rides the query")
}

func TestClientSyncRejectsMissingNextBatch(t *testing.T) {
        hs := newFakeHS(t)
        c := NewClient(hs.srv.URL, nil)
        c.token = hs.validToken
        hs.syncQ <- `{"rooms":{"join":{}}}`

        _, err := c.Sync(context.Background(), "", time.Second)
        require.Error(t, err)
        assert.ErrorContains(t, err, "next_batch")
}

func TestClientSendMessageTxnUniqueAndBodyShape(t *testing.T) {
        hs := newFakeHS(t)
        c := NewClient(hs.srv.URL, nil)
        c.token = hs.validToken
        ctx := context.Background()

        id1, err := c.SendMessage(ctx, "!r:hs", "plain **markdown**", "<p>plain <strong>markdown</strong></p>")
        require.NoError(t, err)
        assert.Equal(t, "$ev", id1)
        id2, err := c.SendMessage(ctx, "!r:hs", "second", "")
        require.NoError(t, err)
        assert.Equal(t, "$ev", id2)

        msgs := hs.sentMessages()
        require.Len(t, msgs, 2)
        assert.NotEqual(t, msgs[0].Path, msgs[1].Path, "two sends, two distinct txn ids")
        assert.Equal(t, "m.text", msgs[0].MsgType)
        assert.Equal(t, "plain **markdown**", msgs[0].Body)
        assert.NotEmpty(t, msgs[0].Formatted, "the HTML spelling rides alongside the plain body")
        assert.Empty(t, msgs[1].Formatted, "no HTML — no format field on the wire")

        nid, err := c.SendNotice(ctx, "!r:hs", "system line")
        require.NoError(t, err)
        assert.Equal(t, "$ev", nid)
        msgs = hs.sentMessages()
        assert.Equal(t, "m.notice", msgs[2].MsgType)
}

func TestClientSendMessageRejectsEmptyBody(t *testing.T) {
        hs := newFakeHS(t)
        c := NewClient(hs.srv.URL, nil)
        _, err := c.SendMessage(context.Background(), "!r:hs", "   ", "")
        require.Error(t, err)
        assert.ErrorContains(t, err, "body is empty")
}

func TestClientJoinResolvesAlias(t *testing.T) {
        hs := newFakeHS(t)
        c := NewClient(hs.srv.URL, nil)
        c.token = hs.validToken

        roomID, err := c.JoinRoom(context.Background(), "#a1:hs")
        require.NoError(t, err)
        assert.Equal(t, "!resolved:hs", roomID)

        hs.mu.Lock()
        joins := append([]string(nil), hs.joins...)
        hs.mu.Unlock()
        require.Len(t, joins, 1)
        // '#' must be escaped (it would truncate the path as a fragment
        // delimiter); ':' and '@' are legal path-segment characters and stay.
        assert.Equal(t, "/_matrix/client/v3/join/%23a1:hs", joins[0])
}

func TestClientTypingPathAndPayload(t *testing.T) {
        hs := newFakeHS(t)
        c := NewClient(hs.srv.URL, nil)
        c.token = hs.validToken
        c.userID = "@bridge:hs"

        require.NoError(t, c.SetTyping(context.Background(), "!r:hs", true, 25*time.Second))
        calls := hs.typingCalls()
        require.Len(t, calls, 1)
        assert.True(t, calls[0].Typing)
        assert.Equal(t, 25000, calls[0].Timeout)
        // '!' escapes (leading-bang room ids); ':' and '@' are legal segment
        // characters and ride unescaped — both forms parse on real homeservers.
        assert.Equal(t, "/_matrix/client/v3/rooms/%21r:hs/typing/@bridge:hs", calls[0].Path)
}

func TestClientTypingRequiresUser(t *testing.T) {
        c := NewClient("https://hs", nil)
        err := c.SetTyping(context.Background(), "!r:hs", true, time.Second)
        require.Error(t, err)
        assert.ErrorContains(t, err, "no user id")
}

func TestClientErrorWithoutErrcode(t *testing.T) {
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
                w.WriteHeader(http.StatusInternalServerError)
                _, _ = w.Write([]byte("plain oops"))
        }))
        t.Cleanup(srv.Close)
        c := NewClient(srv.URL, nil)

        _, err := c.Whoami(context.Background())
        require.Error(t, err)
        assert.ErrorContains(t, err, "unexpected status 500")
}

func TestClientRequestContextCancelsLongPoll(t *testing.T) {
        // The sync long-poll must follow the caller's context, or shutdown would
        // always wait out the full 30s window.
        hs := newFakeHS(t)
        c := NewClient(hs.srv.URL, nil)
        c.token = hs.validToken

        ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
        defer cancel()
        start := time.Now()
        _, err := c.Sync(ctx, "", time.Second)
        require.Error(t, err)
        assert.Less(t, time.Since(start), time.Second, "the long-poll aborted with its context")
}
