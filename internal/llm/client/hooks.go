package client

import (
	"context"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/llm/anthropic"
	"github.com/damien1141/a1/internal/llm/gemini"
	"github.com/damien1141/a1/internal/llm/openai"
	"github.com/damien1141/a1/internal/llm/openai/responses"
)

// Hooks holds optional per-provider request interceptors. Only the field
// matching cfg.API is invoked; nil means no customization.
type Hooks struct {
	OpenAI          llm.RequestInterceptor[openai.Request]
	OpenAIResponses llm.RequestInterceptor[responses.Request]
	Anthropic       llm.RequestInterceptor[anthropic.AnthropicRequest]
	Gemini          llm.RequestInterceptor[gemini.GeminiRequest]
}

func applyHook[Req any](ctx context.Context, h llm.RequestInterceptor[Req], req *Req, cfg llm.ModelConfig) error {
	if h == nil {
		return nil
	}
	return h.Before(ctx, req, cfg)
}
