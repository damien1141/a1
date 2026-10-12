package block

import (
	"fmt"
	"strings"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/chrome"
	"github.com/damien1141/a1/internal/components/status"
	"github.com/damien1141/a1/internal/util"
)

// DiffBlock renders a file edit (write / edit / apply_patch) as a
// Claude-Code-style card:
//
//	✎ internal/x/y.go  +12 -3
//
//	 a  b  c  old
//	 d  e  +  new
//
// The body is a unified diff with per-line +/- coloring and old/new line
// numbers. Added and removed lines are counted from the diff itself, so the
// header stat is never a guess. The card stays collapsed until the user
// expands it, or a run explicitly asks to start open.
type DiffBlock struct {
	// Path is the display path shown in the header.
	Path string
	// Diff is the unified-diff text produced by the edit tool. It may carry
	// ---/+++ file headers and @@ hunk headers, which are recognized and
	// rendered rather than shown as content.
	Diff string
	// Status drives the glyph and header style.
	Status status.ToolStatus
	// Error is the failure text, shown when the edit was rejected or failed.
	Error string
	// Expanded controls whether the diff body is drawn.
	Expanded bool
	Theme    components.Theme
	Spinner  *status.Spinner
	// OnToggle is called when the user expands/collapses.
	OnToggle func(expanded bool)

	titleH int // title row count; body clicks don't toggle (allow selection)
}

func (diffBlock *DiffBlock) theme() components.Theme {
	if diffBlock.Theme.Success.Fg.Kind == 0 && diffBlock.Theme.Foreground.Fg.Kind == 0 {
		return components.DefaultTheme()
	}
	return diffBlock.Theme
}

// HasBody reports whether the row has anything to show beyond its header.
func (diffBlock *DiffBlock) HasBody() bool {
	return strings.TrimSpace(diffBlock.Diff) != "" || strings.TrimSpace(diffBlock.Error) != ""
}

// Handle toggles expansion on Enter/space or a left-click on the title row.
func (diffBlock *DiffBlock) Handle(ctx *components.EventContext, ev xui.Event) {
	if !diffBlock.HasBody() {
		return
	}
	switch e := ev.(type) {
	case xui.KeyEvent:
		if e.Code == xui.KeyEnter || (e.Code == xui.KeyRune && e.Rune == ' ') {
			diffBlock.toggle(ctx)
		}
	case xui.MouseEvent:
		if e.Action == xui.MousePress && e.Button == xui.MouseLeft && e.Y >= 0 && e.Y < diffBlock.titleH {
			diffBlock.toggle(ctx)
		}
	}
}

func (diffBlock *DiffBlock) toggle(ctx *components.EventContext) {
	diffBlock.Expanded = !diffBlock.Expanded
	if diffBlock.OnToggle != nil {
		diffBlock.OnToggle(diffBlock.Expanded)
	}
	ctx.ConsumeAndRedraw()
}

// CopyText returns the path, the stat line, and the raw diff.
func (diffBlock *DiffBlock) CopyText() string {
	var sb strings.Builder
	sb.WriteString(diffBlock.Path)
	if added, removed := diffBlock.Stat(); added > 0 || removed > 0 {
		fmt.Fprintf(&sb, " (+%d -%d)", added, removed)
	}
	if diffText := strings.TrimRight(diffBlock.Diff, "\n"); diffText != "" {
		fmt.Fprintf(&sb, "\n%s", diffText)
	}
	return sb.String()
}

// Stat counts added and removed lines in the diff. File headers, hunk headers,
// and split-at/collapse markers are not counted.
func (diffBlock *DiffBlock) Stat() (added, removed int) {
	for ln := range strings.SplitSeq(diffBlock.Diff, "\n") {
		switch {
		case strings.HasPrefix(ln, "+") && !strings.HasPrefix(ln, "+++"):
			added++
		case strings.HasPrefix(ln, "-") && !strings.HasPrefix(ln, "---"):
			removed++
		}
	}
	return added, removed
}

// Draw renders the "✎ path +N -M" header and, when expanded, the diff body.
func (diffBlock *DiffBlock) Draw(ctx components.DrawContext) components.Surface {
	th := diffBlock.theme()
	w := ctx.Max.Width
	if w <= 0 {
		w = 40
	}

	titleWrapped := components.WrapSpans(diffBlock.titleSpans(th), w, ctx.Method)
	diffBlock.titleH = len(titleWrapped)

	var bodyLines []components.RichLine
	if diffBlock.HasBody() {
		bodyW := w
		if bodyW > 2 {
			bodyW -= 2
		}
		// A failure is the one thing the user must see without expanding: a
		// rejected edit normally means the model needs to retry.
		errText := strings.TrimSpace(diffBlock.Error)
		showBody := diffBlock.Expanded || errText != ""
		if showBody {
			if errText != "" {
				bodyLines = append(bodyLines, components.WrapSpans([]components.Span{
					{Text: "Error: " + errText, Style: th.Destructive},
				}, bodyW, ctx.Method)...)
			}
			bodyLines = append(bodyLines, diffBodyLines(diffBlock.Diff, th, bodyW, ctx.Method)...)
		}
	}

	h := len(titleWrapped) + len(bodyLines)
	h = max(h, 1)
	s := components.NewSurface(w, h, diffBlock)
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

// titleSpans builds the "✎ path +N -M ↓" header.
func (diffBlock *DiffBlock) titleSpans(th components.Theme) []components.Span {
	icon, iconSt := chrome.ToolIcon(diffBlock.Status, th, diffBlock.Spinner)
	spans := []components.Span{
		{Text: icon + " ", Style: iconSt},
		{Text: diffBlock.displayPath(), Style: th.ToolName},
	}
	if added, removed := diffBlock.Stat(); added > 0 || removed > 0 {
		spans = append(spans, components.Span{
			Text:  fmt.Sprintf(" +%d -%d", added, removed),
			Style: th.Muted,
		})
	}
	if suf := chrome.StatusSuffix(diffBlock.Status); suf != "" {
		spans = append(spans, components.Span{Text: suf, Style: th.Muted})
	}
	if diffBlock.HasBody() {
		spans = append(spans, components.Span{Text: chrome.ExpandArrow(diffBlock.Expanded), Style: th.Muted})
	}
	return spans
}

// displayPath reduces the row's path field to the path alone.
func (diffBlock *DiffBlock) displayPath() string {
	path := strings.TrimSpace(diffBlock.Path)
	if base, _, ok := strings.Cut(path, "#"); ok {
		path = strings.TrimSpace(base)
	}
	if path == "" {
		return "file"
	}
	return path
}

// diffBodyLines renders a unified diff with per-line coloring, right-aligned
// line numbers, and hunk separators. File headers (---/+++) are dropped: the
// path is already in the card header.
func diffBodyLines(diff string, th components.Theme, width int, method xui.WidthMethod) []components.RichLine {
	if width < 1 {
		width = 1
	}

	addSt := th.Success
	delSt := th.Destructive
	hunkSt := th.Muted

	// A body with no "@@" hunk header (a brand-new file shown as plain + lines)
	// still begins at line 1; without this the first row has no number and the
	// rest are shifted by one.
	oldNo, newNo := 0, 1
	var out []components.RichLine
	// Tracks whether a hunk separator has any diff content before it, so one is
	// never emitted ahead of the first region (only between two).
	seenContent := false

	for raw := range strings.SplitSeq(util.ReplaceAll(diff, "\r", ""), "\n") {
		// The tool separates its summary from the body with a blank line.
		if raw == "" {
			continue
		}
		// Tools prepend a one-line summary ("Created x (3 lines).", "Updated
		// x.") ahead of the diff body. It is prose, not a unified-diff line, so
		// it is rendered dim and never advances the line counters — otherwise
		// the very first real line is numbered against a line that does not
		// exist in the file.
		if isDiffPrologue(raw) {
			out = append(out, components.WrapSpans([]components.Span{{Text: raw, Style: hunkSt}}, width, method)...)
			continue
		}

		switch {
		case strings.HasPrefix(raw, "---") || strings.HasPrefix(raw, "+++"):
			continue
		case strings.HasPrefix(raw, "@@"):
			oldNo, newNo = parseHunkStart(raw)
			if seenContent {
				out = append(
					out,
					components.WrapSpans(
						[]components.Span{{Text: strings.Repeat("·", min(width, 24)), Style: hunkSt}},
						width,
						method,
					)...)
			}
			continue
		}

		var (
			kind byte
			text string
			no   int
		)
		switch {
		case strings.HasPrefix(raw, "+"):
			kind, text, no = '+', raw[1:], newNo
			newNo++
		case strings.HasPrefix(raw, "-"):
			kind, text, no = '-', raw[1:], oldNo
			oldNo++
		default:
			kind, text, no = ' ', strings.TrimPrefix(raw, " "), newNo
			newNo++
		}

		marker := string(kind)
		var style xui.Style
		switch kind {
		case '+':
			style = addSt
		case '-':
			style = delSt
		default:
			style = th.Foreground
			style.Dim = true
			marker = " "
		}

		// Fixed-width gutter: [old] [marker] text. Keeping the marker in its
		// own padded cell means the +/- column is the same for every line, so
		// the eye can scan changes without re-finding the alignment.
		numText := ""
		if no > 0 {
			numText = fmt.Sprintf("%4d", no)
		}
		spans := []components.Span{
			{Text: numText + "  ", Style: hunkSt},
			{Text: marker + " ", Style: style},
			{Text: text, Style: style},
		}
		seenContent = true
		out = append(out, components.WrapSpans(spans, width, method)...)
	}
	return out
}

// isDiffPrologue reports whether line is the tool's human summary ahead of a
// diff body, rather than a unified-diff line. A real diff line always starts
// with '+', '-', ' ', or '@@'; prose therefore starts with anything else.
func isDiffPrologue(line string) bool {
	if line == "" {
		return false
	}
	switch line[0] {
	case '+', '-', ' ', '@', '\\':
		return false
	}
	return true
}

// parseHunkStart extracts the old and new starting line numbers from an
// "@@ -a,b +c,d @@" header. Unparseable headers leave the counters untouched.
func parseHunkStart(header string) (oldStart, newStart int) {
	for f := range strings.FieldsSeq(header) {
		switch {
		case strings.HasPrefix(f, "-"):
			oldStart = leadingInt(f[1:])
		case strings.HasPrefix(f, "+"):
			newStart = leadingInt(f[1:])
		}
	}
	return oldStart, newStart
}

// leadingInt parses the leading digits of s, stopping at the first non-digit.
func leadingInt(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			break
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}
