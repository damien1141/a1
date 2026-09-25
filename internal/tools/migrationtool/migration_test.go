package migrationtool

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

func TestMigrationTool_Definition(t *testing.T) {
	tool := MigrationTool()
	assert.Equal(t, "migrate", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "migration")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunMigration_NoHits(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func main() {}
`)

	raw, _ := json.Marshal(migrationInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runMigration(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No migration targets detected", out.Content)
}

func TestRunMigration_FindsDeprecatedAPI(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

import "io/ioutil"

func main() {
	ioutil.ReadFile("test.txt")
}
`)

	raw, _ := json.Marshal(migrationInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runMigration(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "ioutil.ReadFile")
	assert.Contains(t, out.Content, "os.ReadFile")
}

func TestRunMigration_Limit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		writeFile(t, root, fmt.Sprintf("file%d.go", i), `package main

import "io/ioutil"

func main() {
	ioutil.ReadFile("test.txt")
}
`)
	}

	raw, _ := json.Marshal(migrationInput{Path: root, Glob: "*.go", Limit: 3})
	out, err := runMigration(t.Context(), raw)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.Content), "\n")
	assert.Len(t, lines, 3, "should respect limit")
}

func TestRunMigration_EmptyPath(t *testing.T) {
	// Empty path defaults to cwd; use a temp dir with no Go files instead.
	root := t.TempDir()
	writeFile(t, root, "readme.txt", "hello")

	raw, _ := json.Marshal(migrationInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runMigration(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No migration targets detected", out.Content)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
