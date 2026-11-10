package project

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The stoat: section's trio: a full section parses field-by-field, a
// missing section means "not configured" (the command's startup gate),
// and a half-written section (token but no channel, or channel but no
// token) is exactly as unconfigured as no section at all.

func TestLoadConfigStoatSectionParses(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
stoat:
  token: "bot-token-1"
  channel: "chan-01ABC"
`), 0o644))

	require.NoError(t, p.LoadConfig())
	sc := p.Config().Stoat
	assert.Equal(t, "bot-token-1", sc.Token)
	assert.Equal(t, "chan-01ABC", sc.Channel)
	assert.Empty(t, sc.API, "the API endpoint defaults; empty means default")
	assert.Empty(t, sc.WS)
	assert.True(t, sc.Configured())
}

func TestLoadConfigStoatOverridesParse(t *testing.T) {
	// Self-hosters (and the test suite) point the bridge elsewhere.
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
stoat:
  token: "t"
  channel: "c"
  api: "https://stoat.example.org/api"
  ws: "wss://stoat.example.org/events"
`), 0o644))

	require.NoError(t, p.LoadConfig())
	sc := p.Config().Stoat
	assert.Equal(t, "https://stoat.example.org/api", sc.API)
	assert.Equal(t, "wss://stoat.example.org/events", sc.WS)
	assert.True(t, sc.Configured())
}

func TestLoadConfigStoatAbsentMeansUnconfigured(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
`), 0o644))

	require.NoError(t, p.LoadConfig())
	assert.False(t, p.Config().Stoat.Configured(), "no section = the bridge does not boot")
	assert.Empty(t, p.Config().Stoat.Token)
}

func TestStoatConfiguredRejectsHalfSections(t *testing.T) {
	// The negative differential: each missing piece disqualifies the whole
	// section, so `a1 stoat` fails at startup with guidance instead of
	// discovering a missing channel mid-conversation.
	cases := []struct {
		name string
		cfg  StoatConfig
	}{
		{"empty", StoatConfig{}},
		{"token only", StoatConfig{Token: "tok"}},
		{"channel only", StoatConfig{Channel: "chan"}},
		{"endpoints only", StoatConfig{API: "https://x", WS: "wss://x"}},
	}
	for _, tc := range cases {
		assert.False(t, tc.cfg.Configured(), tc.name)
	}
}

func TestLoadConfigStoatTrimsWhitespace(t *testing.T) {
	// Pasted channel ids and tokens carry trailing newlines; the parse
	// trims them instead of sending them on the wire.
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
stoat:
  token: "tok
"
  channel: " chan "
`), 0o644))

	require.NoError(t, p.LoadConfig())
	sc := p.Config().Stoat
	assert.Equal(t, "tok", sc.Token)
	assert.Equal(t, "chan", sc.Channel)
	assert.True(t, sc.Configured())
}
