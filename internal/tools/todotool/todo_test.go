package todotool

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTodoTool_Definition(t *testing.T) {
	tool := TodoTool()
	assert.Equal(t, "todo", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "TODO")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunTodo_EmptyRepo(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initGitRepo(t, root)

	raw, _ := json.Marshal(todoInput{})
	out, err := runTodo(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No TODO/FIXME/HACK/XXX/OPTIMIZE/BUG markers found", out.Content)
	assert.Contains(t, out.Detail, "0 markers")
}

func TestRunTodo_FindsMarkers(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initGitRepo(t, root)
	writeFile(t, root, "main.go", "package main\n// TODO: fix this\nfunc main() {}\n")
	writeFile(t, root, "lib.go", "package main\n// FIXME: bug here\n// HACK: quick fix\n")

	raw, _ := json.Marshal(todoInput{Path: "."})
	out, err := runTodo(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "TODO")
	assert.Contains(t, out.Content, "FIXME")
	assert.Contains(t, out.Content, "HACK")
	assert.Contains(t, out.Detail, "markers")
}

func TestRunTodo_WithGlob(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initGitRepo(t, root)
	writeFile(t, root, "main.go", "package main\n// TODO: go thing\n")
	writeFile(t, root, "notes.txt", "TODO: not code\n")

	raw, _ := json.Marshal(todoInput{Path: ".", Glob: "*.go"})
	out, err := runTodo(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "TODO")
	assert.NotContains(t, out.Content, "notes.txt")
}

func TestRunTodo_Limit(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initGitRepo(t, root)
	for i := 0; i < 5; i++ {
		writeFile(t, root, fmt.Sprintf("file%d.go", i), "package main\n// TODO: task\n")
	}

	raw, _ := json.Marshal(todoInput{Path: ".", Limit: 3})
	out, err := runTodo(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "limit reached")
}

func TestRunTodo_NotARepo(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	raw, _ := json.Marshal(todoInput{Path: "."})
	out, err := runTodo(t.Context(), raw)
	require.Error(t, err)
	assert.Empty(t, out.Content)
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "init")
	run(t, dir, "config", "user.email", "test@example.com")
	run(t, dir, "config", "user.name", "Test User")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s\n%s", strings.Join(args, " "), err, string(out))
	}
}
