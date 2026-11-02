package project

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermissionsSandboxParsing(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
permissions:
  sandbox: "on"
  sandbox_network: allow
`), 0o644))
	cfg, err := loadConfig(p.Global())
	require.NoError(t, err)
	assert.Equal(t, "on", cfg.Permissions.Sandbox)
	assert.Equal(t, "allow", cfg.Permissions.SandboxNetwork)
}

func TestPermissionsSandboxDefaultsToAuto(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
`), 0o644))
	cfg, err := loadConfig(p.Global())
	require.NoError(t, err)
	assert.Equal(t, "", cfg.Permissions.Sandbox, "absent means auto (on when the host supports it)")
	assert.Equal(t, "", cfg.Permissions.SandboxNetwork, "absent network means deny — containment first")
}
