package pathutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitBranchOutsideRepo(t *testing.T) {
	assert.Empty(t, GitBranch(t.Context(), t.TempDir()), "non-git dir has no branch")
}

func TestPathWithBranchOutsideRepo(t *testing.T) {
	plain := t.TempDir()
	assert.Equal(t, ShortPath(plain), PathWithBranch(plain), "no branch suffix outside git")
}

func TestResolveGitDir(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, filepath.Join(dir, ".git"), resolveGitDir(dir))

	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: ../modules/sub\n"), 0o644))
	assert.Equal(t, filepath.Join(filepath.Dir(dir), "modules", "sub"), resolveGitDir(dir))

	abs := filepath.Join(t.TempDir(), "modules", "sub")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+abs+"\n"), 0o644))
	assert.Equal(t, abs, resolveGitDir(dir))
}

func TestBranchState(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, "missing", branchState(dir))

	gitDir := filepath.Join(dir, ".git")
	require.NoError(t, os.Mkdir(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644))
	assert.Equal(t, "ref: refs/heads/main", branchState(dir))

	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("abc123\n"), 0o644))
	assert.Equal(t, "abc123", branchState(dir))
}

func TestWatchBranchFiresOnChange(t *testing.T) {
	dir := t.TempDir()
	// PathWithBranch shells out to real git, so the fixture must be a real repo.
	require.NoError(t, exec.CommandContext(t.Context(), "git", "-C", dir, "init", "-q").Run())
	gitDir := filepath.Join(dir, ".git")

	labels := make(chan string, 1)
	WatchBranch(dir, func(label string) {
		labels <- label
	})

	// Wait for the first poll to establish baseline, then change HEAD.
	time.Sleep(1100 * time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/feature\n"), 0o644))

	select {
	case label := <-labels:
		assert.Contains(t, label, "feature")
	case <-time.After(5 * time.Second):
		require.Fail(t, "timeout waiting for branch label update")
	}
}
