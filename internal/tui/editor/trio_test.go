package editor

import (
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/tui/controller"
)

// Nil and zero-value editors: history helpers guard a nil receiver, and a
// zero-value shell reports "nothing blocks the composer" and drops keys and
// bus messages instead of dereferencing nil panes.
func TestEditor_ZeroValueGuards_Negative(t *testing.T) {
	var nilEditor *Editor
	assert.NotPanics(t, func() {
		nilEditor.storeHistory("x")
		nilEditor.loadInputHistory()
	})

	e := &Editor{}
	assert.False(t, e.blocksComposer(), "no panes wired: nothing blocks the composer")

	ctx := &components.EventContext{}
	assert.False(t, e.handleMessageActionsKey(ctx, xui.KeyEvent{
		Press: true, Code: xui.KeyUp, Mods: xui.ModShift,
	}), "shift+up with no transcript falls through")
	assert.False(t, ctx.Consume)

	assert.NotPanics(t, func() {
		e.showActionsHint()
		e.Publish(controller.BranchLabelMsg{Text: "x"})
		e.RequestRedraw()
	})
}

// Hostile /code arguments: whitespace-only args toast the usage line, and a
// ":line" suffix that is not a positive integer silently becomes part of the
// filename, which then fails to load — pinned as the current behavior.
func TestOpenCode_HostileArgs_Negative(t *testing.T) {
	e := newTestEditor(t)

	e.openCode([]string{"   ", "   "})
	assert.False(t, e.code.Active(), "whitespace-only args do not open the pane")
	assert.True(t, e.toast.Visible(), "the usage toast is shown instead")

	e.openCode([]string{"a.go:0"})
	require.True(t, e.code.Active())
	text := components.SurfaceText(frame(t, e))
	assert.Contains(t, text, "no such file", "a :0 suffix is treated as a filename, not a line")
	assert.Contains(t, text, "a.go:0")

	e.openCode([]string{"a.go:abc"})
	require.True(t, e.code.Active())
	text = components.SurfaceText(frame(t, e))
	assert.Contains(t, text, "no such file")
	assert.Contains(t, text, "a.go:abc")
}

// Tiny terminals: the frame must still lay out three bands without panicking,
// with the footer pinned to the last row even when the bands no longer fit.
func TestDraw_TinyTerminal_Negative(t *testing.T) {
	e := newTestEditor(t)
	for _, sz := range []components.Size{
		{Width: 30, Height: 8}, {Width: 20, Height: 6}, {Width: 10, Height: 4},
	} {
		var surf components.Surface
		assert.NotPanics(t, func() {
			surf = e.Draw(components.DrawContext{Max: sz, Method: xui.WidthUnicode})
		}, "draw at %v", sz)
		require.Len(t, surf.Children, 3, "three bands at %v", sz)
		assert.Equal(t, sz.Height-1, surf.Children[2].Origin.Y, "footer pinned to the last row at %v", sz)
	}
}

// oracle: the three-band layout contract, recomputed by arithmetic — the list
// starts at the origin, the chat starts where the list surface ends, the chat
// surface ends where the footer row begins, and the footer sits on the last row.
func TestDraw_LayoutContract_Differential(t *testing.T) {
	e := newTestEditor(t)
	for _, sz := range []components.Size{
		{Width: 100, Height: 30}, {Width: 80, Height: 24}, {Width: 60, Height: 16},
	} {
		root := e.Draw(components.DrawContext{Max: sz, Method: xui.WidthUnicode})
		require.Len(t, root.Children, 3, "bands at %v", sz)

		list, chat, footerRow := root.Children[0], root.Children[1], root.Children[2]
		assert.Equal(t, components.Point{X: 0, Y: 0}, list.Origin, "list at origin at %v", sz)
		assert.Equal(t, sz.Height-1, footerRow.Origin.Y, "footer on the last row at %v", sz)
		assert.Equal(t, list.Origin.Y+list.Surface.Size.Height, chat.Origin.Y,
			"chat starts where the list ends at %v", sz)
		assert.Equal(t, footerRow.Origin.Y, chat.Origin.Y+chat.Surface.Size.Height,
			"chat ends where the footer starts at %v", sz)
		assert.GreaterOrEqual(t, list.Surface.Size.Height, 3, "list keeps a floor of three rows at %v", sz)
	}
}

// oracle: hand-derived golden table of the ":line" suffix parser — the first
// colon cuts, the tail must parse as a positive integer (Atoi takes "+3" and
// "003"), the resulting line clamps to the file, and a non-numeric tail keeps
// the whole token as the path.
func TestOpenCode_LineSuffixParsing_GoldenTable_Differential(t *testing.T) {
	cases := []struct {
		args  []string
		want  string
		fails bool
	}{
		{args: []string{"a.go:+3"}, want: "a.go:3:1"},
		{args: []string{"a.go:003"}, want: "a.go:3:1"},
		{args: []string{"a.go:999"}, want: "a.go:3:1"},
		{args: []string{"a.go:3:5"}, want: "no such file", fails: true},
		{args: []string{"a.go", "b.go"}, want: "no such file", fails: true},
	}
	for _, c := range cases {
		e := newTestEditor(t)
		e.openCode(c.args)
		require.True(t, e.code.Active(), "args %q", c.args)
		text := components.SurfaceText(frame(t, e))
		assert.Contains(t, text, c.want, "args %q", c.args)
	}
}
