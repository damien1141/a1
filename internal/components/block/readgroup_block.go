package block

import (
	"fmt"
	"strings"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/chrome"
	"github.com/damien1141/a1/internal/components/status"
)

// ReadGroupBlock is the collapsed form of a run of read-only calls.
//
//	🔍 Read 12 files, searched for 3 patterns ▼
//	  ├── ✓ read haiku.txt
//	  ├── ✓ grep "cursor" in .
//	  ╰── ✓ read main.go
//
// Claude Code folds consecutive reads/greps into one row reading "Read 12 files,
// searched for 3 patterns". a1 needs it more than the reference does: adding
// the read/grep cards earlier made each row large, so a 15-file exploration was
// 15 large cards. The counts keep the glanceable value; the expansion keeps
// every individual card reachable.
type ReadGroupBlock struct {
	// Detail is the already-composed summary, e.g. "Read 3 files, searched for
	// 2 patterns". Built by the session layer from the group's counts.
	Detail string
	// Hint is the most recent member's target, shown beside the counts so the
	// row says what it happened to be looking at last.
	Hint string
	// Status drives the header glyph.
	Status status.ToolStatus
	// Expanded shows the member rows.
	Expanded bool
	// Members returns the member rows, resolved at draw time so a live group
	// picks up results that arrived after this widget was built.
	Members  func() []ChildTool
	Theme    components.Theme
	Spinner  *status.Spinner
	OnToggle func(expanded bool)

	titleH int
}

func (g *ReadGroupBlock) theme() components.Theme {
	if g.Theme.Success.Fg.Kind == 0 && g.Theme.Foreground.Fg.Kind == 0 {
		return components.DefaultTheme()
	}
	return g.Theme
}

// HasBody reports whether the group has member rows to show.
func (g *ReadGroupBlock) HasBody() bool {
	return g.Members != nil && len(g.Members()) > 0
}

// CopyText returns the summary and every member's label.
func (g *ReadGroupBlock) CopyText() string {
	var sb strings.Builder
	sb.WriteString(g.Detail)
	if g.Hint != "" {
		fmt.Fprintf(&sb, " · %s", g.Hint)
	}
	if !g.HasBody() {
		return sb.String()
	}
	members := g.Members()
	sb.WriteString("\n")
	for i, m := range members {
		last := i == len(members)-1
		gate := "├── "
		if last {
			gate = "╰── "
		}
		fmt.Fprintf(&sb, "%s%s %s\n", gate, m.Name, m.Detail)
	}
	return sb.String()
}

// Handle toggles expansion on Enter/space or a click on the title row.
func (g *ReadGroupBlock) Handle(ctx *components.EventContext, ev xui.Event) {
	if !g.HasBody() {
		return
	}
	switch e := ev.(type) {
	case xui.KeyEvent:
		if e.Code == xui.KeyEnter || (e.Code == xui.KeyRune && e.Rune == ' ') {
			g.toggle(ctx)
		}
	case xui.MouseEvent:
		if e.Action == xui.MousePress && e.Button == xui.MouseLeft && e.Y >= 0 && e.Y < g.titleH {
			g.toggle(ctx)
		}
	}
}

func (g *ReadGroupBlock) toggle(ctx *components.EventContext) {
	g.Expanded = !g.Expanded
	if g.OnToggle != nil {
		g.OnToggle(g.Expanded)
	}
	ctx.ConsumeAndRedraw()
}

// titleSpans builds the summary header. The verb tenses flip with liveness the
// way the reference's do: "Reading" while in flight, "Read" once resolved.
func (g *ReadGroupBlock) titleSpans(th components.Theme) []components.Span {
	icon, iconSt := chrome.ToolIcon(g.Status, th, g.Spinner)
	spans := []components.Span{{Text: icon + " ", Style: iconSt}}

	// The session layer composes the summary in the resolved tense; only the
	// in-flight form has to be re-derived here.
	detail := g.Detail
	if g.Status == status.ToolRunning {
		detail = tenseFlip(detail)
	}
	if detail != "" {
		spans = append(spans, components.Span{Text: detail, Style: th.ToolName})
	}
	if hint := strings.TrimSpace(g.Hint); hint != "" && g.Status == status.ToolRunning {
		// Only while running: a resolved row's last target is history, and the
		// expanded members already carry every path.
		spans = append(spans, components.Span{Text: " " + hint, Style: th.Muted})
	}
	if suf := chrome.StatusSuffix(g.Status); suf != "" {
		spans = append(spans, components.Span{Text: suf, Style: th.Muted})
	}
	if g.HasBody() {
		spans = append(spans, components.Span{Text: chrome.ExpandArrow(g.Expanded), Style: th.Muted})
	}
	return spans
}

// tenseFlip converts a resolved summary to its in-flight form.
func tenseFlip(s string) string {
	repl := strings.NewReplacer("Read ", "Reading ", "searched ", "searching ", "listed ", "listing ")
	return repl.Replace(s)
}

// Draw renders the summary header and, when expanded, one row per member.
func (g *ReadGroupBlock) Draw(ctx components.DrawContext) components.Surface {
	th := g.theme()
	w := ctx.Max.Width
	if w <= 0 {
		w = 40
	}

	titleLines := components.WrapSpans(g.titleSpans(th), w, ctx.Method)
	g.titleH = len(titleLines)

	var body []components.RichLine
	if g.Expanded && g.HasBody() {
		bodyW := w
		if bodyW > 2 {
			bodyW -= 2
		}
		members := g.Members()
		for i, m := range members {
			body = append(body, components.WrapSpans(g.memberSpans(m, i == len(members)-1, th), bodyW, ctx.Method)...)
		}
	}

	h := len(titleLines) + len(body)
	h = max(h, 1)
	s := components.NewSurface(w, h, g)
	y := 0
	for _, line := range titleLines {
		components.PaintSpans(&s, 0, y, line, ctx.Method)
		y++
	}
	for _, line := range body {
		components.PaintSpans(&s, 2, y, line, ctx.Method)
		y++
	}
	return s
}

// memberSpans renders one member row with tree gates, matching the sub-agent
// tree so the two expandable row types look alike.
func (g *ReadGroupBlock) memberSpans(m ChildTool, last bool, th components.Theme) []components.Span {
	gate := "├── "
	if last {
		gate = "╰── "
	}
	icon, iconSt := chrome.ToolIcon(m.Status, th, g.Spinner)
	spans := []components.Span{
		{Text: gate, Style: th.Muted},
		{Text: icon + " ", Style: iconSt},
		{Text: m.Name, Style: th.ToolName},
	}
	// A member with a summary shows it ahead of its detail: for a fetch, "200 OK
	// · 12 KB · 0.4s" is the answer and the url is the provenance, which is the
	// opposite priority to a read row where the path *is* the answer.
	if m.Summary != "" {
		spans = append(spans, components.Span{Text: " " + m.Summary, Style: th.Muted})
		return spans
	}
	if m.Detail != "" {
		spans = append(spans, components.Span{Text: " " + m.Detail, Style: th.Muted})
	}
	return spans
}
