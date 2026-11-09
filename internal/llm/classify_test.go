package llm

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Verdict
	}{
		{
			name: "nil error is unknown and not retryable",
			err:  nil,
			want: Verdict{Kind: KindUnknown},
		},
		{
			name: "proven overflow beats status dispatch",
			err:  fmt.Errorf("fake API error (400): prompt is too long: 210000 > 200000 tokens"),
			want: Verdict{Kind: KindOverflow, ShouldCompress: true},
		},
		{
			name: "throttling that says too many tokens is not an overflow",
			err:  fmt.Errorf("Throttling error: too many tokens, please wait before trying again"),
			want: Verdict{Kind: KindRateLimit, Retryable: true},
		},
		{
			name: "429 overloaded wording backs off the same request",
			err:  &APIError{Provider: "fake", Status: 429, Message: "The server is overloaded, try again in 2 seconds"},
			want: Verdict{Kind: KindOverloaded, Retryable: true, RetryAfter: 2 * time.Second},
		},
		{
			name: "429 plain is a rate limit",
			err:  &APIError{Provider: "fake", Status: 429, Message: "Rate limit reached for requests"},
			want: Verdict{Kind: KindRateLimit, Retryable: true},
		},
		{
			name: "429 with a reset hint keeps the parsed delay",
			err:  &APIError{Provider: "fake", Status: 429, Message: "Rate limit reached. Try again in 30 seconds."},
			want: Verdict{Kind: KindRateLimit, Retryable: true, RetryAfter: 30 * time.Second},
		},
		{
			name: "429 quota wording without a transient signal is billing",
			err:  &APIError{Provider: "fake", Status: 429, Message: "You exceeded your current quota, please check your plan"},
			want: Verdict{Kind: KindBilling},
		},
		{
			name: "429 quota wording with a reset signal stays a rate limit",
			err:  &APIError{Provider: "fake", Status: 429, Message: "quota will reset in 45 seconds"},
			want: Verdict{Kind: KindRateLimit, Retryable: true, RetryAfter: 45 * time.Second},
		},
		{
			name: "402 is billing",
			err:  &APIError{Provider: "fake", Status: 402, Message: "insufficient credits"},
			want: Verdict{Kind: KindBilling},
		},
		{
			name: "401 is auth and terminal",
			err:  &APIError{Provider: "fake", Status: 401, Message: "invalid api key"},
			want: Verdict{Kind: KindAuth},
		},
		{
			name: "500 is a retryable server error",
			err:  &APIError{Provider: "fake", Status: 500, Message: "internal server error"},
			want: Verdict{Kind: KindServer, Retryable: true},
		},
		{
			name: "503 overloaded wording is overloaded, not server",
			err:  &APIError{Provider: "fake", Status: 503, Message: "service overloaded"},
			want: Verdict{Kind: KindOverloaded, Retryable: true},
		},
		{
			name: "opaque 400 is suspected overflow",
			err:  &APIError{Provider: "fake", Status: 400, Message: "Provider returned error."},
			want: Verdict{Kind: KindSuspectedOverflow, ShouldCompress: true},
		},
		{
			name: "413 with explicit payload wording is a proven overflow",
			err:  &APIError{Provider: "fake", Status: 413, Message: "payload too large"},
			want: Verdict{Kind: KindOverflow, ShouldCompress: true},
		},
		{
			name: "413 with an opaque body is suspected overflow",
			err:  &APIError{Provider: "fake", Status: 413, Message: "Provider returned error."},
			want: Verdict{Kind: KindSuspectedOverflow, ShouldCompress: true},
		},
		{
			name: "400 content policy wording is terminal",
			err:  &APIError{Provider: "fake", Status: 400, Message: "your request was rejected by the content policy"},
			want: Verdict{Kind: KindContentPolicy},
		},
		{
			name: "plain 400 with informative text stays unknown",
			err:  &APIError{Provider: "fake", Status: 400, Message: "max_tokens must be at least 1"},
			want: Verdict{Kind: KindUnknown},
		},
		{
			name: "wrapped APIError unwraps to its status",
			err:  fmt.Errorf("turn failed: %w", &APIError{Provider: "fake", Status: 502, Message: "bad gateway"}),
			want: Verdict{Kind: KindServer, Retryable: true},
		},
		{
			name: "context cancellation is never retried",
			err:  context.Canceled,
			want: Verdict{Kind: KindCancelled},
		},
		{
			name: "context deadline is a retryable timeout",
			err:  context.DeadlineExceeded,
			want: Verdict{Kind: KindTimeout, Retryable: true},
		},
		{
			name: "net.Error timeout is a retryable timeout",
			err:  &netOpError{timeout: true},
			want: Verdict{Kind: KindTimeout, Retryable: true},
		},
		{
			name: "connection reset is retryable network",
			err:  errors.New("read tcp 10.0.0.1:1234->10.0.0.2:443: connection reset by peer"),
			want: Verdict{Kind: KindNetwork, Retryable: true},
		},
		{
			name: "unexpected EOF is retryable network",
			err:  errors.New("unexpected EOF"),
			want: Verdict{Kind: KindNetwork, Retryable: true},
		},
		{
			name: "unclassifiable text is unknown and not retryable",
			err:  errors.New("something completely different"),
			want: Verdict{Kind: KindUnknown},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// netOpError satisfies net.Error with a fixed timeout flag.
type netOpError struct {
	timeout bool
}

func (e *netOpError) Error() string   { return "operation timed out" }
func (e *netOpError) Timeout() bool   { return e.timeout }
func (e *netOpError) Temporary() bool { return e.timeout }

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"try again in 30 seconds", 30 * time.Second},
		{"try again in 5s", 5 * time.Second},
		{"retrying in 12 seconds", 12 * time.Second},
		{"quota resets in 45 seconds", 45 * time.Second},
		{"available in 1 minute", time.Minute},
		{"try again in 2 minutes", maxRetryAfter}, // capped: an interactive turn never parks past 60s
		{"try again in 6 hours", maxRetryAfter},   // capped: an interactive turn never parks for hours
		{"no hint here", 0},
		{"", 0},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, parseRetryAfter(tc.in), "input %q", tc.in)
	}
}

func TestVerdictBackoff(t *testing.T) {
	t.Run("provider hint wins", func(t *testing.T) {
		v := Verdict{Retryable: true, RetryAfter: 7 * time.Second}
		assert.Equal(t, 7*time.Second, v.Backoff(0))
		assert.Equal(t, 7*time.Second, v.Backoff(5), "the hint does not scale with attempts")
	})
	t.Run("doubling from 2s to the 30s ceiling", func(t *testing.T) {
		v := Verdict{Retryable: true}
		assert.Equal(t, 2*time.Second, v.Backoff(0))
		assert.Equal(t, 4*time.Second, v.Backoff(1))
		assert.Equal(t, 8*time.Second, v.Backoff(2))
		assert.Equal(t, 30*time.Second, v.Backoff(9), "ceiling holds at 30s")
	})
}

// The APIError must keep surviving fmt.Errorf("%w") wrapping — the
// classifier's status dispatch depends on errors.As reaching it.
func TestClassifyUnwrapChain(t *testing.T) {
	inner := &APIError{Provider: "fake", Status: 429, Message: "rate limited, retry in 1 seconds"}
	wrapped := fmt.Errorf("outer: %w", fmt.Errorf("middle: %w", inner))
	require.Equal(t, Verdict{Kind: KindRateLimit, Retryable: true, RetryAfter: time.Second}, Classify(wrapped))
}
