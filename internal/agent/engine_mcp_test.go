package agent_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/agent"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/mcp"
)

func TestEngineRegistersMCPMetaTools(t *testing.T) {
	pool := mcp.NewPool(map[string]mcp.ServerConfig{
		"echo": {Command: []string{"true"}},
	})

	sess, err := agent.NewSession(agent.WithCwd(t.TempDir()))
	require.NoError(t, err)
	eng, err := agent.NewEngine(
		llm.ModelConfig{Name: "test", APIKey: "x", BaseURL: "http://127.0.0.1:9"},
		sess,
		agent.WithMCP(pool),
	)
	require.NoError(t, err)
	for _, name := range []string{"mcp_list", "mcp_inspect", "mcp_call", "bash"} {
		require.True(t, eng.HasTool(name), "missing tool %s", name)
	}

	// Explicit child tool list without MCP → no meta tools (sub-agent path).
	sess2, err := agent.NewSession(agent.WithCwd(t.TempDir()))
	require.NoError(t, err)
	eng2, err := agent.NewEngine(
		llm.ModelConfig{Name: "test", APIKey: "x", BaseURL: "http://127.0.0.1:9"},
		sess2,
		agent.WithTools(agent.ChildTools()),
	)
	require.NoError(t, err)
	require.False(t, eng2.HasTool("mcp_list"), "child tools should not include mcp_list")
}
