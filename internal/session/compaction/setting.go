package compaction

// Settings configures compaction: whether it is enabled and the token
// thresholds used to decide when to compact.
type Settings struct {
        enabled          bool
        reverseTokens    int
        keepRecentTokens int
        // maxKeptTokens caps the kept zone the cut logic may leave behind.
        // The post-compaction context is the summary (capped at
        // historySummaryRatio × reverseTokens) plus the kept zone; when one
        // huge tail entry (a giant tool result, a pasted file) inflates the
        // kept zone past window − 1.8×T, a compaction that runs "successfully"
        // still leaves a context that overflows on the next stream — the
        // "auto compaction sometimes doesn't work" death spiral on small
        // windows. The cut then moves to older boundaries, summarizing more,
        // until the kept zone fits (INV5 below).
        maxKeptTokens int
        // GrowthThreshold is the minimum growth in context tokens since the last
        // compaction before a new compaction is worth running. Below this the
        // overshoot is noise around the threshold, not a consumed increment
        // (billion-context §3.4 "growth-gated nudge": compress only consumed
        // increments, median fold ~2.6K, rather than active context).
        GrowthThreshold int
        // FloorFraction is the fraction of the context window at which the nudge
        // fires. Compaction runs only when context exceeds this fraction AND has
        // grown by at least GrowthThreshold since the last compaction.
        FloorFraction float64
}

// Default fractions for window-aware settings (SettingsFor). Each scales the
// corresponding absolute setting as a fraction of the context window, with a
// floor (the *_min constant) so very small windows still get meaningful
// values. The fractions and minimums are derived from the original fixed
// defaults (K=20000, T=16384, G=50000) normalized to a 128K window, then
// relaxed so the invariants hold across the realistic window range
// (16K–1M). See SettingsFor for the invariants and the math.
const (
        // keepRecentFraction: kept-recent zone as a fraction of the window.
        // 0.15 ≈ 20K of 128K (the original default).
        keepRecentFraction = 0.15
        // keepRecentMin: even on a tiny window, keep at least this much recent
        // context so the agent retains the last exchange.
        keepRecentMin = 2000

        // reverseFraction: headroom from the window edge that the safety valve
        // reserves. 0.125 ≈ 16K of 128K (the original default).
        reverseFraction = 0.125
        // reverseMin: even on a tiny window, keep at least this much headroom
        // so the safety valve can fire before the window fills.
        reverseMin = 2048

        // growthFraction: growth threshold as a fraction of the window. The
        // original fixed 50K was 39% of 128K; we relax to 30% so the growth
        // gate fires before the safety valve on small windows (where 39%
        // would push floor+G past W-T).
        growthFraction = 0.30
        // growthMin: even on a tiny window, require at least this much growth
        // since the last compaction so a single short turn doesn't re-compact
        // immediately.
        growthMin = 5000
)

var defaultSettings = Settings{
        enabled:          true,
        reverseTokens:    16384,
        keepRecentTokens: 20000,
        // No window to derive from (DefaultSettings / SettingsFor(0)): a cap
        // of 4× the kept zone matches the 128K-window world the fixed
        // defaults were tuned for. Window-aware callers get the real
        // invariant from SettingsFor.
        maxKeptTokens: 80000,
        // Growth gate defaults target consumed increments: the nudge fires at
        // ~45% of the window (matching the paper's floor fraction) and requires
        // at least 50K of growth since the last compression.
        GrowthThreshold: 50000,
        FloorFraction:   0.45,
}

// DefaultSettings returns the fixed-default compaction settings used by
// tests that don't care about the context window. Production callers should
// use SettingsFor(window) so the settings scale with the model's actual
// context window — the fixed defaults here break the post-compaction
// invariant for windows below ~96K (post = 33107 > floor = 0.45*W for
// W < 73571).
func DefaultSettings() Settings {
        return defaultSettings
}

// SettingsFor returns window-aware compaction settings. The fixed defaults
// in DefaultSettings break for small windows: with K=20000, T=16384, G=50000,
// on a 64K window the post-compaction estimate (K + T*r = 33107) is already
// above the floor (0.45*64000 = 28800), so the growth gate can never usefully
// fire again (it would need context >= 33107 + 50000 = 83107, but the window
// is 64K), and the safety valve takes over — firing compaction every ~14K
// of growth. On a 16K window, K=20000 exceeds the window outright.
//
// SettingsFor scales K, T, and G as fractions of the window with minimums,
// so the following invariants hold across the realistic window range:
//
//      INV1: post < floor — K + T*r < W*f
//            Post-compaction the context is below the floor, so the growth
//            gate has headroom before it fires again.
//      INV2: floor + G <= W - T — growth-gate trigger at or below safety valve
//            The growth gate fires before the safety valve, so compaction is
//            triggered by the (cheaper, predictable) growth gate, not the
//            (reactive, last-resort) safety valve.
//      INV3: K + T*r < W — kept zone + summary fit in the window
//            The compaction output actually fits, so compacting doesn't
//            immediately re-overflow.
//      INV4: K, T, G > 0
//
// The invariants hold for W >= ~12.8K (the threshold where the mins stop
// dominating). Below that, the math degrades gracefully — compaction still
// runs, but the safety valve may fire more often than the growth gate.
// Real models start at 16K, so this is the realistic floor.
//
//      INV5: kept zone ≤ W − 1.8×T — maxKeptTokens + summary cap stay
//            below the safety-valve threshold, so a completed compaction
//            leaves a context that fits with headroom. Clamped up to K on
//            degenerate tiny windows (a cap below the kept zone can never
//            move a cut anyway; the overflow-recovery halving takes over).
//
// The fractions: K=15% of W, T=12.5% of W, G=30% of W, f=45% of W.
// Mins: K_min=2000, T_min=2048, G_min=5000. historySummaryRatio (r) stays
// at 0.8 — it's a property of the summarizer, not the window.
func SettingsFor(window int) Settings {
        if window <= 0 {
                return defaultSettings
        }
        k := int(float64(window) * keepRecentFraction)
        if k < keepRecentMin {
                k = keepRecentMin
        }
        t := int(float64(window) * reverseFraction)
        if t < reverseMin {
                t = reverseMin
        }
        g := int(float64(window) * growthFraction)
        if g < growthMin {
                g = growthMin
        }
        maxKept := window - int(1.8*float64(t))
        if maxKept < k {
                maxKept = k
        }
        return Settings{
                enabled:          true,
                keepRecentTokens: k,
                reverseTokens:    t,
                maxKeptTokens:    maxKept,
                GrowthThreshold:  g,
                FloorFraction:    defaultSettings.FloorFraction,
        }
}

// ShouldCompact reports whether contextTokens warrants compaction given
// contextWindow and settings. This is the safety-valve threshold: it fires
// when context is within reverseTokens of the window, regardless of growth.
// Use ShouldCompactGated for the primary growth-gated trigger.
func ShouldCompact(contextTokens, contextWindow int, settings Settings) bool {
        if !settings.enabled || contextWindow <= 0 {
                return false
        }

        // Keep `reverseTokens` headroom from the context window. When current usage
        // exceeds (contextWindow - reverseTokens), we should compact.
        threshold := max(contextWindow-settings.reverseTokens, 0)
        return contextTokens > threshold
}

// ShouldCompactGated implements the billion-context growth gate (§3.4): a
// nudge fires only when context exceeds floorFraction of the window AND has
// grown by at least GrowthThreshold since the last compaction. This targets
// consumed increments rather than active context, so short overshoots above
// the threshold do not trigger a (costly) summarization call.
//
// lastCompactTokens is the estimated context size right after the last
// compaction (see PostCompactionEstimate). Pass 0 when no compaction has
// run yet, which makes the growth check pass trivially so the first
// compaction fires at the floor fraction.
//
// The safety valve (ShouldCompact) still fires when context approaches the
// window, so the growth gate never lets the context exceed the window.
func ShouldCompactGated(contextTokens, contextWindow, lastCompactTokens int, settings Settings) bool {
        if !settings.enabled || contextWindow <= 0 {
                return false
        }
        floor := int(float64(contextWindow) * settings.FloorFraction)
        if contextTokens <= floor {
                return false
        }
        growth := contextTokens - lastCompactTokens
        if growth < settings.GrowthThreshold {
                return false
        }
        return true
}

// PostCompactionEstimate returns the expected context size immediately after a
// compaction: the kept recent zone plus the summary, capped at the same budget
// the summarizer uses. Callers track this as the baseline for the next growth
// gate so the gate measures post-compaction growth rather than pre-compaction
// size (which would never trigger: the context drops sharply on compaction).
func PostCompactionEstimate(settings Settings) int {
        summaryCap := int(float64(settings.reverseTokens) * historySummaryRatio)
        return settings.keepRecentTokens + summaryCap
}

// ReverseTokens returns the safety-valve headroom (settings.reverseTokens).
// Exposed so callers can compute derived values (e.g. the summary cap) from
// the same Settings struct without re-deriving the fraction.
func (s Settings) ReverseTokens() int { return s.reverseTokens }

// KeepRecentTokens returns the kept-recent zone size (settings.keepRecentTokens).
func (s Settings) KeepRecentTokens() int { return s.keepRecentTokens }

// MaxKeptTokens returns the kept-zone ceiling (settings.maxKeptTokens).
// Exposed for the cut logic's ceiling enforcement and its tests.
func (s Settings) MaxKeptTokens() int { return s.maxKeptTokens }
