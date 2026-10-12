package block_test

import (
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/block"
	"github.com/damien1141/a1/internal/components/status"
)

func groupMembers() []block.ChildTool {
	return []block.ChildTool{
		{Name: "read", Detail: "haiku.txt", Status: status.ToolDone},
		{Name: "grep", Detail: `"cursor" in .`, Status: status.ToolDone},
		{Name: "read", Detail: "main.go", Status: status.ToolDone},
	}
}

func newGroup() *block.ReadGroupBlock {
	return &block.ReadGroupBlock{
		Detail:  "Read 2 files, searched for 1 pattern",
		Status:  status.ToolDone,
		Members: groupMembers,
		Theme:   components.DefaultTheme(),
	}
}

func TestReadGroupSummaryHeader(t *testing.T) {
	got := components.SurfaceText(newGroup().Draw(components.DrawContext{
		Max: components.Size{Width: 74, Height: 20},
	}))
	assert.Contains(t, got, "Read 2 files, searched for 1 pattern")
	assert.NotContains(t, got, "haiku.txt", "members stay behind the summary")
}

func TestReadGroupExpandedListsMembers(t *testing.T) {
	g := newGroup()
	g.Expanded = true
	got := components.SurfaceText(g.Draw(components.DrawContext{Max: components.Size{Width: 74, Height: 20}}))
	assert.Contains(t, got, "✓ read haiku.txt")
	assert.Contains(t, got, `✓ grep "cursor" in .`)
	assert.Contains(t, got, "✓ read main.go")
	assert.Contains(t, got, "├── ", "tree gates match the sub-agent rows")
	assert.Contains(t, got, "╰── ", "the last member gets the closing gate")
}

func TestReadGroupRunningUsesPresentTense(t *testing.T) {
	g := newGroup()
	g.Status = status.ToolRunning
	g.Hint = "main.go"
	got := components.SurfaceText(g.Draw(components.DrawContext{Max: components.Size{Width: 74, Height: 20}}))
	assert.Contains(t, got, "Reading 2 files", "in-flight tense")
	assert.Contains(t, got, "searching for 1 pattern")
	assert.Contains(t, got, "main.go", "the current target shows while running")
}

func TestReadGroupResolvedHidesHint(t *testing.T) {
	g := newGroup()
	g.Hint = "main.go"
	got := components.SurfaceText(g.Draw(components.DrawContext{Max: components.Size{Width: 74, Height: 20}}))
	assert.NotContains(t, got, "main.go",
		"a resolved row's last target is history; the expansion carries every path")
}

func TestReadGroupCopyText(t *testing.T) {
	got := newGroup().CopyText()
	assert.Contains(t, got, "Read 2 files, searched for 1 pattern")
	assert.Contains(t, got, "├── read haiku.txt")
	assert.Contains(t, got, "╰── read main.go")
}

func TestReadGroupToggle(t *testing.T) {
	g := newGroup()
	assert.False(t, g.Expanded)
	g.Handle(&components.EventContext{}, xui.KeyEvent{Press: true, Code: xui.KeyEnter})
	assert.True(t, g.Expanded)
	g.Handle(&components.EventContext{}, xui.KeyEvent{Press: true, Code: xui.KeyEnter})
	assert.False(t, g.Expanded)
}

func TestReadGroupNoMembersIsNotExpandable(t *testing.T) {
	g := &block.ReadGroupBlock{
		Detail: "Read 2 files", Status: status.ToolDone,
		Members: func() []block.ChildTool { return nil },
		Theme:   components.DefaultTheme(),
	}
	assert.False(t, g.HasBody())
	require.Equal(t, "Read 2 files", g.CopyText())
}
