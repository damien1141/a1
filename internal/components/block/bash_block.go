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

// BashBlock renders a bash tool / "!cmd" row:
//
//	$ go test ./...
//	  ok      github.com/x/y  0.4s
//	  ok      github.com/x/z  0.1s
//	  … +118 more lines ▶
//
//	$ go test ./...
//	  FAIL github.com/x/y
//	  --- FAIL: TestFoo
//	  [Showing lines 1-1000 of 1000. Full output: /tmp/a1-bash-….log]
//
// A collapsed row always keeps a tail of the output (BashCollapsedTailLines)
// so the user can see how a command ended without expanding. Expanding shows
// everything the tool retained, including its own truncation notice and
// temp-file dump link.
type BashBlock struct {
	Command  string
	Output   string
	Status   status.ToolStatus
	ExitCode int
	Expanded bool
	Theme    components.Theme

	// OnToggle is called when the user expands/collapses (click title / Enter).
	OnToggle func(expanded bool)

	titleH int // title row count; body clicks don't toggle (allow selection)
}

func (bashBlock *BashBlock) theme() components.Theme {
	if bashBlock.Theme.Success.Fg.Kind == 0 && bashBlock.Theme.Foreground.Fg.Kind == 0 {
		return components.DefaultTheme()
	}
	return bashBlock.Theme
}

// Handle toggles expansion on Enter/space or a left-click on the title row.
func (bashBlock *BashBlock) Handle(ctx *components.EventContext, ev xui.Event) {
	switch e := ev.(type) {
	case xui.KeyEvent:
		if e.Code == xui.KeyEnter || (e.Code == xui.KeyRune && e.Rune == ' ') {
			if bashBlock.hasBody() {
				bashBlock.toggle(ctx)
			}
		}
	case xui.MouseEvent:
		if e.Action != xui.MousePress || e.Button != xui.MouseLeft {
			return
		}
		// Only the title toggles expand; body stays selectable for copy-on-select.
		if bashBlock.hasBody() && e.Y >= 0 && e.Y < bashBlock.titleH {
			bashBlock.toggle(ctx)
		}
	}
}

// toggle flips expansion, notifies OnToggle, and schedules a redraw.
func (bashBlock *BashBlock) toggle(ctx *components.EventContext) {
	bashBlock.Expanded = !bashBlock.Expanded
	if bashBlock.OnToggle != nil {
		bashBlock.OnToggle(bashBlock.Expanded)
	}
	ctx.ConsumeAndRedraw()
}

// CopyText returns "$ command" plus output when present.
func (bashBlock *BashBlock) CopyText() string {
	var sb strings.Builder
	sb.WriteString("$ ")
	sb.WriteString(bashBlock.Command)
	out := strings.TrimRight(bashBlock.Output, "\n")
	if out != "" {
		sb.WriteByte('\n')
		sb.WriteString(out)
	}
	return sb.String()
}

// BashCollapsedTailLines is how many trailing output lines a collapsed bash
// row keeps. Enough to show a test summary, a build error, or a diffstat
// without flooding the transcript; the rest is one Enter away.
const BashCollapsedTailLines = 8

func (bashBlock *BashBlock) hasBody() bool {
	return strings.TrimSpace(bashBlock.Output) != "" || bashBlock.Status == status.ToolError
}

// collapsedBody renders the tail shown when the row is closed: the last
// BashCollapsedTailLines of output, plus a dim trailer naming the hidden count.
// A row within the budget shows all of its lines and no trailer, so a short
// command reads exactly like an expanded one.
//
// The budget is measured on wrapped lines rather than source lines, because a
// single 400-column log line occupies eight surface rows once wrapped — and an
// untrimmed row would still blow the budget it exists to bound.
func (bashBlock *BashBlock) collapsedBody(
	th components.Theme,
	width int,
	method xui.WidthMethod,
) []components.RichLine {
	lines := bashBodyLines(bashBlock.Output, th, width, method)
	if len(lines) <= BashCollapsedTailLines {
		return lines
	}
	out := make([]components.RichLine, 0, BashCollapsedTailLines+1)
	out = append(out, lines[len(lines)-BashCollapsedTailLines:]...)
	trailer := fmt.Sprintf("… +%d more lines", len(lines)-BashCollapsedTailLines)
	out = append(out, components.WrapSpans([]components.Span{{Text: trailer, Style: th.Muted}}, width, method)...)
	return out
}

// Draw renders the "$ command" title and, when expanded, the truncated output body.
func (bashBlock *BashBlock) Draw(ctx components.DrawContext) components.Surface {
	th := bashBlock.theme()
	w := ctx.Max.Width
	if w <= 0 {
		w = 40
	}

	titleWrapped := components.WrapSpans(bashBlock.titleSpans(th), w, ctx.Method)
	titleH := len(titleWrapped)
	bashBlock.titleH = titleH

	var bodyLines []components.RichLine
	if bashBlock.hasBody() {
		bodyW := w
		if bodyW > 2 {
			bodyW -= 2
		}
		switch {
		case bashBlock.Expanded:
			// Everything the tool retained, including its own "[Showing lines
			// …]" notice and the temp-file dump link.
			bodyLines = bashBodyLines(bashBlock.Output, th, bodyW, ctx.Method)
		case bashBlock.Status == status.ToolError:
			// A failure is the one case where a collapsed row must not hide
			// anything: the reason is the only thing on screen.
			bodyLines = bashBodyLines(bashBlock.Output, th, bodyW, ctx.Method)
		default:
			// Tail preview: what happened, without the flood.
			bodyLines = bashBlock.collapsedBody(th, bodyW, ctx.Method)
		}
	}

	h := titleH + len(bodyLines)
	h = max(h, 1)
	s := components.NewSurface(w, h, bashBlock)
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

// titleSpans builds the "$ command [exit code] [arrow]" header spans.
func (bashBlock *BashBlock) titleSpans(th components.Theme) []components.Span {
	prefixStyle := th.IdentityOrSuccess()
	switch bashBlock.Status {
	case status.ToolError:
		prefixStyle = th.Destructive
	case status.ToolRunning, status.ToolQueued:
		prefixStyle = th.ToolName
	case status.ToolCancelled, status.ToolRejected:
		prefixStyle = th.Muted
	}

	cmdStyle := th.Foreground
	if bashBlock.Status == status.ToolCancelled || bashBlock.Status == status.ToolRejected {
		cmdStyle.Strikethrough = true
	}

	title := []components.Span{
		{Text: chrome.BashPrompt, Style: prefixStyle},
		{Text: bashBlock.Command, Style: cmdStyle},
	}
	if suf := chrome.StatusSuffix(bashBlock.Status); suf != "" {
		title = append(title, components.Span{Text: suf, Style: th.Muted})
	}
	if bashBlock.Status == status.ToolDone && bashBlock.ExitCode != 0 {
		it := xui.Style{Italic: true}
		title = append(
			title,
			components.Span{Text: " (", Style: it},
			components.Span{Text: "exit code: ", Style: it},
			components.Span{
				Text:  strconv.Itoa(bashBlock.ExitCode),
				Style: xui.Style{Italic: true, Fg: th.Destructive.Fg},
			},
			components.Span{Text: ")", Style: it},
		)
	}
	if bashBlock.hasBody() {
		title = append(title, components.Span{Text: chrome.ExpandArrow(bashBlock.Expanded), Style: th.Muted})
	}
	return title
}

func bashBodyLines(output string, th components.Theme, width int, method xui.WidthMethod) []components.RichLine {
	if output == "" {
		return nil
	}
	text := strings.TrimRight(util.ReplaceAll(output, "\r", ""), "\n")
	fg := th.Foreground
	fg.Dim = true
	spans := []components.Span{{Text: text + "\n", Style: fg}}
	if width < 1 {
		width = 1
	}
	return components.WrapSpans(spans, width, method)
}
