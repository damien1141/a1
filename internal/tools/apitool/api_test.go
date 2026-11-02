package apitool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApiTool_Definition(t *testing.T) {
	tool := ApiTool()
	assert.Equal(t, "api", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "exported")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunApi_NoExports(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\nfunc init() {}")

	raw, _ := json.Marshal(apiInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runApi(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No exported symbols found", out.Content)
}

func TestRunApi_FindsExports(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func PublicFunc() {}
type PublicStruct struct{}
var PublicVar int
const PublicConst = 1
func privateFunc() {}
`)

	raw, _ := json.Marshal(apiInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runApi(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "PublicFunc")
	assert.Contains(t, out.Content, "PublicStruct")
	assert.Contains(t, out.Content, "PublicVar")
	assert.Contains(t, out.Content, "PublicConst")
	assert.NotContains(t, out.Content, "privateFunc")
}

func TestRunApi_UnusedExport(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func UnusedFunc() {}
`)
	writeFile(t, root, "other.go", `package main

func main() {}
`)

	raw, _ := json.Marshal(apiInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runApi(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "unused")
	assert.Contains(t, out.Content, "UnusedFunc")
}

func TestRunApi_UsedExport(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func UsedFunc() {}
`)
	writeFile(t, root, "other.go", `package main

func main() {
	UsedFunc()
}
`)

	raw, _ := json.Marshal(apiInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runApi(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "used")
	assert.Contains(t, out.Content, "UsedFunc")
}

func TestRunApi_SkipsTestFiles(t *testing.T) {
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

	raw, _ := json.Marshal(apiInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runApi(t.Context(), raw)
	require.NoError(t, err)
	// Test file reference should not count as usage.
	assert.Contains(t, out.Content, "unused")
}

func TestRunApi_EmptyPath(t *testing.T) {
	// Empty path defaults to cwd; use a temp dir with no .go files instead.
	root := t.TempDir()
	writeFile(t, root, "readme.txt", "hello")

	raw, _ := json.Marshal(apiInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runApi(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No exported symbols found", out.Content)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
