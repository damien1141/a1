package components

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/xui"
)

func TestCJKTrailNotPaintedColumnByColumn(t *testing.T) {
	s := NewSurface(10, 1, nil)
	s.Print(0, 0, "笔记", xui.Style{}, xui.WidthUnicode)
	require.True(t, s.Buffer[1].Trail, "expected trail at col 1, got %+v", s.Buffer[1])
	screen := xui.NewScreen(10, 1)
	win := xui.NewWindow(screen)
	win.Clear()
	// Anti-pattern: paint every non-default cell. Trail must be skipped.
	for x := 0; x < s.Size.Width; x++ {
		c := s.Buffer[x]
		if !c.Default && !c.Trail {
			win.SetCell(x, 0, c)
		}
	}
	got0 := screen.GetCell(0, 0)
	require.Equal(t, "笔", got0.Char, "primary")
	require.Equal(t, uint8(2), got0.Width, "primary width")
	got1 := screen.GetCell(1, 0)
	require.True(t, got1.Trail, "screen trail = %+v", got1)
	r := xui.NewRenderer()
	screen.MarkRefresh()
	var buf strings.Builder
	_, err := r.RenderDiff(&buf, screen.Diff(), -1, -1, false, 0)
	require.NoError(t, err)
	require.NotContains(t, buf.String(), "笔\x1b[1;2H", "ANSI writes into trail column after 笔")
}
