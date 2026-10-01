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

func (f *fakeBusy) RunningBash() bool { return f.busy }
func (f *fakeBusy) IsBusy() bool     { return f.busy }
func (f *fakeBusy) SyncBashBorder(string) {}