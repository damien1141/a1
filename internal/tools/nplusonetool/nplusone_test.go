package nplusonetool

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

func TestNplusoneTool_Definition(t *testing.T) {
	tool := NplusoneTool()
	assert.Equal(t, "nplusone", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "N+1")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunNplusone_NoPatterns(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func main() {
	println("hello")
}
`)

	raw, _ := json.Marshal(nplusoneInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runNplusone(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No N+1 query patterns detected", out.Content)
}

func TestRunNplusone_FindsPattern(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

import "database/sql"

func main() {
	for _, item := range items {
		row := db.QueryRow("SELECT * FROM users WHERE id = ?", item.ID)
		_ = row
	}
}
`)

	raw, _ := json.Marshal(nplusoneInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runNplusone(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "query")
	assert.Contains(t, out.Content, "main.go")
}

func TestRunNplusone_Limit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		writeFile(t, root, fmt.Sprintf("file%d.go", i), `package main

import "database/sql"

func main() {
	for _, item := range items {
		row := db.QueryRow("SELECT * FROM users WHERE id = ?", item.ID)
		_ = row
	}
}
`)
	}

	raw, _ := json.Marshal(nplusoneInput{Path: root, Glob: "*.go", Limit: 3})
	out, err := runNplusone(t.Context(), raw)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.Content), "\n")
	assert.Len(t, lines, 3, "should respect limit")
}

func TestRunNplusone_EmptyPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "readme.txt", "hello")

	raw, _ := json.Marshal(nplusoneInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runNplusone(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No N+1 query patterns detected", out.Content)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
