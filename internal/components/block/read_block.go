package block

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/chrome"
	"github.com/damien1141/a1/internal/components/status"
	"github.com/damien1141/a1/internal/util"
)

// ReadBlock renders a read / grep row as a card in the same shape as DiffBlock:
//
//	✓ read haiku.txt (5 lines)
//
//	     1  cold bytes on the wire
//	     2  a green cursor waits alone
//
// The `@file path` line the tools emit is dropped — the path is already the card
// header, so repeating it costs a row on a narrow terminal.
type ReadBlock struct {
	// Path is the display path shown in the header.
	Path string
	// Body is the tool output with its `@file path` header line removed.
	Body string
	// Status drives the glyph and header style.
	Status status.ToolStatus
	// Error is the failure text, shown when the call failed.
	Error string
	// Expanded controls whether the body is drawn.
	Expanded bool
	// Name is the tool name ("read" / "grep"); it is what the header verbs from.
	Name string
	// Detail is the arg-derived phrase from DetailFromArgs (grep's carries the
	// pattern and scope). It is preferred over Path for the header target.
	Detail  string
	Theme   components.Theme
	Spinner *status.Spinner
	// OnToggle is called when the user expands/collapses.
	OnToggle func(expanded bool)

	titleH int // title row count; body clicks don't toggle (allow selection)
}

func (readBlock *ReadBlock) theme() components.Theme {
	if readBlock.Theme.Success.Fg.Kind == 0 && readBlock.Theme.Foreground.Fg.Kind == 0 {
		return components.DefaultTheme()
	}
	return readBlock.Theme
}

// HasBody reports whether the row has anything to show beyond its header.
func (readBlock *ReadBlock) HasBody() bool {
	return strings.TrimSpace(readBlock.Body) != "" || strings.TrimSpace(readBlock.Error) != ""
}

// Handle toggles expansion on Enter/space or a left-click on the title row.
func (readBlock *ReadBlock) Handle(ctx *components.EventContext, ev xui.Event) {
	if !readBlock.HasBody() {
		return
	}
	switch e := ev.(type) {
	case xui.KeyEvent:
		if e.Code == xui.KeyEnter || (e.Code == xui.KeyRune && e.Rune == ' ') {
			readBlock.toggle(ctx)
		}
	case xui.MouseEvent:
		if e.Action == xui.MousePress && e.Button == xui.MouseLeft && e.Y >= 0 && e.Y < readBlock.titleH {
			readBlock.toggle(ctx)
		}
	}
}

func (readBlock *ReadBlock) toggle(ctx *components.EventContext) {
	readBlock.Expanded = !readBlock.Expanded
	if readBlock.OnToggle != nil {
		readBlock.OnToggle(readBlock.Expanded)
	}
	ctx.ConsumeAndRedraw()
}

// CopyText returns the path, the line count, and the body.
func (readBlock *ReadBlock) CopyText() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s", readBlock.displayPath())
	if n := readBlock.LineCount(); n > 0 {
		fmt.Fprintf(&sb, " (%d lines)", n)
	}
	if body := strings.TrimRight(readBlock.Body, "\n"); body != "" {
		fmt.Fprintf(&sb, "\n%s", body)
	}
	return sb.String()
}

// LineCount counts the non-blank body lines.
func (readBlock *ReadBlock) LineCount() int {
	n := 0
	for ln := range strings.SplitSeq(readBlock.Body, "\n") {
		if strings.TrimSpace(ln) != "" {
			n++
		}
	}
	return n
}

// displayPath returns the plain path for the row, used by CopyText. The header
// uses headerTarget instead, which also folds in grep's pattern phrase.
func (readBlock *ReadBlock) displayPath() string {
	path := strings.TrimSpace(readBlock.Path)
	path = strings.TrimPrefix(path, "@file ")
	if base, _, ok := strings.Cut(path, "#"); ok {
		path = strings.TrimSpace(base)
	}
	if path == "" {
		return "file"
	}
	return path
}

// Draw renders the "✓ read path (N lines)" header and, when expanded, the body.
func (readBlock *ReadBlock) Draw(ctx components.DrawContext) components.Surface {
	th := readBlock.theme()
	w := ctx.Max.Width
	if w <= 0 {
		w = 40
	}

	titleWrapped := components.WrapSpans(readBlock.titleSpans(th), w, ctx.Method)
	readBlock.titleH = len(titleWrapped)

	var bodyLines []components.RichLine
	if readBlock.HasBody() {
		bodyW := w
		if bodyW > 2 {
			bodyW -= 2
		}
		// A failure is the one thing the user must see without expanding.
		showBody := readBlock.Expanded || strings.TrimSpace(readBlock.Error) != ""
		if showBody {
			if errText := strings.TrimSpace(readBlock.Error); errText != "" {
				bodyLines = append(bodyLines, components.WrapSpans([]components.Span{
					{Text: "Error: " + errText, Style: th.Destructive},
				}, bodyW, ctx.Method)...)
			}
			bodyLines = append(bodyLines, readBodyLines(readBlock.Body, th, bodyW, ctx.Method)...)
		}
	}

	h := len(titleWrapped) + len(bodyLines)
	h = max(h, 1)
	s := components.NewSurface(w, h, readBlock)
	y := 0
	for _, line := range titleWrapped {
		components.PaintSpans(&s, 0, y, line, ctx.Method)
		y++
	}
	for _, line := range bodyLines {
		components.PaintSpans(&s, 2, y, line, ctx.Method)
		y++
	}
	return s
}

// titleSpans builds the "✓ read path (N lines) ↓" header. For grep the detail is
// already a phrase ("grep \"cursor\" in ."), so the name is not repeated and the
// path is left out — the path lives inside every match line anyway.
func (readBlock *ReadBlock) titleSpans(th components.Theme) []components.Span {
	icon, iconSt := chrome.ToolIcon(readBlock.Status, th, readBlock.Spinner)
	spans := []components.Span{{Text: icon + " ", Style: iconSt}}

	// Always lead with the verb ("read" / "grep") then the target, so read and
	// grep rows read the same way and neither is a bare path.
	spans = append(spans, components.Span{Text: readBlock.readerName() + " ", Style: th.ToolName})
	if target := readBlock.headerTarget(); target != "" && target != "file" {
		spans = append(spans, components.Span{Text: target, Style: th.ToolName})
	}
	if n := readBlock.LineCount(); n > 0 {
		spans = append(spans, components.Span{Text: fmt.Sprintf(" (%d lines)", n), Style: th.Muted})
	}
	if suf := chrome.StatusSuffix(readBlock.Status); suf != "" {
		spans = append(spans, components.Span{Text: suf, Style: th.Muted})
	}
	if readBlock.HasBody() {
		spans = append(spans, components.Span{Text: chrome.ExpandArrow(readBlock.Expanded), Style: th.Muted})
	}
	return spans
}

// headerTarget returns the text that identifies the row after the verb. A grep
// run carries an arg-derived phrase ("grep \"cursor\" in .") which is more
// informative than a path, so it is used with its own leading verb stripped —
// the header already prints one. Read carries a bare path.
func (readBlock *ReadBlock) headerTarget() string {
	verb := strings.ToLower(readBlock.readerName())
	for _, d := range []string{readBlock.Detail, readBlock.Path} {
		d = strings.TrimSpace(strings.TrimPrefix(d, "@file "))
		if d == "" || d == "file" {
			continue
		}
		if i := strings.Index(d, " "); i > 0 && strings.EqualFold(d[:i], verb) {
			d = strings.TrimSpace(d[i+1:])
		}
		if d != "" {
			return d
		}
	}
	return ""
}

// readerName names the tool when no target identifies the row.
func (readBlock *ReadBlock) readerName() string {
	if readBlock.Name != "" {
		return readBlock.Name
	}
	return "read"
}

// readBodyLines renders the tool output with a line-number gutter. The read tool
// already prints "  12|text"; grep prints "path:>>  12|text". Both are normalized
// to the same right-aligned gutter so read and grep rows look identical.
func readBodyLines(body string, th components.Theme, width int, method xui.WidthMethod) []components.RichLine {
	if width < 1 {
		width = 1
	}
	numSt := th.Muted
	textSt := th.Foreground
	textSt.Dim = true

	var out []components.RichLine
	for raw := range strings.SplitSeq(util.ReplaceAll(body, "\r", ""), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		no, rest := splitLineNumber(raw)
		numText := ""
		if no > 0 {
			numText = fmt.Sprintf("%6d ", no)
		}
		spans := []components.Span{
			{Text: numText, Style: numSt},
			{Text: rest, Style: textSt},
		}
		out = append(out, components.WrapSpans(spans, width, method)...)
	}
	return out
}

// splitLineNumber pulls a leading "  12|" or "path:>>  12|" gutter off a
// tool-output line, returning the number (0 when absent) and the remaining text.
func splitLineNumber(line string) (int, string) {
	rest := line
	// Drop a "path:" or "path:>> " prefix that grep prepends.
	if i := strings.LastIndex(rest, "|"); i >= 0 {
		head, tail := rest[:i], rest[i+1:]
		trimmed := strings.TrimLeft(head, " ")
		// The head may itself be "path:>>" — keep only its trailing digits.
		if j := strings.LastIndexAny(trimmed, ":>"); j >= 0 {
			trimmed = trimmed[j+1:]
		}
		trimmed = strings.TrimLeft(trimmed, " >")
		if n, err := strconv.Atoi(strings.TrimSpace(trimmed)); err == nil {
			return n, tail
		}
	}
	return 0, rest
}
