package overlays

import (
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/tui/controller"
)

// permWithReply builds an Overlays holding one pending permission request whose
// answer is read back through a reply channel.
func permWithReply(t *testing.T) (*Overlays, chan controller.AskReply) {
	t.Helper()
	reply := make(chan controller.AskReply, 1)
	o := &Overlays{}
	o.perm = newPermAskState(permission.Request{
		Action:  permission.ActionBash,
		Command: "npm install",
		Tool:    "bash",
	}, "", reply)
	return o, reply
}

// selectReject moves the cursor to the Reject row.
func selectReject(o *Overlays) {
	for askOption(o.perm.selected) != askOptDeny {
		o.perm.selected = (o.perm.selected + 1) % len(askOptionLabels)
	}
}

// TestPermissionFeedback_ReachableAndForwardsText_Positive is the regression pin
// for a feature that was fully built and unreachable: the composer, the key
// handler, the AskReply plumbing and the model-facing tool result all existed,
// but nothing ever set feedbackMode, so "reject with an explanation" could not
// be entered at all.
//
// It now lives on Ctrl+E over the Reject row, and the typed sentence must reach
// the model as the rejection reason instead of a bare denial it would retry.
func TestPermissionFeedback_ReachableAndForwardsText_Positive(t *testing.T) {
	o, reply := permWithReply(t)
	selectReject(o)

	ctrlE := xui.KeyEvent{Code: xui.KeyRune, Rune: 'e', Mods: xui.ModCtrl, Press: true}
	require.True(t, o.handlePermissionKey(&components.EventContext{}, ctrlE))
	require.True(t, o.perm.feedbackMode, "Ctrl+E on Reject opens the explanation composer")

	// Type a sentence, then send it.
	for _, r := range "use bun instead" {
		o.handlePermissionFeedbackKey(&components.EventContext{}, xui.KeyEvent{Code: xui.KeyRune, Rune: r, Press: true})
	}
	require.True(t, o.handlePermissionFeedbackKey(&components.EventContext{}, xui.KeyEvent{Code: xui.KeyEnter, Press: true}))

	select {
	case got := <-reply:
		assert.False(t, got.Approved, "it is still a denial")
		assert.Equal(t, "use bun instead", got.Feedback,
			"the explanation rides AskReply.Feedback into the model's tool result")
	case <-t.Context().Done():
		t.Fatal("no reply was sent")
	}
}

// TestPermissionFeedback_CtrlEIgnoredOutsideReject_Negative: the shortcut is
// only meaningful on the row it explains. On Allow it must not open a composer
// whose answer would be an approval carrying a complaint.
func TestPermissionFeedback_CtrlEIgnoredOutsideReject_Negative(t *testing.T) {
	o, _ := permWithReply(t)
	o.perm.selected = int(askOptApprove)

	ctrlE := xui.KeyEvent{Code: xui.KeyRune, Rune: 'e', Mods: xui.ModCtrl, Press: true}
	o.handlePermissionKey(&components.EventContext{}, ctrlE)

	assert.False(t, o.perm.feedbackMode, "Ctrl+E does nothing on the Allow row")
}

// TestPermissionFeedback_EscapeCancelsComposer_Negative: Esc backs out to the
// option list rather than sending a half-typed denial.
func TestPermissionFeedback_EscapeCancelsComposer_Negative(t *testing.T) {
	o, _ := permWithReply(t)
	selectReject(o)
	o.handlePermissionKey(&components.EventContext{}, xui.KeyEvent{Code: xui.KeyRune, Rune: 'e', Mods: xui.ModCtrl, Press: true})
	require.True(t, o.perm.feedbackMode)

	o.handlePermissionFeedbackKey(&components.EventContext{}, xui.KeyEvent{Code: xui.KeyEscape, Press: true})

	assert.False(t, o.perm.feedbackMode, "Esc leaves the composer")
	assert.Empty(t, o.perm.feedback, "and discards the draft")
	assert.NotNil(t, o.perm, "without resolving the request")
}
