package jev

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetSettings isolates each test from the global configured settings.
func resetSettings(t *testing.T) {
	t.Helper()
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Setenv("OLLAMA_HOST", "")
	t.Cleanup(func() { Configure(Settings{}) })
	Configure(Settings{})
}

// TestConfiguredUnconfiguredIsOff: no jev: section and no environment
// means no decision model — the pre-config behavior for everyone who never
// opted in.
func TestConfiguredUnconfiguredIsOff(t *testing.T) {
	resetSettings(t)
	assert.False(t, Configured())
	_, err := NewConfiguredAsker().SystemOneWithOptions(t.Context(), "s", Questions{
		"q": Noul{},
	}, Options{})
	assert.ErrorIs(t, err, ErrNotConfigured)
}

// TestConfiguredEnvFallbackPreserved: an empty Settings section keeps the
// TYPESAFE_API_KEY environment fallback — env-only setups (and the tests
// that pin them) behave exactly as before the jev: section existed.
func TestConfiguredEnvFallbackPreserved(t *testing.T) {
	resetSettings(t)
	t.Setenv("TYPESAFE_API_KEY", "sk-env")
	assert.True(t, Configured())
}

// TestConfiguredAPIProvider: provider api needs a key from settings or
// the environment; a configured key wins without the env var.
func TestConfiguredAPIProvider(t *testing.T) {
	resetSettings(t)
	Configure(Settings{Provider: ProviderAPI})
	assert.False(t, Configured(), "api provider without any key is off")

	Configure(Settings{Provider: ProviderAPI, APIKey: "sk-config"})
	assert.True(t, Configured())

	Configure(Settings{Provider: ProviderAPI})
	t.Setenv("TYPESAFE_API_KEY", "sk-env")
	assert.True(t, Configured(), "api provider falls back to the env key")
}

// TestConfiguredOllamaProvider: provider ollama requires an explicit
// model — pulling a local model is never a default.
func TestConfiguredOllamaProvider(t *testing.T) {
	resetSettings(t)
	Configure(Settings{Provider: ProviderOllama})
	assert.False(t, Configured(), "ollama provider without a model is off")

	Configure(Settings{Provider: ProviderOllama, Model: "qwen3:4b"})
	assert.True(t, Configured())
}

// TestConfiguredProviderNormalization: "API" and " Ollama " resolve like
// their canonical forms (the config parse lowercases; the resolver is
// defensive for direct Configure callers).
func TestConfiguredProviderNormalization(t *testing.T) {
	resetSettings(t)
	Configure(Settings{Provider: " Ollama ", Model: "qwen3:4b"})
	assert.True(t, Configured())
}

// TestConfiguredUnknownProviderIsOff: a typo'd provider disables JEV
// (with a debuglog note at resolution) instead of guessing a backend.
func TestConfiguredUnknownProviderIsOff(t *testing.T) {
	resetSettings(t)
	Configure(Settings{Provider: "typo", APIKey: "sk-x", Model: "m"})
	assert.False(t, Configured())
}

// TestTimeoutHintByProvider: the hosted API has no opinion (call sites
// keep 3s/4s); a local model suggests 20s.
func TestTimeoutHintByProvider(t *testing.T) {
	resetSettings(t)
	assert.Zero(t, TimeoutHint())

	Configure(Settings{Provider: ProviderAPI, APIKey: "sk-x"})
	assert.Zero(t, TimeoutHint())

	Configure(Settings{Provider: ProviderOllama, Model: "qwen3:4b"})
	assert.Equal(t, 20*time.Second, TimeoutHint())
}

// TestEffectiveTimeout pins the ceiling math: site default, raised to the
// provider hint, overridden either way by timeout_sec.
func TestEffectiveTimeout(t *testing.T) {
	resetSettings(t)
	site := 3 * time.Second

	assert.Equal(t, site, EffectiveTimeout(site), "unconfigured keeps the site default")

	Configure(Settings{Provider: ProviderOllama, Model: "qwen3:4b"})
	assert.Equal(t, 20*time.Second, EffectiveTimeout(site), "ollama raises the ceiling")

	Configure(Settings{Provider: ProviderOllama, Model: "qwen3:4b", TimeoutSec: 45})
	assert.Equal(t, 45*time.Second, EffectiveTimeout(site), "timeout_sec raises past the hint")

	Configure(Settings{Provider: ProviderOllama, Model: "qwen3:4b", TimeoutSec: 2})
	assert.Equal(t, 2*time.Second, EffectiveTimeout(site), "timeout_sec can lower the ceiling on fast hardware")

	Configure(Settings{Provider: ProviderAPI, APIKey: "sk-x", TimeoutSec: 9})
	assert.Equal(t, 9*time.Second, EffectiveTimeout(4*time.Second), "timeout_sec applies to the hosted provider too")
}

// TestConfiguredAskerResolvesPerCall: the lazy asker sees Configure
// changes without reconstruction — the mid-session reload path.
func TestConfiguredAskerResolvesPerCall(t *testing.T) {
	resetSettings(t)
	asker := NewConfiguredAsker()
	questions := Questions{"q": Noul{}}

	_, err := asker.SystemOneWithOptions(t.Context(), "s", questions, Options{})
	assert.ErrorIs(t, err, ErrNotConfigured)

	Configure(Settings{Provider: ProviderAPI, APIKey: "sk-x"})
	_, err = asker.SystemOneWithOptions(t.Context(), "s", questions, Options{})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotConfigured, "configured api provider reaches the (unreachable in tests) endpoint")

	Configure(Settings{})
	_, err = asker.SystemOneWithOptions(t.Context(), "s", questions, Options{})
	assert.ErrorIs(t, err, ErrNotConfigured, "deconfiguration takes effect on the next call")
}

// TestCurrentSettingsSnapshots: Configure stores what was pushed; a
// missing section is indistinguishable from never configured.
func TestCurrentSettingsSnapshots(t *testing.T) {
	resetSettings(t)
	assert.Equal(t, Settings{}, CurrentSettings())

	pushed := Settings{Provider: ProviderOllama, Model: "qwen3:4b", TimeoutSec: 12}
	Configure(pushed)
	assert.Equal(t, pushed, CurrentSettings())
}
