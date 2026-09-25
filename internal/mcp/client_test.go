package mcp_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/mcp"
)

func TestConfigLoadSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := mcp.ServerConfig{
		Transport: "stdio",
		Command:   []string{"npx"},
		Args:      []string{"-y", "pkg"},
	}
	require.NoError(t, mcp.AddServer("demo", cfg))

	servers, err := mcp.Load(filepath.Join(t.TempDir(), ".phi", "mcp.json"))
	require.NoError(t, err)
	require.Contains(t, servers, "demo", "expected demo server")
	require.Equal(t, []string{"npx"}, servers["demo"].Command, "command mismatch")

	ok, err := mcp.RemoveServer("demo")
	require.NoError(t, err)
	require.True(t, ok, "expected remove ok")
}

func TestCompactAndSlim(t *testing.T) {
	require.Equal(t, "a b", mcp.CompactServerList([]string{"a", "b"}))
	tools := []mcp.ToolDefinition{
		{
			Name:        "echo",
			Description: "Echo back",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`,
			),
		},
	}
	require.Equal(t, "echo", mcp.CompactToolNames(tools))
	slim := mcp.SlimTool(tools[0])
	require.Contains(t, slim, "echo")
	require.Contains(t, slim, "message:s*")
}

func TestDisabled(t *testing.T) {
	t.Setenv("PHI_MCP", "off")
	require.True(t, mcp.Disabled(), "expected disabled")
	pool, err := mcp.LoadPool(filepath.Join(t.TempDir(), ".phi", "mcp.json"))
	require.NoError(t, err)
	require.Nil(t, pool, "expected nil pool")
}
