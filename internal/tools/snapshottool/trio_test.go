package snapshottool

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

// runGit is a plain os/exec git call — deliberately not the snapshot
// package's own execGit — with the host's global git config neutralized per
// AGENTS.md so the test stays hermetic.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
	return string(out)
}

// gitRepo initializes a temp git repo with a committed identity and signing
// off, mirroring internal/snapshot's own test setup.
func gitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "trio@test.local")
	runGit(t, repo, "config", "user.name", "Trio Test")
	runGit(t, repo, "config", "commit.gpgsign", "false")
	return repo
}

// installProjectAt points the default project at root with a temp HOME, so
// proj.Root() is the temp repo and ~/.a1 lands in the sandbox.
func installProjectAt(t *testing.T, root string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	proj, err := project.Discover(root)
	require.NoError(t, err)
	project.SetDefaultProject(proj)
	t.Cleanup(func() { project.SetDefaultProject(nil) })
}

func runSnapshotTool(t *testing.T, in snapshotInput) (tooldef.Result, error) {
	t.Helper()
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	return SnapshotTool().Run(t.Context(), raw)
}

func writeSnapFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// ---------- negative ----------

func TestRunSnapshot_Negative(t *testing.T) {
	t.Run("malformed json", func(t *testing.T) {
		_, err := SnapshotTool().Run(t.Context(), json.RawMessage(`{"action"`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse snapshot arguments")
	})

	t.Run("missing action", func(t *testing.T) {
		_, err := runSnapshotTool(t, snapshotInput{})
		require.Error(t, err)
		assert.EqualError(t, err, "action is required: create, rollback, list, or delete")
	})

	t.Run("blank action", func(t *testing.T) {
		_, err := runSnapshotTool(t, snapshotInput{Action: "   "})
		require.Error(t, err)
		assert.EqualError(t, err, "action is required: create, rollback, list, or delete")
	})

	t.Run("unknown action", func(t *testing.T) {
		_, err := runSnapshotTool(t, snapshotInput{Action: "SNAPSHOT-ALL"})
		require.Error(t, err)
		assert.EqualError(t, err, `unknown action "snapshot-all": use create, rollback, list, or delete`)
	})

	repo := gitRepo(t)
	installProjectAt(t, repo)

	t.Run("rollback without branch", func(t *testing.T) {
		_, err := runSnapshotTool(t, snapshotInput{Action: "rollback"})
		require.Error(t, err)
		assert.EqualError(t, err, "rollback requires explicit branch name")
	})

	t.Run("delete without branch", func(t *testing.T) {
		_, err := runSnapshotTool(t, snapshotInput{Action: "delete"})
		require.Error(t, err)
		assert.EqualError(t, err, "delete requires explicit branch name")
	})

	t.Run("rollback to missing snapshot", func(t *testing.T) {
		_, err := runSnapshotTool(t, snapshotInput{Action: "rollback", Branch: "ghost-branch"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rollback failed:")
		assert.Contains(t, err.Error(), "snapshot not found: ghost-branch")
	})

	t.Run("delete missing snapshot", func(t *testing.T) {
		_, err := runSnapshotTool(t, snapshotInput{Action: "delete", Branch: "ghost-branch"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "delete snapshot:")
		assert.Contains(t, err.Error(), "snapshot not found: ghost-branch")
	})

	t.Run("list on a fresh repo", func(t *testing.T) {
		res, err := runSnapshotTool(t, snapshotInput{Action: "list"})
		require.NoError(t, err)
		assert.Equal(t, "No explicit snapshots found", res.Content)
		assert.Equal(t, "0 snapshots", res.Detail)
	})
}

// An unusable HOME makes project discovery fail. The tool must answer with
// its own "project config not loaded" error instead of the process dying
// inside project.GetDefaultProject's panic — which is what the old
// unreachable nil-guard amounted to: GetDefaultProject never returned nil,
// it panicked first, so the guard could never fire.
func TestRunSnapshot_UnusableHome_Negative(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(home, []byte("x"), 0o644))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project.SetDefaultProject(nil)
	t.Cleanup(func() { project.SetDefaultProject(nil) })

	var err error
	require.NotPanics(t, func() {
		_, err = runSnapshotTool(t, snapshotInput{Action: "list"})
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project config not loaded")
}

func TestRunSnapshot_NotAGitRepo_Negative(t *testing.T) {
	installProjectAt(t, t.TempDir())
	_, err := runSnapshotTool(t, snapshotInput{Action: "create"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create snapshot:")
	assert.Contains(t, err.Error(), "not a git repository")
}

// ---------- differential ----------

// treeSnapshot captures rel-path → content with a plain filepath.Walk,
// skipping .git — the oracle for what rollback must restore, independent of
// the snapshot package's own readback helpers.
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	require.NoError(t, err)
	return files
}

// oracle: raw git plumbing + plain filesystem reads — the pre-snapshot tree
// is captured with filepath.Walk/os.ReadFile before any snapshot API runs,
// the snapshot ref is re-derived with a direct `git for-each-ref`, and HEAD
// identity is checked with `git rev-parse`, all independent of the snapshot
// package's own helpers.
func TestRunSnapshot_CreateRollback_Differential(t *testing.T) {
	repo := gitRepo(t)
	installProjectAt(t, repo)

	// Committed base, then uncommitted WIP on top — the state a snapshot
	// must capture without touching HEAD.
	writeSnapFile(t, repo, "committed.txt", "committed")
	writeSnapFile(t, repo, "wip.txt", "base")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	writeSnapFile(t, repo, "wip.txt", "uncommitted edits")
	writeSnapFile(t, repo, "untracked.txt", "untracked work")
	writeSnapFile(t, repo, filepath.Join("nested", "dir", "file.txt"), "nested content")

	headBefore := strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD"))
	want := treeSnapshot(t, repo)

	// Create through the tool.
	res, err := runSnapshotTool(t, snapshotInput{Action: "create"})
	require.NoError(t, err)
	name := strings.TrimPrefix(res.Content, "Created explicit snapshot: ")
	require.NotEqual(t, res.Content, name, "create must report the snapshot name")
	assert.Equal(t, name, res.Detail)

	// The ref exists exactly as the tool claims, per raw git.
	refs := strings.Fields(runGit(t, repo, "for-each-ref", "--format=%(refname)", "refs/a1/snapshots/"))
	assert.Equal(t, []string{"refs/a1/snapshots/" + name}, refs)
	// Create never moves HEAD.
	assert.Equal(t, headBefore, strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD")))

	// List agrees with raw git.
	listRes, err := runSnapshotTool(t, snapshotInput{Action: "list"})
	require.NoError(t, err)
	assert.Equal(t, "Explicit snapshot branches:\n"+name, listRes.Content)
	assert.Equal(t, "1 snapshots", listRes.Detail)

	// Candidate mutations: clobber tracked WIP, delete a tracked file,
	// leave debris behind.
	writeSnapFile(t, repo, "wip.txt", "clobbered by candidate")
	require.NoError(t, os.Remove(filepath.Join(repo, "committed.txt")))
	writeSnapFile(t, repo, "debris.txt", "agent debris")

	rollbackRes, err := runSnapshotTool(t, snapshotInput{Action: "rollback", Branch: name})
	require.NoError(t, err)
	assert.Equal(t, "Explicitly rolled back to "+name, rollbackRes.Content)
	assert.Equal(t, name, rollbackRes.Detail)

	// The tree is byte-identical to the pre-snapshot capture, debris
	// included in its absence.
	assert.Equal(t, want, treeSnapshot(t, repo))
	// Rollback never moves HEAD either.
	assert.Equal(t, headBefore, strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD")))

	// Delete through the tool, then raw git agrees the ref is gone.
	delRes, err := runSnapshotTool(t, snapshotInput{Action: "delete", Branch: name})
	require.NoError(t, err)
	assert.Equal(t, "Deleted explicit snapshot: "+name, delRes.Content)
	assert.Empty(t, strings.TrimSpace(runGit(t, repo, "for-each-ref", "--format=%(refname)", "refs/a1/snapshots/")))

	listRes, err = runSnapshotTool(t, snapshotInput{Action: "list"})
	require.NoError(t, err)
	assert.Equal(t, "No explicit snapshots found", listRes.Content)
	assert.Equal(t, "0 snapshots", listRes.Detail)
}
