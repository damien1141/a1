package block

import (
	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
)

// CompactionBlock is a transcript marker shown after context compaction
// (Cursor-style italic "Compacted").
type CompactionBlock struct {
	Theme components.Theme
	// TokensBefore is the context size before the cut; 0 hides the count.
	TokensBefore int
}

func (b *CompactionBlock) theme() components.Theme {
	if b.Theme.Muted.Fg.Kind == 0 && b.Theme.Foreground.Fg.Kind == 0 {
		return components.DefaultTheme()
	}
	return b.Theme
}

// Handle is a no-op; the compaction marker is not interactive.
func (*CompactionBlock) Handle(_ *components.EventContext, _ xui.Event) {}

// Draw renders the italic marker line, with the pre-cut context size when known.
func (b *CompactionBlock) Draw(ctx components.DrawContext) components.Surface {
	th := b.theme()
	w := ctx.Max.Width
	if w <= 0 {
		w = 40
	}
	st := th.Muted
	st.Italic = true
	st.Dim = true
	label := "Compacted"
	if b.TokensBefore > 0 {
		label = "Compacted from " + components.FormatTokens(b.TokensBefore) + " tokens"
	}
	lines := components.WrapSpans([]components.Span{
		{Text: label, Style: st},
	}, w, ctx.Method)
	return components.PaintRichLines(w, lines, ctx.Method, b)
}
