package llm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptCacheKeyDeterministic(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "read", Description: "read files", Params: &FunctionParameters{Type: "object"}},
		{Name: "write", Description: "write files", Params: &FunctionParameters{Type: "object"}},
	}
	a := PromptCacheKey("scope-1", "be brief", tools)
	b := PromptCacheKey("scope-1", "be brief", tools)
	require.Equal(t, a, b)
	require.NotEqual(t, a, PromptCacheKey("scope-2", "be brief", tools), "the scope separates buckets")
	require.NotEqual(t, a, PromptCacheKey("scope-1", "be verbose", tools), "the system prompt is hashed")
	require.NotEqual(t, a, PromptCacheKey("scope-1", "be brief", tools[:1]), "the tool surface is hashed")
}

func TestPromptCacheKeyToolOrderIndependent(t *testing.T) {
	ab := []ToolDefinition{{Name: "a"}, {Name: "b"}}
	ba := []ToolDefinition{{Name: "b"}, {Name: "a"}}
	require.Equal(t,
		PromptCacheKey("s", "sys", ab),
		PromptCacheKey("s", "sys", ba),
		"tool registration order must not change the bucket")
}

func TestPromptCacheKeyEmpty(t *testing.T) {
	require.Empty(t, PromptCacheKey("", "sys", nil), "no scope means no key: the pre-scope wire")
	require.Empty(t, PromptCacheKey("s", "", nil), "nothing static to route on")
	require.NotEmpty(t, PromptCacheKey("s", "sys", nil))
	require.NotEmpty(t, PromptCacheKey("s", "", []ToolDefinition{{Name: "t"}}))
}

func TestPromptCacheKeyBound(t *testing.T) {
	key := PromptCacheKey(strings.Repeat("x", 100), "sys", nil)
	require.Len(t, key, len("pck_")+24, "over-long scopes re-hash into a bounded key")
	require.True(t, strings.HasPrefix(key, "pck_"))
}

func TestSupportsPromptCacheKey(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://api.openai.com/v1", true},
		{"https://api.openai.com", true},
		{"http://api.openai.com/chat/completions", true},
		{"https://api.openai.com:443/v1", true},
		{"https://API.OPENAI.COM/v1", true},
		{"https://api.openai.com.evil.example/v1", false},
		{"https://azure.openai.com/v1", false},
		{"https://api.z.ai/api/prompt/cache/openai", false},
		{"https://openrouter.ai/api/v1", false},
		{"", false},
		{"not a url at all", false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, SupportsPromptCacheKey(tc.url), tc.url)
	}
}
