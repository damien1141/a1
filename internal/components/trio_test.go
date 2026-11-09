package components_test

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

// plainWidget is a Widget that implements nothing else: EntryCopyText must
// return "" for it instead of panicking.
type plainWidget struct{}

func (plainWidget) Handle(*components.EventContext, xui.Event) {}

func (plainWidget) Draw(components.DrawContext) components.Surface {
	return components.NewSurface(1, 1, nil)
}

// copyWidget implements CopyTexter.
type copyWidget struct{ plainWidget }

func (copyWidget) CopyText() string { return "payload" }

func TestBylineHostileItems_Negative(t *testing.T) {
	// Every flavor of blank — including the unicode spaces strings.TrimSpace
	// honors (NBSP) — must vanish with no dangling separator.
	for _, blank := range []string{"", " ", "  ", "\t", "\n", " \t\r\n", "\u00a0"} {
		assert.Equal(t, "", components.Byline(blank), "Byline(%q)", blank)
		assert.Equal(t, "", components.Byline(blank, blank, blank), "Byline(%q x3)", blank)
	}
	// Inner whitespace is content, not blankness: only the edges are trimmed.
	assert.Equal(t, "a\nb", components.Byline("a\nb"))
	assert.Equal(t, "a\nb · c", components.Byline(" a\nb ", "", "c"))
}

func TestSurfaceDegenerateGeometry_Negative(t *testing.T) {
	// Zero-sized surfaces allocate an empty buffer and every painter guards.
	zero := components.NewSurface(0, 0, nil)
	zero.SetCell(0, 0, xui.Cell{Char: "x", Width: 1})
	assert.Empty(t, zero.Buffer)
	assert.Equal(t, 0, zero.Print(0, 0, "hi", xui.Style{}, xui.WidthUnicode), "nothing fits a 0-wide surface")
	assert.Equal(t, "", components.SurfaceText(zero))
	assert.Nil(t, zero.HitTest(0, 0))

	tall := components.NewSurface(0, 3, nil)
	// A 0-wide, 3-tall surface renders three empty rows (one newline each).
	assert.Equal(t, "\n\n\n", components.SurfaceText(tall))

	// Out-of-bounds SetCell is a no-op, not a panic and not a wrap-around.
	s := components.NewSurface(4, 2, nil)
	for _, pt := range [][2]int{{-1, 0}, {4, 0}, {0, 2}, {0, -1}, {-5, -5}} {
		s.SetCell(pt[0], pt[1], xui.Cell{Char: "x", Width: 1})
	}
	for i := range s.Buffer {
		assert.True(t, s.Buffer[i].Default, "cell %d must stay blank after out-of-bounds writes", i)
	}

	// Print clips at the right edge and reports only the painted columns.
	clip := components.NewSurface(3, 1, nil)
	assert.Equal(t, 3, clip.Print(0, 0, "hello", xui.Style{}, xui.WidthUnicode))
	assert.Equal(t, "hel\n", components.SurfaceText(clip))

	edge := components.NewSurface(5, 1, nil)
	assert.Equal(t, 2, edge.Print(3, 0, "abcd", xui.Style{}, xui.WidthUnicode))
	assert.Equal(t, "   ab\n", components.SurfaceText(edge))

	// A one-column surface cannot fit a wide glyph: nothing is painted.
	narrow := components.NewSurface(1, 1, nil)
	assert.Equal(t, 0, narrow.Print(0, 0, "日", xui.Style{}, xui.WidthUnicode))
	assert.Equal(t, " \n", components.SurfaceText(narrow))
	assert.True(t, narrow.Buffer[0].Default, "wide glyph must not be squeezed into one column")

	// Empty text paints nothing.
	fresh := components.NewSurface(3, 1, nil)
	assert.Equal(t, 0, fresh.Print(0, 0, "", xui.Style{}, xui.WidthUnicode))
	assert.Equal(t, "   \n", components.SurfaceText(fresh))

	// HitTest outside the box finds no widget.
	hit := components.NewSurface(4, 2, plainWidget{})
	assert.Equal(t, components.Widget(plainWidget{}), hit.HitTest(1, 1))
	assert.Nil(t, hit.HitTest(4, 1), "x == width")
	assert.Nil(t, hit.HitTest(1, 2), "y == height")
	assert.Nil(t, hit.HitTest(-1, 0), "negative x")
}

func TestSelectionHelpersHostileBounds_Negative(t *testing.T) {
	// Zero-size surface: extraction degrades to empty text, not a panic.
	assert.Equal(t, "", components.ExtractSurfaceText(components.NewSurface(0, 0, nil), 0, 0, 9, 9))

	s := components.NewSurface(10, 3, nil)
	s.Print(0, 0, "hello", xui.Style{}, xui.WidthUnicode)
	s.Print(0, 1, "world", xui.Style{}, xui.WidthUnicode)

	// Selection entirely outside the surface yields nothing.
	assert.Equal(t, "", components.ExtractSurfaceText(s, 0, 5, 9, 9), "below the last row")
	assert.Equal(t, "", components.ExtractSurfaceText(s, 0, -9, 9, -5), "above the first row")

	// Selection over untouched columns yields the blank-trimmed empty line.
	assert.Equal(t, "", components.ExtractSurfaceText(s, 7, 0, 9, 0), "right of the text")

	// Endpoints are inclusive.
	assert.True(t, components.InTextSelection(5, 0, 0, 0, 5, 0), "selection end column is inside")
	assert.False(t, components.InTextSelection(6, 0, 0, 0, 5, 0), "one past the end is outside")

	// nil surfaces are guarded no-ops.
	assert.NotPanics(t, func() {
		components.ApplySelectionHighlight(nil, 0, 0, 1, 1, xui.Style{})
		components.ApplyBlockHighlight(nil, xui.Style{})
		components.ApplyBlockHighlight(&components.Surface{}, xui.Style{})
	})

	// Non-CopyTexter widgets copy as "".
	assert.Equal(t, "", components.EntryCopyText(plainWidget{}))
	assert.Equal(t, "payload", components.EntryCopyText(copyWidget{}))
}

// oracle: hand-computed golden table of the rounding boundaries — each entry
// is derived by pen-and-paper division, never by calling FormatTokens.
func TestFormatTokensBoundaries_Differential(t *testing.T) {
	cases := map[int]string{
		0:         "0",
		1:         "1",
		999:       "999",   // last plain count
		1000:      "1.0k",  // first k value: 1.0, not "1k"
		1001:      "1.0k",  // 1.001 rounds to 1.0
		1234:      "1.2k",  // 1.234 rounds to 1.2
		9999:      "10.0k", // 9.999 rounds up past the k boundary
		10000:     "10k",   // integer division kicks in
		10499:     "10k",   // 10499/1000 == 10
		999999:    "999k",  // last k value
		1000000:   "1.0M",  // first M value
		1500000:   "1.5M",
		9999999:   "10.0M", // 9.999999 rounds up past the M boundary
		10000000:  "10M",
		123456789: "123M", // 123456789/1000000 == 123
	}
	for n, want := range cases {
		assert.Equal(t, want, components.FormatTokens(n), "FormatTokens(%d)", n)
	}
}

// oracle: naive filter+join reference — trim each item, drop blanks, join with
// the separator; assembled with strings.Join instead of the incremental
// append the implementation uses.
func TestBylineMatchesNaiveJoin_Differential(t *testing.T) {
	alphabet := []string{"", " ", "  ", "\t", "\n", "a", "grep · 3 files", "日本語", " lead", "trail ", " mid dle "}
	rng := rand.New(rand.NewSource(42))
	for i := range 300 {
		items := make([]string, rng.Intn(6))
		for j := range items {
			items[j] = alphabet[rng.Intn(len(alphabet))]
		}
		kept := make([]string, 0, len(items))
		for _, it := range items {
			if trimmed := strings.TrimSpace(it); trimmed != "" {
				kept = append(kept, trimmed)
			}
		}
		assert.Equal(t, strings.Join(kept, components.BylineSeparator), components.Byline(items...),
			"case %d: Byline(%q)", i, items)
	}
}

// naiveWrapText is the spec restated with plain byte slicing: greedy chunks of
// `width` bytes, hard newlines flush, and no trailing empty line unless the
// input is empty (in which case exactly one empty line).
func naiveWrapText(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var lines []string
	var cur []byte
	for i := 0; i < len(s); {
		if s[i] == '\n' {
			lines = append(lines, string(cur))
			cur = nil
			i++
			continue
		}
		if len(cur) == width {
			lines = append(lines, string(cur))
			cur = nil
		}
		cur = append(cur, s[i])
		i++
	}
	if len(cur) > 0 || len(lines) == 0 {
		lines = append(lines, string(cur))
	}
	return lines
}

// oracle: the naive byte-wrap reference above on randomized ASCII inputs and
// widths (including the 0 and negative clamps); WrapSpans must produce the
// same visible rows.
func TestWrapSpansMatchesNaiveWrap_Differential(t *testing.T) {
	alphabet := []string{"a", "b", "c", " ", "\n"}
	rng := rand.New(rand.NewSource(7))
	for i := range 300 {
		var sb strings.Builder
		for j := rng.Intn(24); j > 0; j-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		s := sb.String()
		width := rng.Intn(14) - 2 // includes negative and zero widths

		got := components.WrapSpans([]components.Span{{Text: s}}, width, xui.WidthUnicode)
		want := naiveWrapText(s, width)
		require.Len(t, got, len(want), "case %d: WrapSpans(%q, %d) row count", i, s, width)
		for r, line := range got {
			var b strings.Builder
			for _, sp := range line {
				b.WriteString(sp.Text)
			}
			assert.Equal(t, want[r], b.String(), "case %d: WrapSpans(%q, %d) row %d", i, s, width, r)
		}
	}

	// Edge cases the random walk may miss.
	for _, s := range []string{"", "abcd", "abc\n", "ab\ncd", "\n", "abcdefg"} {
		for _, width := range []int{-3, 0, 1, 2, 3, 100} {
			got := components.WrapSpans([]components.Span{{Text: s}}, width, xui.WidthUnicode)
			want := naiveWrapText(s, width)
			require.Len(t, got, len(want), "WrapSpans(%q, %d) rows", s, width)
			for r, line := range got {
				var b strings.Builder
				for _, sp := range line {
					b.WriteString(sp.Text)
				}
				assert.Equal(t, want[r], b.String(), "WrapSpans(%q, %d) row %d", s, width, r)
			}
		}
	}

	// Degenerate width still produces one cluster per row.
	lines := components.WrapSpans([]components.Span{{Text: "abc"}}, 0, xui.WidthUnicode)
	require.Len(t, lines, 3)
	painted := components.PaintRichLines(1, lines, xui.WidthUnicode, nil)
	assert.Equal(t, 3, painted.Size.Height, "one row per cluster at width 1")

	// No lines at all still paint a 1-row surface.
	blank := components.PaintRichLines(5, nil, xui.WidthUnicode, nil)
	assert.Equal(t, 1, blank.Size.Height)
	assert.Equal(t, "     \n", components.SurfaceText(blank))
}

// oracle: paint→extract round trip through two genuinely distinct code paths —
// Print flattens text into cells, ExtractSurfaceText reassembles characters
// from the flattened buffer, so a full-surface selection must return the
// original text verbatim (wide glyphs emitted once, no continuation spaces).
func TestExtractSurfaceTextRoundTrip_Differential(t *testing.T) {
	corpus := [][]string{
		{"hello"},
		{"hello world"},
		{"二进制文件 a1"},
		{"日本語と English mix"},
		{"line one", "line two", "line three"},
		{"😀 emoji row", "ascii row"},
		{"mixed 中西 text on one row"},
	}
	for _, rows := range corpus {
		w := 40
		s := components.NewSurface(w, len(rows), nil)
		for y, row := range rows {
			require.Equal(t, xui.StringWidth(row, xui.WidthUnicode),
				s.Print(0, y, row, xui.Style{}, xui.WidthUnicode),
				"corpus row %q must fit width %d", row, w)
		}
		got := components.ExtractSurfaceText(s, 0, 0, w-1, len(rows)-1)
		assert.Equal(t, strings.Join(rows, "\n"), got, "round trip of %q", rows)
	}
}
