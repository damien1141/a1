package transcript

import (
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/block"
	"github.com/damien1141/a1/internal/components/status"
)

// copyChord is the documented shift+ctrl+C shape. Built from the same xui
// constants the handler reads, so a change in either side fails here.
var copyChord = xui.KeyEvent{
	Code:  xui.KeyRune,
	Rune:  'c',
	Mods:  xui.ModCtrl | xui.ModShift,
	Press: true,
}

// newCopyPane builds a pane wired to a recording clipboard.
func newCopyPane(t *testing.T) (*TranscriptPane, *[]string) {
	t.Helper()
	pane := NewTranscriptPane(components.DefaultTheme(), status.NewSpinner(components.DefaultTheme().Foreground), "")
	var copied []string
	pane.SetCopyHandlers(nil, func(text string) bool {
		copied = append(copied, text)
		return true
	})
	return pane, &copied
}

// TestHandleCopyKey_ShiftCtrlC_CopiesSelectedBlock_Positive: the chord copies
// the block under the cursor and consumes the key, so it never reaches the
// composer as a literal 'C'.
func TestHandleCopyKey_ShiftCtrlC_CopiesSelectedBlock_Positive(t *testing.T) {
	pane, copied := newCopyPane(t)
	pane.list.Entries = []components.Widget{
		newTestUserBlock("first message"),
		newTestUserBlock("second message"),
	}
	pane.list.Selected = 1

	ctx := &components.EventContext{}
	handled := pane.HandleCopyKey(ctx, copyChord)

	require.True(t, handled, "shift+ctrl+C must be consumed by the transcript")
	require.Len(t, *copied, 1)
	assert.Contains(t, (*copied)[0], "second message",
		"the SELECTED block is copied, not the last one")
}

// TestHandleCopyKey_FallsBackToLast_Negative: with no explicit selection the
// chord still does something useful — it copies the most recent block rather
// than silently doing nothing.
func TestHandleCopyKey_FallsBackToLast_Negative(t *testing.T) {
	pane, copied := newCopyPane(t)
	pane.list.Entries = []components.Widget{
		newTestUserBlock("older"),
		newTestUserBlock("newest"),
	}
	pane.list.Selected = -1 // nothing selected

	handled := pane.HandleCopyKey(&components.EventContext{}, copyChord)

	require.True(t, handled)
	require.Len(t, *copied, 1)
	assert.Contains(t, (*copied)[0], "newest", "no selection falls back to the last block")
}

// TestHandleCopyKey_PlainCIsNotTheChord_Negative is the guard that matters for
// typing: a bare 'c' must reach the composer untouched, or the user could not
// write a word containing it.
func TestHandleCopyKey_PlainCIsNotTheChord_Negative(t *testing.T) {
	pane, copied := newCopyPane(t)
	pane.list.Entries = []components.Widget{newTestUserBlock("hello")}

	for _, ev := range []xui.KeyEvent{
		{Code: xui.KeyRune, Rune: 'c', Press: true},                                    // bare c
		{Code: xui.KeyRune, Rune: 'c', Mods: xui.ModCtrl, Press: true},                 // ctrl+c (turn control)
		{Code: xui.KeyRune, Rune: 'c', Mods: xui.ModCtrl | xui.ModShift, Press: false}, // key release
	} {
		assert.False(t, pane.HandleCopyKey(&components.EventContext{}, ev),
			"only the shift+ctrl+C press is the copy chord: %+v", ev)
	}
	assert.Empty(t, *copied, "no copy may happen for a non-chord key")
}

// TestHandleCopyKey_EmptyTranscript_NoClipboardWrite_Negative: nothing to copy
// means no clipboard call, no crash — and the chord is REPORTED UNHANDLED.
//
// That last part is load-bearing. The composer offers "Ctrl+C copies the
// selection" as a fallback for terminals that cannot distinguish Ctrl+Shift+C
// from Ctrl+C (most non-kitty terminals send 0x03 for both). If this returned
// true unconditionally, that fallback would swallow every plain Ctrl+C and
// quitting the TUI would be impossible.
func TestHandleCopyKey_EmptyTranscript_NoClipboardWrite_Negative(t *testing.T) {
	pane, copied := newCopyPane(t)

	handled := pane.HandleCopyKey(&components.EventContext{}, copyChord)

	assert.False(t, handled,
		"with nothing to copy the chord must fall through, or the Ctrl+C fallback eats quit")
	assert.Empty(t, *copied, "and there is nothing to put on the clipboard")
}

// newTestUserBlock builds a minimal block whose CopyText() carries text, so a
// copy assertion can tell WHICH block was taken.
func newTestUserBlock(text string) components.Widget {
	return &block.UserBlock{Text: text}
}
