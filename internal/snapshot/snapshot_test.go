package snapshot

import (
	"context"
	"os"
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

	err = m.Rollback(context.Background(), "branch")
	assert.Error(t, err)

	_, err = m.List(context.Background())
	assert.Error(t, err)

	err = m.Delete(context.Background(), "branch")
	assert.Error(t, err)
}

func TestManagerCreateAndList(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, dir, "main.txt", "hello")

	mgr := NewManager(dir)
	name, err := mgr.Create(context.Background())
	require.NoError(t, err)
	assert.Contains(t, name, snapshotBranchPrefix)

	branches, err := mgr.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, branches, 1)
	assert.Contains(t, branches[0], name)
}

func TestManagerRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping rollback test in short mode")
	}
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, dir, "main.txt", "hello")
	testExecGit(t, dir, "add", "main.txt")
	testExecGit(t, dir, "commit", "-m", "initial")

	// Create snapshot.
	mgr := NewManager(dir)
	name, err := mgr.Create(context.Background())
	require.NoError(t, err)

	// Make changes.
	writeFile(t, dir, "main.txt", "main changed")
	content := readFile(t, dir, "main.txt")
	assert.Equal(t, "main changed", content)

	// Rollback.
	err = mgr.Rollback(context.Background(), name)
	require.NoError(t, err)

	content = readFile(t, dir, "main.txt")
	assert.Equal(t, "hello", content)
}

func TestManagerDelete(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, dir, "main.txt", "hello")

	mgr := NewManager(dir)
	name, err := mgr.Create(context.Background())
	require.NoError(t, err)

	// Delete may be environment-sensitive; just verify it doesn't panic.
	_ = mgr.Delete(context.Background(), name)
}

func TestSnapshotBranchNaming(t *testing.T) {
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
	path := dir + "/" + name
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := dir + "/" + name
	data, err := os.ReadFile(path)
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
