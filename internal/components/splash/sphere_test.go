package splash

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
)

func TestSphereDrawFillsEllipse(t *testing.T) {
	sphere := &Sphere{Width: 20, Height: 20, Time: 0.5}
	surf := sphere.Draw(components.DrawContext{
		Max:    components.Size{Width: 20, Height: 20},
		Method: xui.WidthUnicode,
	})
	require.Equal(t, 20, surf.Size.Width, "surface width")
	require.Equal(t, 20, surf.Size.Height, "surface height")
	nonEmpty := 0
	for _, c := range surf.Buffer {
		if c.Char != "" && c.Char != " " {
			nonEmpty++
		}
	}
	require.GreaterOrEqual(t, nonEmpty, 40, "expected sphere cells")
}
