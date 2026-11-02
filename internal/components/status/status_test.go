package status

import (
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpinnerGlyphs(t *testing.T) {
	sp := NewSpinner(xui.Style{})
	glyphs := map[string]struct{}{}
	for i := range 20 {
		g := sp.Glyph()
		assert.Equal(t, 1, xui.StringWidth(g, xui.WidthUnicode), "frame %d glyph %q", i, g)
		glyphs[g] = struct{}{}
		sp.Tick()
	}
	assert.Len(t, glyphs, 10)
}

func TestSpinnerFlowTravels(t *testing.T) {
	sp := NewSpinner(xui.Style{})
	text := "Generating…"
	litAt := func(frame int) []bool {
		sp.Frame = frame
		var flags []bool
		sp.ForEachFlowCell(text, func(_ string, lit bool) {
			flags = append(flags, lit)
		})
		return flags
	}

	a := litAt(0)
	b := litAt(3)
	require.Len(t, a, len(graphemeClusters(text)))
	assert.NotEqual(t, a, b, "highlight should travel across frames")

	litCount := 0
	for _, lit := range a {
		if lit {
			litCount++
		}
	}
	assert.Equal(t, flowTrail, litCount)
}
