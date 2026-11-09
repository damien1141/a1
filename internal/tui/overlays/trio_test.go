package overlays

import (
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/tui/controller"
)

// Blank confirm fields fall back to Yes/No labels, hostile keys are consumed
// without changing the selection, and resolving without a reply channel or an
// active overlay is a no-op rather than a panic.
func TestConfirmOverlay_HostileInput_Negative(t *testing.T) {
	o := testOverlays(controller.NewActivityHandler(nil))
	ctx := &components.EventContext{}

	assert.False(t, o.HandleConfirmKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyEnter}),
		"no confirm open: the key falls through")
	assert.False(t, o.ConfirmActive())

	reply := make(chan controller.ExtConfirmReply, 1)
	o.beginExtConfirm(controller.OverlayMsg{Kind: controller.OverlayExtConfirm, ConfirmReply: reply})
	require.True(t, o.ConfirmActive())
	assert.Equal(t, "Yes", o.confirm.yes, "blank yes label defaults")
	assert.Equal(t, "No", o.confirm.no, "blank no label defaults")

	assert.False(t, o.HandleConfirmKey(ctx, xui.KeyEvent{Press: false, Code: xui.KeyEnter}),
		"key releases are ignored")
	require.True(t, o.ConfirmActive())
	assert.True(t, o.HandleConfirmKey(ctx, xui.KeyEvent{
		Press: true, Code: xui.KeyRune, Rune: 'z', Mods: xui.ModCtrl,
	}), "even a hostile chord is consumed while the overlay owns keys")
	require.True(t, o.ConfirmActive())
	assert.Equal(t, 0, o.confirm.selected, "the chord changed nothing")

	assert.True(t, o.HandleConfirmKey(ctx, xui.KeyEvent{Press: true, Code: xui.KeyEscape}))
	assert.False(t, o.ConfirmActive())
	select {
	case r := <-reply:
		assert.False(t, r.OK, "escape answers no")
	default:
		require.Fail(t, "escape must reply")
	}

	// No reply channel wired: resolving still clears the overlay without blocking.
	o.beginExtConfirm(controller.OverlayMsg{Kind: controller.OverlayExtConfirm})
	require.Nil(t, o.confirm.reply)
	assert.NotPanics(t, func() {
		o.ResolveConfirm(controller.ExtConfirmReply{OK: true})
	})
	assert.False(t, o.ConfirmActive())
}

// Boundary clamps for the confirm panel height: the bare frame is 8 rows, the
// message adds one row per line, and the panel stops growing at 16.
func TestConfirmPreferredAskHeight_Clamps_Negative(t *testing.T) {
	st := newConfirmAskState(controller.OverlayMsg{})
	assert.Equal(t, 8, st.preferredAskHeight(), "no message: the bare frame")

	one := newConfirmAskState(controller.OverlayMsg{Message: "single line"})
	assert.Equal(t, 9, one.preferredAskHeight(), "one message line adds one row")

	exact := newConfirmAskState(controller.OverlayMsg{Message: strings.Repeat("x\n", 8)})
	assert.Equal(t, 16, exact.preferredAskHeight(), "eight lines hit the cap exactly")

	huge := newConfirmAskState(controller.OverlayMsg{Message: strings.Repeat("x\n", 40)})
	assert.Equal(t, 16, huge.preferredAskHeight(), "a message dump is clamped at the cap")
}

// Degenerate panel geometry: zero or negative width falls back to 80 columns,
// zero height to the preferred height, and a narrow panel still paints.
func TestDrawExtConfirm_DegenerateDimensions_Negative(t *testing.T) {
	o := testOverlays(controller.NewActivityHandler(nil))
	o.beginExtConfirm(controller.OverlayMsg{Kind: controller.OverlayExtConfirm})
	require.True(t, o.ConfirmActive())

	confirmCtx := components.DrawContext{Max: components.Size{Width: 60, Height: 12}, Method: xui.WidthUnicode}
	surf, ok := o.DrawBottom(confirmCtx, 0, 0)
	require.True(t, ok)
	assert.Equal(t, 80, surf.Size.Width, "zero width falls back to 80")
	assert.Equal(t, o.confirm.preferredAskHeight(), surf.Size.Height)
	assert.Contains(t, components.SurfaceText(surf), "Confirm", "the missing title defaults")

	narrowCtx := components.DrawContext{Max: components.Size{Width: 12, Height: 12}, Method: xui.WidthUnicode}
	narrow, ok := o.DrawBottom(narrowCtx, 12, 0)
	require.True(t, ok)
	assert.Equal(t, 12, narrow.Size.Width)
	assert.Positive(t, narrow.Size.Height)
}

// A question overlay without a question payload never opens, and dismissing or
// drawing with nothing active is a no-op.
func TestQuestionOverlay_MissingPayload_Negative(t *testing.T) {
	o := testOverlays(controller.NewActivityHandler(nil))

	o.Apply(controller.OverlayMsg{Kind: controller.OverlayQuestionAsk})
	assert.False(t, o.QuestionActive(), "a nil question never opens the overlay")

	assert.NotPanics(t, func() { o.Apply(controller.OverlayMsg{Kind: controller.OverlayQuestionDismiss}) })

	surf, ok := o.DrawBottom(components.DrawContext{Max: components.Size{Width: 40, Height: 10}}, 40, 10)
	assert.False(t, ok)
	assert.Equal(t, components.Surface{}, surf)

	h, overlay := o.PreferredBottomHeight(40, xui.WidthUnicode)
	assert.False(t, overlay)
	assert.Equal(t, 0, h)
}

// Long bash command dumps are truncated to three lines plus an ellipsis, and
// empty edit/write requests render an empty detail rather than panicking.
func TestFormatAskHeader_HostileRequests_Negative(t *testing.T) {
	h, d := formatAskHeader(permission.Request{Action: permission.ActionBash, Command: "l1\nl2\nl3\nl4\nl5"})
	assert.Equal(t, "Run this command?", h)
	assert.Equal(t, "l1\nl2\nl3\n...", d, "only the first three lines survive")

	h, d = formatAskHeader(permission.Request{Action: permission.ActionBash, Command: "one\ntwo\nthree"})
	assert.Equal(t, "one\ntwo\nthree", d, "three lines are shown in full")

	h, d = formatAskHeader(permission.Request{Action: permission.ActionBash})
	assert.Equal(t, "Run this command?", h)
	assert.Empty(t, d, "an empty command renders an empty detail")

	h, d = formatAskHeader(permission.Request{Action: permission.ActionEdit})
	assert.Equal(t, "Allow editing file:", h)
	assert.Empty(t, d, "an edit request without paths has no detail")
}

// oracle: hand-derived golden table of the confirm key-routing spec — which
// keys move the selection, which resolve immediately, and what each resolves to.
func TestConfirmKeyRouting_GoldenStateMachine_Differential(t *testing.T) {
	ctx := &components.EventContext{}
	type step struct {
		keys    []xui.KeyEvent
		consume []bool
		wantOK  *bool // nil: the overlay must stay open
	}
	yes, no := true, false
	key := func(code xui.KeyCode) xui.KeyEvent { return xui.KeyEvent{Press: true, Code: code} }
	rk := func(r rune) xui.KeyEvent { return xui.KeyEvent{Press: true, Code: xui.KeyRune, Rune: r} }
	cases := []step{
		{keys: []xui.KeyEvent{key(xui.KeyEnter)}, consume: []bool{true}, wantOK: &yes},
		{keys: []xui.KeyEvent{key(xui.KeyRight), key(xui.KeyEnter)}, consume: []bool{true, true}, wantOK: &no},
		{keys: []xui.KeyEvent{key(xui.KeyDown), key(xui.KeyEnter)}, consume: []bool{true, true}, wantOK: &no},
		{keys: []xui.KeyEvent{key(xui.KeyTab), key(xui.KeyEnter)}, consume: []bool{true, true}, wantOK: &no},
		{keys: []xui.KeyEvent{key(xui.KeyLeft), key(xui.KeyEnter)}, consume: []bool{true, true}, wantOK: &yes},
		{keys: []xui.KeyEvent{rk('y')}, consume: []bool{true}, wantOK: &yes},
		{keys: []xui.KeyEvent{rk('n')}, consume: []bool{true}, wantOK: &no},
		{keys: []xui.KeyEvent{rk('N')}, consume: []bool{true}, wantOK: &no},
		{keys: []xui.KeyEvent{rk('l'), rk('y')}, consume: []bool{true, true}, wantOK: &yes},
		{keys: []xui.KeyEvent{key(xui.KeyEscape)}, consume: []bool{true}, wantOK: &no},
		{keys: []xui.KeyEvent{rk('j')}, consume: []bool{true}, wantOK: nil},
		{keys: []xui.KeyEvent{rk('h')}, consume: []bool{true}, wantOK: nil},
	}

	for i, c := range cases {
		o := testOverlays(controller.NewActivityHandler(nil))
		reply := make(chan controller.ExtConfirmReply, 1)
		o.beginExtConfirm(controller.OverlayMsg{Kind: controller.OverlayExtConfirm, ConfirmReply: reply})
		require.True(t, o.ConfirmActive(), "case %d", i)

		for j, k := range c.keys {
			assert.Equal(t, c.consume[j], o.HandleConfirmKey(ctx, k), "case %d key %d", i, j)
		}

		if c.wantOK == nil {
			assert.True(t, o.ConfirmActive(), "case %d: navigation keys keep the overlay open", i)
			assert.Empty(t, reply, "case %d: nothing replied yet", i)
			continue
		}
		assert.False(t, o.ConfirmActive(), "case %d: a resolving key closes the overlay", i)
		select {
		case r := <-reply:
			assert.Equal(t, *c.wantOK, r.OK, "case %d reply", i)
		default:
			require.Fail(t, "case %d: expected a reply", i)
		}
	}
}

// oracle: hand-built golden frame — rounded border, title at row 1, message at
// row 2, blank row, selected "▸● Ship it" / unselected " ○ Hold" option rows,
// hint row, blank interior, bottom border; every row assembled with
// strings.Repeat arithmetic on the fixed 60x12 geometry.
func TestDrawExtConfirm_GoldenPanel_Differential(t *testing.T) {
	o := testOverlays(controller.NewActivityHandler(nil))
	o.beginExtConfirm(controller.OverlayMsg{
		Kind:    controller.OverlayExtConfirm,
		Title:   "Deploy",
		Message: "Push now?",
		Yes:     "Ship it",
		No:      "Hold",
	})
	require.True(t, o.ConfirmActive())

	const w, h = 60, 12
	inner := w - 2
	pad := func(text string, rowLen int) string {
		return "│ " + text + strings.Repeat(" ", inner-1-len([]rune(text))) + "│"
	}
	blank := "│" + strings.Repeat(" ", inner) + "│"
	expected := []string{
		"╭" + strings.Repeat("─", inner) + "╮",
		pad("Deploy", inner),
		pad("Push now?", inner),
		blank,
		pad("▸● Ship it", inner),
		pad(" ○ Hold", inner),
		pad("←→ move · Enter confirm · Y yes · N/Esc cancel", inner),
		blank, blank, blank, blank,
		"╰" + strings.Repeat("─", inner) + "╯",
	}

	surf, ok := o.DrawBottom(components.DrawContext{Max: components.Size{Width: w, Height: h}, Method: xui.WidthUnicode}, w, h)
	require.True(t, ok)
	require.Equal(t, w, surf.Size.Width)
	require.Equal(t, h, surf.Size.Height)
	rows := strings.Split(strings.TrimSuffix(components.SurfaceText(surf), "\n"), "\n")
	require.Len(t, rows, h)
	assert.Equal(t, expected, rows)
}
