package diffview

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
	"github.com/damien1141/a1/internal/util/diffreview"
)

func surfaceRows(t *testing.T, s components.Surface) []string {
	t.Helper()
	rows := strings.Split(strings.TrimSuffix(components.SurfaceText(s), "\n"), "\n")
	require.NotEmpty(t, rows)
	return rows
}

// ---------- negative ----------

// Hostile model state (overscroll, dangling doc indices, broken side-by-side
// cells, runaway cursor bytes) must degrade to blank rows and clamped carets.
func TestPaintHostileModel_Negative(t *testing.T) {
	th := components.DefaultTheme()
	ctx := components.DrawContext{Max: components.Size{Width: 16, Height: 8}, Method: xui.WidthUnicode}
	rows := []diffreview.Row{
		{Kind: diffreview.RowAdd, FileName: "a.go", Code: "new", Gutter: "  1 + "},
	}
	blank := strings.Repeat(" ", 16)

	// Scroll past the last visual row leaves the whole body blank.
	vis := []Vis{{Kind: VisRow, Doc: 0}, {Kind: VisRow, Doc: 0}}
	got := surfaceRows(t, Paint(ctx, Model{Theme: th, Rows: rows, Vis: vis, Scroll: 100}))
	for y := 1; y <= 6; y++ {
		assert.Equal(t, blank, got[y], "body row %d must stay blank", y)
	}

	// A Vis entry pointing outside the document is skipped; its neighbours paint.
	vis = []Vis{{Kind: VisRow, Doc: 42}, {Kind: VisRow, Doc: 0}}
	got = surfaceRows(t, Paint(ctx, Model{Theme: th, Rows: rows, Vis: vis}))
	assert.Equal(t, blank, got[1], "out-of-range doc leaves the row blank")
	assert.Equal(t, "  1 + new"+strings.Repeat(" ", 7), got[2])

	// Negative doc index is dropped the same way.
	vis = []Vis{{Kind: VisRow, Doc: -1}}
	got = surfaceRows(t, Paint(ctx, Model{Theme: th, Rows: rows, Vis: vis}))
	assert.Equal(t, blank, got[1])

	// Side-by-side with an unresolvable left cell: only the resolvable right
	// half washes, and the center rule still paints.
	sideVis := []Vis{{
		Kind:   VisRow,
		Doc:    0,
		Side:   &diffreview.SideBySideRow{Full: -1, Left: 99, Right: -1},
		Active: true,
	}}
	surf := Paint(ctx, Model{Theme: th, Rows: rows, Vis: sideVis, SideBySide: true})
	mid := 16 / 2
	assert.NotEqual(t, th.SelectionBg.Bg, surf.Buffer[1*16+1].Style.Bg, "unresolvable left cell stays plain")
	assert.Equal(t, th.SelectionBg.Bg, surf.Buffer[1*16+mid+1].Style.Bg, "empty right cell still washes when active")
	assert.Equal(t, "│", surf.Buffer[1*16+mid].Char, "center rule stays painted")

	// A note-prompt cursor byte past the prompt clamps to the prompt end.
	surf = Paint(ctx, Model{Theme: th, PromptKind: "note", Prompt: "hi", CursorByte: 1000})
	require.NotNil(t, surf.Cursor)
	assert.Equal(t, components.Point{X: 7, Y: 7}, *surf.Cursor)

	// A note prompt wider than the pane drops the cursor instead of overflowing.
	surf = Paint(ctx, Model{Theme: th, PromptKind: "note", Prompt: strings.Repeat("x", 40), CursorByte: 40})
	assert.Nil(t, surf.Cursor)
}

func TestHighlightRowsHostileInput_Negative(t *testing.T) {
	th := components.DefaultTheme()

	// Past the row cap the pane stays plain.
	big := make([]diffreview.Row, highlightRowLimit+1)
	for i := range big {
		big[i] = diffreview.Row{Kind: diffreview.RowAdd, FileName: "a.go", Code: "x"}
	}
	assert.Nil(t, HighlightRows(big, th))

	// At the cap it still highlights.
	assert.NotEmpty(t, HighlightRows(big[:highlightRowLimit], th))

	// Rows without code (headers, blanks, nil input) produce no spans.
	assert.Empty(t, HighlightRows([]diffreview.Row{{Kind: diffreview.RowFile, Text: "main.go"}}, th))
	assert.Empty(t, HighlightRows(nil, th))

	// Code rows in an unknown language stay plain.
	assert.Empty(t, HighlightRows([]diffreview.Row{{Kind: diffreview.RowAdd, FileName: "x.zzz", Code: "hello"}}, th))
}

// Inline spans are byte ranges produced by a differ; hostile ranges must not
// slice out of bounds and must never lose or duplicate code text.
func TestApplyInlineHostileRanges_Negative(t *testing.T) {
	mark := xui.Style{Reverse: true, Bold: true}
	code := "foo := oldValue"
	hostile := []diffreview.InlineSpan{
		{Start: 100, End: 200}, // wholly past the code
		{Start: -5, End: 3},    // negative start
		{Start: 9, End: 2},     // inverted range
		{Start: 7, End: 7},     // empty range
		{Start: 0, End: 99},    // end past the code
	}
	for _, in := range hostile {
		spans := []components.Span{{Text: code, Style: xui.Style{}}}
		out := applyInline(spans, code, []diffreview.InlineSpan{in}, mark)
		assert.Equal(t, code, trioSpanText(out), "inline %+v must not lose or duplicate text", in)
	}

	// No base spans at all: the code still comes through as one marked span.
	out := applyInline(nil, code, []diffreview.InlineSpan{{Start: 0, End: 99}}, mark)
	require.NotEmpty(t, out)
	assert.Equal(t, code, trioSpanText(out))
	assert.True(t, out[0].Style.Reverse, "covered text carries the mark style")
}

// ---------- differential ----------

// oracle: hand-written golden of the unified view for a fixed parsed diff —
// file row, hunk header, and the two gutter/code rows, with widths derived
// from the renderGutter format string, never regenerated by the painter.
func TestPaintUnifiedGoldenRows_Differential(t *testing.T) {
	doc, err := diffreview.Parse(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1 +1 @@
-old
+new
`)
	require.NoError(t, err)
	rows := doc.Rows()
	require.Len(t, rows, 4)

	vis := make([]Vis, len(rows))
	for i := range vis {
		vis[i] = Vis{Kind: VisRow, Doc: i}
	}
	surf := Paint(components.DrawContext{
		Max:    components.Size{Width: 16, Height: 8},
		Method: xui.WidthUnicode,
	}, Model{Theme: components.DefaultTheme(), Rows: rows, Vis: vis})

	want := []string{
		" diff review" + strings.Repeat(" ", 4),
		"▎ main.go" + strings.Repeat(" ", 7),
		"@@ -1 +1 @@" + strings.Repeat(" ", 5),
		"1   - old" + strings.Repeat(" ", 7),
		"  1 + new" + strings.Repeat(" ", 7),
		strings.Repeat(" ", 16), // unpainted body rows
		strings.Repeat(" ", 16),
		strings.Repeat(" ", 16), // status row is washed blank here
	}
	assert.Equal(t, want, surfaceRows(t, surf))
}

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
	alphabet := []string{"a", "Z", "7", " ", "日", "本", "😀", "é", "\x01", "\xff"}
	rng := rand.New(rand.NewSource(42))
	for i := range 300 {
		spans := trioRandomSpans(rng, alphabet)
		cols := rng.Intn(16) - 2 // includes negative budgets
		got := trioSpanText(skipCols(spans, cols, xui.WidthUnicode))
		assert.Equal(t, trioSkipOracle(spans, cols), got, "case %d: skipCols(%q, %d)", i, trioSpanText(spans), cols)
	}
	for _, text := range []string{"", "日", "ééé", "😀😀", "a日😀é", "\xff\x01"} {
		spans := []components.Span{{Text: text}}
		for cols := -1; cols <= 8; cols++ {
			got := trioSpanText(skipCols(spans, cols, xui.WidthUnicode))
			assert.Equal(t, trioSkipOracle(spans, cols), got, "skipCols(%q, %d)", text, cols)
		}
	}
}
