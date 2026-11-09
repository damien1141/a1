package llm

import (
	"context"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ErrorKind names the recovery strategy a failure calls for. Recovery sites
// branch on the Verdict, never on string matching of their own — one
// classification pipeline (status → wording → transport) keeps every caller
// answering the same question the same way.
type ErrorKind string

const (
	// KindOverflow is a proven context-window overflow (pattern-matched
	// provider text). Recovery: force-compact and retry.
	KindOverflow ErrorKind = "overflow"
	// KindSuspectedOverflow is an unexplained 400/413 that a large session
	// makes probably-overflow. Recovery: attempt compaction; the original
	// error surfaces when shrinking does not help.
	KindSuspectedOverflow ErrorKind = "suspected_overflow"
	// KindOverloaded is 429/503 wording that names capacity, not quota:
	// the endpoint is busy. Recovery: back off and retry the same request.
	KindOverloaded ErrorKind = "overloaded"
	// KindRateLimit is 429 quota/window throttling. Recovery: back off
	// (honoring a provider hint when one is parseable) and retry.
	KindRateLimit ErrorKind = "rate_limit"
	// KindAuth is 401/403: the credential is wrong or lacks access.
	// Terminal for the turn — no retry shape fixes it.
	KindAuth ErrorKind = "auth"
	// KindBilling is 402 or quota-exhausted wording without a transient
	// signal: the account, not the request, is the problem. Terminal.
	KindBilling ErrorKind = "billing"
	// KindServer is a 5xx without more specific wording. Retryable.
	KindServer ErrorKind = "server"
	// KindTimeout is a deadline: ours (context) or the transport's.
	// Retryable — a long prompt is not a wrong prompt.
	KindTimeout ErrorKind = "timeout"
	// KindNetwork is a transport failure (refused/reset/EOF/DNS). Retryable.
	KindNetwork ErrorKind = "network"
	// KindContentPolicy is a safety refusal. Terminal — retrying the same
	// request reproduces the same refusal.
	KindContentPolicy ErrorKind = "content_policy"
	// KindCancelled is caller cancellation. Never retried.
	KindCancelled ErrorKind = "cancelled"
	// KindUnknown is everything else. Not retried: an unclassified error
	// treated as retryable is how hot loops burn accounts.
	KindUnknown ErrorKind = "unknown"
)

// Verdict is the recovery verdict for one failure: what kind of failure it
// is and what the caller may do about it.
type Verdict struct {
	Kind ErrorKind
	// Retryable marks transient failures where re-sending the same request
	// after a backoff has a real chance of succeeding (overloaded, rate
	// limit, 5xx, timeout, network). Everything else is terminal for the
	// turn or belongs to a different recovery path.
	Retryable bool
	// ShouldCompress marks the overflow family: recovery is force-compact
	// and retry, not backoff.
	ShouldCompress bool
	// RetryAfter is a parsed provider hint ("try again in 30s"). Zero means
	// none was stated; callers fall back to their own backoff.
	RetryAfter time.Duration
}

// Classify resolves err into a recovery verdict. The pipeline is ordered so
// the most specific signal wins: cancellation first (never a provider
// property), then the proven-overflow patterns (their own non-overflow
// exclusions run inside), then the API status code with wording refinement,
// then the suspected-overflow shapes, then transport-level heuristics for
// errors that never became an APIError. The final fallback is unknown and
// NOT retryable.
func Classify(err error) Verdict {
	if err == nil {
		return Verdict{Kind: KindUnknown}
	}
	if errors.Is(err, context.Canceled) {
		return Verdict{Kind: KindCancelled}
	}
	// Proven overflow first: the patterns are precise and self-excluding
	// (throttling text that merely says "too many tokens" is filtered out
	// inside IsContextOverflow), so a 429 dressed as overflow text cannot
	// land here.
	if IsContextOverflow(err) {
		return Verdict{Kind: KindOverflow, ShouldCompress: true}
	}
	if apiErr, ok := APIErrorOf(err); ok {
		if v, ok := classifyStatus(apiErr); ok {
			return v
		}
	}
	if IsSuspectedOverflow(err) {
		return Verdict{Kind: KindSuspectedOverflow, ShouldCompress: true}
	}
	if v, ok := classifyTransport(err); ok {
		return v
	}
	return Verdict{Kind: KindUnknown}
}

// classifyStatus dispatches on the HTTP status with wording refinement. The
// bool reports whether the status is one this function owns; a 400 with no
// matching refinement falls through to the caller's later stages.
func classifyStatus(apiErr *APIError) (Verdict, bool) {
	msg := strings.ToLower(apiErr.Message)
	switch apiErr.Status {
	case 429:
		// Disambiguation (hermes error_classifier): the same status covers
		// three different recoveries. Capacity wording backs off; billing
		// wording is terminal; quota wording without a transient signal
		// ("try again", "resets in") is a quota wall, not a window.
		switch {
		case overloadedPattern.MatchString(msg):
			return Verdict{Kind: KindOverloaded, Retryable: true, RetryAfter: parseRetryAfter(msg)}, true
		case billingPattern.MatchString(msg):
			return Verdict{Kind: KindBilling}, true
		case quotaPattern.MatchString(msg) && parseRetryAfter(msg) == 0:
			return Verdict{Kind: KindBilling}, true
		default:
			return Verdict{Kind: KindRateLimit, Retryable: true, RetryAfter: parseRetryAfter(msg)}, true
		}
	case 408:
		return Verdict{Kind: KindTimeout, Retryable: true}, true
	case 401, 403:
		return Verdict{Kind: KindAuth}, true
	case 402:
		return Verdict{Kind: KindBilling}, true
	case 500, 502, 504, 529:
		return Verdict{Kind: KindServer, Retryable: true}, true
	case 503:
		if overloadedPattern.MatchString(msg) {
			return Verdict{Kind: KindOverloaded, Retryable: true, RetryAfter: parseRetryAfter(msg)}, true
		}
		return Verdict{Kind: KindServer, Retryable: true}, true
	case 400:
		if contentPolicyPattern.MatchString(msg) {
			return Verdict{Kind: KindContentPolicy}, true
		}
		// A plain 400 stays unowned here: the caller's proven-overflow
		// stage ran first and the suspected-overflow stage follows; only
		// then is it an unknown.
		return Verdict{Kind: KindUnknown}, false
	default:
		return Verdict{Kind: KindUnknown}, false
	}
}

// classifyTransport recognizes failures that never became an APIError:
// client timeouts, dropped connections, DNS, and (some proxies stream these
// as plain errors) throttling. Errors.As covers the typed cases; the string
// fallbacks cover providers that wrap transport errors in plain fmt.Errorf
// chains.
func classifyTransport(err error) (Verdict, bool) {
	if errors.Is(err, context.DeadlineExceeded) {
		return Verdict{Kind: KindTimeout, Retryable: true}, true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return Verdict{Kind: KindTimeout, Retryable: true}, true
	}
	msg := strings.ToLower(err.Error())
	// Throttling that never became an APIError (a proxy or SDK that streams
	// or wraps the 429): same verdict the status path would give.
	if throttlePattern.MatchString(msg) {
		return Verdict{Kind: KindRateLimit, Retryable: true, RetryAfter: parseRetryAfter(msg)}, true
	}
	if overloadedPattern.MatchString(msg) {
		return Verdict{Kind: KindOverloaded, Retryable: true, RetryAfter: parseRetryAfter(msg)}, true
	}
	for _, p := range transportPatterns {
		if p.MatchString(msg) {
			return Verdict{Kind: KindNetwork, Retryable: true}, true
		}
	}
	return Verdict{}, false
}

// throttlePattern matches throttling text in non-APIError wrappers.
var throttlePattern = regexp.MustCompile(`throttl|rate limit|too many requests`)

// overloadedPattern is capacity wording: the endpoint is busy, the same key
// and request will be served later.
var overloadedPattern = regexp.MustCompile(`overloaded|overload|at capacity|high load|capacity`)

// billingPattern is account wording: credits, billing, payment.
var billingPattern = regexp.MustCompile(`insufficient credits|billing|payment required|credit balance|add credits|exceeded your current quota`)

// quotaPattern is quota wording that may or may not carry a reset signal;
// paired with a parsed RetryAfter it stays a rate limit, without one it is
// a quota wall. Deliberately excludes the bare phrases "rate limit" and
// "too many requests": those are the ordinary window throttle (transient
// by definition), and folding them into the quota wall made every plain
// 429 terminal.
var quotaPattern = regexp.MustCompile(`quota`)

// contentPolicyPattern is safety-refusal wording on a 400.
var contentPolicyPattern = regexp.MustCompile(`content policy|content_policy|content filter|safety system|policy violation|inappropriate content`)

// transportPattern matches transport failure text in wrapped errors.
var transportPatterns = []*regexp.Regexp{
	regexp.MustCompile(`connection refused`),
	regexp.MustCompile(`connection reset`),
	regexp.MustCompile(`broken pipe`),
	regexp.MustCompile(`unexpected eof`),
	regexp.MustCompile(`(?i)\beof\b`),
	regexp.MustCompile(`no such host`),
	regexp.MustCompile(`i/o timeout`),
	regexp.MustCompile(`client.timeout exceeded`),
	regexp.MustCompile(`context deadline exceeded`),
	regexp.MustCompile(`connection timed out`),
}

// retryAfterPattern pulls a provider-stated wait out of error text:
// "try again in 30s", "retrying in 5 seconds", "resets in 1 minute".
var retryAfterPattern = regexp.MustCompile(
	`(?i)(?:try again|retry(?:ing)?|resets?|available)[^0-9]{0,16}(\d{1,6})\s*(seconds?|secs?|s\b|minutes?|mins?|m\b|hours?|h\b)`)

// StatusRetryOwned reports whether err is an APIError whose status the HTTP
// layer's own retry (util.DoWithRetry) has already exhausted: 429s get five
// budgeted, Retry-After-aware attempts under a total-wait cap, 5xx three.
// Re-retrying those at the engine layer would multiply the patient path
// into a many-minute stall — the engine layer covers only what the HTTP
// layer cannot see (mid-stream error events, post-connection drops,
// timeouts, 408).
func StatusRetryOwned(err error) bool {
	apiErr, ok := APIErrorOf(err)
	if !ok {
		return false
	}
	return apiErr.Status == http.StatusTooManyRequests || apiErr.Status >= 500
}

// maxRetryAfter caps a provider hint: a turn loop is interactive, and a
// "try again in 6 hours" must degrade to the ordinary backoff ceiling
// instead of parking the UI.
const maxRetryAfter = 60 * time.Second

// parseRetryAfter extracts a wait hint from message text, capped. Zero when
// nothing parseable is present.
func parseRetryAfter(msg string) time.Duration {
	m := retryAfterPattern.FindStringSubmatch(msg)
	if m == nil {
		return 0
	}
	var n int
	for _, c := range m[1] {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
		if n > 1_000_000 {
			return maxRetryAfter
		}
	}
	if n == 0 {
		return 0
	}
	var d time.Duration
	switch strings.ToLower(m[2])[:1] {
	case "h":
		d = time.Duration(n) * time.Hour
	case "m":
		d = time.Duration(n) * time.Minute
	default:
		d = time.Duration(n) * time.Second
	}
	if d > maxRetryAfter {
		return maxRetryAfter
	}
	return d
}

// Backoff computes the retry delay for the nth attempt (0-based). A parsed
// provider hint wins outright (capped at parse time); otherwise the delay
// doubles per attempt from 2s to a 30s ceiling. Single-client harness: no
// jitter, because there is no herd to decorrelate — one process is one
// caller, and a deterministic schedule keeps tests and logs honest.
func (v Verdict) Backoff(attempt int) time.Duration {
	if v.RetryAfter > 0 {
		return v.RetryAfter
	}
	d := time.Second << (attempt + 1) // 2s, 4s, 8s…
	if d > 30*time.Second || d <= 0 {
		d = 30 * time.Second
	}
	return d
}
