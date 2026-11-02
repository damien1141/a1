package diffview

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/util/diffreview"
)

const highlightRowLimit = 5000

type syntaxLine struct {
	rowIndex int
	fileName string
	code     string
	kind     diffreview.RowKind
}

// HighlightRows tokenizes code lines per file side. Large diffs skip highlighting.
func HighlightRows(rows []diffreview.Row, th components.Theme) map[int][]components.Span {
	if len(rows) > highlightRowLimit {
		return nil
	}
	out := make(map[int][]components.Span, len(rows))
	var oldSide, newSide []syntaxLine
	flush := func() {
		highlightSide(oldSide, th, out)
		highlightSide(newSide, th, out)
		oldSide, newSide = nil, nil
	}
	for i, row := range rows {
		switch row.Kind {
		case diffreview.RowHunk, diffreview.RowFile, diffreview.RowMeta, diffreview.RowPreamble,
			diffreview.RowCommitHeader, diffreview.RowCommitMeta, diffreview.RowCommitMessage,
			diffreview.RowCommitTrailer, diffreview.RowBlank:
			flush()
		}
		if row.Code == "" {
			continue
		}
		line := syntaxLine{rowIndex: i, fileName: row.FileName, code: row.Code, kind: row.Kind}
		switch row.Kind {
		case diffreview.RowContext:
			oldSide = append(oldSide, line)
			newSide = append(newSide, line)
		case diffreview.RowDelete:
			oldSide = append(oldSide, line)
		case diffreview.RowAdd:
			newSide = append(newSide, line)
		}
	}
	flush()
	return out
}

func highlightSide(lines []syntaxLine, th components.Theme, out map[int][]components.Span) {
	if len(lines) == 0 {
		return
	}
	lexer := lexers.Match(lines[0].fileName)
	if lexer == nil || lexer == lexers.Fallback {
		return
	}
	lexer = chroma.Coalesce(lexer)
	var src strings.Builder
	for _, line := range lines {
		src.WriteString(line.code)
		src.WriteByte('\n')
	}
	it, err := lexer.Tokenise(nil, src.String())
	if err != nil {
		return
	}
	tokenLines := chroma.SplitTokensIntoLines(it.Tokens())
	for i, line := range lines {
		if i >= len(tokenLines) {
			continue
		}
		spans := tokensToSpans(tokenLines[i], th, rowStyle(th, line.kind))
		if len(spans) > 0 {
			out[line.rowIndex] = spans
		}
	}
}

func tokensToSpans(tokens []chroma.Token, th components.Theme, base xui.Style) []components.Span {
	spans := make([]components.Span, 0, len(tokens))
	for _, tok := range tokens {
		text := strings.TrimSuffix(tok.Value, "\n")
		if text == "" {
			continue
		}
		st := chromaStyle(tok.Type, th, base)
		spans = append(spans, components.Span{Text: text, Style: st})
	}
	return spans
}

func chromaStyle(t chroma.TokenType, th components.Theme, base xui.Style) xui.Style {
	switch {
	case t.InCategory(chroma.Comment), t.InCategory(chroma.CommentPreproc):
		return th.Muted
	case t.InCategory(chroma.Keyword), t.InCategory(chroma.KeywordType):
		st := th.ToolName
		st.Bold = true
		return st
	case t.InCategory(chroma.String), t.InCategory(chroma.LiteralString):
		st := th.Success
		st.Bold = false
		return st
	case t.InCategory(chroma.LiteralNumber), t.InCategory(chroma.LiteralDate):
		st := th.Identity
		st.Bold = false
		return st
	case t.InCategory(chroma.NameFunction), t.InCategory(chroma.NameClass):
		st := th.Accent
		st.Underline = false
		return st
	case t.InCategory(chroma.NameBuiltin), t.InCategory(chroma.NameDecorator):
		return th.TitleOrForeground()
	case t.InCategory(chroma.Error):
		return th.Destructive
	default:
		return base
	}
}

func applyInline(
	spans []components.Span,
	code string,
	inline []diffreview.InlineSpan,
	mark xui.Style,
) []components.Span {
	if len(inline) == 0 {
		return spans
	}
	if len(spans) == 0 {
		spans = []components.Span{{Text: code, Style: mark}}
	}
	type cut struct {
		start, end int
		style      xui.Style
	}
	var cuts []cut
	off := 0
	for _, sp := range spans {
		end := off + len(sp.Text)
		cuts = append(cuts, cut{start: off, end: end, style: sp.Style})
		off = end
	}
	out := make([]components.Span, 0, len(spans)+len(inline))
	for _, c := range cuts {
		pos := c.start
		for pos < c.end {
			next := c.end
			st := c.style
			for _, in := range inline {
				if in.Start >= c.end || in.End <= pos {
					continue
				}
				if in.Start > pos && in.Start < next {
					next = in.Start
					st = c.style
				} else if in.Start <= pos && in.End > pos {
					if in.End < next {
						next = in.End
					}
					st = mark
					st.Fg = c.style.Fg
					break
				}
			}
			if next <= pos {
				next = pos + 1
			}
			if next > len(code) {
				next = len(code)
			}
			if next > c.end {
				next = c.end
			}
			if next > pos {
				out = append(out, components.Span{Text: code[pos:next], Style: st})
			}
			pos = next
		}
	}
	return out
}
