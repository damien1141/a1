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

func testOverlays(activity *controller.ActivityHandler) *Overlays {
	return NewOverlays(components.DefaultTheme(), activity, nil, nil, nil)
}

func TestResolvePermissionSendsReply(t *testing.T) {
	activity := controller.NewActivityHandler(nil)
	o := testOverlays(activity)
	reply := make(chan controller.AskReply, 1)
	o.beginPermissionAsk(controller.OverlayMsg{
		Kind:      controller.OverlayPermissionAsk,
		Request:   permission.Request{Action: permission.ActionBash, Tool: "bash", Command: "curl x"},
		Reason:    "needs approval",
		PermReply: reply,
	})
	require.NotNil(t, o.perm, "expected permAsk")
	require.Equal(t, "Run this command?", o.perm.header)
	require.Equal(t, controller.ActivityAwaitingApproval, activity.Current)
	o.resolvePermission(controller.AskReply{Approved: true})
	require.Nil(t, o.perm, "expected cleared")
	select {
	case r := <-reply:
		require.True(t, r.Approved, "want approved")
	default:
		require.Fail(t, "expected reply")
	}
}

func TestPermissionDenySendsReject(t *testing.T) {
	o := testOverlays(controller.NewActivityHandler(nil))
	reply := make(chan controller.AskReply, 1)
	o.beginPermissionAsk(controller.OverlayMsg{
		Kind:      controller.OverlayPermissionAsk,
		Request:   permission.Request{Tool: "bash", Action: permission.ActionBash, Command: "curl https://x"},
		PermReply: reply,
	})
	o.acceptPermissionOption(askOptDeny)
	require.Nil(t, o.perm, "overlay should clear")
	select {
	case r := <-reply:
		require.False(t, r.Approved)
	default:
		require.Fail(t, "expected reply")
	}
}

func TestPermissionDismissClearsOverlay(t *testing.T) {
	o := testOverlays(controller.NewActivityHandler(nil))
	reply := make(chan controller.AskReply, 1)
	o.beginPermissionAsk(controller.OverlayMsg{
		Kind:      controller.OverlayPermissionAsk,
		Request:   permission.Request{Tool: "bash", Action: permission.ActionBash, Command: "curl https://x"},
		PermReply: reply,
	})
	o.Apply(controller.OverlayMsg{Kind: controller.OverlayPermissionDismiss})
	require.Nil(t, o.perm, "overlay should clear without consuming reply")
	select {
	case <-reply:
		require.Fail(t, "dismiss must not send on reply")
	default:
	}
}

func TestDrawPermissionAskReplacesComposerSlot(t *testing.T) {
	o := testOverlays(controller.NewActivityHandler(nil))
	reply := make(chan controller.AskReply, 1)
	o.beginPermissionAsk(controller.OverlayMsg{
		Kind:      controller.OverlayPermissionAsk,
		Request:   permission.Request{Action: permission.ActionBash, Tool: "bash", Command: "rm -f todo.list"},
		Reason:    "Matches built-in permissions rule",
		PermReply: reply,
	})
	surf := o.drawPermissionAsk(components.DrawContext{
		Max:    components.Size{Width: 60, Height: 12},
		Method: 0,
	}, 60, 12)
	require.Equal(t, 60, surf.Size.Width)
	require.Equal(t, 12, surf.Size.Height)
}

func TestFormatAskHeader(t *testing.T) {
	h, d := formatAskHeader(permission.Request{Action: permission.ActionWrite, Paths: []string{"/tmp/a"}})
	require.Equal(t, "Allow creating file:", h)
	require.Equal(t, "/tmp/a", d)
}

func TestContinueAskResolveContinue(t *testing.T) {
	activity := controller.NewActivityHandler(nil)
	o := testOverlays(activity)
	reply := make(chan controller.ContinueReply, 1)
	o.beginContinueAsk(controller.OverlayMsg{
		Kind:      controller.OverlayContinueAsk,
		MaxRounds: 64,
		ContReply: reply,
	})
	require.NotNil(t, o.cont, "expected continueAsk")
	require.Equal(t, 64, o.cont.maxRounds)
	require.Equal(t, controller.ActivityAwaitingApproval, activity.Current)
	o.resolveContinue(controller.ContinueReply{Continue: true})
	require.Nil(t, o.cont, "expected continueAsk cleared")
	select {
	case r := <-reply:
		require.True(t, r.Continue, "expected Continue=true")
	default:
		require.Fail(t, "expected reply")
	}
}

func TestContinueAskEscapeStops(t *testing.T) {
	o := testOverlays(controller.NewActivityHandler(nil))
	reply := make(chan controller.ContinueReply, 1)
	o.beginContinueAsk(controller.OverlayMsg{
		Kind:      controller.OverlayContinueAsk,
		MaxRounds: 2,
		ContReply: reply,
	})
	ctx := &components.EventContext{}
	_ = o.handleContinueKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyEscape})
	select {
	case r := <-reply:
		require.False(t, r.Continue, "escape should stop")
	default:
		require.Fail(t, "expected reply on escape")
	}
}

func TestContinueDismissClearsOverlay(t *testing.T) {
	o := testOverlays(controller.NewActivityHandler(nil))
	reply := make(chan controller.ContinueReply, 1)
	o.beginContinueAsk(controller.OverlayMsg{
		Kind:      controller.OverlayContinueAsk,
		MaxRounds: 2,
		ContReply: reply,
	})
	o.Apply(controller.OverlayMsg{Kind: controller.OverlayContinueDismiss})
	require.Nil(t, o.cont, "overlay should clear without consuming reply")
	select {
	case <-reply:
		require.Fail(t, "dismiss must not send on reply")
	default:
	}
}

func testQuestion(q controller.Question) (*Overlays, *controller.ActivityHandler, chan controller.QuestionReply) {
	activity := controller.NewActivityHandler(nil)
	o := testOverlays(activity)
	reply := make(chan controller.QuestionReply, 1)
	o.beginQuestionAsk(controller.OverlayMsg{
		Kind:       controller.OverlayQuestionAsk,
		Question:   &q,
		QuestReply: reply,
	})
	return o, activity, reply
}

func TestQuestionRendersOptionsAndDescription(t *testing.T) {
	o, _, _ := testQuestion(controller.Question{
		Header:   "Approach",
		Question: "Pick a parser:",
		Options: []controller.QuestionOption{
			{Label: "csv-aware (Recommended)", Description: "Handles quoted commas."},
			{Label: "split on comma"},
		},
	})
	require.NotNil(t, o.question)
	defer o.dismissQuestion()

	s, ok := o.DrawBottom(components.DrawContext{Max: components.Size{Width: 60, Height: 30}}, 60, 30)
	require.True(t, ok)
	got := components.SurfaceText(s)
	assert.Contains(t, got, "Approach")
	assert.Contains(t, got, "Pick a parser:")
	assert.Contains(t, got, "csv-aware (Recommended)")
	assert.Contains(t, got, "Handles quoted commas.")
	assert.Contains(t, got, "split on comma")
	assert.Contains(t, got, "Other", "the free-text escape hatch is always present")
}

func TestQuestionSelectSendsLabel(t *testing.T) {
	o, _, reply := testQuestion(controller.Question{
		Question: "Which?",
		Options: []controller.QuestionOption{
			{Label: "first"}, {Label: "second"},
		},
	})
	defer o.dismissQuestion()

	ctx := &components.EventContext{}
	require.True(t, o.HandleQuestionKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyEnter}),
		"the question overlay must consume Enter")
	require.Nil(t, o.question, "answered, so the overlay cleared")

	r := <-reply
	assert.Equal(t, "first", r.Choice, "selection starts on the first option")
	assert.Equal(t, []string{"first"}, r.Choices)
	assert.False(t, r.Cancelled)
}

func TestQuestionArrowMovesAndEscapeCancels(t *testing.T) {
	o, _, reply := testQuestion(controller.Question{
		Question: "Which?",
		Options:  []controller.QuestionOption{{Label: "a"}, {Label: "b"}, {Label: "c"}},
	})
	defer o.dismissQuestion()

	ctx := &components.EventContext{}
	o.HandleQuestionKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyDown})
	require.Equal(t, 1, o.question.selected)
	o.HandleQuestionKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyEnter})
	r := <-reply
	assert.Equal(t, "b", r.Choice)
}

func TestQuestionEscapeReportsCancelled(t *testing.T) {
	o, _, reply := testQuestion(controller.Question{
		Question: "Which?",
		Options:  []controller.QuestionOption{{Label: "a"}, {Label: "b"}},
	})
	defer o.dismissQuestion()

	ctx := &components.EventContext{}
	o.HandleQuestionKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyEscape})
	require.Nil(t, o.question)

	r := <-reply
	assert.True(t, r.Cancelled, "skipping is distinguishable from answering")
	assert.Empty(t, r.Choice)
}

func TestQuestionOtherCollectsFreeText(t *testing.T) {
	o, _, reply := testQuestion(controller.Question{
		Question: "Which?",
		Options:  []controller.QuestionOption{{Label: "a"}, {Label: "b"}},
	})
	defer o.dismissQuestion()

	ctx := &components.EventContext{}
	// Two options, so "Other" is index 2: one Up from the top wraps to it.
	o.HandleQuestionKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyUp})
	require.Equal(t, 2, o.question.selected)
	o.HandleQuestionKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyEnter})
	require.True(t, o.question.customOn, "choosing Other opens the text field")

	for _, rn := range []rune("both") {
		o.HandleQuestionKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyRune, Rune: rn})
	}
	o.HandleQuestionKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyEnter})

	r := <-reply
	assert.Equal(t, "both", r.Choice)
	assert.False(t, r.Cancelled)
}

func TestQuestionMultiSelectSpaceChecks(t *testing.T) {
	o, _, reply := testQuestion(controller.Question{
		Question:    "Which?",
		MultiSelect: true,
		Options:     []controller.QuestionOption{{Label: "a"}, {Label: "b"}, {Label: "c"}},
	})
	defer o.dismissQuestion()

	ctx := &components.EventContext{}
	o.HandleQuestionKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyRune, Rune: ' '})
	require.True(t, o.question.picked[0], "space checks the focused option")
	o.HandleQuestionKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyRune, Rune: 'j'})
	o.HandleQuestionKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyRune, Rune: ' '})
	require.True(t, o.question.picked[1])
	o.HandleQuestionKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyEnter})

	r := <-reply
	assert.Equal(t, []string{"a", "b"}, r.Choices)
}

func TestQuestionSupersedesPermissionAsk(t *testing.T) {
	activity := controller.NewActivityHandler(nil)
	o := testOverlays(activity)
	perm := make(chan controller.AskReply, 1)
	o.beginPermissionAsk(controller.OverlayMsg{
		Kind:      controller.OverlayPermissionAsk,
		Request:   permission.Request{Action: permission.ActionBash, Tool: "bash", Command: "ls"},
		Reason:    "x",
		PermReply: perm,
	})
	require.NotNil(t, o.perm)

	q := make(chan controller.QuestionReply, 1)
	o.beginQuestionAsk(controller.OverlayMsg{
		Kind: controller.OverlayQuestionAsk,
		Question: &controller.Question{
			Question: "?",
			Options:  []controller.QuestionOption{{Label: "a"}, {Label: "b"}},
		},
		QuestReply: q,
	})
	require.Nil(t, o.perm, "the question replaces a live permission prompt")
	require.NotNil(t, o.question)
	// The superseded permission is answered as a reject so the turn unblocks.
	r := <-perm
	assert.False(t, r.Approved)
}
