package block

import (
	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/text"
	"github.com/damien1141/a1/internal/session"
)

// AssistantBlock renders assistant Markdown (GFM) with themed typography,
// path highlights, and syntax-colored fenced code.
type AssistantBlock struct {
	Text  string
	State session.State
	Theme components.Theme
}

func (assistantBlock *AssistantBlock) theme() components.Theme {
	if assistantBlock.Theme.Success.Fg.Kind == 0 && assistantBlock.Theme.Foreground.Fg.Kind == 0 {
		return components.DefaultTheme()
	}
	return assistantBlock.Theme
}

// Handle is a no-op; assistant output is read-only.
func (*AssistantBlock) Handle(_ *components.EventContext, _ xui.Event) {}

// CopyText returns the assistant message body.
func (assistantBlock *AssistantBlock) CopyText() string { return assistantBlock.Text }

// Draw renders the assistant markdown body with themed typography.
func (assistantBlock *AssistantBlock) Draw(ctx components.DrawContext) components.Surface {
	th := assistantBlock.theme()
	w := ctx.Max.Width
	if w <= 0 {
		w = 40
	}
	spans := text.RenderMarkdown(assistantBlock.Text, th)
	if assistantBlock.State == session.StateError && assistantBlock.Text != "" {
		// Error turns read like tool failures: red body under an "Error:" label.
		spans = append([]components.Span{{Text: "Error: ", Style: th.Destructive}}, spans...)
		for i := range spans {
			spans[i].Style = th.Destructive
		}
	}
	if assistantBlock.State == session.StateCancelled && assistantBlock.Text != "" {
		if len(spans) > 0 {
			spans = append(spans, components.Span{Text: "\n", Style: th.Muted})
		}
		spans = append(spans, components.Span{Text: "(cancelled)", Style: th.Muted})
	}
	return components.PaintRichLines(w, components.WrapSpans(spans, w, ctx.Method), ctx.Method, assistantBlock)
}
