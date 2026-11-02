package block_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/block"
	"github.com/damien1141/a1/internal/components/status"
)

// bashText renders a bash row the way the terminal sees it, so these tests
// assert on visible output rather than on widget internals.
func bashText(b *block.BashBlock, width int) string {
	s := b.Draw(components.DrawContext{Max: components.Size{Width: width, Height: 60}})
	return components.SurfaceText(s)
}

func TestBashBlockCollapsedShowsTailNotWholeOutput(t *testing.T) {
	var out strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&out, "line %02d\n", i)
	}
	b := &block.BashBlock{
		Command: "go test ./...",
		Output:  out.String(),
		Status:  status.ToolDone,
		Theme:   components.DefaultTheme(),
	}
	require.False(t, b.Expanded, "starts collapsed")

	got := bashText(b, 80)
	assert.Contains(t, got, "go test ./...", "the command is always shown")
	assert.Contains(t, got, "line 40", "the newest line is visible collapsed")
	assert.Contains(t, got, "… +32 more lines", "the hidden count is reported")
	assert.NotContains(t, got, "line 01", "the oldest line is not shown collapsed")
}

func TestBashBlockCollapsedShortOutputHasNoTrailer(t *testing.T) {
	b := &block.BashBlock{
		Command: "ls",
		Output:  "parser.go\nrun.go\n",
		Status:  status.ToolDone,
		Theme:   components.DefaultTheme(),
	}
	got := bashText(b, 80)
	assert.Contains(t, got, "parser.go")
	assert.NotContains(t, got, "more lines", "a row within budget needs no trailer")
}

func TestBashBlockErrorIsNotTruncatedWhenCollapsed(t *testing.T) {
	var out strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&out, "err %02d\n", i)
	}
	b := &block.BashBlock{
		Command: "make",
		Output:  out.String(),
		Status:  status.ToolError,
		Theme:   components.DefaultTheme(),
	}
	got := bashText(b, 80)
	assert.Contains(t, got, "err 01", "a failed command shows from the top when collapsed")
	assert.Contains(t, got, "err 30", "…and through the end")
	assert.NotContains(t, got, "more lines", "no trailer on the full error body")
}

func TestBashBlockExpandedBeatsCollapsedTail(t *testing.T) {
	var out strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&out, "line %02d\n", i)
	}
	b := &block.BashBlock{
		Command:  "go build ./...",
		Output:   out.String(),
		Status:   status.ToolDone,
		Expanded: true,
		Theme:    components.DefaultTheme(),
	}
	got := bashText(b, 80)
	assert.Contains(t, got, "line 01", "expanded shows the first line")
	assert.Contains(t, got, "line 20", "expanded shows the last line")
	assert.NotContains(t, got, "more lines", "no trailer when expanded")
}

// A wrapped line costs several surface rows, so the tail must be measured on the
// rendered rows: one very long output line collapsed to a preview stays bounded.
func TestBashBlockTailCountsWrappedLines(t *testing.T) {
	b := &block.BashBlock{
		Command: "cat big.log",
		Output:  strings.Repeat("x", 600) + "\n" + "tail line\n",
		Status:  status.ToolDone,
		Theme:   components.DefaultTheme(),
	}
	got := bashText(b, 40)
	assert.Contains(t, got, "tail line", "the newest line survives")
	assert.Contains(t, got, "more lines", "a wrapped flood is still counted and reported")
}

// The tool's own truncation notice must survive into the collapsed view, since
// it is the line that says where the full log went.
func TestBashBlockCollapsedKeepsToolTailNotice(t *testing.T) {
	b := &block.BashBlock{
		Command: "go test ./...",
		Output:  "…\n\n[Showing lines 993-1000 of 1000. Full output: /tmp/a1-bash-abc.log]",
		Status:  status.ToolDone,
		Theme:   components.DefaultTheme(),
	}
	got := bashText(b, 80)
	assert.Contains(t, got, "Showing lines 993-1000 of 1000", "the dump link stays visible collapsed")
}

// Running bash with partial output: the user must watch progress live.
func TestRunningBashShowsLiveOutput(t *testing.T) {
	b := &block.BashBlock{Command: "go test ./...", Output: "ok\n", Status: status.ToolRunning, Theme: components.DefaultTheme()}
	got := bashText(b, 80)
	assert.Contains(t, got, "ok")
}

// Empty output, still running: nothing to tail, row must not panic or draw gaps.
func TestRunningBashNoOutput(t *testing.T) {
	b := &block.BashBlock{Command: "sleep 5", Status: status.ToolRunning, Theme: components.DefaultTheme()}
	got := bashText(b, 40)
	assert.Contains(t, got, "sleep 5")
}

// Output that is exactly the budget: no trailer, full display.
func TestExactBudgetNoTrailer(t *testing.T) {
	var out strings.Builder
	for i := 1; i <= 8; i++ {
		fmt.Fprintf(&out, "line %d\n", i)
	}
	b := &block.BashBlock{Command: "x", Output: out.String(), Status: status.ToolDone, Theme: components.DefaultTheme()}
	got := bashText(b, 80)
	assert.Contains(t, got, "line 1")
	assert.Contains(t, got, "line 8")
	assert.NotContains(t, got, "more lines")
}

// One line over budget: trailer says exactly 1.
func TestOneOverBudget(t *testing.T) {
	var out strings.Builder
	for i := 1; i <= 9; i++ {
		fmt.Fprintf(&out, "line %d\n", i)
	}
	b := &block.BashBlock{Command: "x", Output: out.String(), Status: status.ToolDone, Theme: components.DefaultTheme()}
	got := bashText(b, 80)
	assert.NotContains(t, got, "line 1 ")
	assert.Contains(t, got, "line 9")
	assert.Contains(t, got, "… +1 more lines")
}
