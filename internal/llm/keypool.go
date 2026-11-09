package llm

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Credential-pool tuning. A key's cooldown is the LONGER of the provider's
// Retry-After hint and the backoff ladder: the hint extends the wait beyond
// the ladder, but never shortens it — a pool can afford the conservative
// read, because the other keys absorb the traffic either way.
const (
	// RateLimitCooldownBase is the first cooldown for a key that answered
	// 429 with no Retry-After header. One minute matches the per-minute
	// windows most providers rate on.
	RateLimitCooldownBase = 60 * time.Second
	// rateLimitCooldownCap bounds the ladder (not an explicit Retry-After):
	// a key that keeps tripping 429s sits out longer each time, but never
	// more than a quarter hour — the fail-open Acquire would hand it back
	// anyway once everything else cools, so a longer self-imposed wait
	// would only obscure which key is the problem.
	rateLimitCooldownCap = 15 * time.Minute
	// AuthFailureCooldown quarantines a key that answered 401/403. Auth
	// failures are rarely transient, but a proxy hiccup can wear one, so
	// the key returns after ten minutes rather than being dropped for the
	// process lifetime.
	AuthFailureCooldown = 10 * time.Minute
)

// KeyFailure classifies an error for credential-pool purposes. Errors that
// are not about the key itself (5xx, network, context) are KeyFailureNone —
// rotating keys cannot help them, and the existing retry machinery owns
// them.
type KeyFailure int

const (
	KeyFailureNone KeyFailure = iota
	KeyFailureRateLimit
	KeyFailureAuth
)

// ClassifyKeyFailure reports what, if anything, a provider error means for
// the key that produced it, plus the provider's Retry-After hint when one
// rode along on a 429.
func ClassifyKeyFailure(err error) (KeyFailure, time.Duration) {
	apiErr, ok := APIErrorOf(err)
	if !ok {
		return KeyFailureNone, 0
	}
	switch apiErr.Status {
	case http.StatusTooManyRequests:
		return KeyFailureRateLimit, apiErr.RetryAfter
	case http.StatusUnauthorized, http.StatusForbidden:
		return KeyFailureAuth, 0
	default:
		return KeyFailureNone, 0
	}
}

// KeyPool rotates a set of API keys for one endpoint. The pool is sticky:
// it stays on the current key until that key fails, and a key that just
// succeeded becomes current — rotation is event-driven (a 429 or an auth
// failure), so a healthy setup makes exactly one request per turn carry the
// same key it always carried.
//
// A nil *KeyPool is the single-key world: callers treat it as "no pool" and
// pass cfg.APIKey straight through, which is why NewKeyPool returns nil for
// fewer than two unique keys — a pool of one is not a pool.
type KeyPool struct {
	mu    sync.Mutex
	keys  []string
	cur   int
	cool  []time.Time // per key: unusable until this instant (zero = usable)
	fails []int       // consecutive rate-limit reports, for the backoff ladder
}

// NewKeyPool builds a pool from keys, dropping empties and duplicates. It
// returns nil when fewer than two unique keys remain — the caller then
// keeps the exact pre-pool single-key behavior.
func NewKeyPool(keys []string) *KeyPool {
	uniq := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		uniq = append(uniq, k)
	}
	if len(uniq) < 2 {
		return nil
	}
	return &KeyPool{
		keys:  uniq,
		cool:  make([]time.Time, len(uniq)),
		fails: make([]int, len(uniq)),
	}
}

// KeySetSignature returns a stable identity for a key list after the same
// normalization NewKeyPool applies (trim, drop empties, drop duplicates),
// made order-insensitive by sorting: two configs naming the same keys in
// different orders are the same endpoint account and must share a pool.
// Pool registries that share pools across client rebuilds key them by it.
func KeySetSignature(keys []string) string {
	seen := make(map[string]bool, len(keys))
	uniq := make([]string, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		uniq = append(uniq, k)
	}
	sort.Strings(uniq)
	return strings.Join(uniq, "\x00")
}

// Len reports how many keys the pool holds.
func (p *KeyPool) Len() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.keys)
}

// Acquire returns the key the next request should use. The current key is
// sticky while usable; when it is cooling, Acquire walks forward to the
// first usable key, and when every key is cooling it fails open on the one
// whose cooldown expires soonest — trying the likeliest key beats refusing
// to try, and a pool-wide rate limit is the provider's problem, not a
// reason for the turn to die.
func (p *KeyPool) Acquire(now time.Time) (string, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cool[p.cur].After(now) {
		// The current key is cooling. Walk forward to the first usable
		// key; if none is usable, fail open on the earliest expiry —
		// including the current key's own — because trying the likeliest
		// key beats refusing to try, and a pool-wide rate limit is the
		// provider's problem, not a reason for the turn to die.
		best := p.cur
		for i := 1; i < len(p.keys); i++ {
			j := (p.cur + i) % len(p.keys)
			if !p.cool[j].After(now) {
				best = j
				break
			}
			if p.cool[j].Before(p.cool[best]) {
				best = j
			}
		}
		p.cur = best
	}
	return p.keys[p.cur], p.cur
}

// ReportSuccess clears a key's failure state and makes it current: a key
// that just answered is the best default for the next request.
func (p *KeyPool) ReportSuccess(idx int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if idx < 0 || idx >= len(p.keys) {
		return
	}
	p.fails[idx] = 0
	p.cool[idx] = time.Time{}
	p.cur = idx
}

// ReportRateLimited cools a key that answered 429 and rotates away from
// it. The wait is the longer of the provider's Retry-After (when sent) and
// the backoff ladder — base doubling per consecutive rate-limit, capped.
// Consecutive counts reset on success.
func (p *KeyPool) ReportRateLimited(idx int, retryAfter time.Duration, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if idx < 0 || idx >= len(p.keys) {
		return
	}
	p.fails[idx]++
	wait := retryAfter
	if ladder := rateLimitBackoff(p.fails[idx]); ladder > wait {
		wait = ladder
	}
	// A hint longer than the cap is the provider's own word about its
	// window; only the self-imposed ladder is capped.
	if until := now.Add(wait); p.cool[idx].Before(until) {
		p.cool[idx] = until
	}
	p.rotateLocked(idx, now)
}

// ReportAuthFailure quarantines a key that answered 401/403 and rotates
// away from it. The key is not dropped: proxies can wear a transient auth
// blip, so it rejoins the rotation after AuthFailureCooldown.
func (p *KeyPool) ReportAuthFailure(idx int, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if idx < 0 || idx >= len(p.keys) {
		return
	}
	if until := now.Add(AuthFailureCooldown); p.cool[idx].Before(until) {
		p.cool[idx] = until
	}
	p.rotateLocked(idx, now)
}

// rotateLocked moves current off idx to the next usable key, or — when
// everything is cooling — to the earliest expiry so the next Acquire fails
// open on the likeliest key. Callers hold mu.
func (p *KeyPool) rotateLocked(from int, now time.Time) {
	best := -1
	for i := 1; i < len(p.keys); i++ {
		j := (from + i) % len(p.keys)
		if !p.cool[j].After(now) {
			best = j
			break
		}
		if best == -1 || p.cool[j].Before(p.cool[best]) {
			best = j
		}
	}
	if best >= 0 {
		p.cur = best
	}
}

// rateLimitBackoff is the ladder for n consecutive rate-limit failures:
// base doubled per failure, capped. Growth stops at the cap rather than
// overflowing.
func rateLimitBackoff(n int) time.Duration {
	wait := RateLimitCooldownBase
	for i := 1; i < n && wait < rateLimitCooldownCap; i++ {
		wait *= 2
	}
	if wait > rateLimitCooldownCap {
		wait = rateLimitCooldownCap
	}
	return wait
}
