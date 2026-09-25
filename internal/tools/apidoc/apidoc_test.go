package apidoc

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

func TestApidocTool_Definition(t *testing.T) {
	tool := ApidocTool()
	assert.Equal(t, "apidoc", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "documentation")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunApidoc_NoExports(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func init() {}
`)

	raw, _ := json.Marshal(apidocInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runApidoc(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No exported symbols found for documentation", out.Content)
}

func TestRunApidoc_FindsExports(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

// PublicFunc does something.
func PublicFunc() {}
`)

	raw, _ := json.Marshal(apidocInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runApidoc(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "PublicFunc")
	assert.Contains(t, out.Content, "does something")
}

func TestRunApidoc_Limit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		writeFile(t, root, fmt.Sprintf("file%d.go", i), fmt.Sprintf(`package main

func Func%d() {}
`, i))
	}

	raw, _ := json.Marshal(apidocInput{Path: root, Glob: "*.go", Limit: 3})
	out, err := runApidoc(t.Context(), raw)
	require.NoError(t, err)
	// Count entries by @file markers.
	count := strings.Count(out.Content, "@file")
	assert.Equal(t, 3, count, "should respect limit")
}

func TestRunApidoc_EmptyPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "readme.txt", "hello")

	raw, _ := json.Marshal(apidocInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runApidoc(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No exported symbols found for documentation", out.Content)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
