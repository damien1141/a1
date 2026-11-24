package block

import (
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

// drawHeight renders b at a fixed width and returns the rows it occupies.
func drawHeight(b *BashBlock, width int) int {
	s := b.Draw(components.DrawContext{Max: components.Size{Width: width, Height: 200}})
	return s.Size.Height
}

// longCommand is a realistic oversized command: a test invocation with a long
// package list, the shape that used to claim most of the viewport.
func longCommand() string {
	return "go test " + strings.Repeat("./internal/somepackage/... ", 40)
}

// TestBashBlock_CollapsedCommandIsBounded_Positive: a huge command must not eat
// the screen while collapsed. The budget is rows, and the collapsed row keeps
// its command to BashCollapsedCommandLines plus the title's own wrapping.
func TestBashBlock_CollapsedCommandIsBounded_Positive(t *testing.T) {
	b := &BashBlock{
		Command: longCommand(),
		Output:  "ok  \tgithub.com/x/y\t0.4s",
		Theme:   components.DefaultTheme(),
	}
	collapsed := drawHeight(b, 80)

	b.Expanded = true
	expanded := drawHeight(b, 80)

	assert.Less(t, collapsed, 8,
		"a collapsed oversized command must stay small on screen")
	assert.Greater(t, expanded, collapsed,
		"expanding must actually reveal more, or the toggle is a no-op")
}

// TestBashBlock_ShortCommandUnchanged_Negative: the common case renders exactly
// as before. A layout change that also reflows every one-line command would be
// a regression dressed as a feature.
func TestBashBlock_ShortCommandUnchanged_Negative(t *testing.T) {
	b := &BashBlock{Command: "ls -la", Output: "total 0", Theme: components.DefaultTheme()}
	assert.LessOrEqual(t, drawHeight(b, 80), 4,
		"a short command stays compact and is never truncated")
	assert.False(t, b.commandTruncatable(), "nothing to truncate")
}

// TestBashBlock_LongCommandIsExpandableWithoutOutput_Positive: a long command
// with NO output was previously un-expandable and showed no affordance at all —
// the arrow only appeared when there was a body. The command alone is now a
// reason to expand.
func TestBashBlock_LongCommandIsExpandableWithoutOutput_Positive(t *testing.T) {
	b := &BashBlock{Command: longCommand(), Theme: components.DefaultTheme()}
	require.False(t, b.hasBody(), "precondition: no output")
	assert.True(t, b.expandable(), "a truncated command must be expandable on its own")
	assert.True(t, b.commandTruncatable())

	// And the toggle works.
	ctx := &components.EventContext{}
	b.Handle(ctx, xui.KeyEvent{Code: xui.KeyEnter, Press: true})
	assert.True(t, b.Expanded)
}

// TestTruncateCommand_TwoRegimes_Differential pins WHERE the cut lands, which
// now depends on how long the command is.
//
// The oracle is hand-written: a normal long command keeps its HEAD (the head is
// its identity), while a very long one keeps only the RUNNER plus a size marker
// — because the head of a 745-char inline script is `python3 - <<'PY' p='...`,
// technically correct and useless to read.
func TestTruncateCommand_TwoRegimes_Differential(t *testing.T) {
	// Normal regime: over the row budget, under CommandSynopsisChars.
	normal := "git commit -m \"a long commit message with several words in it\""
	got := truncateCommand(normal, 40)
	assert.True(t, strings.HasPrefix(got, "git commit -m"), "the head is kept: %q", got)
	assert.Contains(t, got, "chars)", "the truncation is disclosed")

	// Synopsis regime: far past the threshold.
	huge := "git commit -m \"a very long message " + strings.Repeat("with more words ", 30) + "\""
	assert.Greater(t, len([]rune(huge)), CommandSynopsisChars)
	syn := truncateCommand(huge, 40)
	assert.True(t, strings.HasPrefix(syn, "git commit"), "runner plus subcommand: %q", syn)
	assert.Contains(t, syn, "chars)")
	assert.NotContains(t, syn, "very long message",
		"the body of a very long command is not reproduced in a collapsed row")

	// A short command is untouched (flattened only).
	assert.Equal(t, "ls -la", truncateCommand("ls -la", 40))
	assert.Equal(t, "echo a echo b", truncateCommand("echo a\necho b", 80),
		"newlines become spaces")
	assert.Equal(t, "a && b || c", truncateCommand("a && b || c", 80),
		"shell operators are preserved verbatim")
}
