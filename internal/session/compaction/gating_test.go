package compaction

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShouldCompactGated implements the billion-context growth gate (§3.4):
// a nudge fires only when context exceeds the floor fraction AND has grown by
// at least GrowthThreshold since the last compaction.
func TestShouldCompactGated(t *testing.T) {
	settings := Settings{
		enabled:          true,
		reverseTokens:    16384,
		keepRecentTokens: 20000,
		GrowthThreshold:  50000,
		FloorFraction:    0.45,
	}
	window := 100_000
	floor := int(0.45 * float64(window)) // 45000

	// Below the floor fraction: no compaction regardless of growth.
	assert.False(t, ShouldCompactGated(40_000, window, 0, settings), "below floor")
	assert.False(t, ShouldCompactGated(floor, window, 0, settings), "at floor (not above)")

	// Above the floor but growth below the threshold: no compaction.
	assert.False(t, ShouldCompactGated(50_000, window, 10_000, settings), "above floor, growth 40K < 50K")

	// Above the floor and growth meets the threshold: compaction.
	assert.True(t, ShouldCompactGated(60_000, window, 0, settings), "above floor, growth 60K >= 50K")
	assert.True(t, ShouldCompactGated(60_000, window, 9_999, settings), "growth exactly at threshold")
	assert.False(t, ShouldCompactGated(60_000, window, 10_001, settings), "growth 50K-1 below threshold")

	// Disabled: never compact.
	disabled := settings
	disabled.enabled = false
	assert.False(t, ShouldCompactGated(60_000, window, 0, disabled), "disabled")

	// Zero window: never compact.
	assert.False(t, ShouldCompactGated(60_000, 0, 0, settings), "zero window")
}

// TestShouldCompactGatedPostCompactionBaseline confirms the growth gate uses
// the post-compaction estimate as its baseline, so a compaction that drops the
// context from 60K to ~26K does not immediately re-trigger on the next turn.
func TestShouldCompactGatedPostCompactionBaseline(t *testing.T) {
	settings := Settings{
		enabled:          true,
		reverseTokens:    16384,
		keepRecentTokens: 20000,
		GrowthThreshold:  50000,
		FloorFraction:    0.45,
	}
	window := 100_000
	// After a compaction the context sits at keepRecentTokens + summary cap.
	post := PostCompactionEstimate(settings)
	require.Greater(t, post, 0)

	// Context grows from the post-compaction estimate by a small amount:
	// below the growth threshold, so no re-compaction.
	assert.False(t, ShouldCompactGated(post+10_000, window, post, settings), "small growth after compaction")

	// Context grows well past the threshold: re-compaction fires.
	assert.True(t, ShouldCompactGated(post+60_000, window, post, settings), "large growth after compaction")
}

// TestPostCompactionEstimate returns the expected post-compaction context
// size: the kept recent zone plus the summary cap.
func TestPostCompactionEstimate(t *testing.T) {
	settings := Settings{
		reverseTokens:    16384,
		keepRecentTokens: 20000,
	}
	// summary cap = 0.8 * 16384 = 13107; total = 20000 + 13107 = 33107.
	assert.Equal(t, 33107, PostCompactionEstimate(settings))
}

// TestGrowthGateIsThePrimaryTrigger confirms the growth gate is the primary
// trigger in the band between the floor fraction and the safety valve. Below
// the floor fraction the gate never fires even when growth is large; above
// the floor it fires once growth meets the threshold.
func TestGrowthGateIsThePrimaryTrigger(t *testing.T) {
	settings := Settings{
		enabled:          true,
		reverseTokens:    16384,
		keepRecentTokens: 20000,
		GrowthThreshold:  50000,
		FloorFraction:    0.45,
	}
	window := 100_000

	// Below the floor: the gate never fires, even with large growth.
	assert.False(t, ShouldCompactGated(44_000, window, 0, settings), "below floor, large growth")

	// Above the floor with enough growth: the gate fires.
	assert.True(t, ShouldCompactGated(55_000, window, 0, settings), "above floor, growth >= threshold")

	// The safety valve (ShouldCompact) only fires at 100K - 16.4K = 83.6K,
	// so in the 45K-83K band the growth gate is the only trigger.
	assert.False(t, ShouldCompact(55_000, window, settings), "safety valve does not fire at 55K")
}