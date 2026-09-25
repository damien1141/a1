package errortool

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorTool_Definition(t *testing.T) {
	tool := ErrorTool()
	assert.Equal(t, "error", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "error")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunError_MatchesGoUndefined(t *testing.T) {
	raw, _ := json.Marshal(errorInput{Text: "undefined: foo"})
	out, err := runError(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Go: undefined variable")
	assert.Contains(t, out.Content, "foo")
	assert.Contains(t, out.Content, "Fix:")
}

func TestRunError_MatchesGoNilPointer(t *testing.T) {
	raw, _ := json.Marshal(errorInput{Text: "runtime error: invalid memory address or nil pointer dereference"})
	out, err := runError(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Go: nil pointer dereference")
	assert.Contains(t, out.Content, "Fix:")
}

func TestRunError_MatchesPythonModuleNotFound(t *testing.T) {
	raw, _ := json.Marshal(errorInput{Text: "ModuleNotFoundError: No module named 'requests'"})
	out, err := runError(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Python: module not found")
	assert.Contains(t, out.Content, "requests")
	assert.Contains(t, out.Content, "Fix:")
}

func TestRunError_MatchesRustBorrowChecker(t *testing.T) {
	raw, _ := json.Marshal(errorInput{Text: "borrow of moved value: `s`"})
	out, err := runError(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Rust: borrow checker error")
	assert.Contains(t, out.Content, "Fix:")
}

func TestRunError_MatchesPermissionDenied(t *testing.T) {
	raw, _ := json.Marshal(errorInput{Text: "open /etc/passwd: permission denied"})
	out, err := runError(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Generic: permission denied")
	assert.Contains(t, out.Content, "Fix:")
}

func TestRunError_NoMatch(t *testing.T) {
	raw, _ := json.Marshal(errorInput{Text: "some random unknown error"})
	out, err := runError(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No known error patterns matched", out.Content)
}

func TestRunError_EmptyInput(t *testing.T) {
	raw, _ := json.Marshal(errorInput{Text: ""})
	out, err := runError(t.Context(), raw)
	require.Error(t, err)
	assert.Empty(t, out.Content)
}

func TestRunError_Limit(t *testing.T) {
	text := "undefined: foo\npanic: boom\nModuleNotFoundError: No module named 'x'\nTypeError: bad\nKeyError: 'k'\npermission denied\n"
	raw, _ := json.Marshal(errorInput{Text: text, Limit: 2})
	out, err := runError(t.Context(), raw)
	require.NoError(t, err)
	parts := strings.Split(out.Content, "\n\n")
	assert.Len(t, parts, 2, "should respect limit")
}
