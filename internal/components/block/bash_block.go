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
			// expandable(), not hasBody(): a block whose COMMAND alone is
			// truncated is also worth expanding, and gating on the body made that
			// state look interactive while the key did nothing.
			if bashBlock.expandable() {
				bashBlock.toggle(ctx)
			}
		}
	case xui.MouseEvent:
		if e.Action != xui.MousePress || e.Button != xui.MouseLeft {
			return
		}
		// Only the title toggles expand; body stays selectable for copy-on-select.
		if bashBlock.expandable() && e.Y >= 0 && e.Y < bashBlock.titleH {
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

// BashCollapsedCommandLines is how many wrapped rows a COLLAPSED command may
// occupy. A multi-line heredoc or a long one-liner with a dozen flags used to
// claim most of the viewport before a single line of its output appeared; the
// command is context, not the result, so a collapsed row bounds it.
const BashCollapsedCommandLines = 2

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

	// Body is painted at x=2, so it must WRAP to width-4: the old width-2
	// wrapped two columns past the right margin and reflowed the overflow back
	// to x=2, which is why every wrapped body line hung past the edge.
	const bodyIndent = 2
	var bodyLines []components.RichLine
	if bashBlock.hasBody() {
		bodyW := w - bodyIndent*2
		if bodyW < 1 {
			bodyW = 1
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
		components.PaintSpans(&s, bodyIndent, y, line, ctx.Method)
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

	cmdText := bashBlock.Command
	if !bashBlock.Expanded {
		cmdText = truncateCommand(bashBlock.Command, bashBlock.commandBudgetChars())
	}
	title := []components.Span{
		{Text: chrome.BashPrompt, Style: prefixStyle},
		{Text: cmdText, Style: cmdStyle},
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
	// The arrow appears when EITHER surface can expand: a long command with no
	// output is still expandable, and before this it showed no affordance at all.
	if bashBlock.expandable() {
		title = append(title, components.Span{Text: chrome.ExpandArrow(bashBlock.Expanded), Style: th.Muted})
	}
	return title
}

// expandable reports whether toggling would reveal anything.
func (bashBlock *BashBlock) expandable() bool {
	return bashBlock.hasBody() || bashBlock.commandTruncatable()
}

// commandTruncatable reports whether the command is long enough to be hidden by
// the collapsed budget. Measured in characters rather than wrapped rows because
// the width is not known until Draw; a command shorter than the budget is never
// touched, so the common case renders byte-identically to before.
func (bashBlock *BashBlock) commandTruncatable() bool {
	return len([]rune(bashBlock.Command)) > bashBlock.commandBudgetChars()
}

// commandBudgetChars is the character budget a collapsed command renders within.
// Derived from the row budget at a conservative width so a command that fits on
// BashCollapsedCommandLines typical rows is left alone.
func (bashBlock *BashBlock) commandBudgetChars() int {
	// ~80 columns is the width every terminal here has had; two rows of it is a
	// generous collapsed command, and the exact width only shifts where the
	// cut lands.
	return 80 * BashCollapsedCommandLines
}

// CommandSynopsisChars is the length beyond which a command is collapsed to its
// RUNNER plus a size marker rather than truncated to its head.
//
// A 745-character inline Python script has no readable head: even two rows of it
// is noise that dominates the block's visual weight, and the only thing a human
// scanning back needs is "it was python3 with a heredoc". Below this threshold
// the head IS the identity and truncating is the right call.
const CommandSynopsisChars = 200

// truncateCommand shortens cmd for a collapsed row.
//
// Two regimes:
//   - Normal long command (> budget, <= CommandSynopsisChars): keep the HEAD.
//     A command's identity is its verb and first arguments ("git commit -m …");
//     keeping a heredoc's tail would show closing braces and quotes.
//   - Very long command: keep the runner and a size marker. The head of an
//     inline script is `python3 - <<'PY' p='...` — technically the identity, and
//     completely uninformative to read.
func truncateCommand(cmd string, budget int) string {
	if budget <= 0 {
		return cmd
	}
	// A multi-line command is flattened first: the collapsed row is a one-glance
	// identity, and a newline would blow the row budget the truncation exists to
	// enforce.
	flat := strings.Join(strings.Split(strings.ReplaceAll(cmd, "\r", ""), "\n"), " ")
	flat = strings.TrimSpace(flat)
	r := []rune(flat)
	if len(r) <= budget {
		return flat
	}
	hidden := len(r) - budget
	if len(r) > CommandSynopsisChars {
		return commandSynopsis(flat, hidden)
	}
	return strings.TrimSpace(string(r[:budget])) + fmt.Sprintf(" … (+%d chars)", hidden)
}

// commandSynopsis renders a very long command as "<runner> … (+N chars)": enough
// to know WHAT ran, without reproducing a script nobody will read in a collapsed
// row. Expand still shows the whole thing.
//
// The first word alone is NOT the runner: the agent routinely issues
// "cd <dir> && <the real command>", so every such row collapsed to "cd …" and
// the transcript became a column of identical lines. A leading directory change
// is connective tissue, and the informative token is the command after it.
func commandSynopsis(flat string, hidden int) string {
	runner := commandRunner(flat)
	return fmt.Sprintf("%s … (+%d chars)", runner, hidden)
}

// commandRunner picks the token that identifies what a shell line actually does.
//
// It skips a leading `cd <dir> &&`, then keeps the command word plus one
// subcommand when that subcommand is a plain word ("go test", "git commit") —
// which distinguishes "go test" from "go build" without dragging in flags or
// paths.
func commandRunner(flat string) string {
	fields := strings.Fields(flat)
	// Drop a leading `cd <dir> &&` / `cd <dir> ;` prefix.
	for len(fields) > 2 && fields[0] == "cd" && (fields[2] == "&&" || fields[2] == ";") {
		fields = fields[3:]
	}
	if len(fields) == 0 {
		return "command"
	}
	runner := fields[0]
	if len(fields) > 1 {
		next := fields[1]
		// A subcommand qualifies when it is a bare word: no flags, no paths, no
		// quoting, and not another shell operator.
		if !strings.HasPrefix(next, "-") &&
			!strings.ContainsAny(next, "=/'\"`|&;<>") &&
			next != "&&" && next != "||" && next != ";" && next != "|" {
			runner += " " + next
		}
	}
	return runner
}

func bashBodyLines(output string, th components.Theme, width int, method xui.WidthMethod) []components.RichLine {
	if output == "" {
		return nil
	}
	if width < 1 {
		width = 1
	}
	text := strings.TrimRight(util.ReplaceAll(output, "\r", ""), "\n")
	return components.WrapSpans(classifyBodySpans(text, th), width, method)
}

// classifyBodySpans colours command output by line significance.
//
// Output used to render as ONE span in one dim colour, so a test run's wall of
// noise looked exactly like its two failures, and a build error read the same as
// a file list. The block's own doc comment promises a collapsed row keeps a tail
// "to show a test summary, a build error, or a diffstat" — three things it then
// rendered identically.
//
// This is not a syntax highlighter: three rules, line-granular, applied to the
// leading text of each line. Anything unrecognised stays the base muted colour,
// so the signal is what stands out rather than the decoration.
func classifyBodySpans(text string, th components.Theme) []components.Span {
	base := th.Foreground
	base.Dim = true

	okSt := th.Success
	errSt := th.Destructive
	warnSt := th.ToolName

	lines := strings.Split(text, "\n")
	spans := make([]components.Span, 0, len(lines)*2)
	for i, line := range lines {
		st := base
		switch bodyLineClass(line) {
		case bodyErr:
			st = errSt
		case bodyOK:
			st = okSt
		case bodyWarn:
			st = warnSt
		}
		spans = append(spans, components.Span{Text: line, Style: st})
		if i < len(lines)-1 {
			spans = append(spans, components.Span{Text: "\n", Style: base})
		}
	}
	// Trailing newline: the wrapped body expects one so the last row is closed.
	spans = append(spans, components.Span{Text: "\n", Style: base})
	return spans
}

type bodyClass int

const (
	bodyPlain bodyClass = iota
	bodyOK
	bodyErr
	bodyWarn
)

// bodyLineClass classifies one output line by its leading token.
//
// Deliberately conservative: a line only earns a colour when it OPENS with a
// known marker. Scanning the whole line would paint ordinary prose that happens
// to contain the word "error", which is worse than no colouring at all.
func bodyLineClass(line string) bodyClass {
	t := strings.TrimSpace(line)
	if t == "" {
		return bodyPlain
	}
	lower := strings.ToLower(t)
	switch {
	case strings.HasPrefix(t, "FAIL"),
		strings.HasPrefix(t, "--- FAIL"),
		strings.HasPrefix(t, "✗"),
		strings.HasPrefix(t, "⨯"),
		strings.HasPrefix(lower, "error"),
		strings.HasPrefix(lower, "fatal"),
		strings.HasPrefix(lower, "panic:"),
		strings.HasPrefix(lower, "traceback"),
		strings.HasPrefix(t, "npm ERR!"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "no such file"),
		strings.Contains(lower, "permission denied"),
		strings.Contains(lower, "command not found"):
		return bodyErr
	case strings.HasPrefix(t, "ok "),
		strings.HasPrefix(t, "ok\t"),
		strings.HasPrefix(t, "PASS"),
		strings.HasPrefix(t, "--- PASS"),
		strings.HasPrefix(t, "✓"),
		strings.HasPrefix(t, "✔"),
		strings.HasPrefix(lower, "success"),
		strings.HasPrefix(lower, "passed"):
		return bodyOK
	case strings.HasPrefix(t, "WARN"),
		strings.HasPrefix(lower, "warning"),
		strings.HasPrefix(t, "SKIP"),
		strings.HasPrefix(t, "--- SKIP"):
		return bodyWarn
	}
	return bodyPlain
}
