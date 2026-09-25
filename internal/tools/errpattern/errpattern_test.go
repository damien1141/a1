package errpattern

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrPatternTool_Definition(t *testing.T) {
	tool := ErrPatternTool()
	assert.Equal(t, "errpattern", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "error")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunErrPattern_NoIssues(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func main() {
	println("hello")
}
`)

	raw, _ := json.Marshal(errPatternInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runErrPattern(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No error handling issues detected", out.Content)
}

func TestRunErrPattern_FindsIgnoredError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func main() {
	err := doSomething()
	_ = err
}
`)

	raw, _ := json.Marshal(errPatternInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runErrPattern(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "ignored")
}

func TestRunErrPattern_FindsPanic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func main() {
	panic("something went wrong")
}
`)

	raw, _ := json.Marshal(errPatternInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runErrPattern(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "panic")
}

func TestRunErrPattern_Limit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		writeFile(t, root, fmt.Sprintf("file%d.go", i), `package main

func main() {
	panic("test")
}
`)
	}

	raw, _ := json.Marshal(errPatternInput{Path: root, Glob: "*.go", Limit: 3})
	out, err := runErrPattern(t.Context(), raw)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.Content), "\n")
	assert.Len(t, lines, 3, "should respect limit")
}

func TestRunErrPattern_EmptyPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "readme.txt", "hello")

	raw, _ := json.Marshal(errPatternInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runErrPattern(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No error handling issues detected", out.Content)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
