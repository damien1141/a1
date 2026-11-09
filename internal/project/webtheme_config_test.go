package project

import (
        "os"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// The theme key's trio (web.dark_mode): the parse distinguishes an absent
// key (nil — follow the OS) from both explicit choices, the setter writes
// and removes it surgically, and a removal never takes keys the setter
// does not own (SetMaxToolCalls' pruning discipline, applied to web:).

func TestLoadConfigWebDarkMode(t *testing.T) {
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
web:
  dark_mode: true
`), 0o644))

        require.NoError(t, p.LoadConfig())
        require.NotNil(t, p.Config().Web.DarkMode)
        assert.True(t, *p.Config().Web.DarkMode, "the explicit dark choice parses")
}

func TestLoadConfigWebDarkModeFalseIsExplicit(t *testing.T) {
        // false is a CHOICE, not an absence — the picker's light state.
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
web:
  dark_mode: false
`), 0o644))

        require.NoError(t, p.LoadConfig())
        require.NotNil(t, p.Config().Web.DarkMode, "false is present, not absent")
        assert.False(t, *p.Config().Web.DarkMode)
}

func TestLoadConfigWebDarkModeAbsent(t *testing.T) {
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
web:
  auto_name_sessions: false
`), 0o644))

        require.NoError(t, p.LoadConfig())
        assert.Nil(t, p.Config().Web.DarkMode, "no key means follow the OS")
        assert.False(t, p.Config().Web.AutoNameSessionsResolved(), "the sibling key still parses")
}

func TestSetWebDarkModePersistsAndToggles(t *testing.T) {
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`# my a1 config
models:
  - name: deepseek-chat
    api_key: sk-test
`), 0o644))

        dark := true
        require.NoError(t, SetWebDarkMode(p.Global(), &dark))

        data, err := os.ReadFile(p.Global().ConfigFile())
        require.NoError(t, err)
        text := string(data)
        assert.Contains(t, text, "dark_mode: true")
        assert.Contains(t, text, "# my a1 config", "header comment preserved")
        assert.Contains(t, text, "sk-test")

        require.NoError(t, p.LoadConfig())
        require.NotNil(t, p.Config().Web.DarkMode)
        assert.True(t, *p.Config().Web.DarkMode)

        // The toggle flips in place — the button's on/off cycle.
        light := false
        require.NoError(t, SetWebDarkMode(p.Global(), &light))
        require.NoError(t, p.LoadConfig())
        require.NotNil(t, p.Config().Web.DarkMode)
        assert.False(t, *p.Config().Web.DarkMode)
}

func TestSetWebDarkModeKeepsSiblingKeys(t *testing.T) {
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
web:
  auto_name_sessions: false
  dark_mode: true
`), 0o644))

        // "System" removes only the theme key; auto_name_sessions owns its
        // own place in the block and must survive.
        require.NoError(t, SetWebDarkMode(p.Global(), nil))

        data, err := os.ReadFile(p.Global().ConfigFile())
        require.NoError(t, err)
        text := string(data)
        assert.NotContains(t, text, "dark_mode")
        assert.Contains(t, text, "auto_name_sessions: false", "unrelated web keys survive")
        assert.Contains(t, text, "web:", "non-empty web block stays")
}

func TestSetWebDarkModeNilPrunesEmptyBlock(t *testing.T) {
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
web:
  dark_mode: true
`), 0o644))

        require.NoError(t, SetWebDarkMode(p.Global(), nil))

        data, err := os.ReadFile(p.Global().ConfigFile())
        require.NoError(t, err)
        text := string(data)
        assert.NotContains(t, text, "dark_mode")
        assert.NotContains(t, text, "web:", "empty web block is pruned")
}
