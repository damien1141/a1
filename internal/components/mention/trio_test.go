package mention

import (
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

func drawCtx() components.DrawContext {
	return components.DrawContext{Max: components.Size{Width: 80, Height: 24}, Method: xui.WidthUnicode}
}

// panelRows extracts one row per line from a picker panel surface.
func panelRows(t *testing.T, s components.Surface) []string {
	t.Helper()
	return strings.Split(strings.TrimSuffix(components.SurfaceText(s), "\n"), "\n")
}

// ---------- negative ----------

// Closed pickers and out-of-range selections must never fire callbacks; the
// picker still closes so Enter cannot leave a stuck overlay.
func TestPickerApplyGuards_Negative(t *testing.T) {
	var accepted, completed, cancelled int
	p := &Picker{
		OnAccept:   func(Item) { accepted++ },
		OnComplete: func(Item) { completed++ },
		OnCancel:   func() { cancelled++ },
	}

	// Closed: everything is a no-op, callbacks stay silent.
	require.False(t, p.Accept())
	require.False(t, p.Complete())
	p.Cancel()
	assert.Zero(t, accepted)
	assert.Zero(t, completed)
	assert.Zero(t, cancelled)

	// Open with an out-of-range selection: closes without applying.
	p.Items = []Item{{Path: "a"}}
	p.Show()
	p.Selected = 99
	require.False(t, p.Accept())
	assert.False(t, p.Open)
	assert.Zero(t, accepted)

	p.Show()
	p.Selected = -1
	require.False(t, p.Accept())
	assert.False(t, p.Open)
	assert.Zero(t, accepted)

	// Empty results: Tab completes nothing and still closes.
	p.SetResults(nil, "No matching commands")
	p.Show()
	require.False(t, p.Complete())
	assert.False(t, p.Open)
	assert.Zero(t, completed)
}

// Navigation clamps at both ends, releases are ignored, plain runes belong to
// the composer, and a closed picker consumes nothing.
func TestPickerHandleNavGuards_Negative(t *testing.T) {
	p := &Picker{Items: []Item{{Path: "a"}, {Path: "b"}}}
	p.Show()

	require.False(t, p.HandleNav(xui.KeyEvent{Press: false, Code: xui.KeyDown}), "release events are not consumed")
	assert.Equal(t, 0, p.Selected)

	require.True(t, p.HandleNav(xui.KeyEvent{Press: true, Code: xui.KeyUp}))
	assert.Equal(t, 0, p.Selected, "up at the top clamps")

	p.Selected = 1
	require.True(t, p.HandleNav(xui.KeyEvent{Press: true, Code: xui.KeyDown}))
	assert.Equal(t, 1, p.Selected, "down at the bottom clamps")

	require.False(t, p.HandleNav(xui.KeyEvent{Press: true, Code: xui.KeyRune, Rune: 'x'}),
		"plain runes are not consumed; the composer owns typing")

	p.Hide()
	ctx := &components.EventContext{}
	p.Handle(ctx, xui.KeyEvent{Press: true, Code: xui.KeyEnter})
	assert.False(t, ctx.Consume, "closed picker consumes nothing")
	assert.False(t, ctx.Redraw)
}

// Degenerate anchors and hostile item text must clamp the panel on-screen
// and truncate the label instead of bleeding past the border.
func TestPickerDrawHostileAnchors_Negative(t *testing.T) {
	// Anchor at the top edge clamps the panel to row 0 with one visible row.
	p := &Picker{
		Theme:         components.DefaultTheme(),
		Items:         []Item{{Path: "a"}, {Path: "b"}},
		AnchorWidth:   40,
		AnchorBottomY: 0,
	}
	p.Show()
	s := p.Draw(drawCtx())
	require.Len(t, s.Children, 1)
	assert.Equal(t, 0, s.Children[0].Origin.Y)
	assert.Equal(t, 3, s.Children[0].Surface.Size.Height, "one visible row plus borders")

	// Anchor X past the right edge clamps so the panel stays inside.
	p.AnchorX = 100
	s = p.Draw(drawCtx())
	require.Len(t, s.Children, 1)
	assert.Equal(t, 40, s.Children[0].Origin.X, "clamped to maxW-boxW")

	// Zero-size context falls back to the 80x24 defaults.
	p.AnchorX = 0
	s = p.Draw(components.DrawContext{Max: components.Size{}, Method: xui.WidthUnicode})
	require.Len(t, s.Children, 1)

	// A hostile 200-char path truncates inside the panel, border intact.
	q := &Picker{
		Theme:         components.DefaultTheme(),
		Items:         []Item{{Path: strings.Repeat("x", 200)}},
		AnchorWidth:   40,
		AnchorBottomY: 10,
	}
	q.Show()
	s = q.Draw(drawCtx())
	require.Len(t, s.Children, 1)
	rows := panelRows(t, s.Children[0].Surface)
	require.Len(t, rows, 3)
	assert.Equal(t, "│ @"+strings.Repeat("x", 36)+"│", rows[1], "label clipped to the panel width")
}

// ---------- differential ----------

// oracle: hand-written golden of the full panel for a fixed state — bordered
// box anchored above the composer, "@path" labels with a two-space gap before
// the description, columns derived by hand from the Draw layout rules.
func TestPickerDrawGoldenRows_Differential(t *testing.T) {
	p := &Picker{
		Theme: components.DefaultTheme(),
		Items: []Item{
			{Path: "go.mod", Description: "Module file"},
			{Path: "a/b.go"},
		},
		AnchorBottomY: 10,
		AnchorWidth:   40,
		AnchorX:       0,
	}
	p.Show()
	s := p.Draw(drawCtx())
	require.Len(t, s.Children, 1)
	child := s.Children[0]
	require.Equal(t, components.Size{Width: 40, Height: 4}, child.Surface.Size)
	assert.Equal(t, components.Point{X: 0, Y: 6}, child.Origin, "panel sits just above the anchor row")

	want := []string{
		"╭" + strings.Repeat("─", 38) + "╮",
		"│ @go.mod  Module file" + strings.Repeat(" ", 17) + "│",
		"│ @a/b.go" + strings.Repeat(" ", 30) + "│",
		"╰" + strings.Repeat("─", 38) + "╯",
	}
	assert.Equal(t, want, panelRows(t, child.Surface))

	// The selected first row carries the picker's soft blue bar, the second
	// row does not, and the text is identical either way.
	selBg := xui.RGBColor(0x3a, 0x5a, 0x7a)
	panel := child.Surface
	assert.Equal(t, selBg, panel.Buffer[1*40+30].Style.Bg, "selected row is washed past its text")
	assert.NotEqual(t, selBg, panel.Buffer[2*40+5].Style.Bg, "unselected row stays plain")

	// Slash-command mode swaps the prefix; the gap and description geometry
	// are unchanged.
	sp := &Picker{
		Theme:         components.DefaultTheme(),
		Prefix:        "/",
		Items:         []Item{{Path: "clear", Description: "Clear screen"}},
		AnchorBottomY: 10,
		AnchorWidth:   40,
	}
	sp.Show()
	ss := sp.Draw(drawCtx())
	require.Len(t, ss.Children, 1)
	wantSlash := []string{
		"╭" + strings.Repeat("─", 38) + "╮",
		"│ /clear  Clear screen" + strings.Repeat(" ", 17) + "│",
		"╰" + strings.Repeat("─", 38) + "╯",
	}
	assert.Equal(t, wantSlash, panelRows(t, ss.Children[0].Surface))
}
