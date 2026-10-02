package responses

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// TestBuildRequest_SystemFirst guards against the bug where a RoleSystem
// message injected in the middle of the conversation (e.g. session memory)
// was emitted as a plain input item after the user turns. OpenAI requires
// system/developer messages at the beginning, so the API rejected the
// request with "System message must be at the beginning".
func TestBuildRequest_SystemFirst(t *testing.T) {
	cfg := llm.ModelConfig{Name: "gpt-test"}
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "first user turn"},
		{Role: llm.RoleSystem, Content: "session memory injected mid-conversation"},
		{Role: llm.RoleUser, Content: "second user turn"},
	}
	req := BuildRequest(cfg, "top-level system prompt", messages, nil)

	// 3 items: one consolidated system message + 2 user turns (the injected
	// RoleSystem message is folded into the system item, not emitted inline).
	require.Len(t, req.Input, 3, "system text must be consolidated into one item")
	// The first item must be the consolidated system message.
	assert.Equal(t, "system", req.Input[0].Role, "system message must be first")
	assert.Contains(t, req.Input[0].Content, "top-level system prompt")
	assert.Contains(t, req.Input[0].Content, "session memory injected mid-conversation")
	// No other item carries a system role.
	for i := 1; i < len(req.Input); i++ {
		assert.NotEqual(t, "system", req.Input[i].Role, "item %d must not be a system message", i)
	}
}

func TestBuildRequest_DeveloperRoleWhenThinking(t *testing.T) {
	cfg := llm.ModelConfig{Name: "o1-test", Think: llm.ThinkConfig{Enabled: true, Mode: "high"}}
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "hi"},
	}
	req := BuildRequest(cfg, "system", messages, nil)
	// 2 items: developer system + 1 user turn.
	require.Len(t, req.Input, 2)
	assert.Equal(t, "developer", req.Input[0].Role, "thinking models use developer role for system text")
	assert.Equal(t, "user", req.Input[1].Role)
}

func TestBuildRequest_NoSystemWhenEmpty(t *testing.T) {
	cfg := llm.ModelConfig{Name: "gpt-test"}
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "hi"},
	}
	req := BuildRequest(cfg, "", messages, nil)
	require.Len(t, req.Input, 1)
	assert.Equal(t, "user", req.Input[0].Role)
}
