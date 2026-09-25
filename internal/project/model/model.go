// Package model holds built-in model presets — connection defaults for the
// models providers advertise, so a config.yaml entry can omit base_url,
// context_window, and image_enabled and still get the right values. Presets
// are keyed by the exact model name the API documents; unknown or legacy
// names fall through to the generic OpenAI defaults the config loader applies.
package model

import (
	"github.com/damien1141/a1/internal/llm"
	llmclient "github.com/damien1141/a1/internal/llm/client"
)

// Preset is a built-in model catalog entry: connection defaults plus optional
// request Hooks for vendor-specific wire shape.
type Preset struct {
	Config llm.ModelConfig
	Hooks  llmclient.Hooks
}

// Lookup returns the built-in preset for a model name. ok is false for names
// without a preset, so callers fall back to the generic OpenAI endpoint.
// The returned config carries no api_key or skill path; the caller layers
// those on top and may override any field.
func Lookup(name string) (Preset, bool) {
	for _, p := range presets {
		if p.Config.Name == name {
			return p, true
		}
	}
	return Preset{}, false
}

// HooksFor returns request hooks from the built-in catalog, or a zero Hooks
// when the name has no preset (or the preset has no interceptors).
func HooksFor(name string) llmclient.Hooks {
	if p, ok := Lookup(name); ok {
		return p.Hooks
	}
	return llmclient.Hooks{}
}

var (
	thinkHigh = llm.ThinkConfig{Enabled: true, Mode: llm.High}
	thinkMax  = llm.ThinkConfig{Enabled: true, Mode: llm.Max}
)

// presets is the built-in catalog, keyed by model name. Values mirror each
// provider's public API docs; re-check the linked page when refreshing a
// model — context length, base URL, and capabilities change between versions.
var presets = []Preset{
	// Source: https://platform.openai.com/docs/models
	{
		Config: llm.ModelConfig{
			Name:          "gpt-6-astra",
			BaseURL:       "https://api.openai.com/v1",
			ContextWindow: 272_000,
			ImageEnabled:  true,
			API:           llm.OpenAIResponses,
			Think:         thinkMax,
		},
	},
	{
		Config: llm.ModelConfig{
			Name:          "gpt-5.6-sol",
			BaseURL:       "https://api.openai.com/v1",
			ContextWindow: 272_000,
			ImageEnabled:  true,
			API:           llm.OpenAIResponses,
			Think:         thinkHigh,
		},
	},
	{
		Config: llm.ModelConfig{
			Name:          "gpt-5.6-terra",
			BaseURL:       "https://api.openai.com/v1",
			ContextWindow: 272_000,
			ImageEnabled:  true,
			API:           llm.OpenAIResponses,
			Think:         thinkHigh,
		},
	},
	{
		Config: llm.ModelConfig{
			Name:          "gpt-5.6-luna",
			BaseURL:       "https://api.openai.com/v1",
			ContextWindow: 272_000,
			ImageEnabled:  true,
			API:           llm.OpenAIResponses,
			Think:         thinkHigh,
		},
	},
	{
		Config: llm.ModelConfig{
			Name:          "gpt-5-chat-latest",
			BaseURL:       "https://api.openai.com/v1",
			ContextWindow: 128_000,
			ImageEnabled:  true,
			API:           llm.OpenAIResponses,
		},
	},
	// Source: https://platform.openai.com/docs/models
	{
		Config: llm.ModelConfig{
			Name:          "gpt-5.5",
			BaseURL:       "https://api.openai.com/v1",
			ContextWindow: 272_000,
			ImageEnabled:  true,
			API:           llm.OpenAIResponses,
			Think:         thinkHigh,
		},
	},
	{
		Config: llm.ModelConfig{
			Name:          "gpt-5.5-pro",
			BaseURL:       "https://api.openai.com/v1",
			ContextWindow: 1_050_000,
			ImageEnabled:  true,
			API:           llm.OpenAIResponses,
			Think:         thinkHigh,
		},
	},
	// Source: https://api-docs.deepseek.com/zh-cn/quick_start/pricing
	{
		Config: llm.ModelConfig{
			Name:          "deepseek-flash",
			BaseURL:       "https://api.deepseek.com",
			ContextWindow: 1_000_000,
			ImageEnabled:  true,
			API:           llm.OpenAI,
			Think:         thinkHigh,
		},
		Hooks: deepseekHooks(),
	},
	{
		Config: llm.ModelConfig{
			Name:          "deepseek-v4-pro",
			BaseURL:       "https://api.deepseek.com",
			ContextWindow: 1_000_000,
			API:           llm.OpenAI,
			Think:         thinkHigh,
		},
		Hooks: deepseekHooks(),
	},
	// Gemini 2.5 — thinkingBudget (token cap). Source: https://ai.google.dev/gemini-api/docs/models
	{
		Config: llm.ModelConfig{
			Name:          "gemini-2.5-pro",
			BaseURL:       "https://generativelanguage.googleapis.com/v1beta",
			ContextWindow: 1_000_000,
			ImageEnabled:  true,
			API:           llm.Gemini,
			Think:         thinkHigh,
		},
		Hooks: geminiBudgetHooks(),
	},
	{
		Config: llm.ModelConfig{
			Name:          "gemini-2.5-flash",
			BaseURL:       "https://generativelanguage.googleapis.com/v1beta",
			ContextWindow: 1_000_000,
			ImageEnabled:  true,
			API:           llm.Gemini,
			Think:         thinkHigh,
		},
		Hooks: geminiBudgetHooks(),
	},
	// Gemini 3 — thinkingLevel. Pro floor LOW; Flash floor MINIMAL. Neither can fully disable.
	{
		Config: llm.ModelConfig{
			Name:          "gemini-3-pro",
			BaseURL:       "https://generativelanguage.googleapis.com/v1beta",
			ContextWindow: 1_000_000,
			ImageEnabled:  true,
			API:           llm.Gemini,
			Think:         thinkHigh,
		},
		Hooks: geminiLevelHooks("LOW"),
	},
	{
		Config: llm.ModelConfig{
			Name:          "gemini-3-flash",
			BaseURL:       "https://generativelanguage.googleapis.com/v1beta",
			ContextWindow: 1_000_000,
			ImageEnabled:  true,
			API:           llm.Gemini,
			Think:         thinkHigh,
		},
		Hooks: geminiLevelHooks("MINIMAL"),
	},
	{
		Config: llm.ModelConfig{
			Name:          "kimi-k3",
			BaseURL:       "https://api.moonshot.cn/v1",
			ContextWindow: 1_000_000,
			ImageEnabled:  true,
			API:           llm.OpenAI,
			Think:         thinkMax,
		},
	},
	{
		Config: llm.ModelConfig{
			Name:          "kimi-k2.7-code",
			BaseURL:       "https://api.moonshot.cn/v1",
			ContextWindow: 10_000_000,
			ImageEnabled:  true,
			API:           llm.OpenAI,
		},
	},
	// GLM on the z.ai coding plan. Both presets are forced-thinking, and
	// thinking rides in extra_body.thinking with clear_thinking false (Preserved
	// Thinking), so reasoning survives across turns.
	// Sources: https://docs.z.ai/api-reference/llm/chat-completion
	//          https://docs.z.ai/guides/capabilities/thinking
	{
		Config: llm.ModelConfig{
			Name:          "glm-5.3-flash",
			BaseURL:       "https://api.z.ai/api/coding/paas/v4",
			ContextWindow: 1_048_576,
			ImageEnabled:  true,
			API:           llm.OpenAI,
			Think:         thinkMax,
		},
		Hooks: glmHooks(),
	},
	{
		Config: llm.ModelConfig{
			Name:          "glm-5.3",
			BaseURL:       "https://api.z.ai/api/coding/paas/v4",
			ContextWindow: 1_048_576,
			API:           llm.OpenAI,
			Think:         thinkMax,
		},
		Hooks: glmHooks(),
	},
}
