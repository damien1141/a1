package block_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/block"
	"github.com/damien1141/a1/internal/components/status"
)

func TestLastRunningChildPrefersInFlight(t *testing.T) {
	got := block.LastRunningChild([]block.ChildTool{
		{Name: "read", Detail: "haiku.txt", Status: status.ToolDone},
		{Name: "grep", Detail: "cursor in .", Status: status.ToolDone},
		{Name: "edit", Detail: "main.go", Status: status.ToolRunning},
		{Name: "read", Detail: "main.go", Status: status.ToolQueued},
	})
	assert.Equal(t, "edit · main.go", got, "the running tool wins over the queued one")
}

func TestLastRunningChildFallsBackToMostRecent(t *testing.T) {
	got := block.LastRunningChild([]block.ChildTool{
		{Name: "read", Detail: "haiku.txt", Status: status.ToolDone},
		{Name: "grep", Detail: "cursor in .", Status: status.ToolDone},
	})
	assert.Equal(t, "grep · cursor in .", got,
		"a finished sub-agent still shows what it last did")
}

func TestLastRunningChildEmpty(t *testing.T) {
	assert.Empty(t, block.LastRunningChild(nil))
	assert.Empty(t, block.LastRunningChild([]block.ChildTool{}))
	assert.Empty(t, block.LastRunningChild([]block.ChildTool{{Name: "  ", Status: status.ToolRunning}}),
		"a nameless child contributes nothing")
}

func TestAgentBlockHeaderShowsActivityWhileRunning(t *testing.T) {
	th := components.DefaultTheme()
	a := &block.AgentBlock{
		Name: "agent_spawn", Detail: "find the writer", Status: status.ToolRunning,
		Activity: "grep · cursor in .", Theme: th,
	}
	got := components.SurfaceText(a.Draw(components.DrawContext{
		Max: components.Size{Width: 74, Height: 20},
	}))
	assert.Contains(t, got, "→ grep · cursor in .")
	assert.Contains(t, got, "find the writer", "the spawn description stays")
}

func TestAgentBlockHidesActivityWhenResolved(t *testing.T) {
	th := components.DefaultTheme()
	a := &block.AgentBlock{
		Name: "agent_spawn", Detail: "find the writer", Status: status.ToolDone,
		Activity: "grep · cursor in .", Theme: th,
	}
	got := components.SurfaceText(a.Draw(components.DrawContext{
		Max: components.Size{Width: 74, Height: 20},
	}))
	assert.NotContains(t, got, "→", "a resolved row names nothing live")
	assert.Contains(t, got, "find the writer")
}
