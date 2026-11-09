package project

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestModelAPIKeysParsing pins the config surface of the credential pool:
// api_keys extends a model's single api_key (which stays first and
// required), in file order, and the env var extends the default model's
// pool the same way.
func TestModelAPIKeysParsing(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: main
    api_key: k-primary
    api_keys:
      - k-extra-1
      - k-extra-2
    default: true
  - name: other
    api_key: k-other
`), 0o644))
	cfg, err := loadConfig(p.Global())
	require.NoError(t, err)

	m, ok := cfg.FindModel("main")
	require.True(t, ok)
	assert.Equal(t, "k-primary", m.APIKey, "the single key stays the identity and the pool's first entry")
	assert.Equal(t, []string{"k-extra-1", "k-extra-2"}, m.APIKeys, "api_keys lists the extras beyond api_key (consumers union)")

	o, ok := cfg.FindModel("other")
	require.True(t, ok)
	assert.Empty(t, o.APIKeys, "no api_keys is the single-key world")
	assert.Equal(t, "k-other", o.APIKey)
}

// TestModelAPIKeysEnvExtend pins A1_API_KEYS: comma-separated extras that
// extend (never replace) the default model's pool, with whitespace and
// empty segments tolerated.
func TestModelAPIKeysEnvExtend(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: main
    api_key: k-primary
    default: true
`), 0o644))
	t.Setenv("A1_API_KEYS", " k-env-1 , k-env-2 ,, k-env-1 ")
	cfg, err := loadConfig(p.Global())
	require.NoError(t, err)

	m := cfg.Model()
	require.Equal(t, "main", m.Name)
	assert.Equal(t, []string{"k-env-1", "k-env-2", "k-env-1"}, m.APIKeys,
		"env extends the extras; trimming and dedupe happen in the pool, which unions with api_key")
}
