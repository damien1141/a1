package scaffoldtool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScaffoldTool_Definition(t *testing.T) {
	tool := ScaffoldTool()
	assert.Equal(t, "scaffold", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "project")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunScaffold_GoModule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/test\n")
	writeFile(t, root, "main.go", "package main\n")

	raw, _ := json.Marshal(scaffoldInput{Path: root, Limit: 10})
	out, err := runScaffold(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Go module")
	assert.Contains(t, out.Content, "go build")
}

func TestRunScaffold_NodeProject(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "package.json", "{\"name\": \"test\"}\n")

	raw, _ := json.Marshal(scaffoldInput{Path: root, Limit: 10})
	out, err := runScaffold(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Node.js")
}

func TestRunScaffold_Makefile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Makefile", "build:\n\techo build\n")

	raw, _ := json.Marshal(scaffoldInput{Path: root, Limit: 10})
	out, err := runScaffold(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Makefile")
}

func TestRunScaffold_Docker(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Dockerfile", "FROM alpine:latest\n")

	raw, _ := json.Marshal(scaffoldInput{Path: root, Limit: 10})
	out, err := runScaffold(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Dockerfile")
	assert.Contains(t, out.Content, "docker build")
}

func TestRunScaffold_Limit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/test\n")
	writeFile(t, root, "Makefile", "build:\n\techo build\n")
	writeFile(t, root, "Dockerfile", "FROM alpine:latest\n")
	writeFile(t, root, "package.json", "{\"name\": \"test\"}\n")
	writeFile(t, root, "Cargo.toml", "[package]\nname = \"test\"\n")

	raw, _ := json.Marshal(scaffoldInput{Path: root, Limit: 3})
	out, err := runScaffold(t.Context(), raw)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.Content), "\n")
	assert.Len(t, lines, 3, "should respect limit")
}

func TestRunScaffold_EmptyPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "readme.txt", "hello")

	raw, _ := json.Marshal(scaffoldInput{Path: root, Limit: 10})
	out, err := runScaffold(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No recognized project scaffold detected", out.Content)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
