package layout_test

import (
	"math/rand"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/layout"
)

// surfaceRows renders a surface the way the terminal sees it, one string per
// row, so tests assert on visible output instead of widget internals.
func surfaceRows(t *testing.T, s components.Surface) []string {
	t.Helper()
	rows := strings.Split(strings.TrimSuffix(components.SurfaceText(s), "\n"), "\n")
	require.NotEmpty(t, rows, "surface with height %d must render rows", s.Size.Height)
	return rows
}

func TestDrawRoundedBorder_BoxAndLabels_Positive(t *testing.T) {
	s := components.NewSurface(24, 5, nil)
	layout.DrawRoundedBorder(&s, layout.BorderRounded, xui.Style{},
		&layout.BorderLabel{Text: "alpha"},
		&layout.BorderLabel{Text: "mid"},
		&layout.BorderLabel{Text: "omega"},
		&layout.BorderLabel{Text: "low"},
		&layout.BorderLabel{Text: "end"},
		xui.WidthUnicode)

	// Hand-derived columns: top-left label starts at x=2 (corner + gap), the
	// center label follows at 2+len("alpha")+1, and the right label's last
	// column is w-3 (one gap before the corner). Everything else stays edge.
	top := "╭─" + "alpha" + "─" + "mid" + strings.Repeat("─", 6) + "omega" + "─╮"
	bottom := "╰─" + "low" + strings.Repeat("─", 14) + "end" + "─╯"
	mid := "│" + strings.Repeat(" ", 22) + "│"
	assert.Equal(t, []string{top, mid, mid, mid, bottom}, surfaceRows(t, s))

	// Multi-style spans paint instead of Text and land on the same columns.
	s2 := components.NewSurface(24, 5, nil)
	layout.DrawRoundedBorder(&s2, layout.BorderRounded, xui.Style{},
		&layout.BorderLabel{Text: "alpha"},
		&layout.BorderLabel{Text: "mid"},
		&layout.BorderLabel{Spans: []layout.BorderSpan{{Text: "om"}, {Text: "ega"}}},
		&layout.BorderLabel{Text: "low"},
		&layout.BorderLabel{Text: "end"},
		xui.WidthUnicode)
	assert.Equal(t, surfaceRows(t, s), surfaceRows(t, s2), "spans label must render like the equivalent text")

	// Square style swaps the corner glyphs, same geometry.
	sq := components.NewSurface(10, 3, nil)
	layout.DrawRoundedBorder(&sq, layout.BorderSquare, xui.Style{}, nil, nil, nil, nil, nil, xui.WidthUnicode)
	assert.Equal(t, []string{
		"┌" + strings.Repeat("─", 8) + "┐",
		"│" + strings.Repeat(" ", 8) + "│",
		"└" + strings.Repeat("─", 8) + "┘",
	}, surfaceRows(t, sq))

	// Label visibility contract.
	assert.True(t, layout.BorderLabel{Text: "x"}.Visible())
	assert.True(t, layout.BorderLabel{Spans: []layout.BorderSpan{{Text: "x"}}}.Visible())
	assert.False(t, layout.BorderLabel{}.Visible())

	// Truncation happy paths: ASCII prefix, wide-glyph pairs, identity.
	assert.Equal(t, "hello", layout.TruncateToWidth("hello world", 5, xui.WidthUnicode))
	assert.Equal(t, "日本", layout.TruncateToWidth("日本語", 4, xui.WidthUnicode))
	assert.Equal(t, "fits", layout.TruncateToWidth("fits", 10, xui.WidthUnicode))
}

func TestDrawRoundedBorder_DegenerateSizeIsNoOp_Negative(t *testing.T) {
	for _, sz := range [][2]int{{0, 0}, {1, 4}, {6, 1}, {0, 9}, {8, 0}} {
		w, h := sz[0], sz[1]
		s := components.NewSurface(w, h, nil)
		// Labels are passed anyway: a too-small box must skip them, not panic.
		layout.DrawRoundedBorder(&s, layout.BorderRounded, xui.Style{},
			&layout.BorderLabel{Text: "tl"}, &layout.BorderLabel{Text: "tc"},
			&layout.BorderLabel{Text: "tr"}, &layout.BorderLabel{Text: "bl"},
			&layout.BorderLabel{Text: "br"}, xui.WidthUnicode)
		for i := range s.Buffer {
			assert.True(t, s.Buffer[i].Default, "cell %d of %dx%d surface must stay blank", i, w, h)
		}
	}
}

func TestTruncateToWidth_HostileBounds_Negative(t *testing.T) {
	assert.Equal(t, "", layout.TruncateToWidth("hello", 0, xui.WidthUnicode), "zero budget")
	assert.Equal(t, "", layout.TruncateToWidth("hello", -3, xui.WidthUnicode), "negative budget")
	assert.Equal(t, "", layout.TruncateToWidth("", 5, xui.WidthUnicode), "empty input")
	// One wide glyph needs two columns; a 1-column budget yields nothing.
	assert.Equal(t, "", layout.TruncateToWidth("日本", 1, xui.WidthUnicode), "wide glyph cannot fit one column")
	// Boundary: the second wide glyph overflows a 3-column budget.
	assert.Equal(t, "日", layout.TruncateToWidth("日本", 3, xui.WidthUnicode))
}

// naiveRuneWidth is the oracle's width table. It only needs to be correct for
// the classes the corpus below draws from: ASCII, CJK ideographs, emoji, and
// combining marks.
func naiveRuneWidth(r rune) int {
	switch {
	case r < 0x80:
		return 1 // ASCII letters, digits, space
	case unicode.Is(unicode.Mn, r):
		return 0 // combining marks attach to the preceding cluster
	case r >= 0x2E80 && r <= 0xA4CF:
		return 2 // CJK ideographs (corpus: 日本語)
	case r >= 0x1F300:
		return 2 // emoji (corpus: 😀)
	default:
		return 1
	}
}

// naiveTruncate rebuilds the "longest fitting prefix" spec from scratch:
// decode clusters with utf8, extend over combining marks, and stop before the
// cluster that would overflow the budget.
func naiveTruncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	var b strings.Builder
	w := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		end := i + size
		for end < len(s) {
			r2, sz := utf8.DecodeRuneInString(s[end:])
			if !unicode.Is(unicode.Mn, r2) {
				break
			}
			end += sz
		}
		cw := naiveRuneWidth(r)
		if w+cw > max {
			break
		}
		b.WriteString(s[i:end])
		w += cw
		i = end
	}
	return b.String()
}

// oracle: naive grapheme-prefix reference (above) on randomized and edge-case
// inputs from a fixed alphabet; both must return the longest prefix of s that
// fits max display columns.
func TestTruncateToWidth_MatchesNaiveReference_Differential(t *testing.T) {
	alphabet := []string{"a", "Z", "7", " ", "日", "本", "語", "😀", "é"}
	rng := rand.New(rand.NewSource(42))
	for i := range 300 {
		var sb strings.Builder
		for j := rng.Intn(13); j > 0; j-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		s := sb.String()
		max := rng.Intn(15) - 2 // includes negative and zero budgets
		assert.Equal(t, naiveTruncate(s, max), layout.TruncateToWidth(s, max, xui.WidthUnicode),
			"case %d: truncate(%q, %d)", i, s, max)
	}
	// Edge cases the random walk may miss.
	for _, s := range []string{"", "日", "ééé", "😀😀", "a日😀é"} {
		for max := -1; max <= 8; max++ {
			assert.Equal(t, naiveTruncate(s, max), layout.TruncateToWidth(s, max, xui.WidthUnicode),
				"truncate(%q, %d)", s, max)
		}
	}
}

// oracle: naive box-frame reference — expected rows assembled with
// strings.Repeat from the rectangle dimensions; label columns derived by hand
// arithmetic (left label at column 2, right label ending at column w-3), not
// by the layout code under test.
func TestDrawRoundedBorder_MatchesNaiveFrame_Differential(t *testing.T) {
	for _, sz := range [][2]int{{2, 2}, {3, 2}, {3, 3}, {7, 4}, {16, 6}, {41, 9}} {
		w, h := sz[0], sz[1]
		s := components.NewSurface(w, h, nil)
		layout.DrawRoundedBorder(&s, layout.BorderRounded, xui.Style{}, nil, nil, nil, nil, nil, xui.WidthUnicode)

		top := "╭" + strings.Repeat("─", w-2) + "╮"
		bottom := "╰" + strings.Repeat("─", w-2) + "╯"
		mid := "│" + strings.Repeat(" ", w-2) + "│"
		rows := surfaceRows(t, s)
		require.Len(t, rows, h)
		assert.Equal(t, top, rows[0], "%dx%d top row", w, h)
		assert.Equal(t, bottom, rows[h-1], "%dx%d bottom row", w, h)
		for y := 1; y < h-1; y++ {
			assert.Equal(t, mid, rows[y], "%dx%d row %d", w, h, y)
			for x := 1; x < w-1; x++ {
				assert.True(t, s.Buffer[y*w+x].Default, "interior cell (%d,%d) must stay blank", x, y)
			}
		}
	}

	// Labeled frame on a width the labels fit without squeezing: the fill
	// between them is plain edge, and the interior stays untouched.
	const w, h = 26, 5
	s := components.NewSurface(w, h, nil)
	layout.DrawRoundedBorder(&s, layout.BorderRounded, xui.Style{},
		&layout.BorderLabel{Text: "dep"}, nil, &layout.BorderLabel{Text: "rev"},
		&layout.BorderLabel{Text: "low"}, &layout.BorderLabel{Text: "end"},
		xui.WidthUnicode)
	fill := (w - 2 - len("rev")) - (2 + len("dep"))
	edge := "╭─dep" + strings.Repeat("─", fill) + "rev─╮"
	edgeBottom := "╰─low" + strings.Repeat("─", fill) + "end─╯"
	mid := "│" + strings.Repeat(" ", w-2) + "│"
	assert.Equal(t, []string{edge, mid, mid, mid, edgeBottom}, surfaceRows(t, s))
}
