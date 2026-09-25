package snapshot

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	snapshotBranchPrefix = "harness-snapshot-"
	snapshotTimeout      = 15 * time.Second
)

// Manager creates and manages git snapshot branches for rollback.
type Manager struct {
	cwd string
}

// NewManager creates a snapshot manager for the given working directory.
func NewManager(cwd string) *Manager {
	return &Manager{cwd: cwd}
}

// Create creates a snapshot branch from the current HEAD and returns its name.
// The branch points to the current commit, capturing the current state.
func (m *Manager) Create(ctx context.Context) (string, error) {
	if m == nil {
		return "", fmt.Errorf("snapshot: manager is nil")
	}
	name := snapshotBranchPrefix + time.Now().Format("20060102-150405")
	if _, err := execGit(ctx, m.cwd, "checkout", "-b", name); err != nil {
		return "", fmt.Errorf("create snapshot branch: %w", err)
	}
	// If there are uncommitted changes, commit them to the snapshot branch.
	out, err := execGit(ctx, m.cwd, "status", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("check working tree status: %w", err)
	}
	if strings.TrimSpace(out) != "" {
		if _, err := execGit(ctx, m.cwd, "add", "-A"); err != nil {
			return "", fmt.Errorf("stage changes for snapshot: %w", err)
		}
		if _, err := execGit(ctx, m.cwd, "commit", "-m", "snapshot: "+name); err != nil {
			return "", fmt.Errorf("commit snapshot: %w", err)
		}
	}
	return name, nil
}

// Rollback checks out the given snapshot branch, discarding current changes.
// It returns to the original branch after checkout.
func (m *Manager) Rollback(ctx context.Context, snapshotBranch string) error {
	if m == nil {
		return fmt.Errorf("snapshot: manager is nil")
	}
	if strings.TrimSpace(snapshotBranch) == "" {
		return fmt.Errorf("snapshot: branch name is empty")
	}
	original, err := currentBranch(ctx, m.cwd)
	if err != nil {
		return err
	}
	if _, err := execGit(ctx, m.cwd, "checkout", snapshotBranch); err != nil {
		return fmt.Errorf("checkout snapshot branch: %w", err)
	}
	if _, err := execGit(ctx, m.cwd, "reset", "--hard", snapshotBranch); err != nil {
		return fmt.Errorf("reset to snapshot: %w", err)
	}
	if _, err := execGit(ctx, m.cwd, "checkout", original); err != nil {
		return fmt.Errorf("return to original branch: %w", err)
	}
	return nil
}

// Delete removes the snapshot branch after a successful operation.
func (m *Manager) Delete(ctx context.Context, snapshotBranch string) error {
	if m == nil {
		return fmt.Errorf("snapshot: manager is nil")
	}
	if strings.TrimSpace(snapshotBranch) == "" {
		return nil
	}
	original, err := currentBranch(ctx, m.cwd)
	if err != nil {
		return err
	}
	if original == snapshotBranch {
		// Cannot delete the branch we're on; switch to main/master first.
		for _, fallback := range []string{"main", "master"} {
			if _, err := execGit(ctx, m.cwd, "rev-parse", "--verify", fallback); err == nil {
				if _, err := execGit(ctx, m.cwd, "checkout", fallback); err != nil {
					return fmt.Errorf("switch to %s before deleting snapshot: %w", fallback, err)
				}
				break
			}
		}
	}
	if _, err := execGit(ctx, m.cwd, "branch", "-D", snapshotBranch); err != nil {
		return fmt.Errorf("delete snapshot branch: %w", err)
	}
	return nil
}

// List returns all snapshot branches in the repository.
func (m *Manager) List(ctx context.Context) ([]string, error) {
	if m == nil {
		return nil, fmt.Errorf("snapshot: manager is nil")
	}
	out, err := execGit(ctx, m.cwd, "branch", "--list", snapshotBranchPrefix+"*")
	if err != nil {
		return nil, fmt.Errorf("list snapshot branches: %w", err)
	}
	var branches []string
	for _, line := range strings.Split(out, "\n") {
		branch := strings.TrimSpace(line)
		if branch != "" {
			branches = append(branches, branch)
		}
	}
	return branches, nil
}

func execGit(ctx context.Context, cwd string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = cwd
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

func currentBranch(ctx context.Context, cwd string) (string, error) {
	out, err := execGit(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
