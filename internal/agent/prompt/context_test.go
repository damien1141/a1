package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadContextFileFromDirPrefersAGENTS(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CLAUDE.md"), "claude rules")
	mustWrite(t, filepath.Join(dir, "AGENTS.md"), "agents rules")

	got := loadContextFileFromDir(dir)
	require.NotNil(t, got, "expected a context file")
	require.Equal(t, "agents rules", got.Content, "prefer AGENTS.md")
}

func TestLoadContextFileFromDirFallsBackToCLAUDE(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CLAUDE.md"), "claude only")

	got := loadContextFileFromDir(dir)
	require.NotNil(t, got)
	require.Equal(t, "claude only", got.Content)
}

func TestLoadProjectContextFilesOrder(t *testing.T) {
	root := t.TempDir()
	agentDir := filepath.Join(root, "agent")
	mid := filepath.Join(root, "proj")
	cwd := filepath.Join(mid, "nested")
	mustMkdir(t, agentDir)
	mustMkdir(t, cwd)

	mustWrite(t, filepath.Join(agentDir, "AGENTS.md"), "global")
	mustWrite(t, filepath.Join(mid, "AGENTS.md"), "mid")
	mustWrite(t, filepath.Join(cwd, "CLAUDE.md"), "cwd")

	files := loadProjectContextFiles(cwd, agentDir)
	require.Len(t, files, 3)
	want := []string{"global", "mid", "cwd"}
	for i, w := range want {
		require.Equal(t, w, files[i].Content, "files[%d]", i)
	}
}

func TestLoadProjectContextFilesDedupesAgentDir(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "AGENTS.md"), "once")

	files := loadProjectContextFiles(dir, dir)
	require.Len(t, files, 1)
}

func TestFormatProjectContext(t *testing.T) {
	got := formatProjectContext([]ContextFile{
		{Path: "/tmp/AGENTS.md", Content: "use gofmt"},
	})
	require.Contains(t, got, "<project_context>")
	require.Contains(t, got, `<project_instructions path="/tmp/AGENTS.md">`)
	require.Contains(t, got, "use gofmt")
	require.Empty(t, formatProjectContext(nil), "empty files should yield empty string")
}

func TestBuildIncludesContext(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "AGENTS.md"), "always use tabs")

	ctx := formatProjectContext(loadProjectContextFiles(dir, t.TempDir()))
	require.Contains(t, ctx, "always use tabs")
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(path, 0o755))
}
