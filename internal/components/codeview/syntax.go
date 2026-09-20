package codeview

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/pulseaiclub/xui"

	"github.com/pulseaiclub/phi/internal/components"
)

// highlightRowLimit caps chroma work on huge files. Beyond it the pane stays
// plain: tokenising a 100k-line file costs more than the viewport shows.
const highlightRowLimit = 5000

// Highlight tokenises lines with chroma for the given path. Returns nil for
// unknown languages, empty input, or more than 5000 lines.
func Highlight(path string, lines []string, th components.Theme) map[int][]components.Span {
	if len(lines) == 0 || len(lines) > highlightRowLimit || strings.TrimSpace(path) == "" {
		return nil
	}
	lexer := lexers.Match(path)
	if lexer == nil || lexer == lexers.Fallback {
		return nil
	}
	lexer = chroma.Coalesce(lexer)
	var src strings.Builder
	for _, line := range lines {
		src.WriteString(line)
		src.WriteByte('\n')
	}
	it, err := lexer.Tokenise(nil, src.String())
	if err != nil {
		return nil
	}
	tokenLines := chroma.SplitTokensIntoLines(it.Tokens())
	out := make(map[int][]components.Span, len(lines))
	for i := range lines {
		if i >= len(tokenLines) {
			break
		}
		spans := tokensToSpans(tokenLines[i], th)
		if len(spans) > 0 {
			out[i] = spans
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func tokensToSpans(tokens []chroma.Token, th components.Theme) []components.Span {
	spans := make([]components.Span, 0, len(tokens))
	for _, tok := range tokens {
		text := strings.TrimSuffix(tok.Value, "\n")
		if text == "" {
			continue
		}
		spans = append(spans, components.Span{Text: text, Style: chromaStyle(tok.Type, th)})
	}
	return spans
}

func chromaStyle(t chroma.TokenType, th components.Theme) xui.Style {
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
		return th.Foreground
	}
}
