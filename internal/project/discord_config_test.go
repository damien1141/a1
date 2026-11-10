package project

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The discord: section's trio: a full section parses field-by-field, a
// missing section means "not configured" (the command's startup gate),
// and a half-written section (token but no channel, or channel but no
// token) is exactly as unconfigured as no section at all.

func TestLoadConfigDiscordSectionParses(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
discord:
  token: "bot-token-1"
  channel: "1234567890123456789"
`), 0o644))

	require.NoError(t, p.LoadConfig())
	dc := p.Config().Discord
	assert.Equal(t, "bot-token-1", dc.Token)
	assert.Equal(t, "1234567890123456789", dc.Channel)
	assert.Empty(t, dc.API, "the API endpoint defaults; empty means default")
	assert.Empty(t, dc.Gateway)
	assert.True(t, dc.Configured())
}

func TestLoadConfigDiscordOverridesParse(t *testing.T) {
	// Proxies (and the test suite) point the bridge elsewhere.
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
discord:
  token: "t"
  channel: "c"
  api: "https://proxy.example.org/api/v10"
  gateway: "wss://proxy.example.org"
`), 0o644))

	require.NoError(t, p.LoadConfig())
	dc := p.Config().Discord
	assert.Equal(t, "https://proxy.example.org/api/v10", dc.API)
	assert.Equal(t, "wss://proxy.example.org", dc.Gateway)
	assert.True(t, dc.Configured())
}

func TestLoadConfigDiscordAbsentMeansUnconfigured(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
`), 0o644))

	require.NoError(t, p.LoadConfig())
	assert.False(t, p.Config().Discord.Configured(), "no section = the bridge does not boot")
	assert.Empty(t, p.Config().Discord.Token)
}

func TestDiscordConfiguredRejectsHalfSections(t *testing.T) {
	// The negative differential: each missing piece disqualifies the whole
	// section, so `a1 discord` fails at startup with guidance instead of
	// discovering a missing channel mid-conversation.
	cases := []struct {
		name string
		cfg  DiscordConfig
	}{
		{"empty", DiscordConfig{}},
		{"token only", DiscordConfig{Token: "tok"}},
		{"channel only", DiscordConfig{Channel: "chan"}},
		{"endpoints only", DiscordConfig{API: "https://x", Gateway: "wss://x"}},
	}
	for _, tc := range cases {
		assert.False(t, tc.cfg.Configured(), tc.name)
	}
}

func TestLoadConfigDiscordTrimsWhitespace(t *testing.T) {
	// Pasted channel ids and tokens carry trailing newlines; the parse
	// trims them instead of sending them on the wire.
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
discord:
  token: "tok
"
  channel: " 123 "
`), 0o644))

	require.NoError(t, p.LoadConfig())
	dc := p.Config().Discord
	assert.Equal(t, "tok", dc.Token)
	assert.Equal(t, "123", dc.Channel)
	assert.True(t, dc.Configured())
}
