package snapshot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// trioGit runs git in cwd and requires success (distinct from testExecGit in
// snapshot_test.go so both files stay independent).
func trioGit(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	out, err := execGit(t.Context(), cwd, args...)
	require.NoError(t, err, "git %v", args)
	return out
}

// trioInitRepo builds a hermetic temp repo: global config neutralized, local
// identity, no signing.
func trioInitRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	dir := t.TempDir()
	trioGit(t, dir, "init")
	trioGit(t, dir, "config", "user.email", "trio@test.local")
	trioGit(t, dir, "config", "user.name", "Trio Test")
	trioGit(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func trioWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755), "fixture dir for %s", rel)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644), "fixture file %s", rel)
}

// trioTree enumerates every file under root except .git internals: slash
// relative path → exact bytes. This is the independent readback — plain
// os.ReadFile over a filepath.WalkDir walk, none of the package's own APIs.
func trioTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	require.NoError(t, err)
	return out
}

// trioIgnored matches the fixture's .gitignore rule ("ignored.log" at any
// depth); those files must never enter a snapshot.
func trioIgnored(rel string) bool {
	return filepath.Base(rel) == "ignored.log"
}

// trioFixture builds the working-tree state a snapshot must capture: a base
// commit, then uncommitted WIP on a tracked file, a staged new file, untracked
// files (root + nested), and ignored noise.
func trioFixture(t *testing.T) string {
	t.Helper()
	dir := trioInitRepo(t)
	for rel, content := range map[string]string{
		".gitignore":        "ignored.log\n",
		"committed.txt":     "committed base\n",
		"tracked-wip.txt":   "base\n",
		"nested/deep/a.txt": "deep a", // no trailing newline on purpose
		"sub/b.txt":         "b\n",
	} {
		trioWrite(t, dir, rel, content)
	}
	trioGit(t, dir, "add", ".gitignore", "committed.txt", "tracked-wip.txt", "nested/deep/a.txt", "sub/b.txt")
	trioGit(t, dir, "commit", "-m", "initial")

	trioWrite(t, dir, "tracked-wip.txt", "uncommitted edits\n")
	trioWrite(t, dir, "staged.txt", "staged work\n")
	trioGit(t, dir, "add", "staged.txt")
	trioWrite(t, dir, "untracked.txt", "untracked work\n")
	trioWrite(t, dir, "newdir/c.txt", "untracked nested\n")
	trioWrite(t, dir, "ignored.log", "root noise\n")
	trioWrite(t, dir, "nested/ignored.log", "nested noise\n")
	return dir
}

// oracle: filepath.WalkDir + os.ReadFile enumeration of the fixture (the
// expected tree = every visible file except the ignored ones) compared
// path-for-path and byte-for-byte against the snapshot's git tree, read back
// with plain `git ls-tree` / `git show` plumbing. The ref name format is
// pinned against a hand-written pattern.
func TestCreateSnapshotTreeVsWalkDirReference_Differential(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git-plumbing differential in short mode")
	}
	dir := trioFixture(t)

	want := map[string]string{}
	for rel, content := range trioTree(t, dir) {
		if !trioIgnored(rel) {
			want[rel] = content
		}
	}
	require.Len(t, want, 8, "reference tree: 8 non-ignored files, got %v", want)
	require.Contains(t, want, "untracked.txt", "untracked files must be captured")
	require.Contains(t, want, "staged.txt", "staged files must be captured")
	require.NotContains(t, want, "ignored.log", "ignored files must be excluded")

	mgr := NewManager(dir)
	name, err := mgr.Create(t.Context())
	require.NoError(t, err)

	assert.Regexp(t, `^harness-snapshot-\d{8}-\d{6}$`, name, "snapshot name format")
	ref := snapshotRefNamespace + name
	trioGit(t, dir, "rev-parse", "--verify", ref) // the ref must exist in git's own db

	got := map[string]string{}
	for _, path := range splitLines(trioGit(t, dir, "ls-tree", "-r", "--name-only", ref)) {
		if path == "" {
			continue
		}
		got[path] = trioGit(t, dir, "show", ref+":"+path)
	}
	assert.Equal(t, want, got, "snapshot tree must equal the WalkDir reference, byte for byte")
}

// oracle: the WalkDir reference from snapshot time — after candidate
// mutations (clobber, delete, debris at root and in new dirs) and a Rollback,
// the entire visible directory (ignored files included: they are never
// touched) must again equal the snapshot-time map, and HEAD must not have
// moved.
func TestRollbackRestoresWalkDirReference_Differential(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git-plumbing differential in short mode")
	}
	dir := trioFixture(t)

	mgr := NewManager(dir)
	name, err := mgr.Create(t.Context())
	require.NoError(t, err)

	want := trioTree(t, dir)
	require.Len(t, want, 10, "full reference: 8 snapshot files + 2 ignored, got %v", want)
	headBefore := trioGit(t, dir, "rev-parse", "HEAD")
	branchBefore, err := currentBranch(t.Context(), dir)
	require.NoError(t, err)

	// Candidate mutations: clobber tracked WIP, delete a tracked file, drop
	// debris at the root and in brand-new nested dirs, modify a deep file.
	trioWrite(t, dir, "tracked-wip.txt", "clobbered by candidate\n")
	trioWrite(t, dir, "nested/deep/a.txt", "mangled\n")
	require.NoError(t, os.Remove(filepath.Join(dir, "committed.txt")))
	trioWrite(t, dir, "debris.txt", "agent debris\n")
	trioWrite(t, dir, "fresh/dir/leftover.txt", "nested debris\n")
	require.NoError(t, os.Remove(filepath.Join(dir, "untracked.txt")))

	require.NoError(t, mgr.Rollback(t.Context(), name))

	assert.Equal(t, want, trioTree(t, dir), "post-rollback tree must equal the snapshot-time WalkDir reference")

	assert.Equal(t, headBefore, trioGit(t, dir, "rev-parse", "HEAD"), "rollback must never move HEAD")
	branchAfter, err := currentBranch(t.Context(), dir)
	require.NoError(t, err)
	assert.Equal(t, branchBefore, branchAfter, "rollback must never switch branches")
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
