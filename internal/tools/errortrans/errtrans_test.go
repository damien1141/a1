package errortrans

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrtransTool_Definition(t *testing.T) {
	tool := ErrtransTool()
	assert.Equal(t, "errtrans", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "language")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunErrtrans_RustBorrowChecker(t *testing.T) {
	raw, _ := json.Marshal(errtransInput{Text: "cannot move out of borrowed data", Lang: "rust", Limit: 5})
	out, err := runErrtrans(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "rust")
	assert.Contains(t, out.Content, "borrow")
}

func TestRunErrtrans_PythonImport(t *testing.T) {
	raw, _ := json.Marshal(errtransInput{Text: "ModuleNotFoundError: No module named 'requests'", Lang: "python", Limit: 5})
	out, err := runErrtrans(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "python")
}

func TestRunErrtrans_BashCommandNotFound(t *testing.T) {
	raw, _ := json.Marshal(errtransInput{Text: "bash: rg: command not found", Lang: "bash", Limit: 5})
	out, err := runErrtrans(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "bash")
	assert.Contains(t, out.Content, "missing")
}

func TestRunErrtrans_LuaIndexNil(t *testing.T) {
	raw, _ := json.Marshal(errtransInput{Text: "attempt to index nil with 'foo'", Lang: "lua", Limit: 5})
	out, err := runErrtrans(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "lua")
}

func TestRunErrtrans_GoUndefined(t *testing.T) {
	raw, _ := json.Marshal(errtransInput{Text: "undefined: foo", Lang: "go", Limit: 5})
	out, err := runErrtrans(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "go")
}

func TestRunErrtrans_EmptyText(t *testing.T) {
	raw, _ := json.Marshal(errtransInput{Text: "", Lang: "rust"})
	_, err := runErrtrans(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text is required")
}

func TestRunErrtrans_AutoDetectLanguage(t *testing.T) {
	raw, _ := json.Marshal(errtransInput{Text: "cargo check failed: cannot move out of borrowed data", Limit: 5})
	out, err := runErrtrans(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "rust")
}

func TestRunErrtrans_Limit(t *testing.T) {
	raw, _ := json.Marshal(errtransInput{Text: "ModuleNotFoundError: No module named 'requests'", Lang: "python", Limit: 2})
	out, err := runErrtrans(t.Context(), raw)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.Content), "\n")
	assert.Len(t, lines, 1, "should respect limit")
}
