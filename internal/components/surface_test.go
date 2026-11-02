package components

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/xui"
)

func TestSurfaceRenderWideCharAfterFill(t *testing.T) {
	// ChatInput fills the body with non-default spaces then Print()'s CJK.
	// Rendering must not paint the leftover column under a width-2 glyph.
	screen := xui.NewScreen(10, 1)
	win := xui.NewWindow(screen)
	win.Clear()

	s := NewSurface(10, 1, nil)
	for x := range 10 {
		s.SetCell(x, 0, xui.Cell{Char: " ", Width: 1})
	}
	s.Print(0, 0, "中文", xui.Style{}, xui.WidthUnicode)
	s.Render(win)

	cell0 := screen.GetCell(0, 0)
	require.Equal(t, "中", cell0.Char, "cell0 char")
	require.Equal(t, uint8(2), cell0.Width, "cell0 width")
	// Column 1 is the wide-char trail owned by screen.SetCell — not a second glyph.
	cell1 := screen.GetCell(1, 0)
	require.Equal(t, " ", cell1.Char, "cell1 char")
	require.Equal(t, uint8(1), cell1.Width, "cell1 width")
	require.True(t, cell1.Trail, "cell1 trail")
	cell2 := screen.GetCell(2, 0)
	require.Equal(t, "文", cell2.Char, "cell2 char")
	require.Equal(t, uint8(2), cell2.Width, "cell2 width")
	// No gap: column 1 must not be an independent printable CJK / reverse block.
	c1Char := screen.GetCell(1, 0).Char
	require.NotEqual(t, "中", c1Char, "continuation column overwritten with a real glyph")
	require.NotEqual(t, "文", c1Char, "continuation column overwritten with a real glyph")
}

// TestSurfaceRenderClipsChildren ensures scrolled content cannot paint past the parent box
// (the MessageList / ScrollView leak into tui/footer).
func TestSurfaceRenderClipsChildren(t *testing.T) {
	screen := xui.NewScreen(20, 8)
	win := xui.NewWindow(screen)
	win.Clear()

	leak := NewSurface(18, 4, nil)
	leak.Print(0, 0, "AAAA", xui.Style{}, xui.WidthUnicode)
	leak.Print(0, 1, "BBBB", xui.Style{}, xui.WidthUnicode)
	leak.Print(0, 2, "CCCC", xui.Style{}, xui.WidthUnicode)
	leak.Print(0, 3, "DDDD", xui.Style{}, xui.WidthUnicode)

	list := Surface{
		Size:   Size{Width: 20, Height: 3},
		Widget: nil,
		Children: []SubSurface{{
			Origin:  Point{X: 0, Y: -1}, // scroll: first row off-screen, last row past list
			Surface: leak,
		}},
	}
	root := Surface{
		Size: Size{Width: 20, Height: 8},
		Children: []SubSurface{
			{Origin: Point{X: 0, Y: 0}, Surface: list},
			{Origin: Point{X: 0, Y: 3}, Surface: NewSurface(20, 5, nil)}, // tui zone
		},
	}
	root.Render(win)

	// Visible list rows: leak rows 1..2 → BBBB, CCCC at screen y=0,1
	require.Equal(t, "B", screen.GetCell(0, 0).Char, "y0")
	require.Equal(t, "C", screen.GetCell(0, 1).Char, "y1")
	// DDDD would be at list-local y=3 which is outside list height 3 — must not leak into tui.
	for y := 3; y < 8; y++ {
		ch := screen.GetCell(0, y).Char
		require.NotEqual(t, "D", ch, "leaked D into row %d", y)
	}
	// AAAA was above the clip (Y=-1) — must not appear.
	for y := range 8 {
		require.NotEqual(t, "A", screen.GetCell(0, y).Char, "leaked A at row %d", y)
	}
}
