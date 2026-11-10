package stoat

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

// ---- the fake stoat REST API ----

// fakeAPI is the REST surface the bridge uses: the channel fetch and
// the message send, with recorders and failure/limit knobs.
type fakeAPI struct {
	srv *httptest.Server

	mu        sync.Mutex
	sends     []sentMessage
	fetches   []string
	failNext  int  // respond 500 to the next N sends
	tooMany   bool // respond 429 with retry_after instead of 200
	remaining int  // X-RateLimit-Remaining to report (−1 = omit headers)
	resetMS   int  // X-RateLimit-Reset-After milliseconds
	channels  map[string]ChannelInfo
	tokens    map[string]bool // accepted bot tokens
}

type sentMessage struct {
	Channel string
	Content string
	Token   string
	Path    string
	HasID   bool // Idempotency-Key present
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	fa := &fakeAPI{
		remaining: 9,
		resetMS:   10000,
		channels: map[string]ChannelInfo{
			"chan-1":  {ID: "chan-1", Type: "TextChannel", Name: "general"},
			"chan-dm": {ID: "chan-dm", Type: "DirectMessage"},
		},
		tokens: map[string]bool{"tok-test": true},
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /channels/{id}", func(w http.ResponseWriter, r *http.Request) {
		fa.mu.Lock()
		fa.fetches = append(fa.fetches, r.PathValue("id"))
		ok := fa.tokens[r.Header.Get("X-Bot-Token")]
		ch, known := fa.channels[r.PathValue("id")]
		fa.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"Unauthorized"}`))
			return
		}
		if !known {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"type":"NotFound"}`))
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
			Token:   r.Header.Get("X-Bot-Token"),
			Path:    r.URL.Path,
			HasID:   r.Header.Get("Idempotency-Key") != "",
		})
		fail := fa.failNext
		tooMany := fa.tooMany
		remaining, resetMS := fa.remaining, fa.resetMS
		fa.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if fail > 0 {
			fa.mu.Lock()
			fa.failNext--
			fa.mu.Unlock()
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"type":"InternalError"}`))
			return
		}
		if tooMany {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"retry_after":60}`))
			return
		}
		if remaining >= 0 {
			w.Header().Set("X-RateLimit-Limit", "10")
			w.Header().Set("X-RateLimit-Bucket", "messaging:"+r.PathValue("id"))
			w.Header().Set("X-RateLimit-Remaining", itoa(remaining))
			w.Header().Set("X-RateLimit-Reset-After", itoa(resetMS))
		}
		_, _ = w.Write([]byte(`{"_id":"msg","channel":"` + r.PathValue("id") + `","author":"bridge-bot","content":"x"}`))
	})

	fa.srv = httptest.NewServer(mux)
	t.Cleanup(fa.srv.Close)
	return fa
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return strings.Trim(string(b), `"`)
}

func (fa *fakeAPI) sent() []sentMessage {
	fa.mu.Lock()
	defer fa.mu.Unlock()
	return append([]sentMessage(nil), fa.sends...)
}

// ---- the client trio ----

func TestClientChannelFetchesAndValidates(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)

	ch, err := c.Channel(context.Background(), "chan-1")
	require.NoError(t, err)
	assert.Equal(t, "chan-1", ch.ID)
	assert.Equal(t, "TextChannel", ch.Type)

	_, err = c.Channel(context.Background(), "chan-dm")
	require.NoError(t, err, "a DM channel is bridgeable")
}

func TestClientChannelRejectsUnbridgeableAndBadTokens(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)

	fa.mu.Lock()
	fa.channels["chan-voice"] = ChannelInfo{ID: "chan-voice", Type: "VoiceChannel"}
	fa.mu.Unlock()
	_, err := c.Channel(context.Background(), "chan-voice")
	require.Error(t, err)
	assert.ErrorContains(t, err, "VoiceChannel")

	_, err = c.Channel(context.Background(), "chan-missing")
	require.Error(t, err)
	assert.ErrorContains(t, err, "404")

	bad := NewClient(fa.srv.URL, "tok-wrong", nil)
	_, err = bad.Channel(context.Background(), "chan-1")
	require.Error(t, err)
	assert.ErrorContains(t, err, "Unauthorized")
}

func TestClientSendMessageShapeAndHeaders(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)

	require.NoError(t, c.SendMessage(context.Background(), "chan-1", "hello"))
	sends := fa.sent()
	require.Len(t, sends, 1)
	assert.Equal(t, "chan-1", sends[0].Channel)
	assert.Equal(t, "hello", sends[0].Content)
	assert.Equal(t, "tok-test", sends[0].Token, "auth rides X-Bot-Token")
	assert.True(t, sends[0].HasID, "every send carries an Idempotency-Key")
	assert.Equal(t, "/channels/chan-1/messages", sends[0].Path)
}

func TestClientSendMessageRejectsEmptyContent(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)
	err := c.SendMessage(context.Background(), "chan-1", "  ")
	require.Error(t, err)
	assert.ErrorContains(t, err, "empty")
	assert.Empty(t, fa.sent())
}

// TestClientSendMessagePacesOnExhaustedWindow pins the BEFORE discipline:
// a reported remaining of 0 with a reset-after window makes the next send
// wait the window out instead of collecting the 429.
func TestClientSendMessagePacesOnExhaustedWindow(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)

	fa.mu.Lock()
	fa.remaining = 0
	fa.resetMS = 400
	fa.mu.Unlock()
	require.NoError(t, c.SendMessage(context.Background(), "chan-1", "first"))

	start := time.Now()
	require.NoError(t, c.SendMessage(context.Background(), "chan-1", "second"))
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 350*time.Millisecond, "the second send must wait out the window")
	assert.Less(t, elapsed, 5*time.Second, "but not something absurd")
}

// TestClientSendMessageRetriesOnceOn429 pins the AFTER discipline: a 429
// in spite of pacing is slept out per its retry_after and retried once.
func TestClientSendMessageRetriesOnceOn429(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)

	fa.mu.Lock()
	fa.tooMany = true
	fa.mu.Unlock()
	go func() {
		// Flip back to healthy after the first 429 lands.
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

// TestClientSendMessageGivesUpAfterTwo429s pins the retry bound: a
// server that only says 429 gets an error, not a spin.
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

func TestClientSendMessageSurfacesErrorTypes(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)
	fa.mu.Lock()
	fa.failNext = 1
	fa.mu.Unlock()

	err := c.SendMessage(context.Background(), "chan-1", "boom")
	require.Error(t, err)
	assert.ErrorContains(t, err, "InternalError")
}

func TestClientPaceHonorsContextCancel(t *testing.T) {
	fa := newFakeAPI(t)
	c := NewClient(fa.srv.URL, "tok-test", nil)
	fa.mu.Lock()
	fa.remaining = 0
	fa.resetMS = 60000
	fa.mu.Unlock()
	require.NoError(t, c.SendMessage(context.Background(), "chan-1", "first"))

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := c.SendMessage(ctx, "chan-1", "impatient")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestClientDefaultsAndBaseTrimming(t *testing.T) {
	c := NewClient("", "t", nil)
	assert.Equal(t, DefaultAPI, c.base)
	c2 := NewClient("https://example.com/", "t", nil)
	assert.Equal(t, "https://example.com", c2.base)
}
