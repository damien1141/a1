package stacktool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStackTool_Definition(t *testing.T) {
	tool := StackTool()
	assert.Equal(t, "stack", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "stack trace")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunStack_GoStyle(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "main.go", "package main\nfunc main() {\n\tpanic(\"boom\")\n}\n")

	trace := `panic: boom

goroutine 1 [running]:
main.main()
	` + filepath.Join(root, "main.go") + `:3 +0x39
`

	raw, _ := json.Marshal(stackInput{Text: trace, Context: 2})
	out, err := runStack(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "main.go")
	assert.Contains(t, out.Content, ">>3#")
	assert.Contains(t, out.Detail, "1 frame")
}

func TestRunStack_PythonStyle(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "script.py", "def main():\n    foo()\ndef foo():\n    raise ValueError('oops')\n")

	trace := `Traceback (most recent call last):
  File "` + filepath.Join(root, "script.py") + `", line 3, in foo
    raise ValueError('oops')
ValueError: oops
`

	raw, _ := json.Marshal(stackInput{Text: trace})
	out, err := runStack(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "script.py")
	assert.Contains(t, out.Detail, "1 frame")
}

func TestRunStack_NodeStyle(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "app.js", "function main() {\n  throw new Error('x');\n}\n")

	trace := `Error: x
    at main (` + filepath.Join(root, "app.js") + `:2:11)
`

	raw, _ := json.Marshal(stackInput{Text: trace})
	out, err := runStack(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "app.js")
	assert.Contains(t, out.Detail, "1 frame")
}

func TestRunStack_RustStyle(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "main.rs", "fn main() {\n    panic!(\"x\");\n}\n")

	trace := `thread 'main' panicked at 'x', ` + filepath.Join(root, "main.rs") + `:2:5
`

	raw, _ := json.Marshal(stackInput{Text: trace})
	out, err := runStack(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "main.rs")
	assert.Contains(t, out.Detail, "1 frame")
}

func TestRunStack_EmptyInput(t *testing.T) {
	raw, _ := json.Marshal(stackInput{Text: ""})
	out, err := runStack(t.Context(), raw)
	require.Error(t, err)
	assert.Empty(t, out.Content)
}

func TestRunStack_NoFrames(t *testing.T) {
	raw, _ := json.Marshal(stackInput{Text: "hello world"})
	out, err := runStack(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No recognizable stack frames found in input", out.Content)
}

func TestRunStack_DeduplicateFrames(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "main.go", "package main\nfunc a() { b() }\nfunc b() { a() }\n")

	trace := `goroutine 1 [running]:
main.a()
	` + filepath.Join(root, "main.go") + `:1 +0x0
main.b()
	` + filepath.Join(root, "main.go") + `:2 +0x0
main.a()
	` + filepath.Join(root, "main.go") + `:1 +0x0
`

	raw, _ := json.Marshal(stackInput{Text: trace})
	out, err := runStack(t.Context(), raw)
	require.NoError(t, err)
	// Should deduplicate to 2 frames (line 1 and line 2)
	assert.Contains(t, out.Detail, "2 frames")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
