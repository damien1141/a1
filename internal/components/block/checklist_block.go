package block

import (
	"fmt"
	"strings"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/chrome"
	"github.com/damien1141/a1/internal/todo"
)

// ChecklistBlock renders a project TODO.md as a live progress row.
//
//	☑ TODO.md 3 done, 2 open ▼
//	  ▸ Patch the parser
//	    still fails on a missing trailing newline
//	  ◦ Update the tests
//	  ✓ Read the current writer
//
// This is the a1 counterpart to Claude Code's TaskListV2. The difference is
// deliberate: there is no todo tool behind this. The agent writes a markdown
// file it already has tools for, and this row reads it — no schema, no
// persistence, no model cooperation needed for the row to exist.
type ChecklistBlock struct {
	// Path is the file the checklist was read from, shown in the header.
	Path  string
	Items []todo.Item
	// Expanded shows the item list; the counts always show.
	Expanded bool
	Theme    components.Theme
	// StaleFor counts file-editing tool rows since the checklist last moved.
	// Non-zero renders a staleness warning, because a checklist the agent
	// stopped updating is worse than no checklist.
	StaleFor int
	// MaxRows caps how many items render. Zero means the default.
	MaxRows  int
	OnToggle func(expanded bool)

	titleH int
}

// defaultChecklistRows matches the reference budget: keep the list legible
// without eating the transcript.
const defaultChecklistRows = 10

func (c *ChecklistBlock) theme() components.Theme {
	if c.Theme.Success.Fg.Kind == 0 && c.Theme.Foreground.Fg.Kind == 0 {
		return components.DefaultTheme()
	}
	return c.Theme
}

// HasBody reports whether there is anything beyond the header to show.
func (c *ChecklistBlock) HasBody() bool { return len(c.Items) > 0 }

func (c *ChecklistBlock) maxRows() int {
	if c.MaxRows > 0 {
		return c.MaxRows
	}
	return defaultChecklistRows
}

// Counts returns the done, open, and in-progress task counts. Boxes only:
// detail lines are attached notes, not claims about state.
func (c *ChecklistBlock) Counts() (done, open, inProgress int) { return c.tasks() }

// tasks counts the boxed items; details are attached, not counted.
func (c *ChecklistBlock) tasks() (done, open, inProgress int) {
	for _, it := range c.Items {
		if it.Detail {
			continue
		}
		switch {
		case it.Done:
			done++
		case it.InProgress:
			inProgress++
		default:
			open++
		}
	}
	return done, open, inProgress
}

// CopyText returns the counts and the item list, preserving each line's
// original shape — a detail line copies back as an unboxed bullet, because
// writing "- [ ]" over it would turn a note into a task the agent never claimed.
func (c *ChecklistBlock) CopyText() string {
	done, open, _ := c.tasks()
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s (%d done, %d open)\n", c.Path, done, open)
	for _, it := range c.Items {
		prefix := strings.Repeat(" ", it.Indent)
		if it.Detail {
			fmt.Fprintf(&sb, "%s- %s\n", prefix, it.Text)
			continue
		}
		box := "- [ ]"
		switch {
		case it.Done:
			box = "- [x]"
		case it.InProgress:
			box = "- [~]"
		}
		fmt.Fprintf(&sb, "%s%s %s\n", prefix, box, it.Text)
	}
	return sb.String()
}

// visibleItems selects which items render, in the reference's priority order:
// in-progress first, then open, then done — work still ahead of you outranks
// completed history, so the row is useful mid-run rather than a changelog.
// Details follow their parent task, and a task cut by the row cap takes its
// details with it rather than leaving them floating over an unrelated row.
func (c *ChecklistBlock) visibleItems() (shown []todo.Item, hidden int) {
	type entry struct {
		task    todo.Item
		details []todo.Item
	}
	var live, open, done []*entry
	var current *entry

	for _, it := range c.Items {
		if it.Detail {
			if current == nil {
				// A detail before any task has nothing to attach to.
				continue
			}
			current.details = append(current.details, it)
			continue
		}
		current = &entry{task: it}
		switch {
		case it.InProgress:
			live = append(live, current)
		case it.Done:
			done = append(done, current)
		default:
			open = append(open, current)
		}
	}

	ordered := make([]*entry, 0, len(live)+len(open)+len(done))
	ordered = append(ordered, live...)
	ordered = append(ordered, open...)
	ordered = append(ordered, done...)
	if len(ordered) > c.maxRows() {
		hidden = len(ordered) - c.maxRows()
		ordered = ordered[:c.maxRows()]
	}
	for _, e := range ordered {
		shown = append(shown, e.task)
		shown = append(shown, e.details...)
	}
	return shown, hidden
}

// Draw renders the header, the item list, and the staleness warning.
func (c *ChecklistBlock) Draw(ctx components.DrawContext) components.Surface {
	th := c.theme()
	w := ctx.Max.Width
	if w <= 0 {
		w = 40
	}

	titleLines := components.WrapSpans(c.titleSpans(th), w, ctx.Method)
	c.titleH = len(titleLines)

	var body []components.RichLine
	if c.Expanded && c.HasBody() {
		bodyW := w
		if bodyW > 2 {
			bodyW -= 2
		}
		shown, hidden := c.visibleItems()
		for _, it := range shown {
			body = append(body, components.WrapSpans(c.itemSpans(it, th), bodyW, ctx.Method)...)
		}
		if hidden > 0 {
			body = append(body, components.WrapSpans([]components.Span{
				{Text: fmt.Sprintf("… +%d more", hidden), Style: th.Muted},
			}, bodyW, ctx.Method)...)
		}
		if c.StaleFor > 0 {
			body = append(body, components.WrapSpans([]components.Span{
				{Text: fmt.Sprintf("⚠ %d file changes since this list moved", c.StaleFor), Style: th.Warning},
			}, bodyW, ctx.Method)...)
		}
	}

	h := len(titleLines) + len(body)
	h = max(h, 1)
	s := components.NewSurface(w, h, c)
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

// Handle toggles expansion on Enter/space or a click on the title row.
func (c *ChecklistBlock) Handle(ctx *components.EventContext, ev xui.Event) {
	if !c.HasBody() {
		return
	}
	switch e := ev.(type) {
	case xui.KeyEvent:
		if e.Code == xui.KeyEnter || (e.Code == xui.KeyRune && e.Rune == ' ') {
			c.toggle(ctx)
		}
	case xui.MouseEvent:
		if e.Action == xui.MousePress && e.Button == xui.MouseLeft && e.Y >= 0 && e.Y < c.titleH {
			c.toggle(ctx)
		}
	}
}

func (c *ChecklistBlock) toggle(ctx *components.EventContext) {
	c.Expanded = !c.Expanded
	if c.OnToggle != nil {
		c.OnToggle(c.Expanded)
	}
	ctx.ConsumeAndRedraw()
}

// titleSpans builds "☑ TODO.md 3 done, 2 open ▼". The counts are the glanceable
// part, so they are always present even when the list is collapsed.
func (c *ChecklistBlock) titleSpans(th components.Theme) []components.Span {
	done, open, _ := c.tasks()
	spans := []components.Span{{Text: "☑ ", Style: th.Success}}
	spans = append(spans, components.Span{Text: c.Path, Style: th.ToolName})
	spans = append(spans, components.Span{Text: " ", Style: th.Muted})

	var stats []components.BylineItem
	if done > 0 {
		stats = append(stats, components.BylineItem{Text: fmt.Sprintf("%d done", done), Style: th.Success})
	}
	if open > 0 {
		stats = append(stats, components.BylineItem{Text: fmt.Sprintf("%d open", open), Style: th.Muted})
	}
	spans = append(spans, components.BylineSpans(stats, th.Muted)...)

	if c.HasBody() {
		spans = append(spans, components.Span{Text: chrome.ExpandArrow(c.Expanded), Style: th.Muted})
	}
	return spans
}

// itemSpans renders one row with the reference's boxes: ✓ done, ◦ open, ▸ the
// in-flight item.
func (c *ChecklistBlock) itemSpans(it todo.Item, th components.Theme) []components.Span {
	if it.Detail {
		// A note, not a task: no box, so it can never be misread as something
		// the agent claimed to have done.
		return []components.Span{
			{Text: strings.Repeat(" ", it.Indent+2) + it.Text, Style: c.textStyle(it, th)},
		}
	}
	box := "◦ "
	switch {
	case it.Done:
		box = "✓ "
	case it.InProgress:
		box = "▸ "
	}
	return []components.Span{
		{Text: strings.Repeat(" ", it.Indent) + box, Style: c.boxStyle(it, th)},
		{Text: it.Text, Style: c.textStyle(it, th)},
	}
}

func (c *ChecklistBlock) boxStyle(it todo.Item, th components.Theme) xui.Style {
	switch {
	case it.Done:
		return th.Success
	case it.InProgress:
		return th.ToolName
	default:
		return th.Muted
	}
}

func (c *ChecklistBlock) textStyle(it todo.Item, th components.Theme) xui.Style {
	base := th.Foreground
	if it.Done || it.Detail {
		base.Dim = true
	}
	return base
}
