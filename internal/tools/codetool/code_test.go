package codetool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodeTool_Definition(t *testing.T) {
	tool := CodeTool()
	assert.Equal(t, "code", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "mode")
	assert.NotNil(t, tool.Run)
	assert.True(t, tool.Definition.Readable)
	// Required: ["mode"]
	require.NotNil(t, tool.Definition.Params)
	assert.Contains(t, tool.Definition.Params.Required, "mode")
}

func TestCodeTool_ModeEnum(t *testing.T) {
	tool := CodeTool()
	require.NotNil(t, tool.Definition.Params)
	modeParam, ok := tool.Definition.Params.Properties["mode"].(map[string]any)
	if !ok {
		// llm.Object is a map[string]any — try the typed form.
		t.Fatalf("mode param is not a map: %T", tool.Definition.Params.Properties["mode"])
	}
	enum, _ := modeParam["enum"].([]string)
	expected := []string{
		"api", "deadcode", "coverage", "impact", "deps", "nplusone",
		"testimpact", "vuln", "scaffold", "migration", "token", "property",
		"runtime", "build", "judge", "errtrans", "error", "stack", "errpattern",
	}
	assert.Equal(t, expected, enum)
}

func TestRunCode_RequiresMode(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{})
	_, err := runCode(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mode is required")
}

func TestRunCode_UnknownMode(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"mode": "nope"})
	_, err := runCode(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown code mode")
	assert.Contains(t, err.Error(), "nope")
}

func TestRunCode_AllModesWired(t *testing.T) {
	// Each mode must be in codeHandlers. We don't run the handler —
	// most need a real Go workspace or external services (Ollama for
	// judge) — but the dispatch table must have an entry for every
	// advertised mode, otherwise the LLM gets a runtime error on a
	// schema-valid call.
	for _, mode := range codeModes {
		t.Run(mode, func(t *testing.T) {
			h, ok := codeHandlers[mode]
			require.True(t, ok, "mode %q is in codeModes but not in codeHandlers", mode)
			assert.NotNil(t, h)
		})
	}
}

func TestCodeTool_ModesHandlersDescriptionAgree(t *testing.T) {
	// The three mode surfaces must stay in lockstep: a mode missing from
	// the enum is undiscoverable, missing from codeHandlers is a runtime
	// error on a schema-valid call, and missing from the description
	// leaves the model guessing what the mode is for. The reverse
	// direction catches handler entries that are no longer advertised.
	for _, mode := range codeModes {
		assert.Contains(t, codeHandlers, mode, "mode %q has no handler", mode)
		assert.Contains(t, codeDescription, mode, "mode %q is not described in codeDescription", mode)
	}
	for mode := range codeHandlers {
		assert.Contains(t, codeModes, mode, "handler %q is not advertised in codeModes", mode)
	}
}

func TestDetailFromArgs_ModeOnly(t *testing.T) {
	tool := CodeTool()
	raw, _ := json.Marshal(map[string]any{"mode": "deadcode"})
	got := tool.DetailFromArgs(raw)
	assert.Equal(t, "deadcode", got)
}

func TestDetailFromArgs_ModeAndPath(t *testing.T) {
	tool := CodeTool()
	raw, _ := json.Marshal(map[string]any{"mode": "api", "path": "./internal/tools"})
	got := tool.DetailFromArgs(raw)
	assert.Equal(t, "api ./internal/tools", got)
}

func TestDetailFromArgs_ModeAndText(t *testing.T) {
	// Text-keyed modes (error/stack/errtrans) carry no path; the detail
	// line falls back to a truncated paste of `text` so the transcript
	// shows what was diagnosed instead of a bare mode name.
	tool := CodeTool()
	raw, _ := json.Marshal(map[string]any{"mode": "error", "text": "undefined: foo"})
	got := tool.DetailFromArgs(raw)
	assert.Equal(t, "error undefined: foo", got)

	long, _ := json.Marshal(map[string]any{"mode": "error", "text": "runtime error: invalid memory address or nil pointer dereference"})
	got = tool.DetailFromArgs(long)
	assert.Contains(t, got, "...")
	assert.LessOrEqual(t, len(got), len("error ")+40+len("..."))
}

// TestRunCode_ErrtransDispatch verifies the dispatch actually forwards
// to the underlying tool — errtrans is one of the few modes that works
// without a real workspace (it just pattern-matches error text).
func TestRunCode_ErrtransDispatch(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"mode": "errtrans",
		"text": "undefined: foo",
	})
	out, err := runCode(t.Context(), raw)
	require.NoError(t, err, "errtrans dispatch should forward to the underlying tool without error")
	// The underlying errortrans tool returns content even when it can't
	// match — we just need to confirm the dispatch ran and got a result.
	assert.NotEmpty(t, out.Content)
}

func TestRunCode_TokenDispatch(t *testing.T) {
	// token mode also works without a workspace — it just counts chars
	// in a prompt string.
	raw, _ := json.Marshal(map[string]any{
		"mode":   "token",
		"prompt": "hello world this is a test",
	})
	out, err := runCode(t.Context(), raw)
	require.NoError(t, err)
	assert.NotEmpty(t, out.Content)
}

// TestRunCode_ErrorDispatch proves the errortool folding: mode=error
// forwards the raw args (including `text`) to errortool.ErrorTool().Run,
// which pattern-matches the pasted message and returns a fix.
func TestRunCode_ErrorDispatch(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"mode": "error",
		"text": "undefined: foo",
	})
	out, err := runCode(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Go: undefined variable")
	assert.Contains(t, out.Content, "Fix:")
}

// TestRunCode_StackDispatch proves the stacktool folding: mode=stack
// parses a Go-style trace and renders the frame with source context.
func TestRunCode_StackDispatch(t *testing.T) {
	root := t.TempDir()
	mainGo := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(mainGo, []byte("package main\nfunc main() {\n\tpanic(\"boom\")\n}\n"), 0o644))

	raw, _ := json.Marshal(map[string]any{
		"mode": "stack",
		"text": "panic: boom\n\ngoroutine 1 [running]:\nmain.main()\n\t" + mainGo + ":3 +0x39\n",
	})
	out, err := runCode(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "main.go")
	assert.Contains(t, out.Detail, "1 frame")
}

// TestRunCode_ErrpatternDispatch proves the errpattern folding: mode=errpattern
// scans a Go tree for error-handling anti-patterns.
func TestRunCode_ErrpatternDispatch(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() { panic(\"boom\") }\n"), 0o644))

	raw, _ := json.Marshal(map[string]any{
		"mode":  "errpattern",
		"path":  root,
		"limit": 10,
	})
	out, err := runCode(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "panic")
}
