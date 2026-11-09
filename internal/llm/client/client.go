package client

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"time"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/llm/anthropic"
	"github.com/damien1141/a1/internal/llm/gemini"
	"github.com/damien1141/a1/internal/llm/openai"
	"github.com/damien1141/a1/internal/llm/openai/responses"
	"github.com/damien1141/a1/internal/util"
)

// Client talks to the configured LLM endpoint: OpenAI chat-completions by
// default, or Anthropic / Gemini / OpenAI Responses when cfg.API is set.
// A credential pool (SetKeyPool) rotates API keys under rate-limit and
// auth failures; without one, cfg.APIKey is every request's key, exactly
// as it was before pools existed.
type Client struct {
	httpClient *http.Client
	cfg        llm.ModelConfig
	hooks      Hooks
	tools      []llm.ToolDefinition
	system     string
	pool       *llm.KeyPool
	cacheScope string
}

// NewClient builds a streaming chat client.
func NewClient(cfg llm.ModelConfig, hooks Hooks, tools []llm.ToolDefinition, systemPrompt string) *Client {
	return &Client{
		httpClient: util.DefaultHTTPClient(),
		cfg:        cfg,
		hooks:      hooks,
		tools:      tools,
		system:     systemPrompt,
	}
}

// SetKeyPool attaches a credential pool. Requests draw their key from the
// pool; a 429 or 401/403 that arrives before any content has streamed
// rotates to the next key and re-issues transparently — each key at most
// once per call. A key failure that arrives mid-stream is still reported
// (future calls avoid the key) but never retried in the same call: the
// consumer has already seen the partial content. A nil pool, or never
// calling this, keeps the single-key behavior byte for byte. Returns the
// client for call chaining.
func (c *Client) SetKeyPool(p *llm.KeyPool) *Client {
	c.pool = p
	return c
}

// SetCacheScope pins the logical prompt-cache scope requests route under
// (see session.Manager.CacheScope). Empty — or never calling this — keeps
// the wire exactly as it was before scopes existed: no prompt_cache_key is
// derived or sent. Returns the client for call chaining.
func (c *Client) SetCacheScope(scope string) *Client {
	c.cacheScope = scope
	return c
}

// CacheScope returns the pinned scope ("" when none was set).
func (c *Client) CacheScope() string {
	return c.cacheScope
}

// Stream runs a streaming chat completion over messages (+ optional system prompt / tools).
func (c *Client) Stream(ctx context.Context, messages []llm.Message) iter.Seq2[llm.StreamEvent, error] {
	// Wire-out repair: a session can hold tool calls whose arguments arrived
	// malformed (a stream cut mid-arguments, a gateway collapsing fragments).
	// Replayed verbatim they are invalid JSON on the request and a strict
	// gateway answers a bare 400 no recovery loop can diagnose — every
	// provider request goes through here, so every provider gets parseable
	// arguments. The session itself is never rewritten.
	messages = llm.SanitizeMessages(messages)
	return func(yield func(llm.StreamEvent, error) bool) {
		key := c.cfg.APIKey
		idx := 0
		if c.pool != nil {
			key, idx = c.pool.Acquire(time.Now())
		}
		tried := map[string]bool{key: true}
		for {
			retry := false
			var failed error
			var failedEv llm.StreamEvent
			sawEvent := false
			for ev, err := range c.streamOnce(ctx, messages, key) {
				if err != nil {
					failed = err
					failedEv = ev
					break
				}
				sawEvent = true
				if !yield(ev, err) {
					// Consumer stopped early (ctx cancel, queue full):
					// neither success nor failure to report.
					return
				}
			}
			if failed == nil {
				if c.pool != nil {
					c.pool.ReportSuccess(idx)
				}
				return
			}
			if next, nextIdx, rotatable := c.rotate(idx, failed); rotatable {
				if !sawEvent && !tried[next] {
					// Nothing reached the consumer on this attempt — the
					// re-issue is invisible, which is the whole point.
					tried[next] = true
					key, idx = next, nextIdx
					retry = true
				}
				if !retry {
					// Either mid-stream (partial content already shown)
					// or every key tried. The pool state is updated
					// either way; only this call's retry is forgone.
					if sawEvent {
						yield(failedEv, failed)
					} else {
						yield(failedEv, poolExhaustedError(failed, c.pool.Len()))
					}
					return
				}
			} else {
				yield(failedEv, failed)
				return
			}
		}
	}
}

// Compact sends a single non-streaming chat request and returns the
// assistant text. It satisfies llm.Compactor for session compaction.
func (c *Client) Compact(ctx context.Context, req llm.CompactRequest) (llm.CompactResult, error) {
	key := c.cfg.APIKey
	idx := 0
	if c.pool != nil {
		key, idx = c.pool.Acquire(time.Now())
	}
	tried := map[string]bool{key: true}
	for {
		res, err := c.compactOnce(ctx, req, key)
		if err == nil {
			if c.pool != nil {
				c.pool.ReportSuccess(idx)
			}
			return res, nil
		}
		next, nextIdx, rotatable := c.rotate(idx, err)
		if !rotatable {
			return res, err
		}
		if !tried[next] {
			tried[next] = true
			key, idx = next, nextIdx
			continue
		}
		return res, poolExhaustedError(err, c.pool.Len())
	}
}

// rotate reports a failed attempt to the pool (so future calls avoid the
// key) and returns the next key to try. ok is false when the error says
// nothing about the key itself — 5xx, network, context — and the existing
// retry machinery owns it. Callers hold the yield policy: whether the next
// key is actually used this call.
func (c *Client) rotate(idx int, err error) (key string, nextIdx int, ok bool) {
	if c.pool == nil {
		return "", 0, false
	}
	kind, retryAfter := llm.ClassifyKeyFailure(err)
	switch kind {
	case llm.KeyFailureRateLimit:
		c.pool.ReportRateLimited(idx, retryAfter, time.Now())
	case llm.KeyFailureAuth:
		c.pool.ReportAuthFailure(idx, time.Now())
	default:
		return "", 0, false
	}
	key, nextIdx = c.pool.Acquire(time.Now())
	return key, nextIdx, true
}

// poolExhaustedError annotates the last provider error when every key in
// the pool had its chance in one call. The %w chain keeps APIErrorOf
// working for callers that branch on the status.
func poolExhaustedError(err error, keys int) error {
	return fmt.Errorf("%w (credential pool: all %d keys failed — last error above)", err, keys)
}

// promptCacheKey derives the prompt_cache_key for one OpenAI-wire request,
// or "" when this endpoint should not get one: hosts other than
// api.openai.com keep the exact pre-scope body. Computed per attempt from
// the client's own system prompt and tool surface — the parts a rebind
// changes — so the chat and Responses routes of one engine derive the same
// key and share the bucket across modes.
func (c *Client) promptCacheKey(baseURL string) string {
	if !llm.SupportsPromptCacheKey(baseURL) {
		return ""
	}
	return llm.PromptCacheKey(c.cacheScope, c.system, c.tools)
}

// streamOnce issues one streaming request under one key. cfg is copied so
// the rotation never mutates the client's own config.
func (c *Client) streamOnce(ctx context.Context, messages []llm.Message, key string) iter.Seq2[llm.StreamEvent, error] {
	cfg := c.cfg
	cfg.APIKey = key
	switch cfg.API {
	case llm.Anthropic:
		req := anthropic.BuildRequest(cfg, c.system, messages, c.tools)
		if err := applyHook(ctx, c.hooks.Anthropic, &req, cfg); err != nil {
			return errorSeq(err)
		}
		return anthropic.Stream(ctx, c.httpClient, cfg, &req)
	case llm.Gemini:
		req := gemini.BuildRequest(c.system, messages, c.tools)
		if err := c.applyGeminiThinking(ctx, &req); err != nil {
			return errorSeq(err)
		}
		return gemini.Stream(ctx, c.httpClient, cfg, &req)
	case llm.OpenAIResponses:
		req := responses.BuildRequest(cfg, c.system, messages, c.tools)
		if key := c.promptCacheKey(cfg.BaseURL); key != "" {
			req.PromptCacheKey = key
		}
		if err := applyHook(ctx, c.hooks.OpenAIResponses, req, cfg); err != nil {
			return errorSeq(err)
		}
		return responses.Stream(ctx, c.httpClient, cfg, req)
	default: // llm.OpenAI or empty — both route to OpenAI-compatible chat completions
		req := openai.BuildRequest(cfg, c.system, messages, c.tools)
		if key := c.promptCacheKey(cfg.BaseURL); key != "" {
			req.PromptCacheKey = key
		}
		if err := applyHook(ctx, c.hooks.OpenAI, req, cfg); err != nil {
			return errorSeq(err)
		}
		return openai.StreamChatCompletion(ctx, c.httpClient, cfg.BaseURL, key, req)
	}
}

// compactOnce issues one non-streaming request under one key.
func (c *Client) compactOnce(ctx context.Context, req llm.CompactRequest, key string) (llm.CompactResult, error) {
	cfg := c.cfg
	cfg.APIKey = key
	switch cfg.API {
	case llm.Anthropic:
		return anthropic.Compact(ctx, c.httpClient, cfg, req)
	case llm.Gemini:
		greReq := gemini.BuildRequest("", []llm.Message{{Role: llm.RoleUser, Content: req.Prompt}}, nil)
		greReq.SetMaxOutputTokens(req.MaxTokens)
		if err := c.applyGeminiThinking(ctx, &greReq); err != nil {
			return llm.CompactResult{}, err
		}
		return gemini.CompactRequest(ctx, c.httpClient, cfg, &greReq)
	case llm.OpenAIResponses:
		respReq := responses.NewCompactRequest(cfg.Name, req.Prompt, req.MaxTokens)
		if err := applyHook(ctx, c.hooks.OpenAIResponses, respReq, cfg); err != nil {
			return llm.CompactResult{}, err
		}
		return responses.CompactRequest(ctx, c.httpClient, cfg, respReq)
	default:
		oaiReq := openai.NewCompactRequest(cfg.Name, req.Prompt, req.MaxTokens)
		if err := applyHook(ctx, c.hooks.OpenAI, oaiReq, cfg); err != nil {
			return llm.CompactResult{}, err
		}
		return openai.CompactRequest(ctx, c.httpClient, cfg, oaiReq)
	}
}

// applyGeminiThinking runs the Gemini hook. Without a preset hook, budget
// style is the safe default for unknown models.
func (c *Client) applyGeminiThinking(ctx context.Context, req *gemini.GeminiRequest) error {
	if c.hooks.Gemini != nil {
		return applyHook(ctx, c.hooks.Gemini, req, c.cfg)
	}
	req.ApplyBudgetThinking(c.cfg.Think)
	return nil
}

func errorSeq(err error) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		yield(llm.StreamEvent{}, err)
	}
}
