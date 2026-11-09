package pathutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Hostile directories must yield an empty branch, not an error or output noise:
// git fails, GitBranch maps any failure to "".
func TestGitBranch_HostileDirs_Negative(t *testing.T) {
	dir := t.TempDir()

	assert.Empty(t, GitBranch(t.Context(), filepath.Join(dir, "no-such-dir")), "nonexistent dir")
	assert.Empty(t, GitBranch(t.Context(), filepath.Join(dir, "plain.txt")), "a regular file is not a repo")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "plain.txt"), []byte("x"), 0o600))
	assert.Empty(t, GitBranch(t.Context(), filepath.Join(dir, "plain.txt")))
}

// Degenerate paths: empty, root, exactly at the 5-part boundary, trailing
// separators, and a path that merely starts with home but is not under it.
func TestShortPath_DegenerateAndBoundary_Negative(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, "", ShortPath(""), "empty path stays empty")
	assert.Equal(t, "/", ShortPath("/"), "root is one part")
	assert.Equal(t, "a/b/c/d/e", ShortPath("a/b/c/d/e"), "five parts are shown in full")
	assert.Equal(t, "/a/../d/e", ShortPath("/a/b/c/d/e"), "leading slash adds a part, so this abbreviates")

	// Pinned quirk: the home substitution is a raw prefix match with no
	// separator boundary check, so a sibling of home also collapses to "~".
	assert.Equal(t, "~-sibling", ShortPath(home+"-sibling"),
		"raw prefix match: /home/user-sibling renders as ~-sibling")
}

// branchState reads <resolved gitdir>/HEAD; every hostile layout must degrade
// to the "missing" sentinel rather than erroring or panicking.
func TestBranchState_HostileGitDir_Negative(t *testing.T) {
	dir := t.TempDir()

	file := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	assert.Equal(t, "missing", branchState(file), "a regular file has no .git/HEAD under it")

	// A .git *file* whose content is not a gitdir pointer resolves back to the
	// literal ".git" path, which is a file — HEAD cannot be read under it.
	inner := filepath.Join(dir, "worktree")
	require.NoError(t, os.Mkdir(inner, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(inner, ".git"), []byte("garbage\n"), 0o600))
	assert.Equal(t, "missing", branchState(inner))

	// A gitdir pointer to an empty target resolves to the worktree itself.
	empty := filepath.Join(dir, "empty-target")
	require.NoError(t, os.Mkdir(empty, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(empty, ".git"), []byte("gitdir:  \n"), 0o600))
	assert.Equal(t, "missing", branchState(empty))
}

// naiveShortPath rebuilds the abbreviation spec without strings.Split/Join:
// it collects separator indexes, keeps paths with at most five parts, and
// otherwise splices "second-part .. last-part" by byte offset.
func naiveShortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		p = "~" + p[len(home):]
	}
	var seps []int
	for i := 0; i < len(p); i++ {
		if p[i] == filepath.Separator {
			seps = append(seps, i)
		}
	}
	// len(parts) == len(seps)+1, so "at most five parts" means at most four seps.
	if len(seps) <= 4 {
		return p
	}
	second := seps[1]
	secondLast := seps[len(seps)-2]
	sep := string(filepath.Separator)
	return p[:second] + sep + ".." + sep + p[secondLast+1:]
}

// oracle: naive separator-index reference (above) over edge cases and paths
// under and around $HOME; both must produce the same display abbreviation.
func TestShortPath_MatchesNaiveReference_Differential(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	cases := []string{
		"", "/", "//", "///", ".", "..", "relative", "a/b/c/d", "a/b/c/d/e",
		"/a", "/a/b", "/a/b/c", "/a/b/c/d", "/a/b/c/d/e", "/a/b/c/d/e/f",
		"/a/b/c/d/e/f/g/h", "/a/b/c/d/e/", "/a//b/c/d/e", "/a//b//c//d//e//f",
		home, home + "/x", home + "/a/b/c/d/e/f", home + "-sibling",
	}
	if home != "" {
		cases = append(cases, strings.TrimSuffix(home, "/")+"/one/two/three/four/five")
	}
	for _, p := range cases {
		assert.Equal(t, naiveShortPath(p), ShortPath(p), "ShortPath(%q)", p)
	}
}

// oracle: naive short-path reference plus the branch name the fixture repo was
// created with — PathWithBranch must render exactly that combination.
func TestPathWithBranch_KnownBranchLabel_Differential(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "init", "-q", "-b", "feature").CombinedOutput()
	require.NoError(t, err, "git init: %s", out)

	assert.Equal(t, naiveShortPath(dir)+" (feature)", PathWithBranch(dir))
}
