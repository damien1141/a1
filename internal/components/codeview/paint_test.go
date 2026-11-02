package codeview

import (
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

// testCtxWidth is the pane width the painter tests draw into.
const testCtxWidth = 80

func testCtx(h int) components.DrawContext {
	return components.DrawContext{
		Max:    components.Size{Width: testCtxWidth, Height: h},
		Method: xui.WidthUnicode,
	}
}

// oneDigitGutter is the gutter width for a file with fewer than ten lines.
func oneDigitGutter() int { return 1 + gutterRuleWidth }

func TestPaintRendersTitleGutterAndStatus(t *testing.T) {
	th := components.DefaultTheme()
	lines := []string{"package main", `func main() {`, "}"}
	surf := Paint(testCtx(10), Model{
		Theme:  th,
		Title:  "src/app.go",
		Status: "go" + " · 214 lines",
		Hint:   "esc close · j/k move",
		Path:   "src/app.go",
		Lines:  lines,
	})
	text := components.SurfaceText(surf)
	assert.Contains(t, text, "src/app.go")
	assert.Contains(t, text, "1 │ package main")
	assert.Contains(t, text, "2 │ func main() {")
	assert.Contains(t, text, "go · 214 lines")
	assert.Contains(t, text, "esc close")

	// Title row is background-filled with the title role.
	assert.Equal(t, th.Title.Bg, surf.Buffer[0].Style.Bg)
	// Status row is background-filled with the muted role.
	statusY := surf.Size.Height - 1
	assert.Equal(t, th.Muted.Bg, surf.Buffer[statusY*surf.Size.Width].Style.Bg)
}

func TestPaintGutterAlignsLineNumbers(t *testing.T) {
	lines := make([]string, 12)
	for i := range lines {
		lines[i] = "line " + string(rune('a'+i))
	}
	surf := Paint(testCtx(16), Model{Theme: components.DefaultTheme(), Lines: lines})
	text := components.SurfaceText(surf)
	// Two-digit width: " 1" is right-aligned, "10" fills the gutter.
	assert.Contains(t, text, " 1 │ line a")
	assert.Contains(t, text, "10 │ line j")
}

func TestPaintRespectsScroll(t *testing.T) {
	lines := []string{"line one", "line two", "line three", "line four", "line five"}
	surf := Paint(testCtx(8), Model{
		Theme:  components.DefaultTheme(),
		Lines:  lines,
		Scroll: 3,
	})
	text := components.SurfaceText(surf)
	assert.Contains(t, text, "4 │ line four")
	assert.Contains(t, text, "5 │ line five")
	assert.NotContains(t, text, "1 │ line one")
}

func TestPaintRespectsViewH(t *testing.T) {
	lines := []string{"first", "second", "third", "fourth"}
	surf := Paint(testCtx(12), Model{
		Theme: components.DefaultTheme(),
		Lines: lines,
		ViewH: 2,
	})
	text := components.SurfaceText(surf)
	assert.Contains(t, text, "1 │ first")
	assert.Contains(t, text, "2 │ second")
	assert.NotContains(t, text, "3 │ third")
}

func TestPaintRespectsXScroll(t *testing.T) {
	surf := Paint(testCtx(8), Model{
		Theme:   components.DefaultTheme(),
		Lines:   []string{"0123456789"},
		XScroll: 4,
	})
	text := components.SurfaceText(surf)
	assert.Contains(t, text, "1 │ 456789")
	assert.NotContains(t, text, "0123456789")
}

func TestPaintScrollsWideGlyphsWhole(t *testing.T) {
	surf := Paint(testCtx(8), Model{
		Theme:   components.DefaultTheme(),
		Lines:   []string{"日本語テスト"},
		XScroll: 2,
	})
	text := components.SurfaceText(surf)
	// One full-width glyph leaves per two columns scrolled; no half glyph.
	assert.Contains(t, text, "1 │ 本語テスト")
	assert.NotContains(t, text, "日")
}

func TestPaintSetsCursorOnCursorLine(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "code line"
	}
	surf := Paint(testCtx(12), Model{
		Theme:      components.DefaultTheme(),
		Lines:      lines,
		Scroll:     5,
		CursorLine: 8,
		CursorCol:  3,
	})
	require.NotNil(t, surf.Cursor)
	assert.Equal(t, 4, surf.Cursor.Y, "cursor row follows scroll")
	assert.Equal(t, 5+3, surf.Cursor.X, "cursor col follows the gutter")
}

func TestPaintDropsCursorWhenScrolledOut(t *testing.T) {
	th := components.DefaultTheme()
	lines := []string{"alpha", "bravo", "charlie"}
	surf := Paint(testCtx(8), Model{
		Theme:      th,
		Lines:      lines,
		Scroll:     2,
		CursorLine: 0,
		CursorCol:  0,
	})
	assert.Nil(t, surf.Cursor, "caret above the viewport is not exposed")

	off := Paint(testCtx(8), Model{
		Theme:      th,
		Lines:      lines,
		CursorLine: 0,
		CursorCol:  0,
		XScroll:    10,
	})
	assert.Nil(t, off.Cursor, "caret scrolled off the left edge is not exposed")
}

func TestPaintCursorLineCarriesWash(t *testing.T) {
	th := components.DefaultTheme()
	surf := Paint(testCtx(8), Model{
		Theme:      th,
		Lines:      []string{"alpha", "bravo", "charlie"},
		CursorLine: 1,
	})
	assert.Equal(t, th.SelectionBg.Bg, surf.Buffer[2*surf.Size.Width].Style.Bg, "cursor row washed")
	assert.NotEqual(t, th.SelectionBg.Bg, surf.Buffer[1*surf.Size.Width].Style.Bg, "other rows plain")
}

func TestPaintSelectionHighlightsRange(t *testing.T) {
	th := components.DefaultTheme()
	surf := Paint(testCtx(10), Model{
		Theme:      th,
		Lines:      []string{"alpha", "bravo", "charlie", "delta"},
		CursorLine: 2,
		Selecting:  true,
		SelStart:   components.Point{X: 1, Y: 1},
		SelEnd:     components.Point{X: 3, Y: 3},
	})
	gutterW := oneDigitGutter()
	row := func(line int) int { return 1 + line } // scroll = 0

	// First selected line starts at the anchor column.
	assert.Equal(t, th.SelectionBg.Bg, surf.Buffer[row(1)*surf.Size.Width+gutterW+1].Style.Bg)
	assert.NotEqual(t, th.SelectionBg.Bg, surf.Buffer[row(1)*surf.Size.Width+gutterW].Style.Bg)
	// Interior line runs edge to edge, gutter included.
	assert.Equal(t, th.SelectionBg.Bg, surf.Buffer[row(2)*surf.Size.Width].Style.Bg)
	assert.Equal(t, th.SelectionBg.Bg, surf.Buffer[row(2)*surf.Size.Width+surf.Size.Width-1].Style.Bg)
	// Last selected line stops at the cursor column.
	assert.Equal(t, th.SelectionBg.Bg, surf.Buffer[row(3)*surf.Size.Width+gutterW+3].Style.Bg)
	assert.NotEqual(t, th.SelectionBg.Bg, surf.Buffer[row(3)*surf.Size.Width+gutterW+4].Style.Bg)
	// Lines outside the range stay clean.
	assert.NotEqual(t, th.SelectionBg.Bg, surf.Buffer[row(0)*surf.Size.Width].Style.Bg)
}

func TestPaintSelectionSurvivesScroll(t *testing.T) {
	th := components.DefaultTheme()
	surf := Paint(testCtx(8), Model{
		Theme:     th,
		Lines:     []string{"alpha", "bravo", "charlie", "delta", "echo"},
		Selecting: true,
		SelStart:  components.Point{X: 0, Y: 0},
		SelEnd:    components.Point{X: 2, Y: 4},
		Scroll:    2,
	})
	// Lines 0-1 are above the viewport, so the first visible row is interior.
	assert.Equal(t, th.SelectionBg.Bg, surf.Buffer[1*surf.Size.Width].Style.Bg)
	// The last selected line ends at the anchor column (line 4 -> row 3).
	gutterW := oneDigitGutter()
	assert.Equal(t, th.SelectionBg.Bg, surf.Buffer[3*surf.Size.Width+gutterW+2].Style.Bg)
	assert.NotEqual(t, th.SelectionBg.Bg, surf.Buffer[3*surf.Size.Width+gutterW+3].Style.Bg)
}

func TestPaintEmptyFileShowsPlaceholder(t *testing.T) {
	surf := Paint(testCtx(8), Model{
		Theme: components.DefaultTheme(),
		Lines: nil,
		Empty: "empty file",
	})
	assert.Contains(t, components.SurfaceText(surf), "empty file")
	assert.Nil(t, surf.Cursor)
}

func TestPaintFallsBackToDefaultHint(t *testing.T) {
	surf := Paint(testCtx(8), Model{
		Theme: components.DefaultTheme(),
		Lines: []string{"package main"},
	})
	assert.Contains(t, components.SurfaceText(surf), "j/k move")
}

func TestSkipColsNoopOnZero(t *testing.T) {
	spans := []components.Span{{Text: "abc"}}
	assert.Equal(t, spans, skipCols(spans, 0, xui.WidthUnicode))
}

func TestClipSpansKeepsWideGlyphWhole(t *testing.T) {
	spans := []components.Span{{Text: "日本語"}}
	got := clipSpans(spans, 3, xui.WidthUnicode)
	require.Len(t, got, 1)
	assert.Equal(t, "日", got[0].Text, "a wide glyph is never split")
}

func TestClipSpansZeroWidth(t *testing.T) {
	assert.Nil(t, clipSpans([]components.Span{{Text: "abc"}}, 0, xui.WidthUnicode))
}

func TestPaintWideGlyphLineRendersContiguous(t *testing.T) {
	surf := Paint(testCtx(8), Model{
		Theme: components.DefaultTheme(),
		Lines: []string{"// 日本語テスト"},
	})
	assert.Contains(t, components.SurfaceText(surf), "│ // 日本語テスト")
}

func TestPaintDoesNotMutateCachedHighlightSpans(t *testing.T) {
	lines := []string{
		"package main",
		"",
		`func main() { println("hi") }`,
	}
	th := components.DefaultTheme()
	highlighted := Highlight("main.go", lines, th)
	require.NotEmpty(t, highlighted)
	cached := highlighted[0]
	before := append([]components.Span(nil), cached...)

	ctx := testCtx(8)
	_ = Paint(ctx, Model{Theme: th, Title: "main.go", Lines: lines, Highlight: highlighted, CursorLine: 0})
	_ = Paint(ctx, Model{Theme: th, Title: "main.go", Lines: lines, Highlight: highlighted, CursorLine: 2})

	got := highlighted[0]
	require.Len(t, got, len(before))
	for i := range got {
		assert.Equal(t, before[i].Style.Bg, got[i].Style.Bg, "span %d bg mutated", i)
		assert.Equal(t, before[i].Style.Fg, got[i].Style.Fg, "span %d fg mutated", i)
	}
}

func TestPaintUsesHighlightSpans(t *testing.T) {
	lines := []string{"package main"}
	th := components.DefaultTheme()
	highlighted := Highlight("main.go", lines, th)
	require.NotEmpty(t, highlighted)
	surf := Paint(testCtx(8), Model{
		Theme:     th,
		Lines:     lines,
		Highlight: highlighted,
	})
	gutterW := oneDigitGutter()
	cell := surf.Buffer[1*surf.Size.Width+gutterW]
	assert.Equal(t, "p", cell.Char)
	assert.Equal(t, th.ToolName.Fg, cell.Style.Fg)
}

// Cutting inside a wide glyph must keep it whole without eating the head of the
// following spans: that used to delete characters from the middle of the line.
func TestSkipColsKeepsFollowingSpansIntact(t *testing.T) {
	spans := []components.Span{{Text: "日本"}, {Text: "abc"}}
	got := skipCols(spans, 1, xui.WidthUnicode)
	var b strings.Builder
	for _, sp := range got {
		b.WriteString(sp.Text)
	}
	assert.Equal(t, "日本abc", b.String())
}

func TestSkipColsDropsWholeNarrowSpans(t *testing.T) {
	spans := []components.Span{{Text: "ab"}, {Text: "cd"}}
	got := skipCols(spans, 3, xui.WidthUnicode)
	var b strings.Builder
	for _, sp := range got {
		b.WriteString(sp.Text)
	}
	assert.Equal(t, "d", b.String())
}

func TestCursorDroppedPastLastLine(t *testing.T) {
	surf := Paint(testCtx(12), Model{
		Theme:      components.DefaultTheme(),
		Lines:      []string{"package main"},
		CursorLine: 99,
	})
	assert.Nil(t, surf.Cursor)
}

// A selection must not bleed below the body into rows that hold no line text.
func TestSelectionClipsToBodyHeight(t *testing.T) {
	th := components.DefaultTheme()
	surf := Paint(testCtx(12), Model{
		Theme:      th,
		Lines:      []string{"one", "two", "three", "four", "five"},
		ViewH:      2,
		Selecting:  true,
		SelStart:   components.Point{X: 0, Y: 0},
		SelEnd:     components.Point{X: 3, Y: 4},
		CursorLine: 0,
	})
	bodyLast := 2 // rows 1..ViewH
	assert.Equal(t, th.SelectionBg.Bg, surf.Buffer[1*surf.Size.Width].Style.Bg)
	assert.NotEqual(t, th.SelectionBg.Bg, surf.Buffer[(bodyLast+1)*surf.Size.Width].Style.Bg)
}
