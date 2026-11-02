package util

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	future := now.Add(3 * time.Second).Format(http.TimeFormat)
	past := now.Add(-3 * time.Second).Format(http.TimeFormat)
	overflow := strconv.FormatUint(uint64(math.MaxInt64/int64(time.Second))+1, 10)

	tests := []struct {
		name  string
		value string
		want  time.Duration
		valid bool
	}{
		{name: "missing", value: "", want: 0, valid: false},
		{name: "seconds with whitespace", value: " 3 ", want: 3 * time.Second, valid: true},
		{name: "zero seconds", value: "0", want: 0, valid: true},
		{name: "negative seconds", value: "-1", want: 0, valid: false},
		{name: "fractional seconds", value: "1.5", want: 0, valid: false},
		{name: "signed seconds", value: "+3", want: 0, valid: false},
		{name: "seconds overflow", value: overflow, want: 0, valid: false},
		{name: "future date", value: future, want: 3 * time.Second, valid: true},
		{name: "past date", value: past, want: 0, valid: true},
		{name: "invalid date", value: "tomorrow", want: 0, valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, valid := parseRetryAfter(tt.value, now)
			require.Equal(t, tt.want, got, "parseRetryAfter(%q) duration", tt.value)
			require.Equal(t, tt.valid, valid, "parseRetryAfter(%q) valid", tt.value)
		})
	}
}

func TestRetryDelayFallbackAndHeaderPrecedence(t *testing.T) {
	tests := []struct {
		name      string
		attempt   int
		header    string
		wantDelay time.Duration
	}{
		{name: "first fallback is immediate", attempt: 0, wantDelay: 0},
		{name: "second fallback is two seconds", attempt: 1, wantDelay: 2 * time.Second},
		{name: "zero header overrides fallback", attempt: 1, header: "0", wantDelay: 0},
		{name: "invalid header uses fallback", attempt: 1, header: "1.5", wantDelay: 2 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp *http.Response
			if tt.header != "" {
				resp = &http.Response{Header: http.Header{"Retry-After": []string{tt.header}}}
			}
			require.Equal(
				t,
				tt.wantDelay,
				retryDelay(resp, tt.attempt),
				"retryDelay(attempt=%d, header=%q)",
				tt.attempt,
				tt.header,
			)
		})
	}
}

func TestDoWithRetryRetryAfterBehavior(t *testing.T) {
	pastDate := time.Now().UTC().Add(-time.Minute).Format(http.TimeFormat)
	futureDate := time.Now().UTC().Add(time.Minute).Format(http.TimeFormat)

	type scriptedResponse struct {
		status     int
		retryAfter string
		body       string
	}
	tests := []struct {
		name         string
		responses    []scriptedResponse
		timeout      time.Duration
		wantErr      bool
		wantStatus   int
		wantRequests int32
	}{
		{
			name: "zero header overrides second fallback",
			responses: []scriptedResponse{
				{status: http.StatusServiceUnavailable},
				{status: http.StatusTooManyRequests, retryAfter: "0"},
				{status: http.StatusOK, body: "ok"},
			},
			timeout:      250 * time.Millisecond,
			wantStatus:   http.StatusOK,
			wantRequests: 3,
		},
		{
			name: "past date overrides second fallback",
			responses: []scriptedResponse{
				{status: http.StatusServiceUnavailable},
				{status: http.StatusServiceUnavailable, retryAfter: pastDate},
				{status: http.StatusOK, body: "ok"},
			},
			timeout:      250 * time.Millisecond,
			wantStatus:   http.StatusOK,
			wantRequests: 3,
		},
		{
			name: "seconds header is cancellable",
			responses: []scriptedResponse{
				{status: http.StatusServiceUnavailable, retryAfter: "60"},
			},
			timeout:      50 * time.Millisecond,
			wantErr:      true,
			wantRequests: 1,
		},
		{
			name: "date header is cancellable",
			responses: []scriptedResponse{
				{status: http.StatusServiceUnavailable, retryAfter: futureDate},
			},
			timeout:      50 * time.Millisecond,
			wantErr:      true,
			wantRequests: 1,
		},
		{
			name: "fallback is cancellable after second failure",
			responses: []scriptedResponse{
				{status: http.StatusServiceUnavailable},
				{status: http.StatusServiceUnavailable},
			},
			timeout:      50 * time.Millisecond,
			wantErr:      true,
			wantRequests: 2,
		},
		{
			name: "final retryable response is returned",
			responses: []scriptedResponse{
				{status: http.StatusServiceUnavailable, retryAfter: "0", body: "first"},
				{status: http.StatusServiceUnavailable, retryAfter: "0", body: "second"},
				{status: http.StatusServiceUnavailable, retryAfter: "0", body: "final"},
			},
			timeout:      250 * time.Millisecond,
			wantStatus:   http.StatusServiceUnavailable,
			wantRequests: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n := int(requests.Add(1))
				response := scriptedResponse{status: http.StatusOK, body: "unexpected"}
				if n <= len(tt.responses) {
					response = tt.responses[n-1]
				}
				if response.retryAfter != "" {
					w.Header().Set("Retry-After", response.retryAfter)
				}
				w.WriteHeader(response.status)
				_, _ = io.WriteString(w, response.body)
			}))
			defer server.Close()

			ctx, cancel := context.WithTimeout(t.Context(), tt.timeout)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, http.NoBody)
			require.NoError(t, err)

			resp, err := DoWithRetry(server.Client(), req)
			if tt.wantErr {
				require.Error(t, err, "DoWithRetry should return error")
				require.ErrorIs(
					t,
					err,
					context.DeadlineExceeded,
					"DoWithRetry error = %v, want context deadline exceeded",
					err,
				)
				if resp != nil {
					resp.Body.Close()
					require.Fail(t, "DoWithRetry returned a response with an error")
				}
			} else {
				require.NoError(t, err, "DoWithRetry returned error")
				require.NotNil(t, resp, "DoWithRetry returned nil response")
				require.Equal(t, tt.wantStatus, resp.StatusCode, "status mismatch")
				_ = resp.Body.Close()
			}

			require.Equal(t, tt.wantRequests, requests.Load(), "requests mismatch")
		})
	}
}

// A 429 must get more patience than a transient 5xx: the limit is temporary by
// definition, so failing a turn after three seconds of waiting is a bug. This
// is the regression test for "LLM API error (429)" surfacing to the user.
func TestDoWithRetryGivesRateLimitMoreAttempts(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		wantRequests int32
	}{
		{name: "rate limit gets five attempts", status: http.StatusTooManyRequests, wantRequests: 5},
		{name: "gateway blip gets three attempts", status: http.StatusServiceUnavailable, wantRequests: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				// Retry-After: 0 keeps the test fast while still exercising the
				// real schedule.
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, http.NoBody)
			require.NoError(t, err)

			resp, err := DoWithRetry(server.Client(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			_ = resp.Body.Close()
			require.Equal(t, tt.wantRequests, requests.Load(), "attempts for status %d", tt.status)
		})
	}
}

// Without Retry-After the 429 waits grow exponentially, so the total is far more
// than the two seconds the old fallback gave it.
func TestRateLimitBackoffIsExponentialAndHonorsHeader(t *testing.T) {
	resp := func(retryAfter string) *http.Response {
		if retryAfter == "" {
			return nil
		}
		return &http.Response{Header: http.Header{"Retry-After": []string{retryAfter}}}
	}

	// Header wins when sane.
	require.Equal(t, 30*time.Second, rateLimitDelay(resp("30"), 0), "Retry-After is honored")
	// Absurd header is capped so an hour-long park is impossible.
	require.Equal(t, maxRetryAfterDelay, rateLimitDelay(resp("3600"), 0), "Retry-After is capped")
	// No header: exponential with jitter, always at least the base step.
	for attempt := range 4 {
		got := rateLimitDelay(nil, attempt)
		floor := time.Second << attempt
		require.GreaterOrEqual(t, got, floor, "attempt %d below its backoff step", attempt)
		require.LessOrEqual(t, got, floor+maxRetryJitter, "attempt %d above its step plus jitter", attempt)
	}
}

// A mixed sequence must use the right budget for each status, so a 429 followed
// by a gateway blip does not inherit five attempts of patience forever.
func TestDoWithRetryMixedSequenceUsesPerStatusBudget(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(requests.Add(1))
		w.Header().Set("Retry-After", "0")
		if n <= 4 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// Fifth request: a gateway blip, which allows one more attempt.
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, http.NoBody)
	require.NoError(t, err)

	resp, err := DoWithRetry(server.Client(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = resp.Body.Close()
	// Four 429s (attempts 0-3) then a 503 on attempt 4. The 5xx budget is three
	// attempts, so attempt 4 is already past it and the sequence stops — the
	// 503 does not inherit the 429's patience.
	require.Equal(t, int32(5), requests.Load(), "four 429s stop at the 503 budget")
}
