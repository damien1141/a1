package project

import (
        "os"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// The matrix: section's trio: a full section parses field-by-field (both
// auth spellings), a missing section means "not configured" (the command's
// startup gate), and a half-written section (homeserver but no auth, or auth
// but no room) is exactly as unconfigured as no section at all.

func TestLoadConfigMatrixSectionParses(t *testing.T) {
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
matrix:
  homeserver: "https://matrix.example.org"
  user: "a1bot"
  password: "hunter2"
  room: "#a1-bridge:matrix.example.org"
`), 0o644))

        require.NoError(t, p.LoadConfig())
        mc := p.Config().Matrix
        assert.Equal(t, "https://matrix.example.org", mc.Homeserver)
        assert.Equal(t, "a1bot", mc.User)
        assert.Equal(t, "hunter2", mc.Password, "password keeps its raw value — no trim")
        assert.Equal(t, "#a1-bridge:matrix.example.org", mc.Room)
        assert.True(t, mc.Configured(), "user+password is a complete auth path")
}

func TestLoadConfigMatrixTokenSpelling(t *testing.T) {
        // The token spelling: user optional (whoami derives it from the token).
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
matrix:
  homeserver: matrix.example.org
  access_token: syt_bridge
  room: "!abc123:matrix.example.org"
`), 0o644))

        require.NoError(t, p.LoadConfig())
        mc := p.Config().Matrix
        assert.Equal(t, "syt_bridge", mc.AccessToken)
        assert.Empty(t, mc.User)
        assert.True(t, mc.Configured(), "token alone is a complete auth path")
}

func TestLoadConfigMatrixAbsentMeansUnconfigured(t *testing.T) {
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
`), 0o644))

        require.NoError(t, p.LoadConfig())
        assert.False(t, p.Config().Matrix.Configured(), "no section = the bridge does not boot")
        assert.Empty(t, p.Config().Matrix.Homeserver)
}

func TestMatrixConfiguredRejectsHalfSections(t *testing.T) {
        // The negative differential: each missing piece disqualifies the whole
        // section, so `a1 matrix` fails at startup with guidance instead of
        // discovering a missing room mid-conversation.
        cases := []struct {
                name string
                cfg  MatrixConfig
        }{
                {"empty", MatrixConfig{}},
                {"homeserver only", MatrixConfig{Homeserver: "https://hs"}},
                {"room only", MatrixConfig{Room: "#r:hs"}},
                {"password without user", MatrixConfig{Homeserver: "https://hs", Room: "#r:hs", Password: "p"}},
                {"user without password", MatrixConfig{Homeserver: "https://hs", Room: "#r:hs", User: "bot"}},
        }
        for _, tc := range cases {
                assert.False(t, tc.cfg.Configured(), tc.name)
        }
}
