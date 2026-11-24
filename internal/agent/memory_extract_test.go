package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/damien1141/a1/internal/project"
)

// The memory write path is config-switchable, and the default must be the
// extractor. These pins exist because the switch decides whether a user's
// memories flow through the decision-model chunk extractor or the old
// consolidation completion — a silent default flip would change what gets
// remembered without anyone noticing.
func TestMemoryConfig_ExtractorIsDefault_Positive(t *testing.T) {
	cases := map[string]project.MemoryConfig{
		"unset":            {},
		"explicit":         {Extractor: "extractor"},
		"whitespace":       {Extractor: "  "},
		"case insensitive": {Extractor: "EXTRACTOR"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			assert.True(t, cfg.ExtractorEnabled(),
				"the extractor is the default write path")
		})
	}
}

// TestMemoryConfig_EscapeHatch_Negative: the only way back to the previous
// behavior is an explicit "consolidation", and near-misses must not match.
func TestMemoryConfig_EscapeHatch_Negative(t *testing.T) {
	assert.False(t, project.MemoryConfig{Extractor: "consolidation"}.ExtractorEnabled())
	assert.False(t, project.MemoryConfig{Extractor: " Consolidation "}.ExtractorEnabled(),
		"trimmed and case-folded, so a hand-edited value still works")

	// A typo is NOT a silent switch to the old path: unknown values keep the
	// extractor, which is the documented default.
	for _, typo := range []string{"consolidate", "old", "llm", "off"} {
		assert.True(t, project.MemoryConfig{Extractor: typo}.ExtractorEnabled(),
			"unknown value %q keeps the default extractor", typo)
	}
}

// TestMaintenanceCadence_IsSlowerThanExtraction_Differential pins the two
// cadences' relationship, since the design claim is "extraction runs often,
// maintenance runs rarely". If maintenance ever fires more often than
// extraction, the expensive LLM completion has quietly become the hot path
// again.
func TestMaintenanceCadence_IsSlowerThanExtraction_Differential(t *testing.T) {
	assert.Equal(t, 1, extractionIntervalTurns)
	assert.Greater(t, maintenanceIntervalTurns, extractionIntervalTurns)
	assert.GreaterOrEqual(t, maintenanceIntervalTurns, 5,
		"maintenance runs an LLM completion and must stay infrequent")

	// The cadence ANCHORS at turn 1 and then follows the engine's own interval
	// (which already encodes local-endpoint batching). Two properties matter and
	// they pull in opposite directions:
	//
	//   - a short session must still remember something, so turn 1 fires
	//   - a warm local prompt cache must not be evicted every turn, so the
	//     request count stays at one per interval
	//
	// Anchoring satisfies both: intervals 10 over 10 turns is ONE request, just
	// starting at turn 1 instead of turn 10.
	assert.True(t, maintenanceRunsOn(1, 10), "the first turn always maintains")
	assert.False(t, maintenanceRunsOn(2, 10))
	assert.True(t, maintenanceRunsOn(11, 10), "one interval after the anchor")

	fires := 0
	for tick := int64(1); tick <= 10; tick++ {
		if maintenanceRunsOn(tick, 10) {
			fires++
		}
	}
	assert.Equal(t, 1, fires,
		"10 turns on a batched cadence must cost exactly ONE consolidation request")

	// A per-turn cadence (remote endpoint, or a pinned interval of 1) maintains
	// on every completed turn.
	assert.True(t, maintenanceRunsOn(1, 1))
	assert.True(t, maintenanceRunsOn(7, 1))
}

// TestExtractionSkipsTheMaintenanceTurn_Negative pins the collision guard.
//
// On a maintenance turn, consolidation has already been handed that turn and
// will store its own statements. Letting extraction run on the same turn would
// ask the decision model about the same text and write a second,
// differently-worded copy — and the store's 0.90 cosine merge cannot be relied
// on to catch it, because the two paths phrase things differently by design
// (one summarizes, one quotes verbatim).
//
// The rule is structural: maintenance wins the turn it fires on, extraction
// resumes the turn after.
func TestExtractionSkipsTheMaintenanceTurn_Negative(t *testing.T) {
	// Turn 1 always maintains, so extraction must be silent there even though
	// its own interval is 1 (every turn).
	assert.True(t, maintenanceRunsOn(1, 10),
		"turn 1 maintains")
	assert.Equal(t, 1, extractionIntervalTurns,
		"extraction wants every turn")
	// The guard is that extractTurn returns after maintenance. This test pins
	// the premise the guard reads; the turn-loop behaviour is covered by
	// memory_loop_test.go's request counts.
}
