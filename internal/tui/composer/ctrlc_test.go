package composer

import (
	"testing"
	"time"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/tui/controller"
)

// ctrlCEvent is a synthetic Ctrl-C key press.
var ctrlCEvent = xui.KeyEvent{Code: xui.KeyRune, Rune: 'c', Mods: xui.ModCtrl, Press: true}

// TestCtrlC_StopsBusyGeneration verifies the first Ctrl-C while the agent is
// busy cancels the stream instead of quitting.
func TestCtrlC_StopsBusyGeneration(t *testing.T) {
	c, bus := wiredComposer(t)
	// Simulate a busy agent: the transcript reports streaming.
	c.submitter = &fakeBusy{busy: true}

	var quit bool
	c.ctrlClose = func() { quit = true }

	ctx := &components.EventContext{}
	c.Handle(ctx, ctrlCEvent)

	assert.False(t, quit, "first Ctrl-C while busy must not quit")
	// CancelStreamMsg must be published on the bus.
	found := false
	for _, m := range bus.Drain() {
		if _, ok := m.(controller.CancelStreamMsg); ok {
			found = true
		}
	}
	assert.True(t, found, "first Ctrl-C must publish CancelStreamMsg")
}

// TestCtrlC_TwiceQuits verifies that two Ctrl-C presses within the
// double-tap window exit the TUI.
func TestCtrlC_TwiceQuits(t *testing.T) {
	c, _ := wiredComposer(t)
	c.submitter = &fakeBusy{busy: false}

	var quit bool
	c.ctrlClose = func() { quit = true }

	ctx := &components.EventContext{}
	c.Handle(ctx, ctrlCEvent)
	require.False(t, quit, "first Ctrl-C when idle must not quit")
	// Second press within the window exits.
	c.Handle(ctx, ctrlCEvent)

	assert.True(t, quit, "second Ctrl-C within the window must quit")
}

// TestCtrlC_TapWindowExpires verifies the double-tap window expires: after
// the window, a single Ctrl-C no longer quits.
func TestCtrlC_TapWindowExpires(t *testing.T) {
	c, _ := wiredComposer(t)
	c.submitter = &fakeBusy{busy: false}

	var quit bool
	c.ctrlClose = func() { quit = true }

	ctx := &components.EventContext{}
	c.Handle(ctx, ctrlCEvent)
	// Move past the 700ms window.
	c.lastCtrlC = c.lastCtrlC.Add(-time.Second)
	c.Handle(ctx, ctrlCEvent)

	assert.False(t, quit, "Ctrl-C after the window must not quit")
}

// fakeBusy is a stand-in for the submit side of ComposerPane wiring.
type fakeBusy struct {
	busy bool
}

func (f *fakeBusy) RunningBash() bool     { return f.busy }
func (f *fakeBusy) IsBusy() bool          { return f.busy }
func (f *fakeBusy) SyncBashBorder(string) {}

// TestCtrlC_StopsTurnBeforeFirstToken pins the bug that made one tap quit the
// TUI: between pressing enter and the first streamed token there is nothing in
// the transcript to derive busy-ness from — the model is thinking, no message is
// streaming, no tool is running. IsBusy() therefore read false and the first
// Ctrl-C took the quit branch.
//
// The submitter now carries an in-flight bridge for exactly that window, so this
// case (busy with an idle transcript) must behave like busy.
func TestCtrlC_StopsTurnBeforeFirstToken(t *testing.T) {
	c, bus := wiredComposer(t)

	// A submitter that reports busy only through the in-flight bridge: the
	// transcript stays idle, exactly as it does while the model thinks.
	c.submitter = &fakeBusy{busy: true}

	var quit bool
	c.ctrlClose = func() { quit = true }

	ctx := &components.EventContext{}
	c.Handle(ctx, ctrlCEvent)

	assert.False(t, quit, "Ctrl-C during the pre-token gap must not quit the app")

	found := false
	for _, m := range bus.Drain() {
		if _, ok := m.(controller.CancelStreamMsg); ok {
			found = true
		}
	}
	assert.True(t, found, "it must cancel the turn instead")
}

// TestCtrlShiftC_CopyWinsOverStopOrQuit_Positive is the regression pin for
// "Ctrl+Shift+C doesn't copy the block".
//
// xui's CtrlC() returns true for ANY ModCtrl+c — shift included — so the
// stop/quit handler claimed the copy chord before the copy handler ever saw it.
// On terminals without the kitty keyboard protocol the two are literally the
// same byte (0x03) and the modifier never arrives at all, so the chord was
// unreachable.
func TestCtrlShiftC_CopyWinsOverStopOrQuit_Positive(t *testing.T) {
	c, bus := wiredComposer(t)
	// Busy: without the ordering fix this press would cancel the turn instead
	// of copying.
	c.submitter = &fakeBusy{busy: true}

	var quit bool
	c.ctrlClose = func() { quit = true }
	var copied bool
	c.handleCopyKey = func(*components.EventContext, xui.KeyEvent) bool {
		copied = true
		return true
	}

	shiftCtrlC := xui.KeyEvent{Code: xui.KeyRune, Rune: 'c', Mods: xui.ModCtrl | xui.ModShift, Press: true}
	c.Handle(&components.EventContext{}, shiftCtrlC)

	assert.True(t, copied, "Ctrl+Shift+C must reach the copy handler")
	assert.False(t, quit, "and must not quit")

	for _, m := range bus.Drain() {
		_, cancelled := m.(controller.CancelStreamMsg)
		assert.False(t, cancelled, "and must not be treated as a stop")
	}
}

// TestCtrlC_CopiesSelectionWhenIdle_Negative pins the fallback for terminals
// that cannot distinguish the chords: idle + a selection means Ctrl+C copies.
func TestCtrlC_CopiesSelectionWhenIdle_Negative(t *testing.T) {
	c, _ := wiredComposer(t)
	c.submitter = &fakeBusy{busy: false}

	var copied bool
	c.handleCopyKey = func(*components.EventContext, xui.KeyEvent) bool {
		copied = true
		return true // there was something to copy
	}
	var quit bool
	c.ctrlClose = func() { quit = true }

	c.Handle(&components.EventContext{}, ctrlCEvent)

	assert.True(t, copied, "an idle Ctrl+C with a selection copies it")
	assert.False(t, quit, "and does not quit on the first press")
}

// TestCtrlC_QuitsWhenNothingToCopy_Negative: the fallback must not break quit.
// With nothing selected the copy handler declines, and Ctrl+C keeps its
// double-tap-to-exit behaviour.
func TestCtrlC_QuitsWhenNothingToCopy_Negative(t *testing.T) {
	c, _ := wiredComposer(t)
	c.submitter = &fakeBusy{busy: false}
	c.handleCopyKey = func(*components.EventContext, xui.KeyEvent) bool {
		return false // nothing to copy
	}
	var quit bool
	c.ctrlClose = func() { quit = true }

	ctx := &components.EventContext{}
	c.Handle(ctx, ctrlCEvent)
	require.False(t, quit, "first press arms the double-tap")
	c.Handle(ctx, ctrlCEvent)

	assert.True(t, quit, "second press quits — the copy fallback must not eat it")
}
