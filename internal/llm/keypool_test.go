package llm

import (
        "fmt"
        "testing"
        "time"

        "github.com/stretchr/testify/require"
)

func TestNewKeyPoolDedupesAndGates(t *testing.T) {
        require.Nil(t, NewKeyPool(nil), "no keys is no pool")
        require.Nil(t, NewKeyPool([]string{""}), "one empty key is no pool")
        require.Nil(t, NewKeyPool([]string{"k1", " k1 ", "k1"}), "duplicates collapse to one key — still no pool")
        require.Nil(t, NewKeyPool([]string{"k1"}), "a pool of one is not a pool")

        p := NewKeyPool([]string{"k1", "", " k2 ", "k1"})
        require.NotNil(t, p)
        require.Equal(t, 2, p.Len(), "empties dropped, whitespace trimmed, duplicate dropped")
}

func TestKeyPoolAcquireStartsSticky(t *testing.T) {
        now := time.Now()
        p := NewKeyPool([]string{"a", "b", "c"})

        key, idx := p.Acquire(now)
        require.Equal(t, "a", key)
        require.Equal(t, 0, idx, "first key is current")

        // A usable current key is sticky: no event, no rotation.
        key, idx = p.Acquire(now.Add(time.Second))
        require.Equal(t, "a", key)
        require.Equal(t, 0, idx)
}

func TestKeyPoolRateLimitRotatesAndCools(t *testing.T) {
        now := time.Now()
        p := NewKeyPool([]string{"a", "b"})

        p.ReportRateLimited(0, 0, now)

        key, idx := p.Acquire(now.Add(time.Second))
        require.Equal(t, "b", key)
        require.Equal(t, 1, idx, "rotated off the rate-limited key")

        // b holds while it works — rotation is event-driven, not round-robin.
        _, idx = p.Acquire(now.Add(RateLimitCooldownBase + time.Second))
        require.Equal(t, 1, idx, "a recovered but b holds current until it fails")

        // b failing rotates back to a, whose cooldown has long expired.
        p.ReportRateLimited(1, 0, now.Add(time.Minute))
        _, idx = p.Acquire(now.Add(time.Minute + time.Second))
        require.Equal(t, 0, idx)
}

func TestKeyPoolRateLimitHonorsRetryAfter(t *testing.T) {
        now := time.Now()
        p := NewKeyPool([]string{"a", "b"})

        // a: Retry-After 5m (beats the 60s ladder). b: ladder only (60s).
        p.ReportRateLimited(0, 5*time.Minute, now)
        p.ReportRateLimited(1, 0, now)

        // At now+2m, b's ladder cooldown has expired but a's Retry-After
        // holds: the walk must land on b, not fail open onto a.
        _, idx := p.Acquire(now.Add(2 * time.Minute))
        require.Equal(t, 1, idx, "a's Retry-After keeps it out past b's ladder cooldown")
}

func TestRateLimitBackoffLadder(t *testing.T) {
        require.Equal(t, RateLimitCooldownBase, rateLimitBackoff(1))
        require.Equal(t, 2*RateLimitCooldownBase, rateLimitBackoff(2))
        require.Equal(t, 4*RateLimitCooldownBase, rateLimitBackoff(3))
        require.Equal(t, rateLimitCooldownCap, rateLimitBackoff(50), "the ladder caps")
        require.Equal(t, rateLimitCooldownCap, rateLimitBackoff(5000), "no overflow, however deep")
}

func TestKeyPoolLadderGrowsWhileCooling(t *testing.T) {
        now := time.Now()
        p := NewKeyPool([]string{"a", "b"})

        // Two consecutive failures on a, the second landing at `now`: doubled
        // cooldown (120s from now), not base (60s).
        p.ReportRateLimited(0, 0, now.Add(-10*time.Second))
        p.ReportRateLimited(0, 0, now)

        // Fail b at `now` with a cooldown BETWEEN base and doubled (90s), and
        // look at now+75s: if a's cooldown is base it has expired and the walk
        // lands on a; if doubled, b's 90s is the earlier expiry instead.
        p.ReportRateLimited(1, 90*time.Second, now)
        _, idx := p.Acquire(now.Add(75 * time.Second))
        require.Equal(t, 1, idx, "a sits out the doubled ladder: b's 90s is the earlier expiry")
}

func TestKeyPoolAuthQuarantine(t *testing.T) {
        now := time.Now()
        p := NewKeyPool([]string{"a", "b"})

        p.ReportAuthFailure(0, now)

        _, idx := p.Acquire(now.Add(time.Minute))
        require.Equal(t, 1, idx, "auth failure rotates away")

        // b is sticky for as long as it works — a's quarantine expiring does
        // not by itself pull the pool back.
        _, idx = p.Acquire(now.Add(AuthFailureCooldown + time.Minute))
        require.Equal(t, 1, idx)

        // b failing after the quarantine lifts rotates back onto a.
        p.ReportRateLimited(1, 0, now.Add(20*time.Minute))
        _, idx = p.Acquire(now.Add(20*time.Minute + time.Second))
        require.Equal(t, 0, idx)
}

func TestKeyPoolAllCoolingFailsOpenToEarliest(t *testing.T) {
        now := time.Now()
        p := NewKeyPool([]string{"a", "b", "c"})

        // Everything cooling: a for 10m (current), b for ~5m, c for ~1m.
        p.ReportRateLimited(0, 10*time.Minute, now)
        p.ReportRateLimited(1, 5*time.Minute, now.Add(time.Second))
        p.ReportRateLimited(2, time.Minute, now.Add(2*time.Second))

        key, idx := p.Acquire(now.Add(3 * time.Second))
        require.Equal(t, "c", key)
        require.Equal(t, 2, idx, "fail-open: the earliest-expiring key is the likeliest try")
}

func TestKeyPoolFailOpenIncludesCurrentKey(t *testing.T) {
        now := time.Now()
        p := NewKeyPool([]string{"a", "b"})

        // Current key a cooling 60s; b cooling much longer. The fail-open walk
        // must consider the CURRENT key too — a is the earliest expiry, so a
        // is the likeliest try even though the walk starts past it.
        p.ReportRateLimited(0, 0, now)                  // a: 60s, rotates to b
        p.ReportRateLimited(1, 0, now.Add(time.Second)) // b: 60s ladder from +1s

        // Rotate current back onto a by failing b again while a still cools...
        // simpler: both cool, a expires first by a second.
        p.ReportRateLimited(1, 5*time.Minute, now.Add(2*time.Second)) // b: +5m
        // Current after b's reports rotates to a (only other key).
        _, idx := p.Acquire(now.Add(3 * time.Second))
        require.Equal(t, 0, idx, "the current key's own earlier cooldown wins the fail-open")
}

func TestKeyPoolReportSuccessResetsAndReElects(t *testing.T) {
        now := time.Now()
        p := NewKeyPool([]string{"a", "b"})

        p.ReportRateLimited(0, 0, now)
        p.ReportRateLimited(0, 0, now) // two consecutive: doubled cooldown on a
        _, idx := p.Acquire(now.Add(time.Second))
        require.Equal(t, 1, idx, "rotated off the doubled-failure key")

        // A success on the rotated-to key makes it current and clears its state.
        p.ReportSuccess(1)
        _, idx = p.Acquire(now.Add(2 * time.Second))
        require.Equal(t, 1, idx, "the proven-good key is the best default")

        // A success on a clears its ladder and re-elects it.
        p.ReportSuccess(0)
        _, idx = p.Acquire(now.Add(3 * time.Second))
        require.Equal(t, 0, idx, "a re-elected after its own success")

        // The reset shows in the next failure's cooldown: base (60s), not the
        // doubled ladder the two earlier consecutive reports had earned. Fail
        // b with a between-cooldowns (100s) and look at +90s: base-cooled a
        // has expired and wins the walk; doubled would still hold it out.
        t1 := now.Add(time.Minute)
        p.ReportRateLimited(0, 0, t1) // first failure again
        p.ReportRateLimited(1, 100*time.Second, t1.Add(time.Second))
        _, idx = p.Acquire(t1.Add(90 * time.Second))
        require.Equal(t, 0, idx, "ladder reset by success: a cools for the base 60s, earlier than b's 100s")
}

func TestKeyPoolConcurrentAccess(t *testing.T) {
        p := NewKeyPool([]string{"a", "b", "c"})
        now := time.Now()
        done := make(chan struct{})
        for g := 0; g < 8; g++ {
                go func(g int) {
                        defer func() { done <- struct{}{} }()
                        for i := 0; i < 200; i++ {
                                t := now.Add(time.Duration(i) * time.Millisecond)
                                _, idx := p.Acquire(t)
                                switch (g + i) % 3 {
                                case 0:
                                        p.ReportRateLimited(idx, 0, t)
                                case 1:
                                        p.ReportAuthFailure(idx, t)
                                default:
                                        p.ReportSuccess(idx)
                                }
                        }
                }(g)
        }
        for g := 0; g < 8; g++ {
                <-done
        }
}

func TestClassifyKeyFailure(t *testing.T) {
        tests := []struct {
                name       string
                err        error
                kind       KeyFailure
                retryAfter time.Duration
        }{
                {"rate limit", &APIError{Status: 429, RetryAfter: 30 * time.Second}, KeyFailureRateLimit, 30 * time.Second},
                {"rate limit no hint", &APIError{Status: 429}, KeyFailureRateLimit, 0},
                {"unauthorized", &APIError{Status: 401}, KeyFailureAuth, 0},
                {"forbidden", &APIError{Status: 403}, KeyFailureAuth, 0},
                {"server error", &APIError{Status: 500}, KeyFailureNone, 0},
                {"overflow", &APIError{Status: 413}, KeyFailureNone, 0},
                {"wrapped rate limit", fmt.Errorf("stream: %w", &APIError{Status: 429, RetryAfter: time.Minute}), KeyFailureRateLimit, time.Minute},
                {"plain error", fmt.Errorf("connection refused"), KeyFailureNone, 0},
                {"nil", nil, KeyFailureNone, 0},
        }
        for _, tc := range tests {
                t.Run(tc.name, func(t *testing.T) {
                        kind, retryAfter := ClassifyKeyFailure(tc.err)
                        require.Equal(t, tc.kind, kind)
                        require.Equal(t, tc.retryAfter, retryAfter)
                })
        }
}

func TestKeyPoolIndexesOutOfRangeAreIgnored(t *testing.T) {
        now := time.Now()
        p := NewKeyPool([]string{"a", "b"})
        p.ReportRateLimited(-1, 0, now)
        p.ReportRateLimited(7, 0, now)
        p.ReportAuthFailure(-3, now)
        p.ReportSuccess(99)
        _, idx := p.Acquire(now)
        require.Equal(t, 0, idx, "bogus reports change nothing")
}
