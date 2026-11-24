package composer

import (
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

var (
	upKey   = xui.KeyEvent{Code: xui.KeyUp, Press: true}
	downKey = xui.KeyEvent{Code: xui.KeyDown, Press: true}
)

// browsingComposer builds a composer holding a draft over a multi-line history.
func browsingComposer(t *testing.T) *ComposerPane {
	t.Helper()
	c, _ := wiredComposer(t)
	c.history = []string{
		"oldest one\noldest two\noldest three",
		"mid one\nmid two\nmid three",
		"newest one\nnewest two\nnewest three",
	}
	c.historyIdx = -1
	c.Chat.Value = "MY DRAFT"
	c.Chat.Cursor = len(c.Chat.Value)
	return c
}

// TestHistoryNav_OnePressPerEntry_Positive is the regression pin for the report
// "I press up to see what I typed, then down, and my text doesn't come back".
//
// The arrows walked the RECALLED entry's wrapped rows before stepping entries,
// so a three-line history entry cost three presses. The draft was not lost —
// it was several presses away, which reads as lost.
func TestHistoryNav_OnePressPerEntry_Positive(t *testing.T) {
	c := browsingComposer(t)
	ctx := &components.EventContext{}

	// Three entries, so three Ups reach the oldest — one press each.
	for want := 2; want >= 0; want-- {
		c.Chat.Handle(ctx, upKey)
		require.Equal(t, want, c.historyIdx,
			"each Up steps exactly one ENTRY, not one wrapped row")
	}

	// And back: three Downs return to the draft.
	c.Chat.Handle(ctx, downKey)
	require.Equal(t, 1, c.historyIdx)
	c.Chat.Handle(ctx, downKey)
	require.Equal(t, 2, c.historyIdx)
	c.Chat.Handle(ctx, downKey)

	assert.Equal(t, -1, c.historyIdx, "past the newest entry")
	assert.Equal(t, "MY DRAFT", c.Chat.Value,
		"the draft must come back — this is the bug that was reported")
}

// TestHistoryNav_MultiLineDraftKeepsCaret_Negative: the other half. A multi-line
// DRAFT must stay navigable — Up moves the caret, and only recalls history from
// the first visual row. Entry-stepping must not swallow ordinary caret motion.
func TestHistoryNav_MultiLineDraftKeepsCaret_Negative(t *testing.T) {
	c, _ := wiredComposer(t)
	c.history = []string{"an old command"}
	c.historyIdx = -1
	c.Chat.Value = "line one\nline two\nline three"
	c.Chat.Cursor = len(c.Chat.Value)

	c.Chat.Handle(&components.EventContext{}, upKey)

	assert.Equal(t, -1, c.historyIdx,
		"Up inside a multi-line draft moves the caret, it does not enter history")
	assert.Equal(t, "line one\nline two\nline three", c.Chat.Value,
		"the draft is untouched")
	assert.Less(t, c.Chat.Cursor, len(c.Chat.Value), "the caret moved up a row")
}

// TestHistoryNav_DraftSurvivesRepeatedBrowsing_Differential: browse out and back
// twice; the draft must survive every round trip. Oracle: the literal draft the
// user typed.
func TestHistoryNav_DraftSurvivesRepeatedBrowsing_Differential(t *testing.T) {
	c := browsingComposer(t)
	ctx := &components.EventContext{}

	for round := 0; round < 2; round++ {
		c.Chat.Handle(ctx, upKey)
		require.Equal(t, 2, c.historyIdx, "round %d: browse to newest entry", round)
		c.Chat.Handle(ctx, downKey)
		require.Equal(t, -1, c.historyIdx, "round %d: back out", round)
		assert.Equal(t, "MY DRAFT", c.Chat.Value, "round %d: draft intact", round)
	}
}
