package tokentool

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenTool_Definition(t *testing.T) {
	tool := TokenTool()
	assert.Equal(t, "tokenbudget", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "token")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunToken_Defaults(t *testing.T) {
	raw, _ := json.Marshal(tokenInput{})
	out, err := runToken(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "grep")
	assert.Contains(t, out.Content, "read")
	assert.Contains(t, out.Detail, "allocations")
}

func TestRunToken_RustBorrowChecker(t *testing.T) {
	raw, _ := json.Marshal(tokenInput{Task: "rust borrow checker failure", Total: 2000, Limit: 5})
	out, err := runToken(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "bash")
	assert.Contains(t, out.Content, "cargo check")
}

func TestRunToken_BuildSystem(t *testing.T) {
	raw, _ := json.Marshal(tokenInput{Task: "build system cmake", Total: 2000, Limit: 5})
	out, err := runToken(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "build")
}

func TestRunToken_EmptyTask(t *testing.T) {
	raw, _ := json.Marshal(tokenInput{Task: "", Total: 1000, Limit: 3})
	out, err := runToken(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "grep")
}
