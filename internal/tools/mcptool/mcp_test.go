package mcptool_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/mcp"
	"github.com/damien1141/a1/internal/tools/mcptool"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

func TestMCPToolsRegister(t *testing.T) {
	pool := mcp.NewPool(map[string]mcp.ServerConfig{
		"echo": {Command: []string{"true"}},
	})
	tools := mcptool.Tools(pool)
	require.Len(t, tools, 3)
	byName := map[string]bool{}
	for _, tool := range tools {
		byName[tool.Definition.Name] = true
	}
	for _, name := range []string{"mcp_list", "mcp_inspect", "mcp_call"} {
		assert.True(t, byName[name], "missing %s", name)
	}
}

func TestMCPToolsNilPool(t *testing.T) {
	require.Nil(t, mcptool.Tools(nil))
}

func TestMCPListRequiresServer(t *testing.T) {
	pool := mcp.NewPool(map[string]mcp.ServerConfig{
		"echo": {Command: []string{"true"}},
	})
	list := findTool(t, mcptool.Tools(pool), "mcp_list")
	req := list.Definition.Params.Required
	require.Len(t, req, 1)
	require.Equal(t, "server", req[0])
	_, err := list.Run(t.Context(), []byte(`{}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "server is required")
}

func findTool(t *testing.T, tools []tooldef.Tool, name string) tooldef.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Definition.Name == name {
			return tool
		}
	}
	require.Failf(t, "tool not found", "missing %s", name)
	return tooldef.Tool{}
}
