package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
)

// promptCacheKeyBound is the longest prompt_cache_key value providers
// document accepting. Longer scopes are re-hashed into a bounded pck_ key
// rather than truncated — truncation could collide two distinct scopes
// onto one bucket, which is exactly what the key exists to prevent.
const promptCacheKeyBound = 64

// PromptCacheKey derives the provider-side prompt-cache routing key for a
// request's static prefix: pck_<sha256[:24]> over (scope, system prompt,
// name-sorted tools). The scope keeps unrelated sessions that happen to
// share a static prefix — sibling sub-agents, independent conversations —
// off one bucket, while everything under one scope (a conversation, the
// fires of one schedule, a forked branch) hits the same bucket so the
// cached prefix stays warm.
//
// Tools are hashed from the normalized ToolDefinition (not either wire
// shape), so the chat-completions and Responses routes of one model derive
// the same key and share the bucket across modes. Sorting by name keeps the
// hash independent of tool registration order. "" when there is nothing to
// route on: no scope, or no system prompt and no tools (a one-shot request
// whose cache write nothing would ever read).
func PromptCacheKey(scope, system string, tools []ToolDefinition) string {
	if scope == "" || (system == "" && len(tools) == 0) {
		return ""
	}
	sorted := make([]ToolDefinition, len(tools))
	copy(sorted, tools)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	toolsPart, err := json.Marshal(sorted)
	if err != nil {
		// ToolDefinition is plain strings and a schema pointer; a marshal
		// failure means an unserializable parameter schema, and the tools
		// half of the hash degrades to empty rather than taking the request
		// down with it.
		toolsPart = []byte("[]")
	}
	// \x00 separators: content containing the separator bytes can never
	// forge a boundary between the three hashed fields.
	sum := sha256.Sum256([]byte(scope + "\x00" + system + "\x00" + string(toolsPart)))
	key := "pck_" + hex.EncodeToString(sum[:12])
	if len(key) > promptCacheKeyBound {
		sum = sha256.Sum256([]byte(key))
		key = "pck_" + hex.EncodeToString(sum[:12])
	}
	return key
}

// SupportsPromptCacheKey reports whether baseURL points at the one endpoint
// family the top-level prompt_cache_key body field is known to accept:
// OpenAI's own api.openai.com. Strict-compatible endpoints elsewhere
// (Azure, aggregators, local servers) may reject unknown body fields
// outright, so they keep the exact pre-scope request bytes — the scope
// machinery costs them nothing and changes nothing.
func SupportsPromptCacheKey(baseURL string) bool {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), "api.openai.com")
}
