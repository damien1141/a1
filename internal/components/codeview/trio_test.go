package codeview

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
)

// ---------- negative ----------

// Hostile scroll / line-count inputs must clamp, never index past the lines.
func TestClampScrollHostileBounds_Negative(t *testing.T) {
	assert.Equal(t, 0, clampScroll(-5, 3), "negative scroll clamps to the top")
	assert.Equal(t, 2, clampScroll(99, 3), "overscroll clamps to the last line")
	assert.Equal(t, 0, clampScroll(7, 0), "no lines means scroll 0")
	assert.Equal(t, 0, clampScroll(0, 0))
	assert.Equal(t, 0, clampScroll(-1, 1))
	assert.Equal(t, 1, clampScroll(1, 2), "last line stays reachable")

	// Gutter width floors at one column so Paint never divides by a zero count.
	assert.Equal(t, 1, lineNumberWidth(0))
	assert.Equal(t, 1, lineNumberWidth(-4))
	assert.Equal(t, 1, lineNumberWidth(1))
	assert.Equal(t, 2, lineNumberWidth(10))
	assert.Equal(t, 3, lineNumberWidth(100))
}

func TestSkipColsHostileBounds_Negative(t *testing.T) {
	spans := []components.Span{{Text: "ab"}, {Text: "cd"}}
	// A negative budget is a no-op, not a backwards skip.
	assert.Equal(t, spans, skipCols(spans, -3, xui.WidthUnicode))
	// A budget at or beyond the text width drops the whole line.
	assert.Empty(t, skipCols(spans, 4, xui.WidthUnicode), "exact budget consumes every cluster")
	assert.Empty(t, skipCols(spans, 99, xui.WidthUnicode))
	// Empty inputs stay empty instead of synthesizing spans.
	assert.Empty(t, skipCols(nil, 3, xui.WidthUnicode))
	assert.Empty(t, skipCols([]components.Span{{}, {Text: ""}}, 2, xui.WidthUnicode))
}

func TestClipSpansHostileWidth_Negative(t *testing.T) {
	// A non-positive budget clips to nothing.
	assert.Nil(t, clipSpans([]components.Span{{Text: "abc"}}, -2, xui.WidthUnicode))
	assert.Nil(t, clipSpans(nil, 5, xui.WidthUnicode))
	// Textless spans are skipped, not fatal.
	assert.Nil(t, clipSpans([]components.Span{{}, {Text: ""}}, 10, xui.WidthUnicode))
	// A leading wide glyph that cannot fit kills the rest of the line rather
	// than letting the next span's text leak into its place.
	assert.Nil(t, clipSpans([]components.Span{{Text: "日"}, {Text: "a"}}, 1, xui.WidthUnicode))
}

// Degenerate geometry, oversized scroll state, and invalid UTF-8 must all
// render without panicking and pin their graceful fallbacks.
func TestPaintHostileStates_Negative(t *testing.T) {
	th := components.DefaultTheme()

	// Zero-size context falls back to the 80x24 default canvas.
	surf := Paint(components.DrawContext{Max: components.Size{}, Method: xui.WidthUnicode}, Model{Theme: th})
	assert.Equal(t, components.Size{Width: 80, Height: 24}, surf.Size)
	assert.Nil(t, surf.Cursor, "no lines means no caret to expose")
	assert.Contains(t, components.SurfaceText(surf), "file", "empty title falls back to the default")

	ctx := components.DrawContext{Max: components.Size{Width: 24, Height: 6}, Method: xui.WidthUnicode}
	lines := []string{"one", "two", "three", "four", "five"}

	// ViewH past the pane clamps to the body height: only four rows fit.
	surf = Paint(ctx, Model{Theme: th, Lines: lines, ViewH: 1000})
	text := components.SurfaceText(surf)
	assert.Contains(t, text, "4 │ four")
	assert.NotContains(t, text, "5 │ five")

	// Negative ViewH is "derive from height", like zero: the body is not
	// shrunk to nothing and the pane does not panic.
	surf = Paint(ctx, Model{Theme: th, Lines: lines, ViewH: -5})
	text = components.SurfaceText(surf)
	assert.Contains(t, text, "4 │ four")
	assert.NotContains(t, text, "5 │ five")

	// Horizontal scroll past the line hides the code but keeps the gutter.
	surf = Paint(ctx, Model{Theme: th, Lines: []string{"abcdef"}, XScroll: 100})
	rows := strings.Split(strings.TrimSuffix(components.SurfaceText(surf), "\n"), "\n")
	require.Len(t, rows, 6)
	assert.Equal(t, "1 │ "+strings.Repeat(" ", 20), rows[1], "code area stays empty")
	assert.Nil(t, surf.Cursor, "caret scrolled off the left edge is dropped")

	// Invalid UTF-8 bytes render as-is (one cell each) instead of panicking.
	surf = Paint(components.DrawContext{
		Max:    components.Size{Width: 14, Height: 4},
		Method: xui.WidthUnicode,
	}, Model{Theme: th, Lines: []string{"\xff\xfe bad"}})
	rows = strings.Split(strings.TrimSuffix(components.SurfaceText(surf), "\n"), "\n")
	require.Len(t, rows, 4)
	assert.Equal(t, "1 │ \xff\xfe bad"+strings.Repeat(" ", 4), rows[1])
}

// ---------- differential ----------

// trioCluster is one grapheme cluster with its display width, as the naive
// oracle below decodes it.
type trioCluster struct {
	text  string
	width int
}

// trioNaiveWidth is the oracle's rune-width table. It only needs to be correct
// for the classes the corpora draw from: ASCII, control bytes, invalid bytes,
// CJK ideographs, emoji, and combining marks.
func trioNaiveWidth(r rune) int {
	switch {
	case r < 0x80:
		return 1 // ASCII
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Mc, r):
		return 0 // combining marks attach to the preceding cluster
	case r >= 0x2E80 && r <= 0xA4CF:
		return 2 // CJK ideographs (corpus: 日本)
	case r >= 0x1F300:
		return 2 // emoji (corpus: 😀)
	default:
		return 1
	}
}

// trioClusters decodes s into clusters independently of xui: first rune, then
// any run of combining marks, width = the base rune's width (floored at 1).
func trioClusters(s string) []trioCluster {
	var out []trioCluster
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		end := i + size
		for end < len(s) {
			r2, sz := utf8.DecodeRuneInString(s[end:])
			if !unicode.Is(unicode.Mn, r2) && !unicode.Is(unicode.Me, r2) && !unicode.Is(unicode.Mc, r2) {
				break
			}
			end += sz
		}
		w := trioNaiveWidth(r)
		if w < 1 {
			w = 1
		}
		out = append(out, trioCluster{text: s[i:end], width: w})
		i = end
	}
	return out
}

func trioSpanText(spans []components.Span) string {
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.Text)
	}
	return b.String()
}

func trioTextWidth(s string) int {
	w := 0
	for _, c := range trioClusters(s) {
		w += c.width
	}
	return w
}

// trioSkipOracle rebuilds the skip spec from scratch: drop leading clusters
// while they fit the budget, then keep everything from the first cluster that
// would overflow onwards (that cluster stays whole).
func trioSkipOracle(spans []components.Span, cols int) string {
	if cols <= 0 {
		return trioSpanText(spans)
	}
	var b strings.Builder
	skipped := 0
	cut := false
	for _, sp := range spans {
		for _, c := range trioClusters(sp.Text) {
			if cut {
				b.WriteString(c.text)
				continue
			}
			if skipped+c.width <= cols {
				skipped += c.width
				continue
			}
			b.WriteString(c.text)
			cut = true
		}
	}
	return b.String()
}

// trioTruncateOracle is the longest cluster prefix of s that fits max columns.
func trioTruncateOracle(s string, max int) string {
	if max <= 0 {
		return ""
	}
	var b strings.Builder
	w := 0
	for _, c := range trioClusters(s) {
		if w+c.width > max {
			break
		}
		b.WriteString(c.text)
		w += c.width
	}
	return b.String()
}

// trioClipOracle rebuilds the clip spec: per span, truncate to the remaining
// budget; stop at the first span that no longer fits; skip textless spans.
func trioClipOracle(spans []components.Span, width int) []string {
	if width <= 0 {
		return nil
	}
	var texts []string
	used := 0
	for _, sp := range spans {
		if used >= width {
			break
		}
		if sp.Text == "" {
			continue
		}
		text := trioTruncateOracle(sp.Text, width-used)
		if text == "" {
			break
		}
		texts = append(texts, text)
		used += trioTextWidth(text)
	}
	return texts
}

func trioRandomSpans(rng *rand.Rand, alphabet []string) []components.Span {
	spans := make([]components.Span, rng.Intn(4))
	for i := range spans {
		var b strings.Builder
		for n := rng.Intn(7); n > 0; n-- {
			b.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		spans[i] = components.Span{Text: b.String()}
	}
	return spans
}

// oracle: naive cluster-walk reference (trioSkipOracle above) on randomized
// span lists from a fixed alphabet including hostile bytes; skipCols must
// return exactly the text suffix that starts at the first cluster crossing
// the scroll budget, with that cluster kept whole.
func TestSkipColsMatchesNaiveReference_Differential(t *testing.T) {
	alphabet := []string{"a", "Z", "7", " ", "日", "本", "😀", "é", "\x01", "\xff"}
	rng := rand.New(rand.NewSource(42))
	for i := range 300 {
		spans := trioRandomSpans(rng, alphabet)
		cols := rng.Intn(16) - 2 // includes negative budgets
		got := trioSpanText(skipCols(spans, cols, xui.WidthUnicode))
		assert.Equal(t, trioSkipOracle(spans, cols), got, "case %d: skipCols(%q, %d)", i, trioSpanText(spans), cols)
	}
	// Edge cases the random walk may miss.
	for _, text := range []string{"", "日", "ééé", "😀😀", "a日😀é", "\xff\x01"} {
		spans := []components.Span{{Text: text}}
		for cols := -1; cols <= 8; cols++ {
			got := trioSpanText(skipCols(spans, cols, xui.WidthUnicode))
			assert.Equal(t, trioSkipOracle(spans, cols), got, "skipCols(%q, %d)", text, cols)
		}
	}
}

// oracle: naive per-span truncation reference (trioClipOracle above) on
// randomized span lists; clipSpans must keep the longest prefix of span texts
// that fits the budget and stop at the first span that no longer fits. The
// corpus avoids control bytes and leading combining marks, where the
// implementation's StringWidth bookkeeping deliberately disagrees with
// cluster walking (zero-width runes count 0 there).
func TestClipSpansMatchesNaiveReference_Differential(t *testing.T) {
	alphabet := []string{"a", "Z", "7", " ", "日", "本", "😀", "é"}
	rng := rand.New(rand.NewSource(43))
	for i := range 300 {
		spans := trioRandomSpans(rng, alphabet)
		width := rng.Intn(14) - 2 // includes negative budgets
		gotSpans := clipSpans(spans, width, xui.WidthUnicode)
		var got []string
		for _, sp := range gotSpans {
			got = append(got, sp.Text)
		}
		assert.Equal(t, trioClipOracle(spans, width), got, "case %d: clipSpans(%q, %d)", i, trioSpanText(spans), width)
	}
	for _, text := range []string{"", "日", "ééé", "😀😀", "a日😀é"} {
		spans := []components.Span{{Text: text}}
		for width := -1; width <= 8; width++ {
			gotSpans := clipSpans(spans, width, xui.WidthUnicode)
			var got []string
			for _, sp := range gotSpans {
				got = append(got, sp.Text)
			}
			assert.Equal(t, trioClipOracle(spans, width), got, "clipSpans(%q, %d)", text, width)
		}
	}
}

// oracle: hand-written golden of the full painted surface for a fixed model —
// title row, right-aligned gutters, body rows, and the status row with the
// hint docked to the right edge, all derived by column arithmetic from the
// Paint layout rules, never regenerated from the painter itself.
func TestPaintGoldenRows_Differential(t *testing.T) {
	surf := Paint(components.DrawContext{
		Max:    components.Size{Width: 24, Height: 6},
		Method: xui.WidthUnicode,
	}, Model{
		Theme:  components.DefaultTheme(),
		Title:  "t.go",
		Status: "2 lines",
		Hint:   "esc",
		Lines:  []string{"alpha", "beta"},
	})
	want := []string{
		" t.go" + strings.Repeat(" ", 19),
		"1 │ alpha" + strings.Repeat(" ", 15),
		"2 │ beta" + strings.Repeat(" ", 16),
		strings.Repeat(" ", 24), // unpainted body rows
		strings.Repeat(" ", 24),
		" 2 lines" + strings.Repeat(" ", 12) + "esc ",
	}
	got := strings.Split(strings.TrimSuffix(components.SurfaceText(surf), "\n"), "\n")
	require.Len(t, got, len(want))
	assert.Equal(t, want, got)

	// Caret sits just after the gutter on the first line.
	require.NotNil(t, surf.Cursor)
	assert.Equal(t, components.Point{X: 4, Y: 1}, *surf.Cursor)
}
