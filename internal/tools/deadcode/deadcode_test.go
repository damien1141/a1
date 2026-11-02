package deadcode

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

func TestDeadcodeTool_Definition(t *testing.T) {
	tool := DeadcodeTool()
	assert.Equal(t, "deadcode", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "dead")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunDeadcode_NoDeadCode(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func PublicFunc() {}
`)
	writeFile(t, root, "other.go", `package main

func main() {
	PublicFunc()
}
`)

	raw, _ := json.Marshal(deadcodeInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runDeadcode(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No dead code detected", out.Content)
}

func TestRunDeadcode_FindsDeadCode(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func UnusedFunc() {}
`)

	raw, _ := json.Marshal(deadcodeInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runDeadcode(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "UnusedFunc")
	assert.Contains(t, out.Content, "func")
}

func TestRunDeadcode_SkipsTestFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func ExportedFunc() {}
`)
	writeFile(t, root, "main_test.go", `package main

import "testing"
func TestExportedFunc(t *testing.T) {
	ExportedFunc()
}
`)

	raw, _ := json.Marshal(deadcodeInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runDeadcode(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "ExportedFunc")
}

func TestRunDeadcode_EmptyPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "readme.txt", "hello")

	raw, _ := json.Marshal(deadcodeInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runDeadcode(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No dead code detected", out.Content)
}

func TestRunDeadcode_Limit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		funcName := fmt.Sprintf("UnusedFunc%d", i)
		writeFile(t, root, fmt.Sprintf("file%d.go", i), fmt.Sprintf(`package main

func %s() {}
`, funcName))
	}

	raw, _ := json.Marshal(deadcodeInput{Path: root, Glob: "*.go", Limit: 3})
	out, err := runDeadcode(t.Context(), raw)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.Content), "\n")
	assert.Len(t, lines, 3, "should respect limit")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
