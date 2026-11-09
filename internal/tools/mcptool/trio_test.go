package mcptool_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/mcp"
	"github.com/damien1141/a1/internal/tools/mcptool"
)

// ---------- differential ----------

// fixtureServerScript is a hermetic stdio MCP server: one line of JSON-RPC in,
// one canned response out. It advertises three tools (echo with a required
// string param, add with two required numbers, ping with no params) and
// answers tools/call on echo with a fixed text part.
const fixtureServerScript = `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"fixture","version":"1.0"}}}\n'
      ;;
    *'"method":"tools/list"'*)
      printf '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"echo","description":"Echo a message","inputSchema":{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}},{"name":"add","description":"Add two numbers","inputSchema":{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}},"required":["a","b"]}},{"name":"ping","description":"Check liveness","inputSchema":{"type":"object","properties":{}}}]}}\n'
      ;;
    *'"method":"tools/call"'*)
      case "$line" in
        *'"name":"echo"'*)
          printf '{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"pong"}]}}\n'
          ;;
        *)
          printf '{"jsonrpc":"2.0","id":4,"result":{"content":[{"type":"text","text":"generic reply"}]}}\n'
          ;;
      esac
      ;;
  esac
done
`

// fixturePool starts the canned server under sh and returns a pool wired to
// it. Logs are pinned to a temp dir so nothing touches the real HOME.
func fixturePool(t *testing.T) *mcp.Pool {
	t.Helper()
	t.Setenv("A1_MCP_LOG_DIR", t.TempDir())

	script := filepath.Join(t.TempDir(), "fixture-mcp.sh")
	require.NoError(t, os.WriteFile(script, []byte(fixtureServerScript), 0o644))

	pool := mcp.NewPool(map[string]mcp.ServerConfig{
		"fakesrv": {Transport: "stdio", Command: []string{"sh"}, Args: []string{script}},
	})
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

// toolCtx bounds every wire call so a broken fixture fails in seconds
// instead of hitting the transport's 60s default.
func toolCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// oracle: hand-written fixture server state plus naive string computation —
// the script above is the source of truth for the tool registry, and the
// expected listing is just the names joined with spaces, computed here
// without going through CompactToolNames.
func TestMCPListRun_MatchesFixtureServer_Differential(t *testing.T) {
	list := findTool(t, mcptool.Tools(fixturePool(t)), "mcp_list")

	res, err := list.Run(toolCtx(t), []byte(`{"server":"fakesrv"}`))
	require.NoError(t, err)

	names := []string{"echo", "add", "ping"}
	var want string
	for i, n := range names {
		if i > 0 {
			want += " "
		}
		want += n
	}
	assert.Equal(t, want, res.Content)
	assert.Equal(t, "fakesrv: 3 tools", res.Detail)
	assert.Equal(t, want, res.Output)
}

// oracle: hand-written golden literals for the documented slim format
// ("name — description" then "name|param:type[*required]", params sorted).
// The inspect tool must render exactly these strings from the fixture
// server's schemas, including the no-params single-line case.
func TestMCPInspectRun_MatchesGoldenLiteral_Differential(t *testing.T) {
	inspect := findTool(t, mcptool.Tools(fixturePool(t)), "mcp_inspect")

	cases := []struct {
		tool string
		want string
	}{
		{"echo", "echo — Echo a message\necho|message:s*"},
		{"add", "add — Add two numbers\nadd|a:n*|b:n*"},
		{"ping", "ping — Check liveness"},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			res, err := inspect.Run(toolCtx(t), []byte(`{"server":"fakesrv","tool":"`+tc.tool+`"}`))
			require.NoError(t, err)
			assert.Equal(t, tc.want, res.Content)
			assert.Equal(t, "fakesrv/"+tc.tool, res.Detail)
		})
	}
}

// oracle: hand-written golden literal for the wire round-trip — the fixture
// server's canned tools/call result must surface verbatim as the tool body
// after the JSON-RPC content extraction and the 32k format cap.
func TestMCPCallRun_MatchesGoldenLiteral_Differential(t *testing.T) {
	call := findTool(t, mcptool.Tools(fixturePool(t)), "mcp_call")

	res, err := call.Run(toolCtx(t), []byte(`{"server":"fakesrv","tool":"echo","args":{"message":"hi"}}`))
	require.NoError(t, err)
	assert.Equal(t, "pong", res.Content)
	assert.Equal(t, "fakesrv/echo", res.Detail)
	assert.Equal(t, "pong", res.Output)

	res, err = call.Run(toolCtx(t), []byte(`{"server":"fakesrv","tool":"add","args":{"a":1,"b":2}}`))
	require.NoError(t, err)
	assert.Equal(t, "generic reply", res.Content)
}
