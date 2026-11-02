package project

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigToolBudget(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
tools:
  max_tool_calls: 25
`), 0o644))

	require.NoError(t, p.LoadConfig())
	assert.Equal(t, 25, p.Config().Tools.MaxToolCalls)
}

func TestLoadConfigToolBudgetAbsent(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
`), 0o644))

	require.NoError(t, p.LoadConfig())
	assert.Equal(t, 0, p.Config().Tools.MaxToolCalls, "absent key means unlimited")
}

func TestSetMaxToolCallsPersistsAndPreserves(t *testing.T) {
	p := discoverInTempHome(t)
	original := `# my a1 config
models:
  - name: deepseek-chat
    api_key: sk-test
weather:
  enabled: false
`
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(original), 0o644))

	require.NoError(t, SetMaxToolCalls(p.Global(), 50))

	data, err := os.ReadFile(p.Global().ConfigFile())
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, "max_tool_calls: 50")
	// Everything else survives the surgical edit.
	assert.Contains(t, text, "# my a1 config", "header comment preserved")
	assert.Contains(t, text, "sk-test")
	assert.Contains(t, text, "weather:", "other keys preserved")

	// The saved file parses back with the budget applied.
	require.NoError(t, p.LoadConfig())
	assert.Equal(t, 50, p.Config().Tools.MaxToolCalls)

	// Setting it again updates in place.
	require.NoError(t, SetMaxToolCalls(p.Global(), 10))
	require.NoError(t, p.LoadConfig())
	assert.Equal(t, 10, p.Config().Tools.MaxToolCalls)
}

func TestSetMaxToolCallsZeroRemovesKey(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
tools:
  max_tool_calls: 25
`), 0o644))

	require.NoError(t, SetMaxToolCalls(p.Global(), 0))

	data, err := os.ReadFile(p.Global().ConfigFile())
	require.NoError(t, err)
	assert.NotContains(t, string(data), "max_tool_calls")
	assert.NotContains(t, string(data), "tools:", "empty tools block is pruned")
	require.NoError(t, p.LoadConfig())
	assert.Equal(t, 0, p.Config().Tools.MaxToolCalls)
}

func TestSetMaxToolCallsKeepsOtherToolsKeys(t *testing.T) {
	// Hand-write a tools block with a second key to prove only the budget is
	// removed when the budget clears.
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
tools:
  max_tool_calls: 25
  future_key: keep-me
`), 0o644))

	require.NoError(t, SetMaxToolCalls(p.Global(), 0))

	data, err := os.ReadFile(p.Global().ConfigFile())
	require.NoError(t, err)
	text := string(data)
	assert.NotContains(t, text, "max_tool_calls")
	assert.Contains(t, text, "future_key: keep-me", "unrelated tools keys survive")
	assert.True(t, strings.Contains(text, "tools:"), "non-empty tools block stays")
}
