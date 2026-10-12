package util

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// maxHTTPRetryAttempts bounds retries for a transient 5xx or a stale
	// connection. The failure is usually a gateway blip, so the caller wants an
	// answer rather than a long wait.
	maxHTTPRetryAttempts = 3
	// maxRateLimitAttempts bounds retries for 429. A rate limit is temporary by
	// definition and the provider says when the window frees, so waiting nearly
	// always beats failing a turn the user is watching. The old policy treated
	// a 429 like a 5xx blip — three attempts totalling two seconds of patience
	// against a limit that needs tens of seconds.
	maxRateLimitAttempts = 5
)

// rateLimitBackoffBase is the first step of the exponential backoff used when a
// 429 carries no usable Retry-After: 1s, 2s, 4s, 8s.
const rateLimitBackoffBase = time.Second

// maxRetryAfterDelay caps an honored Retry-After. A provider can answer a burst
// limit with "3600", and silently parking an interactive agent loop for an hour
// is a worse failure than reporting the limit. The cap still covers every real
// limit window.
const maxRetryAfterDelay = 60 * time.Second

// maxRateLimitTotalWait bounds the whole 429 retry sequence, so a provider that
// keeps answering "not yet" cannot hold the loop open indefinitely. Ctrl-C
// still interrupts any single wait through the request context.
const maxRateLimitTotalWait = 90 * time.Second

// maxRetryJitter spreads retries fired by concurrent callers, so a burst limit
// does not simply re-form on the next attempt.
const maxRetryJitter = 500 * time.Millisecond

func shouldRetryHTTPStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// attemptsFor picks the retry budget for the status that was just returned.
func attemptsFor(status int) int {
	if status == http.StatusTooManyRequests {
		return maxRateLimitAttempts
	}
	return maxHTTPRetryAttempts
}

// retryDelay returns a valid server-provided retry delay, or the existing
// attempt-based fallback when Retry-After is missing or invalid.
func retryDelay(resp *http.Response, attempt int) time.Duration {
	if resp != nil {
		if delay, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
			return delay
		}
	}
	if attempt == 0 {
		return 0
	}
	return time.Duration(attempt+1) * time.Second
}

// rateLimitDelay is how long to wait before retry number attempt (0-based)
// after a 429. Retry-After wins when it is present and sane, capped so an
// absurd value cannot park the loop; otherwise exponential with jitter.
func rateLimitDelay(resp *http.Response, attempt int) time.Duration {
	if resp != nil {
		if delay, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
			return min(delay, maxRetryAfterDelay)
		}
	}
	backoff := min(rateLimitBackoffBase<<attempt, maxRetryAfterDelay)
	return backoff + jitter()
}

// jitter randomizes each wait by up to maxRetryJitter.
func jitter() time.Duration {
	return time.Duration(rand.Int63n(int64(maxRetryJitter) + 1))
}

// delayFor chooses the schedule for the status that was just returned.
func delayFor(resp *http.Response, attempt int) time.Duration {
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		return rateLimitDelay(resp, attempt)
	}
	return retryDelay(resp, attempt)
}

// parseRetryAfter parses the Retry-After header's delay-seconds or HTTP-date
// form. The boolean distinguishes an explicit zero delay from an invalid
// header, which must use the local fallback.
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	if isDecimalDigits(value) {
		seconds, err := strconv.ParseUint(value, 10, 64)
		if err != nil || seconds > uint64(math.MaxInt64/int64(time.Second)) {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}

	retryAt, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := retryAt.Sub(now)
	delay = max(delay, 0)
	return delay, true
}

func isDecimalDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func isStaleConnError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	if opErr, ok := errors.AsType[*net.OpError](err); ok {
		if errors.Is(opErr.Err, syscall.ECONNRESET) || errors.Is(opErr.Err, syscall.EPIPE) {
			return true
		}
	}
	msg := err.Error()
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "use of closed network connection")
}

func sleepWithCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// DoWithRetry retries transient HTTP failures (429/5xx, stale keep-alive).
func DoWithRetry(client *http.Client, req *http.Request) (*http.Response, error) {
	var bodyBytes []byte
	hasBody := req.Body != nil
	if hasBody {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
	}

	newAttempt := func() *http.Request {
		r := req.Clone(req.Context())
		if hasBody {
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			r.ContentLength = int64(len(bodyBytes))
		}
		return r
	}

	var (
		resp *http.Response
		err  error
	)
	// waited tracks total 429 wait so the sequence cannot run unbounded.
	var waited time.Duration
	for attempt := range max(maxHTTPRetryAttempts, maxRateLimitAttempts) {
		if resp != nil {
			_ = resp.Body.Close()
			resp = nil
		}
		resp, err = client.Do(newAttempt())
		if err != nil {
			if attempt == 0 && isStaleConnError(err) {
				continue
			}
			return nil, err
		}
		if !shouldRetryHTTPStatus(resp.StatusCode) {
			return resp, nil
		}
		// The budget depends on the status: a 429 gets more patience than a
		// gateway blip. Compute it per attempt, because a mixed sequence (429,
		// then 503) may switch budget partway through.
		budget := attemptsFor(resp.StatusCode)
		if attempt >= budget-1 {
			break
		}
		delay := delayFor(resp, attempt)
		if resp.StatusCode == http.StatusTooManyRequests {
			if waited+delay > maxRateLimitTotalWait {
				delay = maxRateLimitTotalWait - waited
				if delay <= 0 {
					break
				}
			}
			waited += delay
		}

		// We are definitely retrying, so do not hold the response body while sleeping.
		_ = resp.Body.Close()
		resp = nil
		if err := sleepWithCtx(req.Context(), delay); err != nil {
			return nil, err
		}
	}
	return resp, nil
}
