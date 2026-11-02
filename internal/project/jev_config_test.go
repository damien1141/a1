package project

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm/jev"
)

// TestJevSectionParsed pins the jev: block's parse (provider normalized,
// fields trimmed) and the push to the decision-model package: every
// loadConfig call re-configures jev, which is what makes the web GUI's
// save → reload → next-turn flow apply without a restart.
func TestJevSectionParsed(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
jev:
  provider: Ollama
  model: qwen3:4b
  ollama_base_url: http://127.0.0.1:11500
  timeout_sec: 12
`), 0o644))
	t.Cleanup(func() { jev.Configure(jev.Settings{}) })

	cfg, err := loadConfig(p.Global())
	require.NoError(t, err)
	want := jev.Settings{
		Provider:      "ollama",
		Model:         "qwen3:4b",
		OllamaBaseURL: "http://127.0.0.1:11500",
		TimeoutSec:    12,
	}
	assert.Equal(t, want, cfg.Jev)
	assert.Equal(t, want, jev.CurrentSettings(), "loadConfig pushes the section to jev.Configure")
}

// TestJevSectionApiProvider: the api provider's fields parse and push.
func TestJevSectionApiProvider(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
jev:
  provider: api
  api_key: sk-jev
  base_url: https://api.typesafe.example
  model: jev-latest
`), 0o644))
	t.Cleanup(func() { jev.Configure(jev.Settings{}) })

	cfg, err := loadConfig(p.Global())
	require.NoError(t, err)
	want := jev.Settings{
		Provider: "api",
		APIKey:   "sk-jev",
		BaseURL:  "https://api.typesafe.example",
		Model:    "jev-latest",
	}
	assert.Equal(t, want, cfg.Jev)
	assert.Equal(t, want, jev.CurrentSettings())
}

// TestJevSectionAbsentResolvesZero: no jev: block means zero settings —
// the TYPESAFE_* env fallback in the resolver keeps env-only setups live,
// and nothing here can turn JEV on by accident.
func TestJevSectionAbsentResolvesZero(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
`), 0o644))
	t.Cleanup(func() { jev.Configure(jev.Settings{}) })

	cfg, err := loadConfig(p.Global())
	require.NoError(t, err)
	assert.Equal(t, jev.Settings{}, cfg.Jev)
	assert.Equal(t, jev.Settings{}, jev.CurrentSettings())
}
