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
        raw, _ := json.Marshal(
                errtransInput{Text: "ModuleNotFoundError: No module named 'requests'", Lang: "python", Limit: 5},
        )
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
        raw, _ := json.Marshal(
                errtransInput{Text: "ModuleNotFoundError: No module named 'requests'", Lang: "python", Limit: 2},
        )
        out, err := runErrtrans(t.Context(), raw)
        require.NoError(t, err)
        lines := strings.Split(strings.TrimSpace(out.Content), "\n")
        assert.Len(t, lines, 1, "should respect limit")
}

// TestRunErrtrans_AnchoredMatching is the regression test for the bug where
// patterns used bare strings.Contains — a substring embedded inside a
// larger word counted as a match. Word-boundary anchored matching rejects
// these.
func TestRunErrtrans_AnchoredMatching(t *testing.T) {
        cases := []struct {
                name string
                text string
                lang string
                // The text below deliberately embeds the substring inside larger
                // words (or with adjacent punctuation that breaks the word
                // boundary). The old substring matcher matched; the new anchored
                // matcher must not.
                shouldNotContain string
        }{
                {
                        name:             "rust 'no field' inside 'no fielded'",
                        text:             "the struct has no fielded data here",
                        lang:             "rust",
                        shouldNotContain: "struct-field access error",
                },
                {
                        name:             "rust 'cannot move' inside 'cannot moveable'",
                        text:             "this is a cannot moveable design",
                        lang:             "rust",
                        shouldNotContain: "move-semantics violation",
                },
                {
                        name:             "python 'ImportError' inside 'MyImportErrorHelper'",
                        text:             "class MyImportErrorHelper: pass",
                        lang:             "python",
                        shouldNotContain: "missing dependency or bad import path",
                },
                {
                        name:             "bash 'syntax error' inside 'syntax errors'",
                        text:             "we have several syntax errors here",
                        lang:             "bash",
                        shouldNotContain: "shell syntax failure",
                },
                {
                        name:             "lua 'attempt to index' inside 'attempt to indexer'",
                        text:             "the program uses attempt to indexer pages",
                        lang:             "lua",
                        shouldNotContain: "nil/table access on non-table",
                },
                {
                        name:             "go 'import cycle' inside 'import cycler'",
                        text:             "this is an import cycler utility",
                        lang:             "go",
                        shouldNotContain: "circular package dependency",
                },
        }
        for _, tc := range cases {
                t.Run(tc.name, func(t *testing.T) {
                        raw, _ := json.Marshal(errtransInput{Text: tc.text, Lang: tc.lang, Limit: 20})
                        out, err := runErrtrans(t.Context(), raw)
                        require.NoError(t, err)
                        assert.NotContains(t, out.Content, tc.shouldNotContain,
                                "substring embedded in a larger word must not trigger a translation")
                })
        }
}
