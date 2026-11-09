package splash

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
)

func TestWrapHint_DegenerateInput_Negative(t *testing.T) {
	assert.Nil(t, wrapHint("", 48), "empty hint renders no lines")
	assert.Nil(t, wrapHint("   \t\n \t ", 48), "whitespace-only hint has no words to place")

	// A single word wider than the box is unbreakable: it ships whole rather
	// than being dropped, cut, or recursed over.
	long := strings.Repeat("w", 60)
	assert.Equal(t, []string{long}, wrapHint(long, 48), "over-wide word survives intact")

	// Degenerate widths keep every word on its own line instead of looping
	// or producing zero-length lines.
	assert.Equal(t, []string{"a", "b"}, wrapHint("a b", 0), "zero width")
	assert.Equal(t, []string{"a", "b"}, wrapHint("a b", -3), "negative width")
}

func TestSphereDraw_DegenerateGeometry_Negative(t *testing.T) {
	ctx := components.DrawContext{Method: xui.WidthUnicode}
	// Zero or negative widget sizes fall back to the 40×40 default per axis.
	for _, tc := range [][4]int{
		{0, 0, 40, 40},
		{-7, -3, 40, 40},
		{12, 0, 12, 40},
		{0, 12, 40, 12},
	} {
		o := Sphere{Width: tc[0], Height: tc[1]}
		s := o.Draw(ctx)
		assert.Equal(t, tc[2], s.Size.Width, "Width=%d Height=%d", tc[0], tc[1])
		assert.Equal(t, tc[3], s.Size.Height, "Width=%d Height=%d", tc[0], tc[1])
	}

	// Tiny constraints clamp to a 3×3 floor instead of a zero-size surface.
	clip := components.DrawContext{Max: components.Size{Width: 1, Height: 2}, Method: xui.WidthUnicode}
	clipped := Sphere{Width: 20, Height: 20}
	s := clipped.Draw(clip)
	assert.Equal(t, 3, s.Size.Width, "1-column constraint clamps to 3")
	assert.Equal(t, 3, s.Size.Height, "2-row constraint clamps to 3")
	// At 3×3 the aspect-corrected ellipse (kx=0.5) excludes every row: the
	// surface stays blank rather than smearing partial glyphs.
	for i, c := range s.Buffer {
		assert.True(t, c.Default, "cell %d of the 3×3 sphere must stay blank", i)
	}
}

func TestScreenDraw_ZeroConstraintsFallback_Negative(t *testing.T) {
	// Nil Sphere, zero Theme, empty Brand: the screen must still compose.
	w := Screen{}
	for _, sz := range [][2]int{{0, 0}, {-5, -1}} {
		s := w.Draw(components.DrawContext{
			Max:    components.Size{Width: sz[0], Height: sz[1]},
			Method: xui.WidthUnicode,
		})
		assert.Equal(t, 80, s.Size.Width, "max width %d falls back to 80", sz[0])
		assert.Equal(t, 24, s.Size.Height, "max height %d falls back to 24", sz[1])
		require.Len(t, s.Children, 2, "sphere + text block survive a degenerate terminal size")
	}
}

// oracle: strings.Fields/Join equivalence on stdlib — greedy wrapping may
// only insert breaks at word boundaries, so re-joining the wrapped lines with
// single spaces must reproduce the whitespace-normalized input for every
// width, and no line may exceed the width unless it is one unbreakable word.
func TestWrapHint_RebuildsNormalizedText_Differential(t *testing.T) {
	hints := []string{
		"Type a message below",
		"  leading and   trailing   ",
		"tabs\tand\nnewlines collapse",
		"one supercalifragilistic word",
		strings.Repeat("word ", 30),
		"emoji 😀😀 and CJK 日本語 mixed into the hint",
	}
	rng := rand.New(rand.NewSource(3))
	alphabet := []string{"aa", "bb", "cc", "日", "😀"}
	for range 200 {
		var sb strings.Builder
		for j := rng.Intn(12); j > 0; j-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
			if rng.Intn(3) == 0 {
				sb.WriteString("  ")
			} else {
				sb.WriteString(" ")
			}
		}
		hints = append(hints, strings.TrimSpace(sb.String()))
	}
	for _, h := range hints {
		if strings.TrimSpace(h) == "" {
			continue // blank hints are the nil case pinned in the negative test
		}
		normalized := strings.Join(strings.Fields(h), " ")
		for _, width := range []int{-5, 0, 1, 7, 20, 48, 1000} {
			lines := wrapHint(h, width)
			require.NotNil(t, lines, "hint %q width %d: non-empty input must yield lines", h, width)
			assert.Equal(t, normalized, strings.Join(lines, " "),
				"hint %q width %d: words must survive wrapping verbatim, in order", h, width)
			for _, ln := range lines {
				// The width contract is display columns, not bytes: a CJK rune
				// is 2 columns but 3 bytes, so a line can pass a byte check and
				// still overflow the painted hint box.
				assert.True(t, xui.StringWidth(ln, xui.WidthUnicode) <= width || !strings.Contains(ln, " "),
					"line %q exceeds width %d despite being breakable", ln, width)
			}
			// Greedy packing: a wrap is only allowed when the next line's first
			// word genuinely does not fit by display columns. Byte-based
			// fitting wraps CJK early (3 bytes but 2 columns), leaving lines
			// underfull while the painted box still has room.
			for i := 0; i+1 < len(lines); i++ {
				nextWord, _, _ := strings.Cut(lines[i+1], " ")
				assert.True(t, xui.StringWidth(lines[i], xui.WidthUnicode)+1+xui.StringWidth(nextWord, xui.WidthUnicode) > width,
					"line %q wrapped early: %q still fits in width %d by columns", lines[i], nextWord, width)
			}
		}
	}
}

// naiveScreenGeom re-derives the splash layout arithmetic from the size spec,
// independent of the Draw code path: the sphere is 40, shrunk to maxH-2 when
// the screen is short, capped at half the width, floored at 12; the text
// column is maxW-sphere-6 clamped to [20,50]; the block is centered and the
// text is vertically centered inside it.
func naiveScreenGeom(maxW, maxH, hintLines int) (sphereSize, textW, textH, ox, oy, textX, textOY int) {
	sphere := 40
	if maxH < sphere+2 {
		sphere = maxH - 2
	}
	if sphere > maxW/2 {
		sphere = maxW / 2
	}
	if sphere < 12 {
		sphere = 12
	}
	textW = max(maxW-sphere-6, 20)
	textW = min(textW, 50)
	textH = 4 + hintLines
	blockW := sphere + 2 + textW
	blockH := max(sphere, textH)
	ox = max((maxW-blockW)/2, 0)
	oy = max((maxH-blockH)/2, 0)
	textOY = max(oy+(blockH-textH)/2, 0)
	return sphere, textW, textH, ox, oy, ox + sphere + 2, textOY
}

// oracle: naive re-derivation of the documented layout arithmetic (above) —
// child origins and surface sizes must match the hand-computed geometry for
// every screen shape, including ones where the clamps fight each other.
func TestScreenDraw_MatchesNaiveGeometry_Differential(t *testing.T) {
	sizes := [][2]int{{100, 40}, {80, 24}, {60, 30}, {50, 26}, {44, 20}, {30, 20}, {24, 26}, {20, 15}, {200, 60}, {16, 18}}
	for _, sz := range sizes {
		maxW, maxH := sz[0], sz[1]
		w := Screen{Theme: components.DefaultTheme()}
		s := w.Draw(components.DrawContext{
			Max:    components.Size{Width: maxW, Height: maxH},
			Method: xui.WidthUnicode,
		})
		// Default hint is a single line, so the text block is 4 copy rows + 1.
		sphereSize, textW, textH, ox, oy, textX, textOY := naiveScreenGeom(maxW, maxH, 1)
		require.Len(t, s.Children, 2, "%dx%d: sphere + text", maxW, maxH)
		assert.Equal(t, components.Point{X: ox, Y: oy}, s.Children[0].Origin, "%dx%d sphere origin", maxW, maxH)
		assert.Equal(t, sphereSize, s.Children[0].Surface.Size.Width, "%dx%d sphere width", maxW, maxH)
		assert.Equal(t, sphereSize, s.Children[0].Surface.Size.Height, "%dx%d sphere height", maxW, maxH)
		assert.Equal(t, components.Point{X: textX, Y: textOY}, s.Children[1].Origin, "%dx%d text origin", maxW, maxH)
		assert.Equal(t, textW, s.Children[1].Surface.Size.Width, "%dx%d text width", maxW, maxH)
		assert.Equal(t, textH, s.Children[1].Surface.Size.Height, "%dx%d text height", maxW, maxH)
	}

	// A custom hint engineered (by word-length arithmetic, not by calling
	// wrapHint) to wrap to exactly two 48-column lines grows the block a row.
	w := Screen{Theme: components.DefaultTheme(), Hint: strings.Repeat("a", 41) + " " + strings.Repeat("b", 41)}
	s := w.Draw(components.DrawContext{Max: components.Size{Width: 80, Height: 24}, Method: xui.WidthUnicode})
	_, textW, textH, _, _, _, textOY := naiveScreenGeom(80, 24, 2)
	require.Len(t, s.Children, 2)
	assert.Equal(t, 6, textH, "4 copy rows + 2 wrapped hint rows")
	assert.Equal(t, textH, s.Children[1].Surface.Size.Height)
	assert.Equal(t, textW, s.Children[1].Surface.Size.Width)
	assert.Equal(t, textOY, s.Children[1].Origin.Y)
}
