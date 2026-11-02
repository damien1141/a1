package compaction

import (
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/session"
)

// TestSettingsForMaxKeptInvariant pins INV5 across the realistic window
// range: the kept-zone ceiling plus the summary cap must stay at or below
// the safety-valve threshold (window − T), so a completed compaction
// leaves a context that fits with headroom. The maxKept ≥ K clamp keeps
// degenerate tiny windows from producing an unreachable ceiling.
func TestSettingsForMaxKeptInvariant(t *testing.T) {
        for _, window := range []int{16_384, 32_768, 65_536, 128_000, 200_000, 1_000_000} {
                s := SettingsFor(window)
                k, tt := s.KeepRecentTokens(), s.ReverseTokens()
                maxKept := s.MaxKeptTokens()
                assert.Greater(t, maxKept, 0, "window %d", window)
                assert.GreaterOrEqual(t, maxKept, k, "window %d: ceiling below the kept zone is unreachable", window)
                summaryCap := int(float64(tt) * historySummaryRatio)
                assert.LessOrEqual(t, maxKept+summaryCap, window-tt,
                        "window %d: kept zone + summary must fit below the safety-valve threshold", window)
        }
}

// TestSettingsForMaxKeptDegenerateWindows verifies the clamp on windows
// where the mins dominate: the ceiling never drops below the kept zone.
func TestSettingsForMaxKeptDegenerateWindows(t *testing.T) {
        for _, window := range []int{4096, 8192} {
                s := SettingsFor(window)
                assert.GreaterOrEqual(t, s.MaxKeptTokens(), s.KeepRecentTokens(),
                        "window %d: clamped ceiling must stay >= keepRecent", window)
        }
}

// TestFindCutPoint_CapsKeptZoneOnGiantKeptMessage is the 64K-death-spiral
// regression: a giant message inside the kept window used to survive every
// compaction (the kept zone carried it verbatim), so the post-compaction
// context still overflowed. The ceiling must move the cut NEWER — dropping
// the giant into the summarized zone — until the kept zone fits.
func TestFindCutPoint_CapsKeptZoneOnGiantKeptMessage(t *testing.T) {
        // Geometry: the budget walk (keepRecent 3300) accumulates e4+e3+e2+e1
        // = 3300 and reaches the budget at the giant e1, so the cut lands at e1
        // and the giant rides INSIDE the kept zone [e1, e4] = 3300 tokens.
        entries := []session.MessageEntry{
                msgEntry("e0", llm.RoleUser, 100, 0),
                msgEntry("e1", llm.RoleAssistant, 3000, 0), // the giant paste
                msgEntry("e2", llm.RoleUser, 100, 0),
                msgEntry("e3", llm.RoleAssistant, 100, 0),
                msgEntry("e4", llm.RoleUser, 100, 0),
        }

        t.Run("giant inside the kept zone moves the cut newer", func(t *testing.T) {
                s := Settings{keepRecentTokens: 3300, maxKeptTokens: 500}
                cut := findCutPoint(entries, 0, len(entries), s)
                // kept(1)=3300 > 500 -> newer; kept(2)=300 <= 500 -> stops at e2.
                assert.Equal(t, 2, cut.firstKeptEntryIndex,
                        "the ceiling must drop the giant into the summarized zone by cutting at e2")
        })

        t.Run("under ceiling the budget cut stands", func(t *testing.T) {
                s := Settings{keepRecentTokens: 3300, maxKeptTokens: 4000}
                cut := findCutPoint(entries, 0, len(entries), s)
                assert.Equal(t, 1, cut.firstKeptEntryIndex,
                        "kept zone of 3300 is under the 4000 ceiling: the budget cut must stand")
        })

        t.Run("ceiling reachable only past the newest boundary stops there", func(t *testing.T) {
                s := Settings{keepRecentTokens: 3300, maxKeptTokens: 90}
                cut := findCutPoint(entries, 0, len(entries), s)
                // kept(4)=100 > 90 but e4 is the newest cut point: the floor.
                assert.Equal(t, 4, cut.firstKeptEntryIndex)
        })

        t.Run("zero ceiling disables the cap", func(t *testing.T) {
                s := Settings{keepRecentTokens: 3300, maxKeptTokens: 0}
                cut := findCutPoint(entries, 0, len(entries), s)
                assert.Equal(t, 1, cut.firstKeptEntryIndex)
        })
}

// TestFindCutPoint_HugeTailAfterEveryCutPoint pins the documented floor:
// when the huge entry sits at the tail (after every cut point), no cut can
// exclude it from the kept zone — the cut stands and the overflow-recovery
// window halving takes over.
func TestFindCutPoint_HugeTailAfterEveryCutPoint(t *testing.T) {
        entries := []session.MessageEntry{
                msgEntry("e0", llm.RoleUser, 100, 0),
                msgEntry("e1", llm.RoleAssistant, 100, 0),
                msgEntry("e2", llm.RoleUser, 100, 0),
                msgEntry("e3", llm.RoleAssistant, 100, 0),
                msgEntry("e4", llm.RoleTool, 5000, 0), // after every cut point
        }
        // keepRecent 100: the budget is reached inside e4; no cut point at or
        // after it, so the fallback picks the newest boundary e3.
        s := Settings{keepRecentTokens: 100, maxKeptTokens: 1000}
        cut := findCutPoint(entries, 0, len(entries), s)
        assert.Equal(t, 3, cut.firstKeptEntryIndex,
                "a tail after every cut point is unreachable by cut placement: the cut stands")
}

// TestSerializeConversationCapped pins the summarizer-input budget: below
// the cap the output is byte-identical to SerializeConversation; over it
// the OLDEST parts drop with a marker, and the newest survives.
func TestSerializeConversationCapped(t *testing.T) {
        msgs := []llm.Message{
                {Role: llm.RoleUser, Content: strings.Repeat("A", 100)},
                {Role: llm.RoleAssistant, Content: strings.Repeat("B", 100)},
                {Role: llm.RoleUser, Content: strings.Repeat("C", 100)},
        }
        full := SerializeConversation(msgs)

        t.Run("uncapped on zero budget", func(t *testing.T) {
                assert.Equal(t, full, SerializeConversationCapped(msgs, 0))
        })
        t.Run("uncapped when it fits", func(t *testing.T) {
                assert.Equal(t, full, SerializeConversationCapped(msgs, len(full)+1000))
        })
        t.Run("over budget drops the oldest with a marker", func(t *testing.T) {
                // Budget for the newest part + join only.
                budget := len("[User]: ") + 100 + len(summaryOmissionMarker) + 2 + 2
                got := SerializeConversationCapped(msgs, budget)
                assert.LessOrEqual(t, len(got), budget)
                assert.Contains(t, got, summaryOmissionMarker)
                assert.Contains(t, got, strings.Repeat("C", 100), "the newest part must survive")
                assert.NotContains(t, got, strings.Repeat("A", 100), "the oldest part must drop")
                assert.NotContains(t, got, strings.Repeat("B", 100), "the middle part must drop when the budget only fits one")
        })
        t.Run("sub-part budget keeps the newest tail", func(t *testing.T) {
                got := SerializeConversationCapped(msgs, len(summaryOmissionMarker)+2+50)
                assert.Contains(t, got, summaryOmissionMarker)
                assert.LessOrEqual(t, len(got), len(summaryOmissionMarker)+2+50)
        })
        t.Run("empty input", func(t *testing.T) {
                assert.Equal(t, "", SerializeConversationCapped(nil, 10))
        })
}

// TestSummarizerInputChars pins the budget math: window minus the summary's
// own output cap minus a 1/8 overhead allowance, converted to chars.
func TestSummarizerInputChars(t *testing.T) {
        assert.Equal(t, 0, summarizerInputChars(0, 1000), "unknown window: uncapped")
        assert.Equal(t, 0, summarizerInputChars(64_000, 0), "no output cap to budget around: uncapped")
        // 64000 - 6400 - 8000 = 49600 tokens -> 198400 chars.
        assert.Equal(t, 198_400, summarizerInputChars(64_000, 6400))
        // Tiny windows floor at 1024 tokens -> 4096 chars.
        assert.Equal(t, 4096, summarizerInputChars(2048, 1024))
}
