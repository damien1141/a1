package toast

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
)

func TestToastDrawSuccess(t *testing.T) {
	toast := Toast{Theme: components.DefaultTheme()}
	toast.Show("Selection copied to clipboard", ToastSuccess, time.Second)
	s := toast.Draw(components.DrawContext{Max: components.Size{Width: 80, Height: 24}, Method: xui.WidthUnicode})
	require.Len(t, s.Children, 1)
	panel := s.Children[0].Surface
	var row strings.Builder
	for x := 0; x < panel.Size.Width; x++ {
		ch := panel.Buffer[panel.Size.Width+x].Char // y=1 content row
		if ch == "" {
			ch = " "
		}
		row.WriteString(ch)
	}
	got := row.String()
	require.Contains(t, got, "Selection copied")
	require.Contains(t, got, "✓")
}
