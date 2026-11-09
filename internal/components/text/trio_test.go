package text

import (
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"

	"github.com/damien1141/a1/internal/components"
)

// naiveWrapWidth is the oracle's width table. The corpus below draws only from
// ASCII, CJK ideographs, precomposed accents, and single-rune emoji — classes
// where rune width and cluster width agree, so the oracle stays honest.
func naiveWrapWidth(r rune) int {
	switch {
	case r < 0x80:
		return 1 // printable ASCII (corpus has no control runes)
	case r >= 0x2E80 && r <= 0xA4CF:
		return 2 // CJK ideographs (corpus: 日本語)
	case r >= 0x1F300 && r <= 0x1FAFF:
		return 2 // emoji (corpus: 😀)
	default:
		return 1 // precomposed accents (corpus: é)
	}
}

// naiveWrap is the spec written from scratch: split on hard newlines, then
// greedily fill each visual row with clusters until the next one would
// overflow; a cluster wider than a whole row sits on a row of its own.
func naiveWrap(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		var cur strings.Builder
		curW := 0
		for _, r := range para {
			cw := naiveWrapWidth(r)
			if curW > 0 && curW+cw > width {
				out = append(out, cur.String())
				cur.Reset()
				curW = 0
			}
			cur.WriteRune(r)
			curW += cw
		}
		out = append(out, cur.String())
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

// oracle: naive greedy-wrap reference (above) on randomized and edge-case
// inputs; for pure-ASCII, newline-free strings the same call is cross-checked
// against plain stdlib byte slicing (width-sized chunks).
func TestWrapEditorLines_MatchesNaiveGreedyWrap_Differential(t *testing.T) {
	alphabet := []string{"a", "Z", "7", " ", "é", "日", "本", "語", "😀", "\n"}
	rng := rand.New(rand.NewSource(42))
	for i := range 300 {
		var sb strings.Builder
		for j := rng.Intn(14); j > 0; j-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		s := sb.String()
		width := rng.Intn(15) - 2 // includes negative and zero widths
		assert.Equal(t, naiveWrap(s, width), WrapEditorLines(s, width, xui.WidthUnicode),
			"case %d: wrap(%q, %d)", i, s, width)
	}

	// Edge cases the random walk may miss.
	for _, s := range []string{"", "\n", "\n\n\n", "a\n\nb", "日本語", "é", "😀😀😀",
		strings.Repeat("日", 20), "a\r\nb", "mixed 日 and é and 😀 text"} {
		for width := -1; width <= 12; width++ {
			assert.Equal(t, naiveWrap(s, width), WrapEditorLines(s, width, xui.WidthUnicode),
				"wrap(%q, %d)", s, width)
		}
	}

	// Stdlib cross-check: with only single-column runes and no newlines,
	// wrapping degenerates to exact width-sized chunks of the input.
	asciiChunks := func(s string, width int) []string {
		if width < 1 {
			width = 1
		}
		var out []string
		for i := 0; i < len(s); i += width {
			out = append(out, s[i:min(i+width, len(s))])
		}
		if len(out) == 0 {
			out = []string{""}
		}
		return out
	}
	letters := []byte("azAZ09 _-")
	for i := range 100 {
		var sb strings.Builder
		for j := rng.Intn(30); j > 0; j-- {
			sb.WriteByte(letters[rng.Intn(len(letters))])
		}
		s := sb.String()
		width := rng.Intn(12) - 1
		assert.Equal(t, asciiChunks(s, width), WrapEditorLines(s, width, xui.WidthUnicode),
			"ascii case %d: wrap(%q, %d)", i, s, width)
	}
}

// naiveCursor re-derives the caret position from the wrap spec without
// touching the package: wrap the cursor prefix, take the last visual row, and
// apply the boundary rule (a caret at the right edge of a full row belongs to
// the next row, column 0 — compared against the raw width, like the impl).
func naiveCursor(s string, cursor, width int) (line, col int) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(s) {
		cursor = len(s)
	}
	lines := naiveWrap(s[:cursor], width)
	line = len(lines) - 1
	for _, r := range lines[line] {
		col += naiveWrapWidth(r)
	}
	if col >= width {
		return line + 1, 0
	}
	return line, col
}

// oracle: naive prefix scan (above) on randomized inputs, every rune-boundary
// cursor, and out-of-range cursors (which must clamp, never panic).
func TestCursorLineCol_MatchesNaivePrefixScan_Differential(t *testing.T) {
	alphabet := []string{"a", "Z", "7", " ", "é", "日", "本", "語", "😀", "\n"}
	rng := rand.New(rand.NewSource(7))
	for i := range 200 {
		var sb strings.Builder
		for j := rng.Intn(12); j > 0; j-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		s := sb.String()

		// Byte offsets on rune boundaries (the invariant ChatInput maintains),
		// plus out-of-range cursors that must clamp rather than panic.
		offsets := []int{-3, 0, len(s), len(s) + 5}
		for off := range s {
			offsets = append(offsets, off+runeSizeAt(s, off))
		}

		for _, cursor := range offsets {
			for width := 1; width <= 10; width++ {
				wantLine, wantCol := naiveCursor(s, cursor, width)
				gotLine, gotCol := CursorLineCol(s, cursor, width, xui.WidthUnicode)
				assert.Equal(t, wantLine, gotLine, "case %d: cursor(%q, %d, %d) line", i, s, cursor, width)
				assert.Equal(t, wantCol, gotCol, "case %d: cursor(%q, %d, %d) col", i, s, cursor, width)
			}
		}
	}

	// Hand-pinned edges: caret at a soft-wrap boundary, after a newline, and
	// the degenerate widths.
	for _, tt := range []struct {
		s        string
		cursor   int
		width    int
		wantLine int
		wantCol  int
	}{
		{"", 0, 5, 0, 0},
		{"ab", 2, 2, 1, 0},  // full row: caret drops to the next row
		{"ab", 1, 2, 0, 1},  // mid row
		{"a\n", 2, 5, 1, 0}, // right after the newline
		{"日本", 6, 4, 1, 0},  // two wide glyphs fill two rows
	} {
		gotLine, gotCol := CursorLineCol(tt.s, tt.cursor, tt.width, xui.WidthUnicode)
		assert.Equal(t, tt.wantLine, gotLine, "cursor(%q, %d, %d) line", tt.s, tt.cursor, tt.width)
		assert.Equal(t, tt.wantCol, gotCol, "cursor(%q, %d, %d) col", tt.s, tt.cursor, tt.width)
	}
}

// runeSizeAt returns the byte size of the rune starting at off (off is a
// rune start by construction: callers iterate with range).
func runeSizeAt(s string, off int) int {
	for _, r := range s[off:] {
		return utf8.RuneLen(r)
	}
	return 0
}

// naiveSnap maps every covered column to its glyph's start column first, then
// looks the column up — an interval-index formulation, not the left-to-right
// scan the implementation performs.
func naiveSnap(buf []xui.Cell, rowW, col, row int) int {
	if buf == nil || rowW < 1 || col < 0 || row < 0 || row*rowW >= len(buf) {
		return col
	}
	cover := map[int]int{}
	x := 0
	for x < rowW {
		i := row*rowW + x
		if i >= len(buf) {
			break
		}
		step := int(buf[i].Width)
		step = max(step, 1)
		for c := x; c < x+step; c++ {
			cover[c] = x
		}
		x += step
	}
	if start, ok := cover[col]; ok {
		return start
	}
	return col
}

// oracle: interval-map reference (above) over hand-built and randomized cell
// rows mixing single- and double-width glyphs.
func TestSnapSurfaceColToGlyphStart_MatchesIntervalMap_Differential(t *testing.T) {
	cell := func(ch string, w uint8) xui.Cell { return xui.Cell{Char: ch, Width: w} }
	trail := func() xui.Cell { return xui.Cell{Trail: true} }

	// Two hand-built rows (rowW 6). Row 0: "a 日 b 本" (a:0, 日:1-2,
	// b:3, 本:4-5). Row 1: "😀 x y z" with column 5 left blank.
	buf := []xui.Cell{
		cell("a", 1), cell("日", 2), trail(), cell("b", 1), cell("本", 2), trail(),
		cell("😀", 2), trail(), cell("x", 1), cell("y", 1), cell("z", 1), cell("", 0),
	}
	// Hand-derived expectations for row 0: 日 covers columns 1-2, 本 covers 4-5.
	assert.Equal(t, 0, SnapSurfaceColToGlyphStart(buf, 6, 0, 0))
	assert.Equal(t, 1, SnapSurfaceColToGlyphStart(buf, 6, 1, 0), "日's primary column")
	assert.Equal(t, 1, SnapSurfaceColToGlyphStart(buf, 6, 2, 0), "日's trailing column snaps to its start")
	assert.Equal(t, 3, SnapSurfaceColToGlyphStart(buf, 6, 3, 0))
	assert.Equal(t, 4, SnapSurfaceColToGlyphStart(buf, 6, 5, 0), "inside 本")
	// Row 1: 😀 covers 0-1; column 5 has no glyph start and stays put.
	assert.Equal(t, 0, SnapSurfaceColToGlyphStart(buf, 6, 1, 1), "inside 😀")
	assert.Equal(t, 2, SnapSurfaceColToGlyphStart(buf, 6, 2, 1))
	assert.Equal(t, 5, SnapSurfaceColToGlyphStart(buf, 6, 5, 1))
	// Guards: identity for nil buffer, degenerate width, negative inputs, and
	// a row past the buffer.
	assert.Equal(t, 4, SnapSurfaceColToGlyphStart(nil, 6, 4, 0))
	assert.Equal(t, 4, SnapSurfaceColToGlyphStart(buf, 0, 4, 0))
	assert.Equal(t, -1, SnapSurfaceColToGlyphStart(buf, 6, -1, 0))
	assert.Equal(t, 3, SnapSurfaceColToGlyphStart(buf, 6, 3, -2))
	assert.Equal(t, 2, SnapSurfaceColToGlyphStart(buf, 6, 2, 2), "row past the buffer")

	for row := 0; row*6 < len(buf); row++ {
		for col := 0; col <= 7; col++ {
			assert.Equal(t, naiveSnap(buf, 6, col, row), SnapSurfaceColToGlyphStart(buf, 6, col, row),
				"hand buffer: snap(row=%d, col=%d)", row, col)
		}
	}

	// Randomized rows that tile exactly: each glyph is a primary cell plus
	// its trail slots, so every row occupies exactly rowW buffer slots.
	rng := rand.New(rand.NewSource(13))
	for i := range 200 {
		rowW := 1 + rng.Intn(8)
		rows := 1 + rng.Intn(3)
		var buf2 []xui.Cell
		for range rows {
			remaining := rowW
			for remaining > 0 {
				w := 1 + rng.Intn(2)
				if w > remaining {
					w = remaining
				}
				buf2 = append(buf2, cell("g", uint8(w)))
				for range w - 1 {
					buf2 = append(buf2, trail())
				}
				remaining -= w
			}
		}
		// Sometimes cut the buffer mid-row: the walk must stop cleanly.
		if len(buf2) > 0 && rng.Intn(3) == 0 {
			buf2 = buf2[:rng.Intn(len(buf2))]
		}
		col := rng.Intn(rowW+4) - 2
		row := rng.Intn(rows+2) - 1
		assert.Equal(t, naiveSnap(buf2, rowW, col, row), SnapSurfaceColToGlyphStart(buf2, rowW, col, row),
			"case %d: snap(rowW=%d, col=%d, row=%d)", i, rowW, col, row)
	}
}

// oracle: hand-written golden literals — the expected concatenated span text
// for fixed markdown documents, derived from the CommonMark/GFM source by
// hand, never regenerated from RenderMarkdown's output.
func TestRenderMarkdown_GoldenSpanText_Differential(t *testing.T) {
	th := components.DefaultTheme()

	t.Run("mixed document", func(t *testing.T) {
		src := "# Title\n" +
			"\n" +
			"some **bold** text and `go test`.\n" +
			"\n" +
			"- alpha\n" +
			"- beta\n" +
			"\n" +
			"> quoted line\n" +
			"\n" +
			"plain tail"
		// Structural markers (#, **, `, -, >) are stripped; blocks are
		// separated by one blank line; bullets and the quote rule are chrome.
		want := "Title\n" +
			"\n" +
			"some bold text and go test.\n" +
			"\n" +
			"• alpha\n" +
			"• beta\n" +
			"\n" +
			"▎ quoted line\n" +
			"\n" +
			"plain tail"
		assert.Equal(t, want, spansText(RenderMarkdown(src, th)))
	})

	t.Run("gfm table", func(t *testing.T) {
		src := "| a | bb |\n" +
			"|---|---|\n" +
			"| 1 | 2 |"
		// Columns are padded to the widest cell (the header's "bb"),
		// separated by " │ ", with a rule row whose ┼ sits under the │.
		want := "a │ bb\n" +
			"──┼───\n" +
			"1 │ 2 "
		assert.Equal(t, want, spansText(RenderMarkdown(src, th)))
	})

	t.Run("ordered list numbering", func(t *testing.T) {
		src := "3. third\n" +
			"4. fourth"
		want := "3. third\n" +
			"4. fourth"
		assert.Equal(t, want, spansText(RenderMarkdown(src, th)))
	})

	t.Run("fenced code keeps its lines", func(t *testing.T) {
		src := "```go\n" +
			"a := 1\n" +
			"b := 2\n" +
			"```"
		// The language becomes a caption line; the body survives verbatim
		// (syntax coloring is style-only, invisible in span text).
		want := "go\n" +
			"a := 1\n" +
			"b := 2"
		assert.Equal(t, want, spansText(RenderMarkdown(src, th)))
	})
}
