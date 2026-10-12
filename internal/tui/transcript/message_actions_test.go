package transcript_test

import (
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/transcript"
)

// threeBlocks gives the pane three distinct copyable rows.
func threeBlocks() *transcript.TranscriptPane {
	pane := transcript.NewTranscriptPane(components.DefaultTheme(), nil, "A1 test")
	snap := session.Snapshot{
		Messages: []session.Message{
			{ID: "u1", Role: session.RoleUser, Text: "first prompt"},
			{
				ID: "a1", Role: session.RoleAssistant, State: session.StateComplete,
				Content: []session.ContentBlock{{
					Type: session.BlockToolUse, ID: "r1", Name: "read", Input: "haiku.txt", Complete: true,
				}},
			},
			{ID: "u2", Role: session.RoleUser, Text: "second prompt"},
		},
		Tools: map[string]session.ToolRun{"r1": {
			ToolUseID: "r1", Name: "read", Status: session.ToolDone, Detail: "haiku.txt",
		}},
	}
	pane.LoadReplay(snap)
	pane.Sync()
	return pane
}

func TestMessageActionsEnterStartsOnLatest(t *testing.T) {
	pane := threeBlocks()
	a := pane.Actions()
	assert.False(t, a.Active)

	require.True(t, a.Enter(), "entering with entries present works")
	assert.True(t, a.Active)

	assert.NotEmpty(t, a.CopyText(), "and it has copyable text")
}

func TestMessageActionsEmptyPaneDoesNotEnter(t *testing.T) {
	pane := transcript.NewTranscriptPane(components.DefaultTheme(), nil, "A1 test")
	a := pane.Actions()
	assert.False(t, a.Enter(), "an empty transcript has nothing to point at")
	assert.False(t, a.Active)
}

func TestMessageActionsMoveClamps(t *testing.T) {
	pane := threeBlocks()
	a := pane.Actions()
	require.True(t, a.Enter())

	// Start at 2 (last). Up to 1, then 0, then stop — no wraparound.
	assert.True(t, a.Move(-1))
	assert.Equal(t, 1, a.CursorIndex())
	assert.True(t, a.Move(-1))
	assert.Equal(t, 0, a.CursorIndex())
	assert.False(t, a.Move(-1), "clamped at the top rather than wrapping")
	assert.Equal(t, 0, a.CursorIndex())

	assert.True(t, a.Move(1))
	assert.Equal(t, 1, a.CursorIndex())
}

func TestMessageActionsPosition(t *testing.T) {
	pane := threeBlocks()
	a := pane.Actions()
	assert.Empty(t, a.Position(), "no position outside the mode")

	require.True(t, a.Enter())
	assert.Equal(t, "3/3", a.Position())
	require.True(t, a.Move(-1))
	assert.Equal(t, "2/3", a.Position())
}

func TestMessageActionsEscapeExits(t *testing.T) {
	pane := threeBlocks()
	a := pane.Actions()
	require.True(t, a.Enter())

	ctx := &components.EventContext{}
	consumed := a.Handle(ctx, xui.KeyEvent{Press: true, Code: xui.KeyEscape})
	assert.True(t, consumed)
	assert.False(t, a.Active, "escape leaves the mode")
	assert.Equal(t, -1, a.CursorIndex())
	assert.Equal(t, -1, pane.SelectedEntryForTest(), "and clears the highlight")
}

func TestMessageActionsKeyRouting(t *testing.T) {
	pane := threeBlocks()
	a := pane.Actions()
	require.True(t, a.Enter())

	ctx := &components.EventContext{}
	assert.True(t, a.Handle(ctx, xui.KeyEvent{Press: true, Code: xui.KeyUp}))
	assert.Equal(t, 1, a.CursorIndex())
	assert.True(t, a.Handle(ctx, xui.KeyEvent{Press: true, Code: xui.KeyRune, Rune: 'k'}))
	assert.Equal(t, 0, a.CursorIndex())
	assert.True(t, a.Handle(ctx, xui.KeyEvent{Press: true, Code: xui.KeyRune, Rune: 'j'}))
	assert.Equal(t, 1, a.CursorIndex())
}

func TestMessageActionsCopyTextPerBlock(t *testing.T) {
	pane := threeBlocks()
	a := pane.Actions()
	require.True(t, a.Enter())

	// Cursor lands on the last user message.
	assert.Equal(t, "second prompt", a.CopyText())

	require.True(t, a.Move(-1))
	assert.NotEmpty(t, a.CopyText(), "the read row copies its own text")
}

func TestMessageActionsIgnoresKeysWhileInactive(t *testing.T) {
	pane := threeBlocks()
	a := pane.Actions()
	ctx := &components.EventContext{}
	// A bare 'c' with no modifiers must fall through to the composer while the
	// mode is off, or typing would be impossible.
	assert.False(t, a.Handle(ctx, xui.KeyEvent{Press: true, Code: xui.KeyRune, Rune: 'c'}))
	assert.False(t, a.Active)
}

func TestMessageActionsModifiersFallThrough(t *testing.T) {
	pane := threeBlocks()
	a := pane.Actions()
	require.True(t, a.Enter())

	ctx := &components.EventContext{}
	// Ctrl+C is the interrupt chord, never a copy from the cursor.
	assert.False(t, a.Handle(ctx, xui.KeyEvent{
		Press: true, Code: xui.KeyRune, Rune: 'c', Mods: xui.ModCtrl,
	}))
}

func TestMessageActionsCursorSurvivesSync(t *testing.T) {
	pane := threeBlocks()
	a := pane.Actions()
	require.True(t, a.Enter())
	require.True(t, a.Move(-1))

	// An unrelated sync must not silently retarget the cursor onto a different
	// block; the ids are unchanged, so the cursor stays exactly where it was.
	before := a.CursorIndex()
	pane.Sync()
	assert.Equal(t, before, a.CursorIndex(), "a no-op sync keeps the cursor put")
}

func TestMessageActionsEntryIDIsExposed(t *testing.T) {
	pane := threeBlocks()
	a := pane.Actions()
	require.True(t, a.Enter())
	assert.NotEmpty(t, a.ItemID(), "the cursor names the item, which an edit needs")
}
