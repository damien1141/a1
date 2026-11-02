package diffreview

import "strings"

// SideBySideRow maps one visual row onto document rows.
// Full is a spanning row (file/hunk/preamble). Left/Right are -1 when empty.
type SideBySideRow struct {
	Full  int
	Left  int
	Right int
}

// SideBySideRows pairs delete/add blocks for two-column display.
func SideBySideRows(rows []Row) []SideBySideRow {
	out := make([]SideBySideRow, 0, len(rows))
	contextLeft := true
	contextRight := true
	for i := 0; i < len(rows); {
		if rows[i].Kind == RowDelete {
			deleteStart := i
			for i < len(rows) && rows[i].Kind == RowDelete {
				i++
			}
			addStart := i
			for i < len(rows) && rows[i].Kind == RowAdd {
				i++
			}
			for deleteOffset, addOffset := 0, 0; deleteOffset < addStart-deleteStart || addOffset < i-addStart; {
				row := SideBySideRow{Full: -1, Left: -1, Right: -1}
				if deleteOffset < addStart-deleteStart {
					row.Left = deleteStart + deleteOffset
					deleteOffset++
				}
				if addOffset < i-addStart {
					row.Right = addStart + addOffset
					addOffset++
				}
				out = append(out, row)
			}
			continue
		}
		switch rows[i].Kind {
		case RowHunk:
			contextLeft, contextRight = sideBySideHunkContextSides(rows, i)
			out = append(out, SideBySideRow{Full: i, Left: -1, Right: -1})
		case RowAdd:
			out = append(out, SideBySideRow{Full: -1, Left: -1, Right: i})
		case RowContext:
			row := SideBySideRow{Full: -1, Left: -1, Right: -1}
			if contextLeft {
				row.Left = i
			}
			if contextRight {
				row.Right = i
			}
			out = append(out, row)
		default:
			contextLeft = true
			contextRight = true
			out = append(out, SideBySideRow{Full: i, Left: -1, Right: -1})
		}
		i++
	}
	return out
}

func sideBySideHunkContextSides(rows []Row, hunk int) (bool, bool) {
	hasDeletes := false
	hasAdds := false
	for i := hunk + 1; i < len(rows); i++ {
		switch rows[i].Kind {
		case RowDelete:
			hasDeletes = true
		case RowAdd:
			hasAdds = true
		case RowHunk, RowFile, RowMeta, RowCommitHeader, RowCommitMeta, RowCommitMessage, RowCommitTrailer,
			RowDiffStat, RowDiffStatSummary, RowBlank:
			return sideBySideContextSides(hasDeletes, hasAdds)
		}
	}
	return sideBySideContextSides(hasDeletes, hasAdds)
}

func sideBySideContextSides(hasDeletes, hasAdds bool) (bool, bool) {
	switch {
	case hasAdds && !hasDeletes:
		return false, true
	case hasDeletes && !hasAdds:
		return true, false
	default:
		return true, true
	}
}

// ContainsDoc reports whether the visual row includes docRow.
func (r SideBySideRow) ContainsDoc(docRow int) bool {
	return r.Full == docRow || r.Left == docRow || r.Right == docRow
}

// FirstDoc is the smallest non-negative document row on this visual row.
func (r SideBySideRow) FirstDoc() int {
	first := -1
	for _, docRow := range []int{r.Full, r.Left, r.Right} {
		if docRow >= 0 && (first < 0 || docRow < first) {
			first = docRow
		}
	}
	return first
}

// SideGutter is the one-column line-number gutter for side-by-side cells.
func SideGutter(rows []Row, row Row, left bool) string {
	width := sideLineNumberWidth(rows)
	oldNumber, newNumber := splitGutterNumbers(row)
	number := newNumber
	marker := " "
	if left {
		number = oldNumber
		if row.Kind == RowDelete {
			marker = "-"
		}
	} else if row.Kind == RowAdd {
		marker = "+"
	}
	return padLeft(number, width) + " " + marker + " "
}

func sideLineNumberWidth(rows []Row) int {
	width := 1
	for _, row := range rows {
		oldNumber, newNumber := splitGutterNumbers(row)
		width = max(width, len(oldNumber), len(newNumber))
	}
	return width
}

func splitGutterNumbers(row Row) (string, string) {
	fields := strings.Fields(row.Gutter)
	switch row.Kind {
	case RowContext:
		if len(fields) >= 2 {
			return fields[0], fields[1]
		}
	case RowDelete:
		if len(fields) >= 1 {
			return fields[0], ""
		}
	case RowAdd:
		if len(fields) >= 1 {
			return "", fields[0]
		}
	}
	return "", ""
}

func padLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}
