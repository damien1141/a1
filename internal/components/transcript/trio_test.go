package transcript

import (
	"math/rand"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
)

func TestMessageList_OutOfRangeGuards_Negative(t *testing.T) {
	list := &MessageList{
		Entries: []components.Widget{
			&rowStub{text: "a", h: 2},
			&rowStub{text: "b", h: 3},
		},
	}
	_ = list.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 20}})
	require.Equal(t, 2, list.CachedHeight(0))
	require.Equal(t, 3, list.CachedHeight(1))

	// Out-of-range cache reads are 0, not panics.
	for _, i := range []int{-1, 2, 1000} {
		assert.Zero(t, list.CachedHeight(i), "CachedHeight(%d)", i)
	}
	// Out-of-range invalidation is a no-op: cached heights survive untouched.
	list.InvalidateHeightsAt(-1, 2, 77)
	assert.Equal(t, 2, list.CachedHeight(0), " InvalidateHeightsAt(-1)")
	assert.Equal(t, 3, list.CachedHeight(1), " InvalidateHeightsAt(2)")

	// Hostile selection: an out-of-range index highlights nothing and copies "".
	list.Selected = 99
	s := list.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 20}})
	assert.Equal(t, "", list.SelectedCopyText(), "Selected past the end copies nothing")
	for _, sub := range s.Children {
		for _, c := range sub.Surface.Buffer {
			assert.Equal(t, xui.Color{}, c.Style.Bg, "no row may be highlighted for Selected=99")
		}
	}
	list.Selected = -5
	assert.Equal(t, "", list.SelectedCopyText(), "negative Selected copies nothing")

	// ScrollToEntry outside [0,len) is a no-op.
	list.ScrollFromBottom = 0
	list.ScrollToEntry(-1)
	list.ScrollToEntry(2)
	list.ScrollToEntry(1 << 30)
	assert.Equal(t, 0, list.ScrollFromBottom, "out-of-range ScrollToEntry must not move the viewport")

	// Nil receiver is guarded.
	var nilList *MessageList
	nilList.ScrollToEntry(0)
}

func TestMessageList_EmptyListAndDegenerateContext_Negative(t *testing.T) {
	list := &MessageList{}
	for _, sz := range [][2]int{{0, 0}, {-3, -7}} {
		s := list.Draw(components.DrawContext{Max: components.Size{Width: sz[0], Height: sz[1]}})
		assert.Equal(t, 40, s.Size.Width, "max width %d falls back to 40", sz[0])
		assert.Equal(t, 10, s.Size.Height, "max height %d falls back to 10", sz[1])
		assert.Empty(t, s.Children, "no entries, no children")
	}
	first, last := list.VisibleRange()
	assert.Equal(t, -1, first)
	assert.Equal(t, -1, last)
	assert.Equal(t, -1, list.IndexAtPoint(0, 0), "empty list hit-tests to nothing")
	assert.Equal(t, "", list.LastCopyText())
	assert.Equal(t, "", list.SelectedCopyText())

	// Hit-testing: no layout yet, coordinates outside the content, and the
	// spacing row between two items all miss.
	filled := &MessageList{
		Entries: []components.Widget{
			&rowStub{text: "a", h: 2},
			&rowStub{text: "b", h: 2},
		},
	}
	assert.Equal(t, -1, filled.IndexAtPoint(5, 5), "before any Draw there is no geometry")
	_ = filled.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 10}})
	// totalH = 2+1+2 = 5, view 10 → originY = 5: rows 5-6, gap 7, rows 8-9.
	assert.Equal(t, 0, filled.IndexAtPoint(0, 5), "row 0 starts at the top item")
	assert.Equal(t, -1, filled.IndexAtPoint(0, 7), "the spacing row belongs to no entry")
	assert.Equal(t, -1, filled.IndexAtPoint(0, -3), "above the list")
	assert.Equal(t, -1, filled.IndexAtPoint(0, 1<<20), "far below the list")

	// Over-scroll clamps to the content extent; negative scroll to follow mode.
	tall := &MessageList{
		Entries: []components.Widget{
			&rowStub{text: "a", h: 4},
			&rowStub{text: "b", h: 4},
		},
	}
	tall.ScrollFromBottom = 500
	_ = tall.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 6}})
	assert.Equal(t, 3, tall.ScrollFromBottom, "totalH=9 view=6 → max scroll 3")
	tall.ScrollFromBottom = -9
	_ = tall.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 6}})
	assert.Equal(t, 0, tall.ScrollFromBottom, "negative scroll clamps to follow mode")
}

// A nil entry whose row intersects the viewport must not panic: Draw skips it
// (it keeps the height-1 slot measure() grants so the scroll extent stays
// correct), Handle forwards nothing to it on either path, and the copy sites
// treat it as having no copyable text.
func TestMessageList_NilEntryIntersectsViewport_Negative(t *testing.T) {
	list := &MessageList{
		Entries: []components.Widget{
			&rowStub{text: "a", h: 2},
			nil,
			&rowStub{text: "c", h: 2},
		},
	}
	var s components.Surface
	require.NotPanics(t, func() {
		s = list.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 10}})
	}, "nil entry intersecting the viewport must render, not panic")
	// totalH = 2+1+1+1+2 = 7 < the 10-row viewport, so the nil row is on
	// screen; only the two real rows become children.
	require.Len(t, s.Children, 2, "the nil row paints nothing")
	assert.Equal(t, 1, list.CachedHeight(1), "nil entry still measures one row")
	first, last := list.VisibleRange()
	assert.Equal(t, 0, first, "row 0 is realized")
	assert.Equal(t, 2, last, "row 2 is realized")
	// Hit-testing: rows 3-4 belong to entry 0, row 6 is the nil slot, rows 8-9
	// to entry 2 (originY = 10-7 = 3).
	assert.Equal(t, 0, list.IndexAtPoint(0, 4), "real row still hit-tests")
	assert.Equal(t, -1, list.IndexAtPoint(0, 6), "the nil row belongs to no entry")
	assert.Equal(t, 2, list.IndexAtPoint(0, 9), "real row below the nil still hit-tests")

	// Post-Draw Handle walks the realized (non-nil) rows only...
	ctx := &components.EventContext{}
	require.NotPanics(t, func() {
		list.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'x', Press: true})
	}, "Handle must skip the nil entry on the lastItems path")

	// ...and the pre-Draw fallback walks every entry, nils included, unharmed.
	fresh := &MessageList{Entries: []components.Widget{nil, &rowStub{text: "z", h: 1}}}
	require.NotPanics(t, func() {
		fresh.Handle(&components.EventContext{}, xui.MouseEvent{})
	}, "Handle must skip the nil entry on the all-entries path")

	// Copy sites treat the nil entry as copy-less rather than panicking.
	list.Selected = 1
	assert.Equal(t, "", list.SelectedCopyText(), "nil entry has no copy text")
	assert.Equal(t, "", list.LastCopyText(), "LastCopyText skips the nil and stops at the copy-less stub")
}

// naiveLayout recomputes the bottom-anchored windowed layout with plain
// arithmetic: prefix sums for row tops, one gap between rows, scroll clamped
// to the content extent, and a row realized iff [top, top+h) intersects
// [0, viewH).
func naiveLayout(heights []int, gap, viewH, scroll int) (tops map[int]int, total int) {
	total = 0
	offsets := make([]int, len(heights))
	for i, h := range heights {
		if i > 0 {
			total += gap
		}
		offsets[i] = total
		total += h
	}
	maxScroll := max(total-viewH, 0)
	scroll = min(max(scroll, 0), maxScroll)
	originY := viewH - total + scroll
	tops = map[int]int{}
	for i, h := range heights {
		top := originY + offsets[i]
		if top+h > 0 && top < viewH {
			tops[i] = top
		}
	}
	return tops, total
}

// oracle: naive bottom-anchored layout reference (above) on randomized
// heights, viewports, and scroll offsets; Draw must realize exactly the rows
// the reference marks visible, at the same Y origins and padding X, and
// VisibleRange must bracket them.
func TestMessageList_MatchesNaiveGeometry_Differential(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	ctx := components.DrawContext{Max: components.Size{Width: 40}, Method: xui.WidthUnicode}
	for i := range 120 {
		n := 1 + rng.Intn(8)
		entries := make([]components.Widget, n)
		heights := make([]int, n)
		for j := range n {
			heights[j] = 1 + rng.Intn(5)
			entries[j] = &rowStub{text: strconv.Itoa(j), h: heights[j]}
		}
		viewH := 1 + rng.Intn(12)
		scroll := rng.Intn(19) - 3
		list := &MessageList{Entries: entries, ScrollFromBottom: scroll}
		ctx.Max.Height = viewH
		root := list.Draw(ctx)

		want, total := naiveLayout(heights, 1, viewH, scroll)
		var wantIdx, wantY []int
		for idx := range n {
			if y, ok := want[idx]; ok {
				wantIdx = append(wantIdx, idx)
				wantY = append(wantY, y)
			}
		}
		// A 1-row viewport can land entirely inside the inter-item gap: both
		// the reference and Draw must then realize zero children.
		assert.Len(t, root.Children, len(wantIdx), "case %d: realized child count", i)
		if len(wantIdx) == 0 {
			first, last := list.VisibleRange()
			assert.Equal(t, -1, first, "case %d: empty window", i)
			assert.Equal(t, -1, last, "case %d: empty window", i)
			continue
		}
		for k, sub := range root.Children {
			assert.Equal(t, wantY[k], sub.Origin.Y, "case %d child %d (entry %d): origin Y", i, k, wantIdx[k])
			assert.Equal(t, 1, sub.Origin.X, "case %d child %d: padding X", i, k)
			assert.Equal(t, heights[wantIdx[k]], sub.Surface.Size.Height, "case %d child %d: entry height", i, k)
		}
		first, last := list.VisibleRange()
		assert.Equal(t, wantIdx[0], first, "case %d: first visible", i)
		assert.Equal(t, wantIdx[len(wantIdx)-1], last, "case %d: last visible", i)
		// The scroll clamp the Draw applied must match the reference's.
		wantScroll := min(max(scroll, 0), max(total-viewH, 0))
		assert.Equal(t, wantScroll, list.ScrollFromBottom, "case %d: clamped scroll", i)
	}
}
