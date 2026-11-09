package anthropic

import "encoding/json"

type cacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

// cacheTTL reports the TTL any cache_control marker in the request carries,
// "" when none does. The walk is structural (system blocks, message content
// blocks, tools) rather than a body-bytes sniff, so a future marker placed
// anywhere turns the beta header on by existing — the header requirement
// follows the field wherever it lands. Markers without a ttl (the plain
// {"type":"ephemeral"} 5m default) report "", matching the API's own rule
// that the header only matters when ttl is set.
func (r *AnthropicRequest) cacheTTL() string {
	for i := range r.System {
		if cc := r.System[i].CacheControl; cc != nil && cc.TTL != "" {
			return cc.TTL
		}
	}
	for i := range r.Messages {
		blocks, ok := r.Messages[i].Content.([]anthropicContentBlock)
		if !ok {
			continue
		}
		for j := range blocks {
			if cc := blocks[j].CacheControl; cc != nil && cc.TTL != "" {
				return cc.TTL
			}
		}
	}
	for i := range r.Tools {
		if cc := r.Tools[i].CacheControl; cc != nil && cc.TTL != "" {
			return cc.TTL
		}
	}
	return ""
}

// thinkingConfig controls Anthropic's extended thinking feature.
// Type "adaptive" lets the model decide how much to think;
// Type "enabled" with BudgetTokens gives a fixed token budget.
type thinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens *int   `json:"budget_tokens,omitempty"`
}

type AnthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    []sysBlock         `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Stream    bool               `json:"stream"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	Thinking  *thinkingConfig    `json:"thinking,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []anthropicContentBlock
}

type anthropicTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type sysBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

// anthropicImageSource is the base64 source inside an image content block.
type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// anthropicContentBlock represents a single content block inside an Anthropic
// message. Content is the tool_result payload: a plain string, or an array
// of text/image blocks when the tool result carries images (multimodal
// tool_result).
type anthropicContentBlock struct {
	Type         string                `json:"type"`
	Text         string                `json:"text,omitempty"`
	ID           string                `json:"id,omitempty"`
	Name         string                `json:"name,omitempty"`
	Input        json.RawMessage       `json:"input,omitempty"`
	ToolUseID    string                `json:"tool_use_id,omitempty"`
	Content      any                   `json:"content,omitempty"`
	Source       *anthropicImageSource `json:"source,omitempty"`
	CacheControl *cacheControl         `json:"cache_control,omitempty"`
}
