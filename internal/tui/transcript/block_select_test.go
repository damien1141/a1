package transcript

import (
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/block"
	"github.com/damien1141/a1/internal/components/status"
)

// selectablePane builds a painted transcript with three blocks and a recording
// clipboard. Draw must run with real dimensions: listH is set by Draw, and the
// mouse handler refuses clicks when it is zero.
func selectablePane(t *testing.T) (*TranscriptPane, *[]string) {
	t.Helper()
	pane := NewTranscriptPane(components.DefaultTheme(), status.NewSpinner(components.DefaultTheme().Foreground), "")
	var copied []string
	pane.SetCopyHandlers(nil, func(s string) bool { copied = append(copied, s); return true })
	pane.list.Entries = []components.Widget{
		&block.UserBlock{Text: "first block"},
		&block.UserBlock{Text: "second block"},
		&block.UserBlock{Text: "third block"},
	}
	_ = pane.Draw(components.DrawContext{Max: components.Size{Width: 80, Height: 40}}, 80, 40)
	return pane, &copied
}

// clickAt drives a full press+release at one view row.
func clickAt(pane *TranscriptPane, y int) {
	pane.HandleMouse(&components.EventContext{}, xui.MouseEvent{
		Action: xui.MousePress, Button: xui.MouseLeft, X: 10, Y: y,
	}, nil)
	pane.HandleMouse(&components.EventContext{}, xui.MouseEvent{
		Action: xui.MouseRelease, Button: xui.MouseLeft, X: 10, Y: y,
	}, nil)
}

// TestBlockSelection_ClickSelectsBlock_Positive: clicking a block marks it
// selected, and Ctrl+Shift+C then copies THAT block rather than the last one.
//
// This is the path that was reported broken: text selection worked, block
// selection appeared to do nothing.
func TestBlockSelection_ClickSelectsBlock_Positive(t *testing.T) {
	pane, copied := selectablePane(t)
	require.Greater(t, pane.listH, 0, "precondition: the pane was painted")

	// Derive the row from the block's own painted origin rather than assuming a
	// gap width: turn spacing now varies the gaps, and a hard-coded offset would
	// silently test the wrong row.
	mid, ok := pane.list.EntryOrigin(1)
	require.True(t, ok)
	clickAt(pane, mid.Y)
	require.GreaterOrEqual(t, pane.list.Selected, 0, "the click selects a block")

	want := []string{"first block", "second block", "third block"}[pane.list.Selected]
	require.True(t, pane.HandleCopyKey(&components.EventContext{}, copyChord))
	require.Len(t, *copied, 1)
	assert.Contains(t, (*copied)[0], want,
		"the SELECTED block is copied, not whatever came last")
}

// TestBlockSelection_SelectionIsVisible_Differential pins the highlight to the
// theme's selection token. The row used to paint a hardcoded dark green that was
// invisible on other palettes — the selection existed and the user could not see
// it, which reads exactly like the click not working.
func TestBlockSelection_SelectionIsVisible_Differential(t *testing.T) {
	pane, _ := selectablePane(t)
	pane.list.Selected = 1

	surf := pane.list.Draw(components.DrawContext{Max: components.Size{Width: 80, Height: 40}})
	theme := components.DefaultTheme()

	// The selected entry's surface must carry the theme's selection background
	// on at least one cell.
	found := false
	for _, child := range surf.Children {
		for _, c := range child.Surface.Buffer {
			if c.Style.Bg == theme.SelectionBg.Bg {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	assert.True(t, found,
		"the selected block must be tinted with the theme's selection colour, "+
			"not a hardcoded one that vanishes on other palettes")
}

// TestBlockSelection_NoSelectionCopiesLast_Negative: with nothing selected the
// chord still copies the newest block, so it never does nothing.
func TestBlockSelection_NoSelectionCopiesLast_Negative(t *testing.T) {
	pane, copied := selectablePane(t)
	pane.list.Selected = -1

	require.True(t, pane.HandleCopyKey(&components.EventContext{}, copyChord))
	require.Len(t, *copied, 1)
	assert.Contains(t, (*copied)[0], "third block")
}

// TestBlockSelection_GapRowStillSelects_Positive is the regression pin for the
// actual "block select doesn't work" cause.
//
// Blocks are laid out with a one-row spacing gap that belongs to no entry by
// the list's own contract. With one-row blocks that means every OTHER row was
// dead: a click landing there resolved to -1, no selection was recorded, and
// Ctrl+Shift+C quietly copied the last block instead. Half of all clicks
// failed, which is indistinguishable from the feature being broken.
//
// The list keeps its exact contract (a spacing row belongs to no entry — see
// its own trio test); the CLICK is forgiving by one row, because a user aiming
// at a block should not have to hit its exact row.
func TestBlockSelection_GapRowStillSelects_Positive(t *testing.T) {
	pane, _ := selectablePane(t)
	origin := pane.list.ContentOrigin()
	require.Greater(t, pane.listH, 0)

	// Every row the content occupies must select something. Walk the real
	// painted extent: turn spacing makes the gap width non-uniform, so a fixed
	// row count would drift out of sync with the layout.
	lastOrigin, lastOK := pane.list.EntryOrigin(2)
	require.True(t, lastOK)
	for y := origin; y <= lastOrigin.Y; y++ {
		pane.list.Selected = -1
		clickAt(pane, y)
		assert.GreaterOrEqual(t, pane.list.Selected, 0,
			"a click at row %d inside the content must select a block", y)
	}

	// Rows past the content still select nothing — the forgiveness is bounded.
	pane.list.Selected = -1
	clickAt(pane, origin+40)
	assert.Equal(t, -1, pane.list.Selected,
		"a click far below the content is not a near-miss")
}

// TestBlockSelection_BashBlockCopyIncludesOutput_Positive: the whole point of
// selecting a bash block — Ctrl+Shift+C must yield the command AND its retained
// output, not just the command line.
func TestBlockSelection_BashBlockCopyIncludesOutput_Positive(t *testing.T) {
	pane := NewTranscriptPane(components.DefaultTheme(), status.NewSpinner(components.DefaultTheme().Foreground), "")
	var copied []string
	pane.SetCopyHandlers(nil, func(s string) bool { copied = append(copied, s); return true })
	pane.list.Entries = []components.Widget{
		&block.BashBlock{
			Command: "go test ./...",
			Output:  "ok  github.com/x/a\nok  github.com/x/b\nFAIL github.com/x/c",
			Theme:   components.DefaultTheme(),
		},
	}
	_ = pane.Draw(components.DrawContext{Max: components.Size{Width: 80, Height: 40}}, 80, 40)
	clickAt(pane, pane.list.ContentOrigin())

	require.Equal(t, 0, pane.list.Selected, "the bash block is selectable")
	require.True(t, pane.HandleCopyKey(&components.EventContext{}, copyChord))
	require.Len(t, copied, 1)

	got := copied[0]
	assert.Contains(t, got, "go test ./...", "the command is included")
	assert.Contains(t, got, "FAIL github.com/x/c", "the output is included")
}

// TestBlockSelection_ClickTogglesBashBlock_Positive pins the dispatch that was
// missing entirely: mouse events reached the transcript pane but were never
// forwarded to the block widgets, so a block's own click handling (expand,
// collapse) was dead in the TUI. Selecting a long command without being able to
// expand it is half a feature.
func TestBlockSelection_ClickTogglesBashBlock_Positive(t *testing.T) {
	pane := NewTranscriptPane(components.DefaultTheme(), status.NewSpinner(components.DefaultTheme().Foreground), "")
	bb := &block.BashBlock{
		Command: "go test ./...",
		Output:  "a\nb\nc\nd\ne\nf\ng\nh\ni\nj",
		Theme:   components.DefaultTheme(),
	}
	pane.list.Entries = []components.Widget{bb}
	_ = pane.Draw(components.DrawContext{Max: components.Size{Width: 80, Height: 40}}, 80, 40)

	origin, ok := pane.list.EntryOrigin(0)
	require.True(t, ok)
	require.False(t, bb.Expanded, "precondition: collapsed")

	clickAt(pane, origin.Y) // the title row
	assert.True(t, bb.Expanded, "clicking the title expands the block")
	assert.Equal(t, 0, pane.list.Selected, "and it is selected for copying")

	clickAt(pane, origin.Y)
	assert.False(t, bb.Expanded, "clicking again collapses it")
}

// TestBlockSelection_ClickInBodyDoesNotToggle_Negative: the body stays a
// selection/copy surface. Only the title is a toggle, so dragging to select text
// inside the output never collapses the block out from under the user.
func TestBlockSelection_ClickInBodyDoesNotToggle_Negative(t *testing.T) {
	pane := NewTranscriptPane(components.DefaultTheme(), status.NewSpinner(components.DefaultTheme().Foreground), "")
	bb := &block.BashBlock{
		Command: "go test ./...",
		Output:  "a\nb\nc\nd\ne\nf\ng\nh\ni\nj",
		Theme:   components.DefaultTheme(),
	}
	pane.list.Entries = []components.Widget{bb}
	_ = pane.Draw(components.DrawContext{Max: components.Size{Width: 80, Height: 40}}, 80, 40)

	origin, _ := pane.list.EntryOrigin(0)
	clickAt(pane, origin.Y+4) // deep in the body

	assert.False(t, bb.Expanded, "a body click must not toggle")
	assert.Equal(t, 0, pane.list.Selected, "but it still selects the block")
}
