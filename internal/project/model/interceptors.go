package model

import (
	"context"

	"github.com/damien1141/a1/internal/llm"
	llmclient "github.com/damien1141/a1/internal/llm/client"
	"github.com/damien1141/a1/internal/llm/gemini"
	"github.com/damien1141/a1/internal/llm/openai"
)

// deepseekThinking enables DeepSeek's extra_body.thinking on OpenAI-shaped requests.
type deepseekThinking struct{}

func (deepseekThinking) Before(_ context.Context, req *openai.Request, _ llm.ModelConfig) error {
	req.ExtraBody = &openai.ExtraBody{Thinking: &openai.ThinkingConfig{Type: "enabled"}}
	return nil
}

func deepseekHooks() llmclient.Hooks {
	return llmclient.Hooks{OpenAI: deepseekThinking{}}
}

// glmThinking drives GLM reasoning through z.ai's extra_body.thinking, the same
// envelope DeepSeek uses. Both presets are forced-thinking — z.ai errors on
// thinking.type=disabled — so type is always enabled, and reasoning_effort stays
// as BuildRequest set it, because z.ai reads that as the depth of the thought
// chain. clear_thinking: false turns on Preserved Thinking, keeping
// reasoning_content from earlier turns in context, which interleaved tool
// calling across turns needs.
type glmThinking struct{}

func (glmThinking) Before(_ context.Context, req *openai.Request, _ llm.ModelConfig) error {
	preserve := false
	req.ExtraBody = &openai.ExtraBody{
		Thinking: &openai.ThinkingConfig{Type: "enabled", ClearThinking: &preserve},
	}
	return nil
}

func glmHooks() llmclient.Hooks {
	return llmclient.Hooks{OpenAI: glmThinking{}}
}

// geminiBudgetThinking maps ThinkMode → thinkingBudget (Gemini 2.x).
type geminiBudgetThinking struct{}

func (geminiBudgetThinking) Before(_ context.Context, req *gemini.GeminiRequest, cfg llm.ModelConfig) error {
	req.ApplyBudgetThinking(cfg.Think)
	return nil
}

// geminiLevelThinking maps ThinkMode → thinkingLevel (Gemini 3.x).
// offLevel is the floor when thinking is disabled.
type geminiLevelThinking struct {
	offLevel string
}

func (h geminiLevelThinking) Before(_ context.Context, req *gemini.GeminiRequest, cfg llm.ModelConfig) error {
	req.ApplyLevelThinking(cfg.Think, h.offLevel)
	return nil
}

func geminiBudgetHooks() llmclient.Hooks {
	return llmclient.Hooks{Gemini: geminiBudgetThinking{}}
}

func geminiLevelHooks(offLevel string) llmclient.Hooks {
	return llmclient.Hooks{Gemini: geminiLevelThinking{offLevel: offLevel}}
}
