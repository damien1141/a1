package toast

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
)

func TestToast_InvisibleStatesRenderNothing_Negative(t *testing.T) {
	ctx := components.DrawContext{Max: components.Size{Width: 80, Height: 24}, Method: xui.WidthUnicode}

	expired := &Toast{Theme: components.DefaultTheme(), Message: "too late", Until: time.Now().Add(-time.Hour)}
	emptyMsg := &Toast{Theme: components.DefaultTheme(), Message: "", Until: time.Now().Add(time.Hour)}
	cleared := &Toast{Theme: components.DefaultTheme()}
	cleared.Show("gone", ToastInfo, time.Second)
	cleared.Clear()

	for name, tt := range map[string]*Toast{
		"zero value":    {},
		"expired":       expired,
		"empty message": emptyMsg,
		"cleared":       cleared,
	} {
		assert.False(t, tt.Visible(), "%s: Visible must be false", name)
		s := tt.Draw(ctx)
		assert.Equal(t, 80, s.Size.Width, "%s: host width kept", name)
		assert.Equal(t, 24, s.Size.Height, "%s: host height kept", name)
		assert.Empty(t, s.Children, "%s: hidden toast draws no panel", name)
	}
}

func TestToastShow_NonPositiveDurationDefaults_Negative(t *testing.T) {
	tt := &Toast{}
	for _, d := range []time.Duration{0, -time.Second} {
		before := time.Now()
		tt.Show("saved", ToastSuccess, d)
		assert.Equal(t, "saved", tt.Message, "duration %v: message stored", d)
		assert.Equal(t, ToastSuccess, tt.Kind, "duration %v: kind stored", d)
		// The default 2s window is granted in full: Until is built from a
		// time.Now() taken at or after `before`, so it is never shorter.
		assert.GreaterOrEqual(t, tt.Until.Sub(before), 2*time.Second,
			"duration %v must fall back to the 2s default", d)
		assert.True(t, tt.Visible(), "duration %v: toast is live", d)
	}
}

func TestToastDraw_TinyAndZeroScreens_Negative(t *testing.T) {
	tt := &Toast{Theme: components.DefaultTheme()}
	tt.Show("Saved", ToastSuccess, time.Hour)

	// A 6×2 screen cannot honor any of the width rules; the final 8-column
	// floor wins and the panel overflows the host instead of collapsing.
	tiny := tt.Draw(components.DrawContext{Max: components.Size{Width: 6, Height: 2}, Method: xui.WidthUnicode})
	require.Len(t, tiny.Children, 1, "toast stays visible on a tiny screen")
	assert.Equal(t, 8, tiny.Children[0].Surface.Size.Width, "8-column floor beats the 6-column host")
	assert.Equal(t, 2, tiny.Children[0].Surface.Size.Height, "box height is capped by the host")
	assert.Equal(t, components.Point{X: 0, Y: 0}, tiny.Children[0].Origin, "negative centering clamps to 0")

	// Zero-size context falls back to the 80×24 defaults, not a 0×0 host.
	zero := tt.Draw(components.DrawContext{Method: xui.WidthUnicode})
	assert.Equal(t, 80, zero.Size.Width)
	assert.Equal(t, 24, zero.Size.Height)
	require.Len(t, zero.Children, 1)
	assert.Equal(t, 1, zero.Children[0].Origin.Y, "default position is one row below the top")
}

// oracle: hand-written golden panels — the exact three rendered rows for
// fixed (message, kind) states on an 80×24 screen, derived from the box rule
// by hand (content width = 1+icon+1+len(msg)+1 plus borders; 80*95/100=76 is
// never the binding constraint for these short messages), never captured from
// Draw. Icons: success ✓, error ✗, warning/info none.
func TestToastDraw_MatchesGoldenPanels_Differential(t *testing.T) {
	ctx := components.DrawContext{Max: components.Size{Width: 80, Height: 24}, Method: xui.WidthUnicode}

	cases := []struct {
		name    string
		message string
		kind    ToastKind
		originX int
		rows    string
	}{
		{
			name: "success", message: "Saved", kind: ToastSuccess, originX: 34,
			rows: "╭─────────╮\n│✓ Saved  │\n╰─────────╯\n",
		},
		{
			name: "error", message: "Boom", kind: ToastError, originX: 35,
			rows: "╭────────╮\n│✗ Boom  │\n╰────────╯\n",
		},
		{
			name: "warning has no icon", message: "Careful", kind: ToastWarning, originX: 34,
			rows: "╭─────────╮\n│Careful  │\n╰─────────╯\n",
		},
		{
			name: "info has no icon", message: "Info", kind: ToastInfo, originX: 36,
			rows: "╭──────╮\n│Info  │\n╰──────╯\n",
		},
	}
	for _, tc := range cases {
		tt := &Toast{Theme: components.DefaultTheme()}
		tt.Show(tc.message, tc.kind, time.Hour)
		s := tt.Draw(ctx)
		require.Len(t, s.Children, 1, "%s: one panel", tc.name)
		panel := s.Children[0]
		assert.Equal(t, tc.originX, panel.Origin.X, "%s: centered origin (80-boxW)/2", tc.name)
		assert.Equal(t, 1, panel.Origin.Y, "%s: one row below the top", tc.name)
		assert.Equal(t, 50, panel.Z, "%s: overlay z-order", tc.name)
		assert.Equal(t, tc.rows, components.SurfaceText(panel.Surface), "%s: rendered rows", tc.name)
	}

	// A 200-column message on the 76-column box is cut to the available
	// columns: avail = 76-1-3 = 72 after the "✓ " icon block.
	tt := &Toast{Theme: components.DefaultTheme()}
	tt.Show(strings.Repeat("x", 200), ToastSuccess, time.Hour)
	s := tt.Draw(ctx)
	require.Len(t, s.Children, 1)
	assert.Equal(t, 2, s.Children[0].Origin.X, "(80-76)/2")
	expected := "╭" + strings.Repeat("─", 74) + "╮\n" +
		"│✓ " + strings.Repeat("x", 72) + "│\n" +
		"╰" + strings.Repeat("─", 74) + "╯\n"
	assert.Equal(t, expected, components.SurfaceText(s.Children[0].Surface), "truncated ascii message")

	// Wide glyphs cut on cell boundaries: 72 columns hold exactly 36 CJK
	// ideographs.
	tt = &Toast{Theme: components.DefaultTheme()}
	tt.Show(strings.Repeat("日", 100), ToastSuccess, time.Hour)
	s = tt.Draw(ctx)
	require.Len(t, s.Children, 1)
	expected = "╭" + strings.Repeat("─", 74) + "╮\n" +
		"│✓ " + strings.Repeat("日", 36) + "│\n" +
		"╰" + strings.Repeat("─", 74) + "╯\n"
	assert.Equal(t, expected, components.SurfaceText(s.Children[0].Surface), "truncated wide-glyph message")
}
