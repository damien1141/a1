package components

import "github.com/pulseaiclub/xui"

// Span is a styled run of text.
type Span struct {
	Text  string
	Style xui.Style
}

// RichLine is one visual line of spans.
type RichLine []Span

// PaintSpans writes spans starting at (x,y) on surface; returns columns advanced.
func PaintSpans(s *Surface, x, y int, spans []Span, method xui.WidthMethod) int {
	col := x
	for _, sp := range spans {
		if sp.Text == "" {
			continue
		}
		before := col
		// Advance by what Print actually painted — StringWidth can disagree
		// (e.g. clipped at edge, or control runes) and leave Default holes that
		// survive as blank gaps after win.Clear().
		adv := s.Print(col, y, sp.Text, sp.Style, method)
		col += adv
		if col == before && sp.Text != "" {
			col++
		}
	}
	return col - x
}

// WrapSpans soft-wraps spans to width, preserving style boundaries and hard
// newlines, and breaking BETWEEN words.
//
// The earlier implementation walked grapheme by grapheme with no word awareness,
// so it split mid-token: "head -3" became "hea" / "d -3", and flags and paths
// were cut with no mark. Every wrapped block read as noise.
//
// Two phases keep the break rules tractable:
//
//  1. FLATTEN the styled spans into grapheme cells, then group them into
//     tokens — a word (maximal run of non-space), a space run, or a hard break.
//  2. PACK tokens greedily. A word that does not fit ends the row and retries on
//     the next one; only a word wider than an entire row is hard-split, because
//     a URL or an unbroken path leaves no choice.
//
// The one subtlety is spaces, which are content in one place and a break
// artifact in another. The rule is WHERE THE ROW CAME FROM:
//
//   - a row that began at a real line start (input start, or after a newline)
//     keeps its leading spaces — that is indentation, in pasted code and in
//     tool output alike;
//   - a row that began because a word wrapped drops them — the space IS the
//     break, and starting a row with it would indent text that had no indent.
//
// There is no walk-back and no recursion: a row is final the moment it is
// flushed, so no later token can change what an earlier row contained.
func WrapSpans(spans []Span, width int, method xui.WidthMethod) []RichLine {
	if width < 1 {
		width = 1
	}

	// Phase 1: styled grapheme cells.
	type cell struct {
		text  string
		width int
		style xui.Style
	}
	var cells []cell
	for _, sp := range spans {
		rest := sp.Text
		for rest != "" {
			cluster, cw, next := xui.FirstGrapheme(rest, method)
			if cw < 1 {
				cw = 1
			}
			cells = append(cells, cell{text: cluster, width: cw, style: sp.Style})
			rest = next
		}
	}

	// Phase 2: greedy pack.
	var rows [][]cell
	var cur []cell
	curW := 0
	// wrappedRow records that the CURRENT row began because a word wrapped, as
	// opposed to a real line start. That is the whole rule for spaces.
	wrappedRow := false

	flush := func(fromWrap bool) {
		// Trailing spaces are invisible and inflate the row, so they never
		// survive a flush — EXCEPT when the space is the row's only content: at
		// a real line start that space is the arrow column, and dropping it
		// would shift every row one cell left.
		onlySpace := len(cur) > 0
		for _, cc := range cur {
			if cc.text != " " {
				onlySpace = false
				break
			}
		}
		if !onlySpace {
			for len(cur) > 0 && cur[len(cur)-1].text == " " {
				cur = cur[:len(cur)-1]
			}
		}
		rows = append(rows, cur)
		cur = nil
		curW = 0
		wrappedRow = fromWrap
	}

	for i := 0; i < len(cells); {
		if cells[i].text == "\n" {
			flush(false) // a hard break starts a real line: keep indentation
			i++
			continue
		}

		// Gather the maximal run of one kind: a space run, or a word.
		isSpace := cells[i].text == " "
		start := i
		tokW := 0
		for i < len(cells) && cells[i].text != "\n" && (cells[i].text == " ") == isSpace {
			tokW += cells[i].width
			i++
		}
		tok := cells[start:i]

		if isSpace {
			if len(cur) == 0 && wrappedRow {
				// The space is what caused the wrap. Drop it: a blank row adds
				// height and conveys nothing.
				continue
			}
			if curW+tokW > width {
				// It does not fit, so it is the break point. The next row is a
				// WRAP row, so any further leading space is dropped too.
				flush(true)
				continue
			}
			cur = append(cur, tok...)
			curW += tokW
			continue
		}

		if curW+tokW <= width {
			cur = append(cur, tok...)
			curW += tokW
			continue
		}
		if len(cur) > 0 {
			// End the row and retry this word on the fresh one.
			flush(true)
			i = start
			continue
		}
		// The word is wider than an entire row and the row is empty: hard-split.
		for _, cc := range tok {
			if curW+cc.width > width && curW > 0 {
				flush(true)
			}
			cur = append(cur, cc)
			curW += cc.width
		}
	}
	if len(cur) > 0 || len(rows) == 0 {
		flush(false)
	}

	// Re-emit each row, merging adjacent cells that share a style so a row is a
	// handful of spans rather than one per grapheme.
	out := make([]RichLine, 0, len(rows))
	for _, row := range rows {
		line := make(RichLine, 0, len(row))
		for _, c := range row {
			if n := len(line); n > 0 && line[n-1].Style == c.style {
				line[n-1].Text += c.text
				continue
			}
			line = append(line, Span{Text: c.text, Style: c.style})
		}
		out = append(out, line)
	}
	return out
}

// PaintRichLines paints wrapped lines into a new surface of given width.
func PaintRichLines(width int, lines []RichLine, method xui.WidthMethod, widget Widget) Surface {
	h := len(lines)
	h = max(h, 1)
	s := NewSurface(width, h, widget)
	for y, line := range lines {
		PaintSpans(&s, 0, y, line, method)
	}
	return s
}
