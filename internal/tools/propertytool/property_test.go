package propertytool

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

func TestPropertyTool_Definition(t *testing.T) {
	tool := PropertyTool()
	assert.Equal(t, "property", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "property")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunProperty_NoFunctions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func init() {}
`)

	raw, _ := json.Marshal(propertyInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runProperty(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No exported functions found for property testing", out.Content)
}

func TestRunProperty_FindsFunctions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func Add(a, b int) int {
	return a + b
}
`)

	raw, _ := json.Marshal(propertyInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runProperty(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Add")
	assert.Contains(t, out.Content, "quick.Check")
}

func TestRunProperty_Limit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		writeFile(t, root, fmt.Sprintf("file%d.go", i), fmt.Sprintf(`package main

func Func%d(a int) int {
	return a
}
`, i))
	}

	raw, _ := json.Marshal(propertyInput{Path: root, Glob: "*.go", Limit: 3})
	out, err := runProperty(t.Context(), raw)
	require.NoError(t, err)
	// Count suggestions by @file markers.
	count := strings.Count(out.Content, "@file")
	assert.Equal(t, 3, count, "should respect limit")
}

func TestRunProperty_EmptyPath(t *testing.T) {
	// Empty path defaults to cwd; use a temp dir with no Go files instead.
	root := t.TempDir()
	writeFile(t, root, "readme.txt", "hello")

	raw, _ := json.Marshal(propertyInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runProperty(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No exported functions found for property testing", out.Content)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
