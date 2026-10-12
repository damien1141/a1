package transcript

import (
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"

	"github.com/damien1141/a1/internal/components"
)

// hlStub is a minimal widget that fills its constraint box.
type hlStub struct{}

func (*hlStub) Handle(*components.EventContext, xui.Event) {}

func (*hlStub) Draw(ctx components.DrawContext) components.Surface {
	return components.NewSurface(ctx.Max.Width, 2, hlStubPtr())
}

func hlStubPtr() *hlStub { return &hlStub{} }

func newList(n int) *MessageList {
	entries := make([]components.Widget, n)
	for i := range entries {
		entries[i] = &hlStub{}
	}
	return &MessageList{Entries: entries, Selected: -1, Theme: components.DefaultTheme()}
}

// highlightedChild returns which child indexes carry a background fill.
func highlightedChild(t *testing.T, m *MessageList) map[int]bool {
	t.Helper()
	root := m.Draw(components.DrawContext{Max: components.Size{Width: 20, Height: 10}})
	out := map[int]bool{}
	for _, sub := range root.Children {
		for _, c := range sub.Surface.Buffer {
			if c.Style.Bg != (xui.Color{}) {
				out[sub.Origin.Y] = true
			}
		}
	}
	return out
}

func TestSelectedEntryIsHighlighted(t *testing.T) {
	m := newList(3)
	m.Selected = 1
	got := highlightedChild(t, m)
	assert.Len(t, got, 1, "exactly one child is highlighted")
	assert.True(t, got[5], "the selected child — index 1, y 5 — is the highlighted one")
}

func TestNoSelectionNoHighlight(t *testing.T) {
	m := newList(3)
	assert.Empty(t, highlightedChild(t, m), "no selection, no fill")
}

func TestSelectionMoves(t *testing.T) {
	m := newList(3)
	m.Selected = 0
	first := highlightedChild(t, m)
	assert.Len(t, first, 1)

	m.Selected = 2
	second := highlightedChild(t, m)
	assert.Len(t, second, 1, "still exactly one row")
	assert.NotEqual(t, first, second, "and it is a different row")
}

func TestScrollToEntryRevealsTarget(t *testing.T) {
	m := newList(40)
	m.Selected = 39
	// Draw once so heights and geometry exist.
	_ = m.Draw(components.DrawContext{Max: components.Size{Width: 20, Height: 10}})

	m.ScrollToEntry(0)
	_ = m.Draw(components.DrawContext{Max: components.Size{Width: 20, Height: 10}})
	first, _ := m.VisibleRange()
	assert.Equal(t, 0, first, "scrolling to entry 0 puts it at the top")
}

func TestScrollToEntryLastSticksToBottom(t *testing.T) {
	m := newList(40)
	_ = m.Draw(components.DrawContext{Max: components.Size{Width: 20, Height: 10}})
	m.ScrollToEntry(39)
	assert.Equal(t, 0, m.ScrollFromBottom, "the last entry is the natural bottom")
}
