package compaction

import (
        "fmt"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/session"
)

// TestEstimateContextTokens_SumsAllMessages is the regression test for #12:
// the growth gate (ShouldCompactGated) used to receive provider-reported
// ContextTokens() (real tokens) while lastCompactEstimate is bytes/4 —
// mixing units in the growth calculation. EstimateContextTokens gives the
// gate a bytes/4 estimate that matches the cut ruler and lastCompactEstimate.
func TestEstimateContextTokens_SumsAllMessages(t *testing.T) {
        entries := []session.MessageEntry{
                session.SessionMessageEntry{
                        SessionBaseEntry: session.SessionBaseEntry{ID: "m1"},
                        Message:          llm.Message{Role: llm.RoleUser, Content: sizedContent(10)},
                },
                session.SessionMessageEntry{
                        SessionBaseEntry: session.SessionBaseEntry{ID: "m2"},
                        Message:          llm.Message{Role: llm.RoleAssistant, Content: sizedContent(20)},
                },
                session.SessionMessageEntry{
                        SessionBaseEntry: session.SessionBaseEntry{ID: "m3"},
                        Message:          llm.Message{Role: llm.RoleUser, Content: sizedContent(5)},
                },
        }

        got := EstimateContextTokens(entries)
        assert.Equal(t, 35, got, "10+20+5 = 35 estimated tokens")
}

// TestEstimateContextTokens_SkipsNonMessageEntries verifies that
// compaction entries and other non-message entries are not counted.
func TestEstimateContextTokens_SkipsNonMessageEntries(t *testing.T) {
        entries := []session.MessageEntry{
                session.SessionMessageEntry{
                        SessionBaseEntry: session.SessionBaseEntry{ID: "m1"},
                        Message:          llm.Message{Role: llm.RoleUser, Content: sizedContent(10)},
                },
                session.CompactionEntry{
                        SessionBaseEntry: session.SessionBaseEntry{ID: "c1"},
                        Compaction:       session.Compaction{Summary: "old summary"},
                },
                session.SessionMessageEntry{
                        SessionBaseEntry: session.SessionBaseEntry{ID: "m2"},
                        Message:          llm.Message{Role: llm.RoleAssistant, Content: sizedContent(5)},
                },
        }

        got := EstimateContextTokens(entries)
        assert.Equal(t, 15, got, "only message entries are counted, not compaction entries")
}

// TestShouldCompactGated_ConsistentRulerWithLastCompactEstimate verifies the
// end-to-end consistency: after a compaction, lastCompactTokens is set to
// PostCompactionEstimate (bytes/4). The growth gate should fire when
// EstimateContextTokens exceeds that by GrowthThreshold. With the old code
// (provider tokens), the growth check was broken because real tokens and
// bytes/4 are different scales.
func TestShouldCompactGated_ConsistentRulerWithLastCompactEstimate(t *testing.T) {
        settings := DefaultSettings()
        // floor = contextWindow * FloorFraction = 200_000 * 0.45 = 90_000
        // GrowthThreshold = 50_000

        // Use a large lastCompact so the "above floor but below growth" case
        // is reachable (with the default lastCompact=33107, any value above
        // the floor automatically satisfies the growth threshold).
        lastCompact := 60_000

        // Above floor AND growth >= GrowthThreshold — should fire.
        current := 120_000
        assert.True(t, ShouldCompactGated(current, 200_000, lastCompact, settings),
                "above floor and growth (%d >= %d) fires the gate",
                current-lastCompact, settings.GrowthThreshold)

        // Above floor but growth below threshold — should not fire.
        current = 95_000
        assert.False(t, ShouldCompactGated(current, 200_000, lastCompact, settings),
                "above floor but growth (%d < %d) does not fire",
                current-lastCompact, settings.GrowthThreshold)

        // Below floor — should not fire even with large growth.
        assert.False(t, ShouldCompactGated(100, 200_000, 0, settings),
                "below floor does not fire")
}

func TestEstimateMessageTokens(t *testing.T) {
        tests := []struct {
                name string
                msg  llm.Message
                want int
        }{
                {"empty message", llm.Message{}, 0},
                {"rounds up to 4 chars per token", llm.Message{Role: llm.RoleUser, Content: "abcde"}, 2},
                {"tool result content counts", llm.Message{Role: llm.RoleTool, Content: sizedContent(50)}, 50},
                {"reasoning counts", llm.Message{Role: llm.RoleAssistant, ReasoningContent: strings.Repeat("t", 400)}, 100},
                {
                        "tool call name and arguments count",
                        llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
                                {Function: llm.Function{Name: "read", Arguments: `{"path":"a.go"}`}},
                        }},
                        5, // 4 + 16 chars
                },
                {
                        "images use a fixed stand-in",
                        llm.Message{Role: llm.RoleUser, Images: []llm.Image{{Data: "x", MimeType: "image/png"}}},
                        estimatedImageChars / 4,
                },
        }
        for _, tt := range tests {
                t.Run(tt.name, func(t *testing.T) {
                        assert.Equal(t, tt.want, estimateMessageTokens(tt.msg))
                })
        }
}

func TestFindCutIndex_ExceedsTokensChoosesNearestCutPoint(t *testing.T) {
        entries := []session.MessageEntry{
                msgEntry("e1", llm.RoleUser, 10, 0),
                msgEntry("e2", llm.RoleUser, 10, 0),
                session.CompactionEntry{}, // non-message, contributes no size
                msgEntry("e3", llm.RoleUser, 10, 0),
                msgEntry("e4", llm.RoleUser, 10, 0),
        }
        startIndex := 0
        endIndex := len(entries)
        keepRecentTokens := 15
        cutPoints := []int{1, 3, 4}

        cutIndex := findCutIndex(entries, startIndex, endIndex, keepRecentTokens, cutPoints)

        // The last two messages cost 20 >= 15, so the budget is reached at e3.
        assert.Equal(t, 3, cutIndex)
}

func TestFindCutIndex_NotExceedTokensReturnsFirstCutPoint(t *testing.T) {
        entries := []session.MessageEntry{
                msgEntry("e1", llm.RoleUser, 10, 0),
                msgEntry("e2", llm.RoleUser, 10, 0),
                msgEntry("e3", llm.RoleUser, 10, 0),
        }
        startIndex := 0
        endIndex := len(entries)
        keepRecentTokens := 200
        cutPoints := []int{0, 1, 2}

        cutIndex := findCutIndex(entries, startIndex, endIndex, keepRecentTokens, cutPoints)

        assert.Equal(t, cutPoints[0], cutIndex)
}

func TestFindCutIndex_BudgetReachedExactlyCutsThere(t *testing.T) {
        entries := []session.MessageEntry{
                msgEntry("e1", llm.RoleUser, 10, 0),
                msgEntry("e2", llm.RoleAssistant, 20, 0),
                msgEntry("e3", llm.RoleUser, 10, 0),
        }

        cutIndex := findCutIndex(entries, 0, len(entries), 30, []int{0, 1, 2})

        // 10 + 20 == 30 exactly: the budget is reached at e2 (>= comparison).
        // A strict > would keep walking and cut at e1 instead.
        assert.Equal(t, 1, cutIndex)
}

// The provider reports the size of the whole context on every assistant
// message, and only assistant messages carry it. Summing that instead of
// per-message sizes tripped keepRecentTokens on the newest message, so the cut
// collapsed to the last entry and the budget never bound anything.
func TestFindCutIndex_IgnoresCumulativeUsage(t *testing.T) {
        const messages = 30
        entries := make([]session.MessageEntry, 0, messages)
        cutPoints := make([]int, 0, messages)
        for i := range messages {
                role := llm.RoleUser
                usage := 0
                if i%2 == 1 {
                        role = llm.RoleAssistant
                        usage = 70000 + i // cumulative context, as real sessions report it
                }
                entries = append(entries, msgEntry(fmt.Sprintf("e%d", i), role, 100, usage))
                cutPoints = append(cutPoints, i)
        }

        // 100 tokens per message and a 1000-token budget keep the last 10 messages.
        cutIndex := findCutIndex(entries, 0, len(entries), 1000, cutPoints)

        assert.Equal(t, 20, cutIndex)
}
