package listpicker

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

func drawCtx() components.DrawContext {
	return components.DrawContext{Max: components.Size{Width: 80, Height: 24}, Method: xui.WidthUnicode}
}

// ---------- negative ----------

// Enter with zero filtered matches must be a no-op, not an accept and not a
// close: there is nothing to select, so the picker waits for the user.
func TestPickerAcceptWithoutMatchesStaysOpen_Negative(t *testing.T) {
	accepted := ""
	p := &Picker{
		Theme:    components.DefaultTheme(),
		OnAccept: func(item Item) { accepted = item.ID },
	}
	p.Show([]Item{{ID: "aaaaaaaa-1111", Primary: "aaaaaaaa", Detail: "fix resume leak"}}, ShowConfig{})
	ctx := &components.EventContext{}
	for _, r := range "zzz" {
		p.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: r, Press: true})
	}
	require.Empty(t, p.filtered)

	p.Handle(ctx, xui.KeyEvent{Code: xui.KeyEnter, Press: true})
	assert.True(t, p.Open, "accept with no matches must not close the picker")
	assert.Empty(t, accepted)

	// The empty-filter state renders the configured message, not a panic.
	s := p.Draw(drawCtx())
	require.Len(t, s.Children, 1)
	text := components.SurfaceText(s.Children[0].Surface)
	assert.Contains(t, text, `No matches for "zzz"`, "default empty-filter copy with the quoted query")
	assert.Contains(t, text, "0/1", "title shows the filtered count")
}

// A hostile Selected index must not reach Items through the filtered mapping.
func TestPickerSelectedOutOfRangeIsNoOp_Negative(t *testing.T) {
	accepted := ""
	p := &Picker{
		Theme:    components.DefaultTheme(),
		OnAccept: func(item Item) { accepted = item.ID },
	}
	p.Show([]Item{{ID: "a"}, {ID: "b"}}, ShowConfig{})
	ctx := &components.EventContext{}

	for _, sel := range []int{99, -1, 2} {
		p.Selected = sel
		p.Handle(ctx, xui.KeyEvent{Code: xui.KeyEnter, Press: true})
		assert.Empty(t, accepted, "Selected=%d must not accept", sel)
	}

	// Drawing with the hostile index still lays out the panel.
	p.Selected = 99
	s := p.Draw(drawCtx())
	require.Len(t, s.Children, 1)
}

// Cursor and rune guards: bounds hold, control runes and releases are
// ignored, pastes are sanitized, and a closed picker ignores everything.
func TestPickerEditingGuards_Negative(t *testing.T) {
	p := &Picker{Theme: components.DefaultTheme()}
	p.Show([]Item{{ID: "a"}}, ShowConfig{})
	ctx := &components.EventContext{}
	press := func(code xui.KeyCode, r rune) {
		p.Handle(ctx, xui.KeyEvent{Code: code, Rune: r, Press: true})
	}

	// Backspace / arrows at the boundaries do nothing.
	press(xui.KeyBackspace, 0)
	assert.Equal(t, "", p.Query)
	press(xui.KeyLeft, 0)
	assert.Equal(t, 0, p.Cursor)
	press(xui.KeyRight, 0)
	assert.Equal(t, 0, p.Cursor, "right at end of empty query stays put")

	for _, r := range "ab" {
		press(xui.KeyRune, r)
	}
	press(xui.KeyEnd, 0)
	press(xui.KeyRight, 0)
	assert.Equal(t, 2, p.Cursor, "right at end stays put")
	press(xui.KeyHome, 0)
	press(xui.KeyLeft, 0)
	assert.Equal(t, 0, p.Cursor, "left at start stays put")

	// Control runes never enter the query.
	press(xui.KeyRune, 0x01)
	assert.Equal(t, "ab", p.Query)

	// Key releases are ignored entirely.
	p.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'z', Press: false})
	assert.Equal(t, "ab", p.Query)

	// A hostile paste is sanitized: newlines and CRs become spaces. The cursor
	// sits at column 0 after Home, so the paste lands in front of the query.
	p.Handle(ctx, xui.PasteEvent{Text: "x\ny\rz"})
	assert.Equal(t, "x y zab", p.Query)
	assert.Equal(t, len("x y z"), p.Cursor)

	// A closed picker ignores keys and pastes without consuming; Hide has
	// already cleared the query.
	p.Hide()
	ctx2 := &components.EventContext{}
	p.Handle(ctx2, xui.KeyEvent{Code: xui.KeyEnter, Press: true})
	p.Handle(ctx2, xui.PasteEvent{Text: "nope"})
	assert.False(t, ctx2.Consume)
	assert.Equal(t, "", p.Query, "Hide clears the query and a closed picker ignores input")
}

// Degenerate geometry and close semantics.
func TestPickerDrawDegenerate_Negative(t *testing.T) {
	// Zero-size context falls back to the 80x24 default canvas.
	p := &Picker{Theme: components.DefaultTheme()}
	p.Show([]Item{{ID: "a", Primary: "a"}}, ShowConfig{})
	s := p.Draw(components.DrawContext{Max: components.Size{}, Method: xui.WidthUnicode})
	require.Len(t, s.Children, 1)
	assert.Equal(t, components.Size{Width: 80, Height: 24}, s.Size)

	// A three-column terminal still gets a (tiny) panel, not a panic.
	s = p.Draw(components.DrawContext{
		Max:    components.Size{Width: 3, Height: 24},
		Method: xui.WidthUnicode,
	})
	require.Len(t, s.Children, 1)
	assert.Equal(t, 3, s.Children[0].Surface.Size.Width)

	// Hide drops the panel, clears the filter state, and fires OnClose. A
	// second Hide on an already-closed picker re-fires OnClose (pinned actual
	// behavior — Hide has no open guard).
	closed := 0
	p.OnClose = func() { closed++ }
	p.Hide()
	assert.False(t, p.Open)
	s = p.Draw(drawCtx())
	assert.Empty(t, s.Children)
	p.Hide()
	assert.Equal(t, 2, closed, "Hide re-fires OnClose on an already-closed picker")
}

// ---------- differential ----------

// pickerOracleFilter is a naive reference for the filter: lowercase the
// trimmed query, build the haystack by plain field concatenation, and use
// stdlib Contains. Written from the Item contract, not from refilter's code.
func pickerOracleFilter(items []Item, query string) []int {
	q := strings.ToLower(strings.TrimSpace(query))
	var want []int
	for i, it := range items {
		if q == "" {
			want = append(want, i)
			continue
		}
		hay := strings.ToLower(it.ID + " " + it.Leading + " " + it.Primary + " " + it.Detail + " " + it.Badge + " " + it.Keywords)
		if strings.Contains(hay, q) {
			want = append(want, i)
		}
	}
	return want
}

// oracle: naive haystack-containment filter (pickerOracleFilter above) on
// randomized item sets and queries; refilter must produce the same index list
// and the same clamped Selected position.
func TestRefilterMatchesNaiveFilter_Differential(t *testing.T) {
	ids := []string{"", "aaaa-1", "bbbb-2", "日本-id", "Mixed-Case"}
	leads := []string{"", "2m ago", "1h ago"}
	prims := []string{"", "aaaaaaaa", "MixedPrimary"}
	details := []string{"", "fix resume leak", "add themes", "hello 世界"}
	badges := []string{"", "current"}
	keywords := []string{"", "alias-one two"}
	queries := []string{"", " ", "res", "RES", "zzz", "日", "mixed", "  res  ", "themes", "aaaa-1", "x y", "hello 世界"}

	rng := rand.New(rand.NewSource(7))
	for i := range 200 {
		items := make([]Item, rng.Intn(6))
		for j := range items {
			items[j] = Item{
				ID:       ids[rng.Intn(len(ids))],
				Leading:  leads[rng.Intn(len(leads))],
				Primary:  prims[rng.Intn(len(prims))],
				Detail:   details[rng.Intn(len(details))],
				Badge:    badges[rng.Intn(len(badges))],
				Keywords: keywords[rng.Intn(len(keywords))],
			}
		}
		query := queries[rng.Intn(len(queries))]
		sel := rng.Intn(8) - 2 // hostile: negative or past the end

		p := &Picker{Items: items, Query: query, Selected: sel}
		p.refilter()

		wantIdx := pickerOracleFilter(items, query)
		assert.Equal(t, wantIdx, p.filtered, "case %d: query %q", i, query)
		wantSel := 0
		if n := len(wantIdx); n > 0 {
			wantSel = min(max(sel, 0), n-1)
		}
		assert.Equal(t, wantSel, p.Selected, "case %d: selection clamp for query %q", i, query)
	}
}

// oracle: hand-written golden of the whole panel for a fixed item — border
// with centered count title, prompt row with placeholder, the selected item
// row (leading column, primary column, detail), and the hint caption, all
// placed by column arithmetic from the Draw layout rules.
func TestPickerDrawGoldenRows_Differential(t *testing.T) {
	p := &Picker{Theme: components.DefaultTheme()}
	p.Show([]Item{{
		ID:      "aaaaaaaa-1111",
		Leading: "2m ago",
		Primary: "aaaaaaaa",
		Detail:  "fix resume leak",
	}}, ShowConfig{Title: "Sessions", FilterHint: "filter", Hint: "HINT"})

	s := p.Draw(drawCtx())
	require.Len(t, s.Children, 1)
	child := s.Children[0]
	require.Equal(t, components.Size{Width: 72, Height: 4}, child.Surface.Size)
	assert.Equal(t, components.Point{X: 4, Y: 6}, child.Origin, "panel is centered horizontally, a third down")

	rows := strings.Split(strings.TrimSuffix(components.SurfaceText(child.Surface), "\n"), "\n")
	// Title " Sessions · 1 " is 14 columns, centered at tx=(72-14)/2=29.
	want := []string{
		"╭" + strings.Repeat("─", 28) + " Sessions · 1 " + strings.Repeat("─", 28) + "╮",
		"│> filter" + strings.Repeat(" ", 62) + "│",
		"│ 2m ago    aaaaaaaa  fix resume leak" + strings.Repeat(" ", 34) + "│",
		"╰" + strings.Repeat("─", 33) + "HINT" + strings.Repeat("─", 33) + "╯",
	}
	require.Len(t, rows, len(want))
	assert.Equal(t, want, rows)

	// Placeholder prompt parks the caret right after the prompt glyph.
	require.NotNil(t, child.Surface.Cursor)
	assert.Equal(t, components.Point{X: 3, Y: 1}, *child.Surface.Cursor)

	// The selected row is washed across the interior, gutter to edge.
	th := components.DefaultTheme()
	panel := child.Surface
	assert.Equal(t, th.SelectionBg.Bg, panel.Buffer[2*72+1].Style.Bg, "selected row interior is washed")
}
