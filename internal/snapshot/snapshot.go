package snapshot

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// snapshotBranchPrefix is the human-readable name prefix shared by all
	// snapshots. It is kept from the legacy branch-based implementation so
	// old harness-snapshot-* branches remain recognizable.
	snapshotBranchPrefix = "harness-snapshot-"
	// snapshotRefNamespace stores snapshot commits under a dedicated ref
	// namespace instead of creating branches. A snapshot must never appear
	// in branch listings or move HEAD; refs are invisible to `git branch`
	// and cannot be checked out by accident.
	snapshotRefNamespace = "refs/a1/snapshots/"
	snapshotTimeout      = 30 * time.Second
)

// Manager captures and restores full working-tree snapshots.
//
// A snapshot is a commit object holding the complete working tree state at
// creation time — tracked modifications, staged changes, and untracked
// non-ignored files — stored under refs/a1/snapshots/<name>. The real index
// and HEAD are never touched during Create, so snapshotting is safe to run
// at any moment, including mid-task.
//
// Rollback restores the working tree to the snapshot with
// `git read-tree --reset -u` plus an untracked-cleanup pass:
//
//   - tracked files restored byte-exact to snapshot content,
//   - tracked files deleted after the snapshot are recreated,
//   - files created after the snapshot are removed,
//   - files that existed (tracked or not) at snapshot time are restored,
//     arriving in the index as staged changes so no work is lost,
//   - HEAD and the current branch never move.
//
// The pre-rollback semantics — `checkout -f <branch>` — destroyed every
// uncommitted change including the user's own WIP and switched branches
// mid-session. Those behaviors are gone.
type Manager struct {
	cwd string
}

// NewManager creates a snapshot manager for the given working directory.
func NewManager(cwd string) *Manager {
	return &Manager{cwd: cwd}
}

// Create captures the full working tree as refs/a1/snapshots/<name> and
// returns the snapshot name. The mechanism: a temporary index populated via
// `git add -A` (respects .gitignore), materialized with `git write-tree`,
// committed with `git commit-tree` (parent = current HEAD when one exists),
// and pinned with `git update-ref`. The real index is never modified.
func (m *Manager) Create(ctx context.Context) (string, error) {
	if m == nil {
		return "", fmt.Errorf("snapshot: manager is nil")
	}
	name := snapshotBranchPrefix + time.Now().Format("20060102-150405")

	tmp, err := os.CreateTemp("", "a1-snapshot-*.index")
	if err != nil {
		return "", fmt.Errorf("create temp index: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("create temp index: %w", err)
	}
	// GIT_INDEX_FILE must point to a path that does not exist yet — git
	// rejects an existing zero-byte file ("index file smaller than
	// expected"). Remove it; git creates a fresh index on first write.
	os.Remove(tmpPath)
	defer os.Remove(tmpPath)

	indexEnv := append(os.Environ(), "GIT_INDEX_FILE="+tmpPath)

	// Stage everything (tracked changes + untracked non-ignored files) into
	// the temporary index. The real index is untouched.
	if _, err := execGitEnv(ctx, m.cwd, indexEnv, "add", "-A", "--", "."); err != nil {
		return "", fmt.Errorf("stage working tree: %w", err)
	}
	tree, err := execGitEnv(ctx, m.cwd, indexEnv, "write-tree")
	if err != nil {
		return "", fmt.Errorf("write tree: %w", err)
	}
	tree = strings.TrimSpace(tree)

	// Commit the tree. On an unborn branch (fresh repo, no commits) the
	// snapshot is a root commit.
	head, headErr := execGit(ctx, m.cwd, "rev-parse", "--verify", "HEAD")
	args := []string{"commit-tree", tree, "-m", "a1 snapshot " + name}
	if headErr == nil && strings.TrimSpace(head) != "" {
		args = append(args, "-p", strings.TrimSpace(head))
	}
	commit, err := execGitEnv(ctx, m.cwd, indexEnv, args...)
	if err != nil && isIdentityError(err) {
		// Repos without configured user.name/user.email (CI containers,
		// fresh clones) cannot commit. Snapshot commits are internal
		// bookkeeping objects, so a fixed identity is fine as a fallback.
		args = append([]string{"-c", "user.name=a1-snapshot", "-c", "user.email=a1-snapshot@local"}, args...)
		commit, err = execGitEnv(ctx, m.cwd, indexEnv, args...)
	}
	if err != nil {
		return "", fmt.Errorf("commit tree: %w", err)
	}
	commit = strings.TrimSpace(commit)

	if _, err := execGit(ctx, m.cwd, "update-ref", snapshotRefNamespace+name, commit); err != nil {
		return "", fmt.Errorf("store snapshot ref: %w", err)
	}
	return name, nil
}

// Rollback restores the working tree to the named snapshot. The snapshot
// may be given as a bare name (as returned by Create), a full
// refs/a1/snapshots/ ref, or a legacy harness-snapshot-* branch name.
func (m *Manager) Rollback(ctx context.Context, name string) error {
	if m == nil {
		return fmt.Errorf("snapshot: manager is nil")
	}
	ref, err := m.resolve(ctx, name)
	if err != nil {
		return err
	}

	// Untracked non-ignored files right now. Anything on this list that the
	// snapshot does not contain was created after the snapshot (agent
	// debris from the failed candidate) and is removed after the restore.
	untrackedOut, err := execGit(ctx, m.cwd, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return fmt.Errorf("list untracked files: %w", err)
	}
	untracked := strings.Split(strings.TrimRight(untrackedOut, "\x00"), "\x00")

	// Restore index + working tree to the snapshot tree in one step. Paths
	// tracked in the snapshot but missing from the worktree are recreated;
	// paths tracked now but absent from the snapshot are dropped from both
	// index and worktree.
	if _, err := execGit(ctx, m.cwd, "read-tree", "--reset", "-u", ref); err != nil {
		return fmt.Errorf("restore snapshot tree: %w", err)
	}

	// Remove post-snapshot untracked files.
	treeOut, err := execGit(ctx, m.cwd, "ls-tree", "-r", "--name-only", "-z", ref)
	if err != nil {
		return fmt.Errorf("list snapshot tree: %w", err)
	}
	inSnapshot := make(map[string]bool)
	for _, p := range strings.Split(strings.TrimRight(treeOut, "\x00"), "\x00") {
		if p != "" {
			inSnapshot[p] = true
		}
	}
	removed := 0
	for _, p := range untracked {
		p = strings.TrimSpace(p)
		if p == "" || inSnapshot[p] {
			continue
		}
		full := filepath.Join(m.cwd, filepath.FromSlash(p))
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			if rmErr := os.Remove(full); rmErr == nil {
				removed++
			}
		}
	}
	if removed > 0 {
		// Best-effort empty-dir cleanup; failures are harmless.
		_, _ = execGit(ctx, m.cwd, "clean", "-f", "-d", "--quiet")
	}
	return nil
}

// Delete removes the snapshot ref. Legacy branch snapshots are deleted via
// the branch ref, with no branch switching required anymore.
func (m *Manager) Delete(ctx context.Context, name string) error {
	if m == nil {
		return fmt.Errorf("snapshot: manager is nil")
	}
	if strings.TrimSpace(name) == "" {
		return nil
	}
	ref, err := m.resolve(ctx, name)
	if err != nil {
		return err
	}
	if strings.HasPrefix(ref, snapshotRefNamespace) {
		if _, err := execGit(ctx, m.cwd, "update-ref", "-d", ref); err != nil {
			return fmt.Errorf("delete snapshot ref: %w", err)
		}
		return nil
	}
	// Legacy branch.
	if _, err := execGit(ctx, m.cwd, "branch", "-D", ref); err != nil {
		return fmt.Errorf("delete snapshot branch: %w", err)
	}
	return nil
}

// List returns all snapshot names: current refs/a1/snapshots/ entries plus
// any legacy harness-snapshot-* branches still lying around.
func (m *Manager) List(ctx context.Context) ([]string, error) {
	if m == nil {
		return nil, fmt.Errorf("snapshot: manager is nil")
	}
	out, err := execGit(ctx, m.cwd, "for-each-ref", "--format=%(refname)", snapshotRefNamespace)
	if err != nil {
		return nil, fmt.Errorf("list snapshot refs: %w", err)
	}
	seen := make(map[string]bool)
	var names []string
	for _, ref := range strings.Split(out, "\n") {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		name := strings.TrimPrefix(ref, snapshotRefNamespace)
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	// Legacy branches.
	branches, err := execGit(ctx, m.cwd, "branch", "--list", "--format=%(refname:short)", snapshotBranchPrefix+"*")
	if err == nil {
		for _, branch := range strings.Split(branches, "\n") {
			branch = strings.TrimSpace(branch)
			if branch != "" && !seen[branch] {
				seen[branch] = true
				names = append(names, branch)
			}
		}
	}
	return names, nil
}

// resolve maps a user-supplied name to a concrete git ref. Resolution order:
// refs/a1/snapshots/<name>, the literal input (a full ref or legacy branch),
// then <name> as a branch shorthand.
func (m *Manager) resolve(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("snapshot: name is empty")
	}
	if !strings.HasPrefix(name, "refs/") {
		if _, err := execGit(ctx, m.cwd, "rev-parse", "--verify", "--quiet", snapshotRefNamespace+name); err == nil {
			return snapshotRefNamespace + name, nil
		}
	}
	if _, err := execGit(ctx, m.cwd, "rev-parse", "--verify", "--quiet", name+"^{commit}"); err == nil {
		return name, nil
	}
	return "", fmt.Errorf("snapshot not found: %s", name)
}

func execGit(ctx context.Context, cwd string, args ...string) (string, error) {
	return execGitEnv(ctx, cwd, nil, args...)
}

// isIdentityError reports whether err is a git commit failure caused by a
// missing committer/author identity.
func isIdentityError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "ident") ||
		strings.Contains(s, "user.email") ||
		strings.Contains(s, "user.name") ||
		strings.Contains(s, "COMMITTER") ||
		strings.Contains(s, "AUTHOR")
}

// currentBranch returns the checked-out branch (or "HEAD" when detached).
// Used by tests to prove rollback never moves HEAD.
func currentBranch(ctx context.Context, cwd string) (string, error) {
	out, err := execGit(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func execGitEnv(ctx context.Context, cwd string, env []string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = cwd
	if len(env) > 0 {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if cctx.Err() != nil {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), cctx.Err())
		}
		text := strings.TrimSpace(string(out))
		if text != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), text)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
