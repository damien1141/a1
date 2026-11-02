package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagerNilSafety(t *testing.T) {
	var m *Manager
	_, err := m.Create(context.Background())
	assert.Error(t, err)

	err = m.Rollback(context.Background(), "name")
	assert.Error(t, err)

	_, err = m.List(context.Background())
	assert.Error(t, err)

	err = m.Delete(context.Background(), "name")
	assert.Error(t, err)
}

func TestManagerCreateAndList(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, dir, "main.txt", "hello")
	testExecGit(t, dir, "add", "main.txt")
	testExecGit(t, dir, "commit", "-m", "initial")

	mgr := NewManager(dir)
	branchBefore, err := currentBranch(context.Background(), dir)
	require.NoError(t, err)

	name, err := mgr.Create(context.Background())
	require.NoError(t, err)
	assert.Contains(t, name, snapshotBranchPrefix)

	// Create must never switch branches: HEAD stays where it was.
	branch, err := currentBranch(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, branchBefore, branch)

	names, err := mgr.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, names, 1)
	assert.Contains(t, names[0], name)
}

// TestManagerRollbackRestoresFullWorktree is the core Phase 0 guarantee: the
// snapshot captures uncommitted WIP and rollback restores it exactly, without
// switching branches.
func TestManagerRollbackRestoresFullWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping rollback test in short mode")
	}
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, dir, "committed.txt", "committed")
	writeFile(t, dir, "tracked-wip.txt", "base")
	testExecGit(t, dir, "add", "committed.txt", "tracked-wip.txt")
	testExecGit(t, dir, "commit", "-m", "initial")

	// Pre-snapshot state: one tracked file modified but NOT committed, one
	// staged-but-never-committed new file, one untracked file. The old
	// branch-at-HEAD snapshot lost all three on rollback.
	writeFile(t, dir, "tracked-wip.txt", "uncommitted edits")
	writeFile(t, dir, "staged.txt", "staged work")
	writeFile(t, dir, "untracked.txt", "untracked work")
	testExecGit(t, dir, "add", "staged.txt")

	mgr := NewManager(dir)
	branchBefore, err := currentBranch(context.Background(), dir)
	require.NoError(t, err)

	name, err := mgr.Create(context.Background())
	require.NoError(t, err)

	// Candidate mutations after the snapshot: overwrite tracked WIP, delete
	// a tracked file, create debris.
	writeFile(t, dir, "tracked-wip.txt", "clobbered by candidate")
	require.NoError(t, os.Remove(filepath.Join(dir, "committed.txt")))
	writeFile(t, dir, "debris.txt", "agent-created after snapshot")

	require.NoError(t, mgr.Rollback(context.Background(), name))

	// Pre-snapshot WIP restored byte-exact.
	assert.Equal(t, "uncommitted edits", readFile(t, dir, "tracked-wip.txt"))
	assert.Equal(t, "staged work", readFile(t, dir, "staged.txt"))
	assert.Equal(t, "untracked work", readFile(t, dir, "untracked.txt"))
	// Deleted tracked file recreated.
	assert.Equal(t, "committed", readFile(t, dir, "committed.txt"))
	// Post-snapshot debris removed.
	assert.Equal(t, "", readFile(t, dir, "debris.txt"))

	// Rollback never moved HEAD.
	branch, err := currentBranch(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, branchBefore, branch)
}

// TestManagerRollbackRemovesNestedDebris covers agent-created files in
// agent-created directories.
func TestManagerRollbackRemovesNestedDebris(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping rollback test in short mode")
	}
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, dir, "main.txt", "hello")
	testExecGit(t, dir, "add", "main.txt")
	testExecGit(t, dir, "commit", "-m", "initial")

	mgr := NewManager(dir)
	name, err := mgr.Create(context.Background())
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "pkg", "sub"), 0o755))
	writeFile(t, dir, "pkg/sub/new.go", "package pkg")

	require.NoError(t, mgr.Rollback(context.Background(), name))
	assert.Equal(t, "", readFile(t, dir, "pkg/sub/new.go"))
}

// TestManagerLegacyBranchSnapshot proves old branch-based snapshots are
// still resolvable for rollback and delete.
func TestManagerLegacyBranchSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping rollback test in short mode")
	}
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, dir, "main.txt", "hello")
	testExecGit(t, dir, "add", "main.txt")
	testExecGit(t, dir, "commit", "-m", "initial")

	// Simulate a pre-upgrade snapshot: a plain branch at HEAD.
	legacy := snapshotBranchPrefix + "20250101-000000"
	testExecGit(t, dir, "branch", legacy)

	mgr := NewManager(dir)
	names, err := mgr.List(context.Background())
	require.NoError(t, err)
	assert.Contains(t, names, legacy)

	writeFile(t, dir, "main.txt", "changed")
	require.NoError(t, mgr.Rollback(context.Background(), legacy))
	assert.Equal(t, "hello", readFile(t, dir, "main.txt"))

	require.NoError(t, mgr.Delete(context.Background(), legacy))
	names, err = mgr.List(context.Background())
	require.NoError(t, err)
	assert.NotContains(t, names, legacy)
}

func TestManagerDelete(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, dir, "main.txt", "hello")
	testExecGit(t, dir, "add", "main.txt")
	testExecGit(t, dir, "commit", "-m", "initial")

	mgr := NewManager(dir)
	name, err := mgr.Create(context.Background())
	require.NoError(t, err)
	require.NoError(t, mgr.Delete(context.Background(), name))

	names, err := mgr.List(context.Background())
	require.NoError(t, err)
	assert.NotContains(t, names, name)

	// Deleting an unknown snapshot is an error, not a panic.
	assert.Error(t, mgr.Delete(context.Background(), "harness-snapshot-nonexistent"))
}

// TestManagerCreateWithoutIdentity covers fresh repos with no git identity
// configured (CI containers): Create falls back to an internal identity.
func TestManagerCreateWithoutIdentity(t *testing.T) {
	dir := t.TempDir()
	testExecGit(t, dir, "init")
	// Deliberately no user.name / user.email configured.
	// Force empty identity even if the machine has global git config.
	testExecGit(t, dir, "config", "user.email", "")
	writeFile(t, dir, "main.txt", "hello")

	mgr := NewManager(dir)
	_, err := mgr.Create(context.Background())
	// Either works (fallback identity) or fails for an unrelated reason;
	// a missing-identity failure is the regression this test guards.
	if err != nil {
		assert.False(t, isIdentityError(err), "identity fallback did not fire: %v", err)
	}
}

func TestSnapshotNaming(t *testing.T) {
	name := snapshotBranchPrefix + time.Now().Format("20060102-150405")
	assert.Contains(t, name, snapshotBranchPrefix)
	assert.Regexp(t, `^\d{8}-\d{6}$`, strings.TrimPrefix(name, snapshotBranchPrefix))
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	testExecGit(t, dir, "init")
	testExecGit(t, dir, "config", "user.email", "test@test.com")
	testExecGit(t, dir, "config", "user.name", "Test")
	testExecGit(t, dir, "config", "commit.gpgsign", "false")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return string(data)
}

func testExecGit(t *testing.T, cwd string, args ...string) {
	t.Helper()
	_, err := execGit(context.Background(), cwd, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
}
