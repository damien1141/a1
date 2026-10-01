package compaction

// Settings configures compaction: whether it is enabled and the token
// thresholds used to decide when to compact.
type Settings struct {
	enabled          bool
	reverseTokens    int
	keepRecentTokens int
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

var defaultSettings = Settings{
	enabled:          true,
	reverseTokens:    16384,
	keepRecentTokens: 20000,
	// Growth gate defaults target consumed increments: the nudge fires at
	// ~45% of the window (matching the paper's floor fraction) and requires
	// at least 50K of growth since the last compression.
	GrowthThreshold: 50000,
	FloorFraction:  0.45,
}

// DefaultSettings returns the default compaction settings for use by callers outside this package.
func DefaultSettings() Settings {
	return defaultSettings
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
