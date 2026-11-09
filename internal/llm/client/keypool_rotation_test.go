package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// keyHit records one HTTP request the test server saw.
type keyHit struct {
	key  string // Authorization bearer
	path string
}

// keyServer serves OpenAI-compatible chat completions and records every
// request's bearer key. respond inspects the hit and writes the response:
// a non-200 status (with the given Retry-After) or a 200 SSE stream built
// from events.
type keyServer struct {
	mu      sync.Mutex
	hits    []keyHit
	srvURL  string
	respond func(hit keyHit, n int) (status int, retryAfter string, sse string)
}

func newKeyServer(t *testing.T, respond func(hit keyHit, n int) (int, string, string)) *keyServer {
	t.Helper()
	ks := &keyServer{respond: respond}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ks.mu.Lock()
		hit := keyHit{key: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), path: r.URL.Path}
		ks.hits = append(ks.hits, hit)
		n := len(ks.hits)
		ks.mu.Unlock()

		status, retryAfter, sse := ks.respond(hit, n)
		if status != http.StatusOK {
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	t.Cleanup(srv.Close)
	ks.srvURL = srv.URL
	return ks
}

// newPoolClient builds a default-API client over the key server with a
// two-key pool attached.
func newPoolClient(baseURL string, keys ...string) *Client {
	c := NewClient(llm.ModelConfig{
		Name:    "test-model",
		BaseURL: baseURL,
		APIKey:  keys[0],
	}, Hooks{}, nil, "")
	return c.SetKeyPool(llm.NewKeyPool(keys))
}

func (ks *keyServer) keys() []string {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	out := make([]string, len(ks.hits))
	for i, h := range ks.hits {
		out[i] = h.key
	}
	return out
}

func (ks *keyServer) hitCount() int {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	return len(ks.hits)
}

// okSSE is a minimal successful stream saying "hi".
const okSSE = "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n" +
	"data: {\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2,\"total_tokens\":6}}\n\n" +
	"data: [DONE]\n\n"

// TestStreamRotatesOnRateLimitTransparently pins the golden path: a 429
// before any content rotates to the next key and re-issues, and the
// consumer sees only the success — no error event, no double content. The
// transport-level DoWithRetry burns its same-key budget first (5 attempts
// at Retry-After: 0, all instant), then the pool moves the request to the
// second key.
func TestStreamRotatesOnRateLimitTransparently(t *testing.T) {
	ks := newKeyServer(t, func(hit keyHit, n int) (int, string, string) {
		if hit.key == "k1" {
			return http.StatusTooManyRequests, "0", ""
		}
		return http.StatusOK, "", okSSE
	})
	client := newPoolClient(ks.srvURL, "k1", "k2")

	events := collectEvents(client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hello"}}))

	for _, ev := range events {
		require.Empty(t, ev.Err, "rotation must be invisible: no error may reach the consumer")
	}
	require.NotEmpty(t, events, "expected the successful stream's events")
	require.Equal(t, "hi", events[len(events)-1].Final.Content, "final message from the rotated attempt")

	keys := ks.keys()
	require.Equal(t, []string{"k1", "k1", "k1", "k1", "k1", "k2"}, keys,
		"five same-key transport retries, then one rotated request on k2")
}

// TestStreamPoolExhaustedSurfacesAnnotatedError pins the exhaustion
// contract: every key had its chance within one call, the surfaced error
// names that fact, and the APIError chain survives so status-branching
// callers (overflow recovery) keep working.
func TestStreamPoolExhaustedSurfacesAnnotatedError(t *testing.T) {
	ks := newKeyServer(t, func(hit keyHit, n int) (int, string, string) {
		return http.StatusTooManyRequests, "0", ""
	})
	client := newPoolClient(ks.srvURL, "k1", "k2")

	var lastErr error
	for ev, err := range client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hello"}}) {
		if err != nil {
			lastErr = err
		}
		require.Empty(t, ev.Err, "no event-level error text expected")
	}
	require.Error(t, lastErr)
	require.Contains(t, lastErr.Error(), "credential pool: all 2 keys failed")
	apiErr, ok := llm.APIErrorOf(lastErr)
	require.True(t, ok, "the wrapped 429 must still resolve as an APIError")
	require.Equal(t, http.StatusTooManyRequests, apiErr.Status)
	require.Equal(t, 10, ks.hitCount(), "both keys' full transport budgets spent")
}

// TestStreamAuthFailureRotates pins 401: not retried at transport level
// (it is not a transient status), rotated by the pool, and the retried
// request succeeds.
func TestStreamAuthFailureRotates(t *testing.T) {
	ks := newKeyServer(t, func(hit keyHit, n int) (int, string, string) {
		if hit.key == "k1" {
			return http.StatusUnauthorized, "", ""
		}
		return http.StatusOK, "", okSSE
	})
	client := newPoolClient(ks.srvURL, "k1", "k2")

	events := collectEvents(client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hello"}}))
	for _, ev := range events {
		require.Empty(t, ev.Err)
	}
	require.Equal(t, []string{"k1", "k2"}, ks.keys(), "one 401, one rotated success")
}

// TestStreamNonKeyErrorDoesNotRotate pins the classification boundary: a
// 400 is about the request, not the key — exactly one attempt, no rotation,
// and the plain provider error surfaces without the pool annotation.
func TestStreamNonKeyErrorDoesNotRotate(t *testing.T) {
	ks := newKeyServer(t, func(hit keyHit, n int) (int, string, string) {
		return http.StatusBadRequest, "", ""
	})
	client := newPoolClient(ks.srvURL, "k1", "k2")

	var lastErr error
	for _, err := range iterErrors(client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hello"}})) {
		lastErr = err
	}
	require.Error(t, lastErr)
	require.NotContains(t, lastErr.Error(), "credential pool")
	require.Equal(t, 1, ks.hitCount(), "no second key spent on a request problem")
}

// TestStreamNoPoolKeepsSingleKeyBehavior pins the null world: without
// SetKeyPool, the transport's own retry budget runs and the error is the
// provider's, unannotated — byte-for-byte the pre-pool behavior.
func TestStreamNoPoolKeepsSingleKeyBehavior(t *testing.T) {
	ks := newKeyServer(t, func(hit keyHit, n int) (int, string, string) {
		return http.StatusTooManyRequests, "0", ""
	})
	client := NewClient(llm.ModelConfig{Name: "m", BaseURL: ks.srvURL, APIKey: "only"}, Hooks{}, nil, "")

	var lastErr error
	for _, err := range iterErrors(client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hello"}})) {
		lastErr = err
	}
	require.Error(t, lastErr)
	require.NotContains(t, lastErr.Error(), "credential pool")
	require.Equal(t, 5, ks.hitCount(), "transport budget only — no rotation without a pool")
}

// TestStreamSuccessAfterRotationIsSticky pins pool memory across calls:
// after k2 answers, the next Stream starts on k2 (no k1 request at all),
// because ReportSuccess re-elected it.
func TestStreamSuccessAfterRotationIsSticky(t *testing.T) {
	ks := newKeyServer(t, func(hit keyHit, n int) (int, string, string) {
		if hit.key == "k1" {
			return http.StatusUnauthorized, "", ""
		}
		return http.StatusOK, "", okSSE
	})
	client := newPoolClient(ks.srvURL, "k1", "k2")

	events := collectEvents(client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "one"}}))
	for _, ev := range events {
		require.Empty(t, ev.Err)
	}
	events = collectEvents(client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "two"}}))
	for _, ev := range events {
		require.Empty(t, ev.Err)
	}
	require.Equal(t, []string{"k1", "k2", "k2"}, ks.keys(), "the proven-good key holds current")
}

// TestCompactRotatesOnRateLimit pins the non-streaming half: compaction
// calls rotate too, so a background compact survives a rate-limited key
// without burning the turn.
func TestCompactRotatesOnRateLimit(t *testing.T) {
	// Compact needs a JSON completion response, not SSE.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key == "k1" {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"summary text"}}]}`))
	}))
	t.Cleanup(srv.Close)

	client := newPoolClient(srv.URL, "k1", "k2")
	res, err := client.Compact(t.Context(), llm.CompactRequest{Prompt: "summarize"})
	require.NoError(t, err)
	require.Equal(t, "summary text", res.Text)
	require.False(t, res.Truncated)
}

// TestCompactPoolExhausted annotates too: same contract as Stream.
func TestCompactPoolExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	}))
	t.Cleanup(srv.Close)

	client := newPoolClient(srv.URL, "k1", "k2")
	_, err := client.Compact(t.Context(), llm.CompactRequest{Prompt: "summarize"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "credential pool: all 2 keys failed")
	apiErr, ok := llm.APIErrorOf(err)
	require.True(t, ok)
	require.Equal(t, http.StatusTooManyRequests, apiErr.Status)
}

// TestCompactNoPoolSingleAttempt pins the no-pool compact: one key, the
// transport budget, the plain error.
func TestCompactNoPoolSingleAttempt(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(llm.ModelConfig{Name: "m", BaseURL: srv.URL, APIKey: "only"}, Hooks{}, nil, "")
	_, err := client.Compact(t.Context(), llm.CompactRequest{Prompt: "summarize"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "credential pool")
	require.Equal(t, 1, hits, "403 is not retried at transport level either")
}

// iterErrors drains a stream sequence collecting only errors, mirroring
// collectEvents for the error path.
func iterErrors(seq func(func(llm.StreamEvent, error) bool)) []error {
	var errs []error
	for _, err := range seq {
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}
