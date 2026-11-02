package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAgentsEnabledToggle(t *testing.T) {
	with := Build("", true, 4, nil)
	without := Build("", false, 0, nil)

	require.Contains(t, with, "agent_spawn")
	require.Contains(t, with, "Sub-agents:")
	require.Contains(t, with, "At most 4 sub-agents run concurrently")
	require.NotContains(t, without, "agent_spawn")
	require.NotContains(t, without, "sub-agents run concurrently")
	require.Contains(t, without, "`find` / `grep` / `ls` yourself")
}

func TestBuildEditMatchesOldStrNewStr(t *testing.T) {
	got := Build("", false, 0, nil)
	// The hash-anchor system is gone: the prompt must not tell the model to
	// compute or copy a hash, and must name the fields edit actually matches on.
	require.NotContains(t, got, "4 hex chars after `#`")
	require.NotContains(t, got, "`edit.hash`")
	require.NotContains(t, got, "@file path#TAG")
	require.Contains(t, got, "`old_str` / `new_str`")
	require.NotContains(t, got, "Known path or exact symbol")
	require.NotContains(t, got, "creates a new file only")
	require.NotContains(t, got, "fails if it already exists")
	require.Contains(t, got, "`write` creates or overwrites")
	require.Contains(t, got, "Prefer cwd-relative paths")
}

// The task row is driven entirely by prompt compliance — there is no tool
// holding the state — so the instructions that keep TODO.md honest are
// load-bearing and need a guard.
func TestBuildIncludesTaskInstructions(t *testing.T) {
	got := Build("", false, 0, nil)
	assert.Contains(t, got, "# Tasks")
	assert.Contains(t, got, "TODO.md")
	assert.Contains(t, got, "- [~]", "the in-progress marker the renderer keys on")
	assert.Contains(t, got, "must not go stale")
	assert.Contains(t, got, "3 or more steps", "small work is explicitly out of scope")
}

// The prompt rules are the mechanism for both features — there is no tool state
// behind either — so each needs a guard.
func TestBuildIncludesQuestionInstructions(t *testing.T) {
	got := Build("", false, 0, nil)
	assert.Contains(t, got, "# Questions")
	assert.Contains(t, got, "ask_user")
	assert.Contains(t, got, "(Recommended)", "steering by rank is the documented pattern")
	assert.Contains(t, got, "confirmation", "the 'not a confirm dialog' rule is the one that decays")
	assert.Contains(t, got, "2 to 4 options")
}

func TestBuildMCPCatalog(t *testing.T) {
	none := Build("", false, 0, nil)
	require.NotContains(t, none, "# MCP")
	require.NotContains(t, none, "External docs/URLs")
	got := Build("", false, 0, []string{"browsermcp", "github"})
	require.Contains(t, got, "# MCP")
	require.Contains(t, got, "- browsermcp")
	require.Contains(t, got, "- github")
	require.Contains(t, got, "mcp_list")
	require.Contains(t, got, "mcp_inspect")
	require.Contains(t, got, "mcp_call")
	require.Contains(t, got, "docs/URLs")
	require.NotContains(t, got, `"properties"`)
	require.NotContains(t, got, "inputSchema")
}
