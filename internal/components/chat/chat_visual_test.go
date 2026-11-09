package chat

import (
	"testing"

	"github.com/pulseaiclub/xui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/text"
)

// The vertical arrow keys are display-row aware: the editor soft-wraps long
// lines and paints the caret in wrapped coordinates, so Up/Down must walk the
// wrapped rows the user sees before history navigation gets a say (the
// readline contract: history only at the first/last visual row). The old
// logical-line walk disagreed with the renderer — Up on a wrapped row
// recalled history mid-composition, and Down on a fresh prompt wiped the
// input outright.

func newVisualInput(value string, cursor int, width int) *ChatInput {
	c := &ChatInput{MinBodyRows: 3, Value: value, Cursor: cursor}
	c.drawWidth = width
	c.drawMethod = xui.WidthUnicode
	return c
}

func TestMoveVisualRowSoftWrap(t *testing.T) {
	// "abcdefghij" at width 4 wraps to ["abcd","efgh","ij"] (starts 0,4,8).
	c := newVisualInput("abcdefghij", 10, 10)
	c.drawWidth = 4 // caret at end → row 2, col 2
	require.True(t, c.moveVisualRow(-1))
	assert.Equal(t, 6, c.Cursor, "row 'efgh' at column 2")

	require.True(t, c.moveVisualRow(-1))
	assert.Equal(t, 2, c.Cursor, "row 'abcd' at column 2")

	assert.False(t, c.moveVisualRow(-1), "no row above the first")
	assert.Equal(t, 2, c.Cursor, "a failed move leaves the caret alone")

	// And back down.
	require.True(t, c.moveVisualRow(1))
	assert.Equal(t, 6, c.Cursor)
	require.True(t, c.moveVisualRow(1))
	assert.Equal(t, 10, c.Cursor, "row 'ij' is only 2 columns — column 2 clamps to its end")
	assert.False(t, c.moveVisualRow(1), "no row below the last")
}

func TestMoveVisualRowPhantomRowBelowFullWidthLine(t *testing.T) {
	// "abcdefgh" at width 4 wraps to ["abcd","efgh"]; a caret at the very end
	// of the full-width last row reports the phantom row below it (2,0) —
	// the same convention the renderer uses for the caret position.
	c := newVisualInput("abcdefgh", 8, 4)
	require.True(t, c.moveVisualRow(-1))
	assert.Equal(t, 4, c.Cursor, "phantom row up → row 'efgh' at column 0")

	// A caret at the soft-wrap boundary (offset 4) reports the phantom row
	// below "abcd" — visually the start of "efgh" — so Down has nowhere to
	// go and Up returns to the start of "abcd".
	assert.False(t, c.moveVisualRow(1))
	require.True(t, c.moveVisualRow(-1))
	assert.Equal(t, 0, c.Cursor)
}

func TestMoveVisualRowHardNewlines(t *testing.T) {
	// "ab\ncdef" at width 10 wraps to ["ab","cdef"] (starts 0,3).
	c := newVisualInput("ab\ncdef", 6, 10)
	require.True(t, c.moveVisualRow(-1))
	assert.Equal(t, 2, c.Cursor, "column 4 past the end of row 'ab' clamps to its end")
	assert.False(t, c.moveVisualRow(-1))

	// Mixed: "abcdef\ngh" at width 3 → ["abc","def","gh"] (starts 0,3,7).
	m := newVisualInput("abcdef\ngh", 8, 3) // caret after 'h' → row 2, col 1
	require.True(t, m.moveVisualRow(-1))
	assert.Equal(t, 4, m.Cursor, "row 'def' at column 1")
	require.True(t, m.moveVisualRow(-1))
	assert.Equal(t, 1, m.Cursor, "row 'abc' at column 1 — the newline costs one byte")
	assert.False(t, m.moveVisualRow(-1))
}

func TestMoveVisualRowWideGlyphs(t *testing.T) {
	// CJK: each glyph is width 2. "日本語です" at width 4 wraps to
	// ["日本","語で","す"] (byte starts 0,6,12; each glyph is 3 bytes).
	c := newVisualInput("日本語です", 15, 4) // caret at end → row 2, col 2
	require.True(t, c.moveVisualRow(-1))
	assert.Equal(t, 9, c.Cursor, "row '語で' at column 2 lands after 語")

	c = newVisualInput("日本語です", 12, 4) // caret at start of 'す' row
	require.True(t, c.moveVisualRow(-1))
	assert.Equal(t, 6, c.Cursor, "row '語で' at column 0")

	c = newVisualInput("日本語です", 9, 4) // caret after 語 (row 1, col 2)
	require.True(t, c.moveVisualRow(-1))
	assert.Equal(t, 3, c.Cursor, "row '日本' at column 2 lands after 日, not inside a glyph")
}

func TestMoveVisualRowSingleRow(t *testing.T) {
	c := newVisualInput("short", 5, 40)
	assert.False(t, c.moveVisualRow(-1))
	assert.False(t, c.moveVisualRow(1))
	assert.Equal(t, 5, c.Cursor)
}

func TestWrappedRowStarts(t *testing.T) {
	cases := []struct {
		name  string
		value string
		width int
	}{
		{"soft wrap only", "abcdefghij", 4},
		{"hard newline only", "ab\ncdef", 10},
		{"mixed", "abcdef\ngh", 3},
		{"empty lines", "a\n\nb", 10},
		{"exact width row", "abcabc", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := text.WrapEditorLines(tc.value, tc.width, xui.WidthUnicode)
			starts := wrappedRowStarts(tc.value, lines)
			require.Len(t, starts, len(lines))
			for i, start := range starts {
				end := start + len(lines[i])
				require.LessOrEqual(t, end, len(tc.value))
				assert.Equal(t, lines[i], tc.value[start:end],
					"row %d must be the contiguous bytes at its start offset", i)
			}
		})
	}
}

// TestUpOnWrappedRowMovesCaretNotHistory is the integration pin for the
// reported jank: a long wrapped prompt plus Up must move the caret up one
// screen row, NOT recall history and stash the draft.
func TestUpOnWrappedRowMovesCaretNotHistory(t *testing.T) {
	// "one two three four five six seven eight" (39 chars) at width 12 wraps
	// to ["one two thre","e four five ","six seven ei","ght"].
	c := newVisualInput("one two three four five six seven eight", 30, 12)
	historyCalls := 0
	c.OnHistoryNav = func(int) bool {
		historyCalls++
		return true
	}
	ctx := &components.EventContext{}
	c.Handle(ctx, xui.KeyEvent{Code: xui.KeyUp, Press: true})

	assert.Zero(t, historyCalls, "Up on a wrapped row must not touch history")
	assert.True(t, ctx.Consume)
	assert.Equal(t, 18, c.Cursor, "the caret moved up a visual row, column preserved")
}

func TestUpAtFirstVisualRowRecallsHistory(t *testing.T) {
	c := newVisualInput("short", 5, 40)
	deltas := []int{}
	c.OnHistoryNav = func(d int) bool {
		deltas = append(deltas, d)
		return true
	}
	ctx := &components.EventContext{}
	c.Handle(ctx, xui.KeyEvent{Code: xui.KeyUp, Press: true})
	require.Equal(t, []int{-1}, deltas)
}

func TestDownAtLastVisualRowWithoutHistoryNeverWipes(t *testing.T) {
	// The data-loss pin: Down on a fresh prompt must not clear the composer.
	// OnHistoryNav returns false ("not browsing") and the value survives.
	c := newVisualInput("a half-typed prompt", 20, 40)
	c.OnHistoryNav = func(int) bool { return false }
	ctx := &components.EventContext{}
	c.Handle(ctx, xui.KeyEvent{Code: xui.KeyDown, Press: true})

	assert.Equal(t, "a half-typed prompt", c.Value, "Down must never wipe the input")
	assert.Equal(t, len(c.Value), c.Cursor, "Down at the last row clamps to the end")
	assert.True(t, ctx.Consume)
}

func TestUpDownWithoutHistoryNavClamp(t *testing.T) {
	// A bare ChatInput with no OnHistoryNav (other embedders): the boundary
	// clamps survive.
	c := newVisualInput("multi\nline", 5, 40)
	ctx := &components.EventContext{}
	c.Handle(ctx, xui.KeyEvent{Code: xui.KeyUp, Press: true})
	assert.Equal(t, 0, c.Cursor, "Up at the top clamps to start")
	c.Handle(ctx, xui.KeyEvent{Code: xui.KeyDown, Press: true})
	// caret at 0 is on row "multi"; Down moves to row "line" column 0 → 6.
	assert.Equal(t, 6, c.Cursor)
	c.Cursor = 6
	c.Handle(ctx, xui.KeyEvent{Code: xui.KeyDown, Press: true})
	assert.Equal(t, len("multi\nline"), c.Cursor, "Down at the bottom clamps to end")
}
