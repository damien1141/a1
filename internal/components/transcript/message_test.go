package transcript

import (
	"testing"

	"github.com/pulseaiclub/xui"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

// rowStub is a fixed-height Widget used to exercise MessageList without
// importing the block subpackage (avoids components↔block test-type cycles).
type rowStub struct {
	text string
	h    int
}

func (*rowStub) Handle(_ *components.EventContext, _ xui.Event) {}

func (r *rowStub) Draw(ctx components.DrawContext) components.Surface {
	w := ctx.Max.Width
	w = max(w, 1)
	h := r.h
	h = max(h, 1)
	s := components.NewSurface(w, h, r)
	s.Print(0, 0, r.text, xui.Style{}, ctx.Method)
	return s
}

func TestMessageListBottomPin(t *testing.T) {
	list := &MessageList{
		Entries: []components.Widget{
			&rowStub{text: "one", h: 1},
			&rowStub{text: "two", h: 1},
			&rowStub{text: "three", h: 1},
		},
	}
	s := list.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 4}})
	require.NotEmpty(t, s.Children, "expected visible children")
	last := s.Children[len(s.Children)-1]
	require.LessOrEqual(t, last.Origin.Y+last.Surface.Size.Height, 4,
		"last overflows: origin=%+v h=%d", last.Origin, last.Surface.Size.Height)
}

func TestMessageListInvalidateHeightsAt(t *testing.T) {
	list := &MessageList{
		Entries: []components.Widget{
			&rowStub{text: "a", h: 2},
			&rowStub{text: "b", h: 3},
			&rowStub{text: "c", h: 4},
		},
	}
	_ = list.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 20}})
	require.Equal(t, 2, list.CachedHeight(0))
	require.Equal(t, 3, list.CachedHeight(1))
	require.Equal(t, 4, list.CachedHeight(2))
	list.InvalidateHeightsAt(1)
	require.Equal(t, 2, list.CachedHeight(0))
	require.Equal(t, 0, list.CachedHeight(1))
	require.Equal(t, 4, list.CachedHeight(2))
	_ = list.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 20}})
	require.Equal(t, 3, list.CachedHeight(1))
}

func TestMessageListReindexHeights(t *testing.T) {
	list := &MessageList{
		Entries: []components.Widget{
			&rowStub{text: "a", h: 2},
			&rowStub{text: "b", h: 5},
			&rowStub{text: "c", h: 3},
		},
	}
	_ = list.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 20}})
	oldIDs := []string{"a", "b", "c"}
	// Insert "x" between a and b; heights must follow ids, not old indices.
	list.Entries = []components.Widget{
		&rowStub{text: "a", h: 2},
		&rowStub{text: "x", h: 7},
		&rowStub{text: "b", h: 5},
		&rowStub{text: "c", h: 3},
	}
	list.ReindexHeights(oldIDs, []string{"a", "x", "b", "c"})
	require.Equal(t, 2, list.CachedHeight(0))
	require.Equal(t, 0, list.CachedHeight(1))
	require.Equal(t, 5, list.CachedHeight(2))
	require.Equal(t, 3, list.CachedHeight(3))
	list.InvalidateHeightsAt(1)
	_ = list.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 20}})
	require.Equal(t, 7, list.CachedHeight(1))
}

func TestMessageListVirtualizes(t *testing.T) {
	const n = 80
	entries := make([]components.Widget, n)
	for i := range n {
		entries[i] = &rowStub{text: "row", h: 1}
	}
	list := &MessageList{Entries: entries}
	const viewH = 6
	s := list.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: viewH}})
	require.Less(t, len(s.Children), n, "expected windowed draw, children=%d for %d entries", len(s.Children), n)
	require.LessOrEqual(
		t,
		len(s.Children),
		viewH+2,
		"too many realized children: %d (viewH=%d)",
		len(s.Children),
		viewH,
	)
	first, last := list.VisibleRange()
	require.GreaterOrEqual(t, first, 0, "visible range %d..%d", first, last)
	require.GreaterOrEqual(t, last, first, "visible range %d..%d", first, last)
	require.Equal(t, n-1, last, "bottom pin: last visible=%d want %d", last, n-1)
	list.ScrollFromBottom = 40
	s2 := list.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: viewH}})
	f2, l2 := list.VisibleRange()
	require.NotEmpty(t, s2.Children, "scroll did not move window: %d..%d (was %d..%d)", f2, l2, first, last)
	require.False(t, l2 >= n-1 && f2 == first, "scroll did not move window: %d..%d (was %d..%d)", f2, l2, first, last)
}
