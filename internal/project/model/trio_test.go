package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// ---------- negative ----------

// Unknown, legacy, and hostile names must fall through to the documented
// generic default: ok=false with a fully zero Preset — no base URL, no
// context window, no hooks, no thinking config — so the config loader's own
// OpenAI defaults layer on top cleanly and nothing half-resolved leaks.
func TestLookupHostileNames_Negative(t *testing.T) {
	hostile := []string{
		"",                              // empty
		"   ",                           // blank
		"\tgpt-5.5\n",                   // whitespace-wrapped
		"GPT-5.5",                       // case-flipped
		"gpt-5.5 ",                      // trailing space
		"gpt-5.5\x00evil",               // NUL injection
		"deepseek-chat",                 // legacy name
		"glm-4.5-air",                   // retired name
		"claude-opus-4",                 // foreign provider
		"ɡpt-5.5",                       // homoglyph
		"ｇｐｔ-５.５",                       // fullwidth
		"gpt-5.5/../../gpt-5.5",         // path-traversal shape
		"https://api.openai.com/v1",     // a URL is not a name
		strings.Repeat("gpt-5.5-", 100), // absurd length
	}
	for _, name := range hostile {
		p, ok := Lookup(name)
		require.False(t, ok, "%q must not be a preset", name)
		assert.Equal(t, Preset{}, p, "%q must resolve to the zero preset, got %+v", name, p)

		// The whole resolution surface degrades the same way: no preset
		// thinking, no hooks, and the Default sentinel collapses to off.
		assert.Equal(t, llm.ThinkConfig{}, PresetThink(name), "PresetThink(%q)", name)
		h := HooksFor(name)
		assert.Nil(t, h.OpenAI, "HooksFor(%q).OpenAI", name)
		assert.Nil(t, h.OpenAIResponses, "HooksFor(%q).OpenAIResponses", name)
		assert.Nil(t, h.Anthropic, "HooksFor(%q).Anthropic", name)
		assert.Nil(t, h.Gemini, "HooksFor(%q).Gemini", name)
		assert.Equal(t, llm.ThinkConfig{}, ResolveThink(name, llm.ThinkConfig{Mode: llm.Default, Enabled: true}),
			"the Default sentinel on %q must collapse to thinking-off", name)
	}
}

// Near-miss mutations of real preset names must not resolve: the catalog is
// keyed by the exact documented name, and a fuzzy hit would silently wire
// the wrong endpoint defaults.
func TestLookupNearMissMutations_Negative(t *testing.T) {
	for _, name := range []string{"gpt-5.5", "deepseek-flash", "gemini-3-pro", "glm-5.3", "kimi-k2.7-code", "kilo"} {
		for _, m := range []string{
			strings.ToUpper(name),
			"x" + name,
			name + "x",
			name[:len(name)-1], // truncated
		} {
			_, ok := Lookup(m)
			assert.False(t, ok, "mutation %q of %q must not resolve", m, name)
		}
	}
}

// ResolveThink's contract: only the Default sentinel consults the catalog;
// every other mode — including hostile, blank, or future ones — passes
// through verbatim, and Default on a thinking-less model stays off.
func TestResolveThinkBoundary_Negative(t *testing.T) {
	// Non-Default modes are returned untouched, however malformed.
	for _, cfg := range []llm.ThinkConfig{
		{Mode: "", Enabled: false},
		{Mode: "ULTRA", Enabled: true},
		{Mode: "\x00off", Enabled: true},
		{Mode: llm.Off, Enabled: false},
		{Mode: llm.Max, Enabled: false},
	} {
		assert.Equal(t, cfg, ResolveThink("gpt-6-astra", cfg), "mode %q must pass through unchanged", cfg.Mode)
		assert.Equal(t, cfg, ResolveThink("no-such-model", cfg), "mode %q must pass through unchanged", cfg.Mode)
	}

	// Default on models that ship without thinking stays thinking-off —
	// the sentinel never turns thinking on by itself.
	for _, name := range []string{"gpt-5-chat-latest", "kimi-k2.7-code", "kilo", "totally-unknown"} {
		got := ResolveThink(name, llm.ThinkConfig{Mode: llm.Default, Enabled: true})
		assert.Equal(t, llm.ThinkConfig{}, got, "Default on %s must collapse to the zero config", name)
	}
}

// ---------- differential ----------

// refEntry is one hand-written catalog row: the connection defaults each
// documented model name must resolve to, transcribed here from the provider
// docs model.go cites — independent of the presets slice the implementation
// reads. hookKind pins which hook slot the row wires: "", "openai", or
// "gemini".
type refEntry struct {
	baseURL  string
	window   int
	image    bool
	api      llm.RouterType
	think    llm.ThinkConfig
	hookKind string
}

var refCatalog = map[string]refEntry{
	"gpt-6-astra":       {baseURL: "https://api.openai.com/v1", window: 272_000, image: true, api: llm.OpenAIResponses, think: llm.ThinkConfig{Enabled: true, Mode: llm.Max}},
	"gpt-5.6-sol":       {baseURL: "https://api.openai.com/v1", window: 272_000, image: true, api: llm.OpenAIResponses, think: llm.ThinkConfig{Enabled: true, Mode: llm.High}},
	"gpt-5.6-terra":     {baseURL: "https://api.openai.com/v1", window: 272_000, image: true, api: llm.OpenAIResponses, think: llm.ThinkConfig{Enabled: true, Mode: llm.High}},
	"gpt-5.6-luna":      {baseURL: "https://api.openai.com/v1", window: 272_000, image: true, api: llm.OpenAIResponses, think: llm.ThinkConfig{Enabled: true, Mode: llm.High}},
	"gpt-5-chat-latest": {baseURL: "https://api.openai.com/v1", window: 128_000, image: true, api: llm.OpenAIResponses},
	"gpt-5.5":           {baseURL: "https://api.openai.com/v1", window: 272_000, image: true, api: llm.OpenAIResponses, think: llm.ThinkConfig{Enabled: true, Mode: llm.High}},
	"gpt-5.5-pro":       {baseURL: "https://api.openai.com/v1", window: 1_050_000, image: true, api: llm.OpenAIResponses, think: llm.ThinkConfig{Enabled: true, Mode: llm.High}},
	"deepseek-flash":    {baseURL: "https://api.deepseek.com", window: 1_000_000, image: true, api: llm.OpenAI, think: llm.ThinkConfig{Enabled: true, Mode: llm.High}, hookKind: "openai"},
	"deepseek-v4-pro":   {baseURL: "https://api.deepseek.com", window: 1_000_000, image: false, api: llm.OpenAI, think: llm.ThinkConfig{Enabled: true, Mode: llm.High}, hookKind: "openai"},
	"gemini-2.5-pro":    {baseURL: "https://generativelanguage.googleapis.com/v1beta", window: 1_000_000, image: true, api: llm.Gemini, think: llm.ThinkConfig{Enabled: true, Mode: llm.High}, hookKind: "gemini"},
	"gemini-2.5-flash":  {baseURL: "https://generativelanguage.googleapis.com/v1beta", window: 1_000_000, image: true, api: llm.Gemini, think: llm.ThinkConfig{Enabled: true, Mode: llm.High}, hookKind: "gemini"},
	"gemini-3-pro":      {baseURL: "https://generativelanguage.googleapis.com/v1beta", window: 1_000_000, image: true, api: llm.Gemini, think: llm.ThinkConfig{Enabled: true, Mode: llm.High}, hookKind: "gemini"},
	"gemini-3-flash":    {baseURL: "https://generativelanguage.googleapis.com/v1beta", window: 1_000_000, image: true, api: llm.Gemini, think: llm.ThinkConfig{Enabled: true, Mode: llm.High}, hookKind: "gemini"},
	"kimi-k3":           {baseURL: "https://api.moonshot.cn/v1", window: 1_000_000, image: true, api: llm.OpenAI, think: llm.ThinkConfig{Enabled: true, Mode: llm.Max}},
	"kimi-k2.7-code":    {baseURL: "https://api.moonshot.cn/v1", window: 10_000_000, image: true, api: llm.OpenAI},
	"glm-5.3-flash":     {baseURL: "https://api.z.ai/api/coding/paas/v4", window: 1_048_576, image: true, api: llm.OpenAI, think: llm.ThinkConfig{Enabled: true, Mode: llm.Max}, hookKind: "openai"},
	"glm-5.3":           {baseURL: "https://api.z.ai/api/coding/paas/v4", window: 1_048_576, image: false, api: llm.OpenAI, think: llm.ThinkConfig{Enabled: true, Mode: llm.Max}, hookKind: "openai"},
	"kilo":              {baseURL: "https://api.kilo.ai/api/gateway", window: 1_000_000, image: true, api: llm.OpenAI},
}

// oracle: hand-written fixture catalog — Lookup, HooksFor, PresetThink, and
// ResolveThink are checked against a map-based reference transcribed in this
// test from the provider docs model.go cites, over every catalog name plus a
// mutation closure of names that must not resolve.
func TestResolutionAgainstReferenceCatalog_Differential(t *testing.T) {
	require.NotEmpty(t, refCatalog)
	for name, ref := range refCatalog {
		t.Run(name, func(t *testing.T) {
			p, ok := Lookup(name)
			require.True(t, ok, "%s is in the reference catalog and must resolve", name)

			assert.Equal(t, name, p.Config.Name)
			assert.Equal(t, ref.baseURL, p.Config.BaseURL)
			assert.Equal(t, ref.window, p.Config.ContextWindow)
			assert.Equal(t, ref.image, p.Config.ImageEnabled)
			assert.Equal(t, ref.api, p.Config.API)
			assert.Equal(t, ref.think, p.Config.Think)

			// Presets carry no credentials or skill paths — those stay
			// caller-owned.
			assert.Empty(t, p.Config.APIKey)
			assert.Empty(t, p.Config.APIKeys)
			assert.Empty(t, p.Config.SkillPath)

			// Hook slots agree with the reference row, slot by slot.
			h := HooksFor(name)
			switch ref.hookKind {
			case "":
				assert.Nil(t, h.OpenAI, "%s wires no OpenAI hook", name)
				assert.Nil(t, h.OpenAIResponses, "%s wires no OpenAIResponses hook", name)
				assert.Nil(t, h.Anthropic, "%s wires no Anthropic hook", name)
				assert.Nil(t, h.Gemini, "%s wires no Gemini hook", name)
			case "openai":
				assert.NotNil(t, h.OpenAI, "%s wires an OpenAI hook", name)
				assert.Nil(t, h.Gemini, "%s wires no Gemini hook", name)
			case "gemini":
				assert.NotNil(t, h.Gemini, "%s wires a Gemini hook", name)
				assert.Nil(t, h.OpenAI, "%s wires no OpenAI hook", name)
			}

			// The thinking surface resolves to the same reference row, and
			// explicit modes never consult the catalog.
			assert.Equal(t, ref.think, PresetThink(name))
			assert.Equal(t, ref.think, ResolveThink(name, llm.ThinkConfig{Mode: llm.Default}))
			explicit := llm.ThinkConfig{Mode: llm.Low, Enabled: true}
			assert.Equal(t, explicit, ResolveThink(name, explicit))
		})
	}

	// Mutation closure: names outside the reference catalog never resolve.
	for name := range refCatalog {
		for _, m := range []string{strings.ToUpper(name), "x" + name, name + "x"} {
			_, ok := Lookup(m)
			assert.False(t, ok, "%q (mutation of %s) must not resolve", m, name)
		}
	}
}

// oracle: encoding/json round-trip against hand-written golden literals —
// the preset configs marshal to (and unmarshal back from) JSON transcribed
// by hand here, pinning the serialized field names and values through a code
// path the package itself never uses.
func TestPresetConfigJSONRoundTrip_Differential(t *testing.T) {
	golden := map[string]string{
		"gpt-6-astra":       `{"Name":"gpt-6-astra","APIKey":"","BaseURL":"https://api.openai.com/v1","APIKeys":null,"SkillPath":"","ContextWindow":272000,"ImageEnabled":true,"PersonalRead":false,"PersonalWrite":false,"API":"OpenAIResponses","Think":{"Mode":"max","Enabled":true}}`,
		"gpt-5-chat-latest": `{"Name":"gpt-5-chat-latest","APIKey":"","BaseURL":"https://api.openai.com/v1","APIKeys":null,"SkillPath":"","ContextWindow":128000,"ImageEnabled":true,"PersonalRead":false,"PersonalWrite":false,"API":"OpenAIResponses","Think":{"Mode":"","Enabled":false}}`,
		"kimi-k2.7-code":    `{"Name":"kimi-k2.7-code","APIKey":"","BaseURL":"https://api.moonshot.cn/v1","APIKeys":null,"SkillPath":"","ContextWindow":10000000,"ImageEnabled":true,"PersonalRead":false,"PersonalWrite":false,"API":"OpenAI","Think":{"Mode":"","Enabled":false}}`,
		"kilo":              `{"Name":"kilo","APIKey":"","BaseURL":"https://api.kilo.ai/api/gateway","APIKeys":null,"SkillPath":"","ContextWindow":1000000,"ImageEnabled":true,"PersonalRead":false,"PersonalWrite":false,"API":"OpenAI","Think":{"Mode":"","Enabled":false}}`,
	}
	for name, want := range golden {
		t.Run(name, func(t *testing.T) {
			p, ok := Lookup(name)
			require.True(t, ok)

			got, err := json.Marshal(p.Config)
			require.NoError(t, err)
			assert.Equal(t, want, string(got), "byte-exact golden: struct field order is part of the pin")

			var back llm.ModelConfig
			require.NoError(t, json.Unmarshal([]byte(want), &back))
			assert.Equal(t, p.Config, back, "the golden literal must survive the round-trip")

			// A hook-less preset marshals its Hooks as all-nil — no hidden
			// interceptor state rides along.
			hb, err := json.Marshal(p.Hooks)
			require.NoError(t, err)
			assert.Equal(t, `{"OpenAI":null,"OpenAIResponses":null,"Anthropic":null,"Gemini":null}`, string(hb))
		})
	}
}
