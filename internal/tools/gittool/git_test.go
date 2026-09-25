package gittool

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitTool_Definition(t *testing.T) {
	tool := GitTool()
	assert.Equal(t, "git", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "git")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunGit_RequiresSubcommand(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initGitRepo(t, root)

	raw, _ := json.Marshal(gitInput{})
	out, err := runGit(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subcommand is required")
	assert.Empty(t, out.Content)
}

func TestRunGit_Log_EmptyRepo(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initGitRepo(t, root)

	raw, _ := json.Marshal(gitInput{Subcommand: "log"})
	out, err := runGit(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "(no commits)", out.Content)
	assert.Contains(t, out.Detail, "git log")
}

func TestRunGit_Log_WithCommits(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initGitRepo(t, root)
	writeFile(t, root, "main.go", "package main\n")
	run(t, root, "add", "main.go")
	run(t, root, "commit", "-m", "init")

	raw, _ := json.Marshal(gitInput{Subcommand: "log", Limit: 10})
	out, err := runGit(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "init")
	assert.Contains(t, out.Detail, "git log")
}

func TestRunGit_Blame_RequiresPath(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initGitRepo(t, root)

	raw, _ := json.Marshal(gitInput{Subcommand: "blame"})
	out, err := runGit(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
	assert.Empty(t, out.Content)
}

func TestRunGit_Blame_File(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initGitRepo(t, root)
	writeFile(t, root, "main.go", "package main\n")
	run(t, root, "add", "main.go")
	run(t, root, "commit", "-m", "init")

	raw, _ := json.Marshal(gitInput{Subcommand: "blame", Path: "main.go"})
	out, err := runGit(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "main.go:1#")
	assert.Contains(t, out.Detail, "git blame main.go")
}

func TestRunGit_MergeTree_RequiresBranches(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initGitRepo(t, root)

	raw, _ := json.Marshal(gitInput{Subcommand: "merge-tree"})
	out, err := runGit(t.Context(), raw)
	require.Error(t, err)
	assert.Empty(t, out.Content)
}

func TestRunGit_MergeTree_Clean(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initGitRepo(t, root)
	writeFile(t, root, "main.go", "package main\n")
	run(t, root, "add", "main.go")
	run(t, root, "commit", "-m", "init")
	run(t, root, "branch", "feat")

	raw, _ := json.Marshal(gitInput{Subcommand: "merge-tree", Branch: "HEAD", Branch2: "feat"})
	out, err := runGit(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Clean merge")
	assert.Contains(t, out.Detail, "git merge-tree HEAD feat (clean)")
}

func TestRunGit_NotARepo(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	raw, _ := json.Marshal(gitInput{Subcommand: "log"})
	out, err := runGit(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
	assert.Empty(t, out.Content)
}

func TestRunGit_UnsupportedSubcommand(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initGitRepo(t, root)

	raw, _ := json.Marshal(gitInput{Subcommand: "diff"})
	out, err := runGit(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unsupported git subcommand "diff"`)
	assert.Empty(t, out.Content)
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "init")
	run(t, dir, "config", "user.email", "test@example.com")
	run(t, dir, "config", "user.name", "Test User")
	// Create an empty global config to avoid inheriting host credential helpers.
	emptyCfg := filepath.Join(dir, ".gitconfig-empty")
	require.NoError(t, os.WriteFile(emptyCfg, []byte(""), 0o644))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"HOME="+dir,
		"GIT_CONFIG_GLOBAL="+filepath.Join(dir, ".gitconfig-empty"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s\n%s", strings.Join(args, " "), err, string(out))
	}
}
