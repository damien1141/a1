package gittool

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// rawGit is the independent oracle: a direct exec.Command("git", ...) with
// the argv each read mode documents, run in the same hermetic repo (same
// neutralized config and identity env) the tool runs in.
func rawGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

func writeGitFile(t *testing.T, dir, name, content string) {
	t.Helper()
	fp := filepath.Join(dir, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(fp), 0o755))
	require.NoError(t, os.WriteFile(fp, []byte(content), 0o644))
}

// gitOracleRepo builds a repo with a known three-commit history (each commit
// authored through the tool's own add/commit modes) and a mixed working-tree
// state: staged addition, staged deletion, unstaged modification, untracked
// file. The staging actions that need raw git (the deletion) use the oracle.
func gitOracleRepo(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)

	writeGitFile(t, dir, "a.txt", "a1\n")
	writeGitFile(t, dir, "gone.txt", "g1\n")
	writeGitFile(t, dir, "mod.txt", "m1\n")
	_, err := runInRepo(t, dir, `{"mode":"add","files":["a.txt","gone.txt","mod.txt"]}`)
	require.NoError(t, err)
	_, err = runInRepo(t, dir, `{"mode":"commit","message":"seed"}`)
	require.NoError(t, err)

	writeGitFile(t, dir, "a.txt", "a2\n")
	_, err = runInRepo(t, dir, `{"mode":"commit","message":"second change","files":["a.txt"]}`)
	require.NoError(t, err)

	writeGitFile(t, dir, "sub/c.txt", "c1\n")
	_, err = runInRepo(t, dir, `{"mode":"add","files":["sub/c.txt"]}`)
	require.NoError(t, err)
	_, err = runInRepo(t, dir, `{"mode":"commit","message":"third touches sub"}`)
	require.NoError(t, err)

	writeGitFile(t, dir, "mod.txt", "m2\n")
	writeGitFile(t, dir, "new.txt", "n1\n")
	writeGitFile(t, dir, "untracked.txt", "u1\n")
	rawGit(t, dir, "add", "new.txt")
	rawGit(t, dir, "rm", "-q", "gone.txt")
	return dir
}

// oracle: os/exec git equivalence — the status mode's output is compared
// byte-for-byte with a direct `git status --porcelain=v1 -b`, and the
// porcelain entries are cross-checked against hand-derived XY codes from the
// known fixture state (what was staged, deleted, modified, left untracked).
func TestGitTool_StatusVsDirectGit_Differential(t *testing.T) {
	dir := gitOracleRepo(t)

	res, err := runInRepo(t, dir, `{"mode":"status"}`)
	require.NoError(t, err)
	assert.Equal(t, rawGit(t, dir, "status", "--porcelain=v1", "-b"), res.Content)
	assert.Equal(t, "status", res.Detail)

	var entries []string
	for _, line := range strings.Split(strings.TrimRight(res.Content, "\n"), "\n") {
		if !strings.HasPrefix(line, "## ") {
			entries = append(entries, line)
		}
	}
	sort.Strings(entries)
	assert.Equal(t, []string{
		" M mod.txt",       // unstaged modification of a tracked file
		"?? untracked.txt", // never added
		"A  new.txt",       // staged addition
		"D  gone.txt",      // staged deletion (index and worktree)
	}, entries, "porcelain codes must match the fixture state")
}

// oracle: os/exec git equivalence — log with oneline, the default count, and
// a pathspec filter is compared byte-for-byte with the direct git calls the
// mode builds, and the newest-first message order must match the sequence of
// messages the fixture committed.
func TestGitTool_LogVsDirectGit_Differential(t *testing.T) {
	dir := gitOracleRepo(t)

	t.Run("oneline with n", func(t *testing.T) {
		res, err := runInRepo(t, dir, `{"mode":"log","oneline":true,"n":2}`)
		require.NoError(t, err)
		assert.Equal(t, rawGit(t, dir, "log", "-n", "2", "--oneline"), res.Content)

		lines := strings.Split(strings.TrimRight(res.Content, "\n"), "\n")
		require.Len(t, lines, 2)
		assert.True(t, strings.HasSuffix(lines[0], "third touches sub"), "newest commit first")
		assert.True(t, strings.HasSuffix(lines[1], "second change"))
		assert.NotContains(t, res.Content, " seed", "n=2 must cut the oldest commit")
	})

	t.Run("default count is 20", func(t *testing.T) {
		res, err := runInRepo(t, dir, `{"mode":"log"}`)
		require.NoError(t, err)
		assert.Equal(t, rawGit(t, dir, "log", "-n", "20"), res.Content)
		assert.Equal(t, "log", res.Detail)
	})

	t.Run("pathspec filter", func(t *testing.T) {
		res, err := runInRepo(t, dir, `{"mode":"log","oneline":true,"path":"sub"}`)
		require.NoError(t, err)
		assert.Equal(t, rawGit(t, dir, "log", "-n", "20", "--oneline", "--", "sub"), res.Content)

		lines := strings.Split(strings.TrimRight(res.Content, "\n"), "\n")
		require.Len(t, lines, 1, "only the commit that touched sub/")
		assert.True(t, strings.HasSuffix(lines[0], "third touches sub"))
	})
}

// oracle: os/exec git equivalence — diff (staged and unstaged), show, and
// branch listing match their direct git invocations, with the diff hunks
// cross-checked against the old/new contents the fixture wrote.
func TestGitTool_DiffShowBranchVsDirectGit_Differential(t *testing.T) {
	dir := gitOracleRepo(t)

	t.Run("unstaged diff", func(t *testing.T) {
		res, err := runInRepo(t, dir, `{"mode":"diff"}`)
		require.NoError(t, err)
		assert.Equal(t, rawGit(t, dir, "diff"), res.Content)
		assert.Contains(t, res.Content, "-m1", "old mod.txt content")
		assert.Contains(t, res.Content, "+m2", "new mod.txt content")
		assert.NotContains(t, res.Content, "untracked.txt", "untracked files never appear in git diff")
	})

	t.Run("staged diff", func(t *testing.T) {
		res, err := runInRepo(t, dir, `{"mode":"diff","staged":true}`)
		require.NoError(t, err)
		assert.Equal(t, rawGit(t, dir, "diff", "--cached"), res.Content)
		assert.Contains(t, res.Content, "+n1", "staged new.txt addition")
		assert.Contains(t, res.Content, "-g1", "staged gone.txt deletion")
		assert.NotContains(t, res.Content, "m2", "the unstaged mod.txt change is not in the staged view")
	})

	t.Run("show", func(t *testing.T) {
		res, err := runInRepo(t, dir, `{"mode":"show","rev":"HEAD"}`)
		require.NoError(t, err)
		assert.Equal(t, rawGit(t, dir, "show", "HEAD"), res.Content)
		assert.Equal(t, "show HEAD", res.Detail)
	})

	t.Run("branch list", func(t *testing.T) {
		_, err := runInRepo(t, dir, `{"mode":"branch","name":"feat"}`)
		require.NoError(t, err)

		res, err := runInRepo(t, dir, `{"mode":"branch"}`)
		require.NoError(t, err)
		assert.Equal(t, rawGit(t, dir, "branch", "-vv"), res.Content)
		assert.Contains(t, res.Content, "* main")
		assert.Contains(t, res.Content, "feat")
	})
}
