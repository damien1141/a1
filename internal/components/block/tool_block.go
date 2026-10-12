package block

import (
	"fmt"
	"strings"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/chrome"
	"github.com/damien1141/a1/internal/components/status"
)

// ToolBlock renders a generic tool_use row: status glyph, name,
// detail, and optional expandable output body.
type ToolBlock struct {
	Name   string
	Detail string
	// Summary is the tool's one-line outcome ("200 OK · 42.1 KB · 1.2s"), shown
	// after Detail once the call resolves. Distinct from Detail, which is the
	// call's identity for the whole run.
	Summary  string
	Output   string
	Error    string
	Status   status.ToolStatus
	Expanded bool
	Theme    components.Theme
	Spinner  *status.Spinner
	OnToggle func(expanded bool)

	titleH int
}

func (toolBlock *ToolBlock) theme() components.Theme {
	if toolBlock.Theme.Success.Fg.Kind == 0 && toolBlock.Theme.Foreground.Fg.Kind == 0 {
		return components.DefaultTheme()
	}
	return toolBlock.Theme
}

func (toolBlock *ToolBlock) hasBody() bool {
	return strings.TrimSpace(toolBlock.Output) != "" || strings.TrimSpace(toolBlock.Error) != ""
}

// Handle toggles expansion on Enter/space or a left-click on the title row.
func (toolBlock *ToolBlock) Handle(ctx *components.EventContext, ev xui.Event) {
	if !toolBlock.hasBody() {
		return
	}
	switch e := ev.(type) {
	case xui.KeyEvent:
		if e.Code == xui.KeyEnter || (e.Code == xui.KeyRune && e.Rune == ' ') {
			toolBlock.Expanded = !toolBlock.Expanded
			if toolBlock.OnToggle != nil {
				toolBlock.OnToggle(toolBlock.Expanded)
			}
			ctx.ConsumeAndRedraw()
		}
	case xui.MouseEvent:
		if e.Action == xui.MousePress && e.Button == xui.MouseLeft && e.Y >= 0 && e.Y < toolBlock.titleH {
			toolBlock.Expanded = !toolBlock.Expanded
			if toolBlock.OnToggle != nil {
				toolBlock.OnToggle(toolBlock.Expanded)
			}
			ctx.ConsumeAndRedraw()
		}
	}
}

// CopyText returns name, detail, and body.
func (toolBlock *ToolBlock) CopyText() string {
	var b strings.Builder
	b.WriteString(toolBlock.Name)
	if toolBlock.Detail != "" {
		b.WriteByte(' ')
		b.WriteString(toolBlock.Detail)
	}
	if out := strings.TrimSpace(toolBlock.Output); out != "" {
		b.WriteByte('\n')
		b.WriteString(out)
	}
	if err := strings.TrimSpace(toolBlock.Error); err != "" {
		b.WriteByte('\n')
		b.WriteString("Error: ")
		b.WriteString(err)
	}
	return b.String()
}

// resultStat summarizes how much came back from a finished tool, so the row
// reports its outcome without expanding. Empty while running, on failure, or
// when there is no output.
func (toolBlock *ToolBlock) resultStat() string {
	if toolBlock.Status != status.ToolDone {
		return ""
	}
	n := 0
	for ln := range strings.SplitSeq(toolBlock.Output, "\n") {
		if strings.TrimSpace(ln) != "" {
			n++
		}
	}
	switch n {
	case 0:
		return ""
	case 1:
		return "(1 line)"
	default:
		return fmt.Sprintf("(%d lines)", n)
	}
}

// Draw renders the tool status glyph, name, detail, and optional output body.
func (toolBlock *ToolBlock) Draw(ctx components.DrawContext) components.Surface {
	th := toolBlock.theme()
	w := ctx.Max.Width
	if w <= 0 {
		w = 40
	}

	icon, iconSt := chrome.ToolIcon(toolBlock.Status, th, toolBlock.Spinner)
	spans := []components.Span{
		{Text: icon + " ", Style: iconSt},
		{Text: toolBlock.Name, Style: th.ToolName},
	}
	if toolBlock.Detail != "" {
		spans = append(spans, components.Span{Text: " " + toolBlock.Detail, Style: th.Muted})
	}
	if suf := chrome.StatusSuffix(toolBlock.Status); suf != "" {
		spans = append(spans, components.Span{Text: suf, Style: th.Muted})
	}
	// The tool's own summary wins over the generic line count: "200 OK · 42.1 KB
	// · 1.2s" says something, "(412 lines)" does not. The fallback stays for the
	// tools that set no summary.
	if toolBlock.Summary != "" {
		spans = append(spans, components.Span{Text: " " + toolBlock.Summary, Style: th.Muted})
	} else if stat := toolBlock.resultStat(); stat != "" {
		spans = append(spans, components.Span{Text: " " + stat, Style: th.Muted})
	}
	if toolBlock.hasBody() {
		spans = append(spans, components.Span{Text: chrome.ExpandArrow(toolBlock.Expanded), Style: th.Muted})
	}

	titleLines := components.WrapSpans(spans, w, ctx.Method)
	toolBlock.titleH = len(titleLines)

	var bodyLines []components.RichLine
	if toolBlock.Expanded && toolBlock.hasBody() {
		bodyW := w
		if bodyW > 2 {
			bodyW -= 2
		}
		if err := strings.TrimSpace(toolBlock.Error); err != "" {
			bodyLines = append(bodyLines, components.WrapSpans([]components.Span{
				{Text: "Error: " + err, Style: th.Destructive},
			}, bodyW, ctx.Method)...)
		}
		if out := strings.TrimSpace(toolBlock.Output); out != "" {
			fg := th.Foreground
			fg.Dim = true
			bodyLines = append(bodyLines, components.WrapSpans([]components.Span{
				{Text: out, Style: fg},
			}, bodyW, ctx.Method)...)
		}
	}

	h := len(titleLines) + len(bodyLines)
	h = max(h, 1)
	s := components.NewSurface(w, h, toolBlock)
	y := 0
	for _, line := range titleLines {
		components.PaintSpans(&s, 0, y, line, ctx.Method)
		y++
	}
	for _, line := range bodyLines {
		components.PaintSpans(&s, 2, y, line, ctx.Method)
		y++
	}
	return s
}
