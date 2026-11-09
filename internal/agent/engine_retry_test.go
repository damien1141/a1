package agent

import (
        "encoding/json"
        "fmt"
        "net/http"
        "net/http/httptest"
        "sync/atomic"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/tools"
)

// This file pins the engine-layer transient retry (stolen from
// hermes-agent's turn recovery) and the byte-scored overflow progress gate.
// The layering contract under test:
//
//   - the HTTP layer (util.DoWithRetry) owns status-level 429/5xx retries;
//     the engine never re-retries those (TestLoopDoesNotDoubleRetryOwned429)
//   - the engine layer retries what the HTTP layer cannot see — a 408
//     status (TestLoopRetriesTimeoutStatus), and only while nothing was
//     delivered (TestLoopDoesNotRetryAfterPartialDelivery)
//   - terminal verdicts (auth) never retry (TestLoopAuthErrorIsNotRetried)
//   - a recovery compaction that cannot shrink the request by at least 5%
//     in bytes made no progress: the original overflow surfaces instead of
//     a retry that re-sends almost the same bytes
//     (TestLoopOverflowNoByteProgressSurfaces)

// timeoutThenOKServer answers the first stream request with 408 and every
// later one with a final text response.
func timeoutThenOKServer(hits *atomic.Int32) *httptest.Server {
        return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/chat/completions" {
                        http.NotFound(w, r)
                        return
                }
                if hits.Add(1) == 1 {
                        w.Header().Set("Content-Type", "application/json")
                        w.WriteHeader(http.StatusRequestTimeout)
                        _, _ = w.Write([]byte(`{"error":{"message":"request timeout"}}`))
                        return
                }
                w.Header().Set("Content-Type", "text/event-stream")
                _, _ = fmt.Fprint(w, sseTextChunk("recovered"))
                _, _ = fmt.Fprint(w, "data: [DONE]\n\n")
        }))
}

func TestLoopRetriesTimeoutStatus(t *testing.T) {
        var hits atomic.Int32
        server := timeoutThenOKServer(&hits)
        defer server.Close()

        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        engine, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 32768},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools([]tools.Tool{}),
        )
        require.NoError(t, err)

        var lastErr error
        var finalText string
        for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
                if err != nil {
                        lastErr = err
                        break
                }
                if update, ok := ev.(session.AssistantMessageUpdate); ok && update.Message.State == session.StateComplete {
                        finalText = update.Message.FlatText()
                }
        }
        require.NoError(t, lastErr, "a 408 is transient: the engine retries it")
        require.Equal(t, int32(2), hits.Load(), "one failed attempt then one retry")
        require.Equal(t, "recovered", finalText)
}

// The partial-delivery guard: a stream that already showed text and then
// died must not be retried — a fresh assistant id would duplicate what the
// user already saw.
func TestLoopDoesNotRetryAfterPartialDelivery(t *testing.T) {
        var hits atomic.Int32
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/chat/completions" {
                        http.NotFound(w, r)
                        return
                }
                hits.Add(1)
                w.Header().Set("Content-Type", "text/event-stream")
                fl, _ := w.(http.Flusher)
                _, _ = fmt.Fprint(w, sseTextChunk("partial answer the user already saw"))
                fl.Flush()
                // Cut the connection mid-stream: the client sees a network
                // error (retryable by verdict) AFTER a delivered delta.
                panic(http.ErrAbortHandler)
        }))
        defer server.Close()

        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        engine, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 32768},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools([]tools.Tool{}),
        )
        require.NoError(t, err)

        var lastErr error
        for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
                _ = ev
                if err != nil {
                        lastErr = err
                        break
                }
        }
        require.Error(t, lastErr, "the mid-stream cut surfaces")
        require.Equal(t, int32(1), hits.Load(),
                "a retryable network error after a delivered partial must NOT be retried")
}

func TestLoopAuthErrorIsNotRetried(t *testing.T) {
        var hits atomic.Int32
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/chat/completions" {
                        http.NotFound(w, r)
                        return
                }
                hits.Add(1)
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusUnauthorized)
                _, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
        }))
        defer server.Close()

        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        engine, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 32768},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools([]tools.Tool{}),
        )
        require.NoError(t, err)

        var lastErr error
        for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
                _ = ev
                if err != nil {
                        lastErr = err
                        break
                }
        }
        require.Error(t, lastErr)
        assert.Equal(t, llm.KindAuth, llm.Classify(lastErr).Kind)
        require.Equal(t, int32(1), hits.Load(), "auth is terminal: no retry at any layer")
}

// The layer-ownership guard: the HTTP client already retried the 429 five
// times under its own budget (Retry-After: 0 keeps it instant). The engine
// must surface the error instead of multiplying that patience.
func TestLoopDoesNotDoubleRetryOwned429(t *testing.T) {
        var hits atomic.Int32
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/chat/completions" {
                        http.NotFound(w, r)
                        return
                }
                hits.Add(1)
                w.Header().Set("Retry-After", "0")
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusTooManyRequests)
                _, _ = w.Write([]byte(`{"error":{"message":"rate limit reached"}}`))
        }))
        defer server.Close()

        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        engine, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 32768},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools([]tools.Tool{}),
        )
        require.NoError(t, err)

        var lastErr error
        for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
                _ = ev
                if err != nil {
                        lastErr = err
                        break
                }
        }
        require.Error(t, lastErr)
        require.Equal(t, int32(5), hits.Load(),
                "exactly the HTTP layer's five budgeted attempts; the engine layer adds none")
}

// TestLoopOverflowNoByteProgressSurfaces pins the byte-scored progress gate:
// when the recovery compaction summarizes only a tiny prefix while a huge
// kept zone dominates the request, the rebuilt request is within 5% of the
// failed one. Retrying would re-send almost the same bytes and fail
// identically, so the original overflow surfaces after the single attempt.
func TestLoopOverflowNoByteProgressSurfaces(t *testing.T) {
        var streamHits atomic.Int32
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                var body map[string]any
                _ = json.NewDecoder(r.Body).Decode(&body)
                if stream, _ := body["stream"].(bool); stream {
                        streamHits.Add(1)
                        w.Header().Set("Content-Type", "application/json")
                        w.WriteHeader(http.StatusBadRequest)
                        _, _ = w.Write([]byte(`{"error":{"message":"prompt is too long: 210000 > 200000 tokens"}}`))
                        return
                }
                w.Header().Set("Content-Type", "application/json")
                _ = json.NewEncoder(w).Encode(map[string]any{
                        "choices": []any{map[string]any{
                                "message": map[string]any{"role": "assistant", "content": "summary"},
                        }},
                })
        }))
        defer server.Close()

        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        // A tiny summarizable prefix in front of a kept zone the compaction
        // cannot shrink: three 12k-token messages (36k) exceed the 30k
        // keepRecentTokens derived from the stated 200k window, so the cut
        // lands at the first seed and summarizes only "one"/"two".
        require.NoError(t, sess.Append(
                llm.Message{Role: llm.RoleUser, Content: "one"},
                llm.Message{Role: llm.RoleAssistant, Content: "two"},
                llm.Message{Role: llm.RoleUser, Content: compactionSeed},
                llm.Message{Role: llm.RoleAssistant, Content: compactionSeed},
                llm.Message{Role: llm.RoleUser, Content: compactionSeed},
        ))

        engine, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 200_000},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools([]tools.Tool{}),
        )
        require.NoError(t, err)

        var lastErr error
        var sawCompact bool
        for ev, err := range engine.Loop(t.Context(), "continue", LoopOpts{}) {
                if err != nil {
                        lastErr = err
                        break
                }
                if _, ok := ev.(session.CompactionStarted); ok {
                        sawCompact = true
                }
        }
        require.Error(t, lastErr)
        require.True(t, llm.IsContextOverflow(lastErr), "the original overflow surfaces")
        require.True(t, sawCompact, "the recovery compaction did run")
        require.Equal(t, int32(1), streamHits.Load(),
                "no retry: a compaction that shrank the request under 5% bought nothing")
}
