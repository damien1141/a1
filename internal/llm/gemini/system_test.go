package gemini

import (
	"testing"

	"github.com/damien1141/a1/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildRequest_SystemInstruction guards against the bug where a RoleSystem
// message injected in the middle of the conversation was silently dropped
// (Gemini's BuildRequest only handled the top-level system string and skipped
// RoleSystem messages, so session memory injected mid-conversation was lost).
func TestBuildRequest_SystemInstruction(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "first user turn"},
		{Role: llm.RoleSystem, Content: "session memory injected mid-conversation"},
		{Role: llm.RoleUser, Content: "second user turn"},
	}
	req := BuildRequest("top-level system prompt", messages, nil)

	require.NotNil(t, req.SystemInstruction, "SystemInstruction must be set")
	assert.Contains(t, req.SystemInstruction.Parts[0].Text, "top-level system prompt")
	assert.Contains(t, req.SystemInstruction.Parts[0].Text, "session memory injected mid-conversation")

	// No conversation turn may carry a system role.
	for _, c := range req.Contents {
		assert.NotEqual(t, "system", c.Role)
	}
}

func TestBuildRequest_NoSystemInstructionWhenEmpty(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "hi"},
	}
	req := BuildRequest("", messages, nil)
	assert.Nil(t, req.SystemInstruction)
}