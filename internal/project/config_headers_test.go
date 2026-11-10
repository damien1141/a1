package project

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestModelHeadersParsing pins the models[].headers surface: extra HTTP
// headers ride on every request to that endpoint, names and values are
// trimmed, and blank names are dropped at parse so runtime consumers never
// see them.
func TestModelHeadersParsing(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: main
    api_key: k-main
    base_url: https://api.nano-gpt.com/api/v1
    headers:
      X-Provider: munito
      X-Tenant: "  acme corp  "
      "  X-Spaced  ": kept
      "": dropped
    default: true
  - name: plain
    api_key: k-plain
`), 0o644))
	cfg, err := loadConfig(p.Global())
	require.NoError(t, err)

	m, ok := cfg.FindModel("main")
	require.True(t, ok)
	assert.Equal(t, map[string]string{
		"X-Provider": "munito",
		"X-Tenant":   "acme corp",
		"X-Spaced":   "kept",
	}, m.Headers, "headers parse with trimmed names and values; blank names drop")

	plain, ok := cfg.FindModel("plain")
	require.True(t, ok)
	assert.Nil(t, plain.Headers, "a model without headers stays in the nil world")
}
