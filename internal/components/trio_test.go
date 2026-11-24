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

// TestWrapSpans_BreaksBetweenWords_Differential pins the wrapping CONTRACT with
// hand-written expectations.
//
// It deliberately does NOT compare against a second wrapping implementation. The
// previous oracle was a naive per-grapheme reference, which is a tautology the
// moment the behaviour being specified IS per-grapheme wrapping — it certified
// "head -3" splitting into "hea" / "d -3", the very defect that made wrapped
// blocks unreadable.
func TestWrapSpans_BreaksBetweenWords_Differential(t *testing.T) {
	join := func(lines []components.RichLine) []string {
		out := make([]string, len(lines))
		for i, l := range lines {
			var b strings.Builder
			for _, sp := range l {
				b.WriteString(sp.Text)
			}
			out[i] = b.String()
		}
		return out
	}

	cases := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		// The reported bug: a shell line used to split mid-token.
		{"command breaks at a space", "head -3; echo x", 12, []string{"head -3;", "echo x"}},
		// Whole words fill a row; a row that is exactly full simply ends.
		{"words fill", "aaa bbb ccc", 3, []string{"aaa", "bbb", "ccc"}},
		{"row exactly full", "aaa bbb ccc", 7, []string{"aaa bbb", "ccc"}},
		// A token wider than a whole row has no space to break at: hard-split.
		{"unbreakable token", "babbb", 3, []string{"bab", "bb"}},
		// Hard newlines are never wrapped away.
		{"newline preserved", "a\nb", 40, []string{"a", "b"}},
		// Width clamps to 1.
		{"zero width clamps", "ab", 0, []string{"a", "b"}},
		// Spaces beginning a REAL line are indentation and survive...
		{"leading indent kept", "  indented", 20, []string{"  indented"}},
		{"indent after newline kept", "a\n  b", 20, []string{"a", "  b"}},
		// ...but a space that CAUSED a wrap is consumed, never a row of its own:
		// a blank row is invisible height.
		{"wrap space consumed", "aa bb", 2, []string{"aa", "bb"}},
		// Empty input still yields one (empty) row so callers can paint it.
		{"empty input", "", 5, []string{""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, join(components.WrapSpans([]components.Span{{Text: tc.in}}, tc.width, xui.WidthUnicode)))
		})
	}
}

// TestWrapSpans_Invariants_Negative checks properties that hold for ANY input,
// without re-deriving the algorithm:
//
//   - no painted row is wider than the wrap width;
//   - no non-space character is lost or reordered;
//   - hard newlines survive.
func TestWrapSpans_Invariants_Negative(t *testing.T) {
	alphabet := []string{"a", "b", "c", " ", "\n"}
	rng := rand.New(rand.NewSource(7))
	for i := range 300 {
		var sb strings.Builder
		for j := rng.Intn(24); j > 0; j-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		s := sb.String()
		width := rng.Intn(14) - 2
		w := max(width, 1)

		var got strings.Builder
		rows := components.WrapSpans([]components.Span{{Text: s}}, width, xui.WidthUnicode)
		for r, line := range rows {
			var b strings.Builder
			for _, sp := range line {
				b.WriteString(sp.Text)
			}
			row := b.String()
			assert.LessOrEqual(t, xui.StringWidth(row, xui.WidthUnicode), w,
				"case %d: WrapSpans(%q, %d) row %d = %q exceeds width", i, s, width, r, row)
			got.WriteString(row)
		}

		// Content preservation: ignore spaces AND newlines, so a consumed break
		// space is not a failure but a lost character is.
		squash := func(x string) string {
			return strings.NewReplacer(" ", "", "\n", "").Replace(x)
		}
		assert.Equal(t, squash(s), squash(got.String()),
			"case %d: WrapSpans(%q, %d) must not lose content", i, s, width)

		assert.GreaterOrEqual(t, len(rows), strings.Count(s, "\n"),
			"case %d: %q keeps its hard newlines", i, s)
	}
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
