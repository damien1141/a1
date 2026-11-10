package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- the fake Discord REST API ----

// fakeAPI is the REST surface the bridge uses: the boot validations,
// the message send, and the typing beat, with recorders and
// failure/limit knobs. Rate-limit headers are Discord's units —
// Reset-After in SECONDS (floating) — because that is the trap.
type fakeAPI struct {
	srv *httptest.Server

	mu            sync.Mutex
	sends         []sentMessage
	typingBeats   []string // channel ids that got a typing beat
	failNext      int      // respond 500 to the next N sends
	tooMany       bool     // respond 429 with retry_after instead of 200
	tooManyGlobal bool     // the 429 is a global one
	retrySecs     float64  // the 429 body's retry_after, seconds
	remaining     int      // X-RateLimit-Remaining to report (−1 = omit headers)
	resetSecs     float64  // X-RateLimit-Reset-After seconds
	bucket        string   // X-RateLimit-Bucket to report ("" = none)
	channels      map[string]ChannelInfo
	tokens        map[string]bool // accepted bot tokens
}

type sentMessage struct {
	Channel string
	Content string
	Auth    string
	Path    string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	fa := &fakeAPI{
		remaining: 4,
		resetSecs: 5,
		retrySecs: 0.06,
		channels: map[string]ChannelInfo{
			"chan-1":  {ID: "chan-1", Type: 0, Name: "general"},
			"chan-dm": {ID: "chan-dm", Type: 1},
		},
		tokens: map[string]bool{"tok-test": true},
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /users/@me", func(w http.ResponseWriter, r *http.Request) {
		fa.mu.Lock()
		ok := fa.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bot ")]
		fa.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":0,"message":"401: Unauthorized"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"bridge-bot","username":"a1","bot":true}`))
	})

	mux.HandleFunc("GET /channels/{id}", func(w http.ResponseWriter, r *http.Request) {
		fa.mu.Lock()
		ok := fa.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bot ")]
		ch, known := fa.channels[r.PathValue("id")]
		fa.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":0,"message":"401: Unauthorized"}`))
			return
		}
		if !known {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":50001,"message":"Missing Access"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(ch)
	})

	mux.HandleFunc("POST /channels/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Content string `json:"content"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		fa.mu.Lock()
		fa.sends = append(fa.sends, sentMessage{
			Channel: r.PathValue("id"),
			Content: req.Content,
			Auth:    r.Header.Get("Authorization"),
			Path:    r.URL.Path,
		})
		fail := fa.failNext
		tooMany, global := fa.tooMany, fa.tooManyGlobal
		remaining, resetSecs, bucket := fa.remaining, fa.resetSecs, fa.bucket
		fa.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if fail > 0 {
			fa.mu.Lock()
			fa.failNext--
			fa.mu.Unlock()
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":0,"message":"Internal Server Error"}`))
			return
		}
		if tooMany {
			if global {
				w.Header().Set("X-RateLimit-Global", "true")
				w.Header().Set("X-RateLimit-Scope", "global")
			}
			fa.mu.Lock()
			retry := fa.retrySecs
			fa.mu.Unlock()
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"global":` + boolJSON(global) + `,"retry_after":` + ftoa(retry) + `}`))
			return
		}
		if remaining >= 0 {
			w.Header().Set("X-RateLimit-Limit", "5")
			if bucket != "" {
				w.Header().Set("X-RateLimit-Bucket", bucket)
			}
			w.Header().Set("X-RateLimit-Remaining", itoa(remaining))
			w.Header().Set("X-RateLimit-Reset-After", ftoa(resetSecs))
		}
		_, _ = w.Write([]byte(`{"id":"msg","channel_id":"` + r.PathValue("id") + `","content":"x"}`))
	})

	mux.HandleFunc("POST /channels/{id}/typing", func(w http.ResponseWriter, r *http.Request) {
		fa.mu.Lock()
		fa.typingBeats = append(fa.typingBeats, r.PathValue("id"))
		fa.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	fa.srv = httptest.NewServer(mux)
	t.Cleanup(fa.srv.Close)
	return fa
}

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return strings.Trim(string(b), `"`)
}

func ftoa(f float64) string {
	b, _ := json.Marshal(f)
	return strings.Trim(string(b), `"`)
}

func (fa *fakeAPI) sent() []sentMessage {
	fa.mu.Lock()
	defer fa.mu.Unlock()
	return append([]sentMessage(nil), fa.sends...)
}

func (fa *fakeAPI) typings() []string {
	fa.mu.Lock()
	defer fa.mu.Unlock()
	return append([]string(nil), fa.typingBeats...)
}

// ---- the client trio ----

func TestClientMeValidatesTheToken(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)

	id, err := c.Me(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "bridge-bot", id)

	bad := NewClient(fa.srv.URL, "tok-wrong", nil)
	_, err = bad.Me(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, "Unauthorized")
}

func TestClientChannelFetchesAndValidatesTypes(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)

	for _, id := range []string{"chan-1", "chan-dm"} {
		_, err := c.Channel(context.Background(), id)
		require.NoError(t, err, id)
	}

	fa.mu.Lock()
	fa.channels["chan-voice"] = ChannelInfo{ID: "chan-voice", Type: 2}
	fa.channels["chan-cat"] = ChannelInfo{ID: "chan-cat", Type: 4}
	fa.mu.Unlock()
	for _, id := range []string{"chan-voice", "chan-cat"} {
		_, err := c.Channel(context.Background(), id)
		require.Error(t, err, id)
		assert.ErrorContains(t, err, "type")
	}

	_, err := c.Channel(context.Background(), "chan-missing")
	require.Error(t, err)
	assert.ErrorContains(t, err, "Missing Access")
}

func TestClientSendMessageShapeAndAuth(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)

	require.NoError(t, c.SendMessage(context.Background(), "chan-1", "hello"))
	sends := fa.sent()
	require.Len(t, sends, 1)
	assert.Equal(t, "chan-1", sends[0].Channel)
	assert.Equal(t, "hello", sends[0].Content)
	assert.Equal(t, "Bot tok-test", sends[0].Auth, "auth rides Authorization: Bot")
	assert.Equal(t, "/channels/chan-1/messages", sends[0].Path)
}

func TestClientSendMessageRejectsEmptyContent(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)
	err := c.SendMessage(context.Background(), "chan-1", " \n ")
	require.Error(t, err)
	assert.ErrorContains(t, err, "empty")
	assert.Empty(t, fa.sent())
}

// TestClientSendMessagePacesOnExhaustedBucket pins the BEFORE discipline
// in Discord's own units: Reset-After is SECONDS (0.4 means 400ms), and
// an exhausted bucket makes the next send wait the window out.
func TestClientSendMessagePacesOnExhaustedBucket(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)

	fa.mu.Lock()
	fa.remaining = 0
	fa.resetSecs = 0.4
	fa.bucket = "bucket-A"
	fa.mu.Unlock()
	require.NoError(t, c.SendMessage(context.Background(), "chan-1", "first"))

	start := time.Now()
	require.NoError(t, c.SendMessage(context.Background(), "chan-1", "second"))
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 350*time.Millisecond, "the second send must wait out the window")
	assert.Less(t, elapsed, 5*time.Second, "but not something absurd")
}

// TestClientSendMessageRetriesOnceOn429 pins the AFTER discipline: a 429
// in spite of pacing is slept out per its retry_after (SECONDS) and
// retried once — no more.
func TestClientSendMessageRetriesOnceOn429(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)

	fa.mu.Lock()
	fa.tooMany = true
	fa.mu.Unlock()
	go func() {
		require.Eventually(t, func() bool {
			fa.mu.Lock()
			defer fa.mu.Unlock()
			return len(fa.sends) == 1
		}, 3*time.Second, 10*time.Millisecond)
		fa.mu.Lock()
		fa.tooMany = false
		fa.mu.Unlock()
	}()

	start := time.Now()
	require.NoError(t, c.SendMessage(context.Background(), "chan-1", "retry me"))
	assert.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond, "retry_after (60ms) is slept out")
	require.Len(t, fa.sent(), 2, "one 429, then the retried send")
}

func TestClientSendMessageGivesUpAfterTwo429s(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)
	fa.mu.Lock()
	fa.tooMany = true
	fa.mu.Unlock()

	err := c.SendMessage(context.Background(), "chan-1", "hopeless")
	require.Error(t, err)
	require.Len(t, fa.sent(), 2, "the retry is bounded at one")
}

// TestClientGlobal429LocksEveryRoute pins the global bucket's ruling: a
// global 429 says stop EVERYTHING, so even an unrelated route waits.
func TestClientGlobal429LocksEveryRoute(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)

	// Trip the global bucket and let the send give up (bounded retries),
	// with a retry_after long enough that the lockout is still live when
	// the typing beat lands.
	fa.mu.Lock()
	fa.tooMany = true
	fa.tooManyGlobal = true
	fa.retrySecs = 0.5
	fa.mu.Unlock()
	_ = c.SendMessage(context.Background(), "chan-1", "tripped the global")

	// The typing route is not the messages route — but the global lockout
	// applies anyway.
	start := time.Now()
	require.NoError(t, c.Typing(context.Background(), "chan-1"))
	assert.GreaterOrEqual(t, time.Since(start), 350*time.Millisecond, "the global lockout spans routes")
}

func TestClientTypingBeats(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)
	require.NoError(t, c.Typing(context.Background(), "chan-1"))
	require.NoError(t, c.Typing(context.Background(), "chan-1"))
	assert.Equal(t, []string{"chan-1", "chan-1"}, fa.typings())
}

func TestClientPaceHonorsContextCancel(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)
	fa.mu.Lock()
	fa.remaining = 0
	fa.resetSecs = 60
	fa.mu.Unlock()
	require.NoError(t, c.SendMessage(context.Background(), "chan-1", "first"))

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := c.SendMessage(ctx, "chan-1", "impatient")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestClientSurfacesErrorMessages(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)
	fa.mu.Lock()
	fa.failNext = 1
	fa.mu.Unlock()

	err := c.SendMessage(context.Background(), "chan-1", "boom")
	require.Error(t, err)
	assert.ErrorContains(t, err, "Internal Server Error")
}

func TestClientDefaultsAndBaseTrimming(t *testing.T) {
	c := NewClient("", "t", nil)
	assert.Equal(t, DefaultAPI, c.base)
	c2 := NewClient("https://example.com/api/v10/", "t", nil)
	assert.Equal(t, "https://example.com/api/v10", c2.base)
}
