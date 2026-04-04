package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitRunner abstracts git command execution for local and SSH contexts.
type GitRunner interface {
	// Run executes a command in dir and returns combined output.
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)

	// Exists checks if a path exists on the target (local or remote).
	Exists(ctx context.Context, path string) (bool, error)

	// MkdirAll creates directories recursively on the target.
	MkdirAll(ctx context.Context, path string) error
}

// LocalRunner executes commands locally via exec.Command.
type LocalRunner struct{}

func (r *LocalRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no command specified")
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func (r *LocalRunner) Exists(ctx context.Context, path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (r *LocalRunner) MkdirAll(ctx context.Context, path string) error {
	return os.MkdirAll(path, 0o755)
}

// CreateWorktreeWithRunner creates a git worktree using the provided runner.
// Replaces both createGitWorktree (local) and createGitWorktreeRemote (SSH).
func CreateWorktreeWithRunner(ctx context.Context, runner GitRunner, projectRoot, wtPath, branch string) error {
	// Verify projectRoot is a git repository.
	if _, err := runner.Run(ctx, projectRoot, "git", "rev-parse", "--git-dir"); err != nil {
		return fmt.Errorf("not a git repository: %s", projectRoot)
	}

	// If the worktree directory already exists, assume reuse.
	exists, err := runner.Exists(ctx, wtPath)
	if err != nil {
		return fmt.Errorf("check worktree path: %w", err)
	}
	if exists {
		return nil
	}

	// Ensure parent directory exists.
	if err := runner.MkdirAll(ctx, filepath.Dir(wtPath)); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	// Try creating worktree with a new branch first.
	out, err := runner.Run(ctx, projectRoot, "git", "worktree", "add", "-b", branch, wtPath)
	if err == nil {
		return nil
	}
	// Branch may already exist — try without -b.
	out2, err2 := runner.Run(ctx, projectRoot, "git", "worktree", "add", wtPath, branch)
	if err2 != nil {
		return fmt.Errorf("%s\n%s", strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)))
	}
	return nil
}

// ListWorktreesWithRunner returns existing git worktrees under .lazyclaude/worktrees/.
// Replaces the host-branching ListWorktrees function.
func ListWorktreesWithRunner(ctx context.Context, runner GitRunner, projectRoot string) ([]WorktreeInfo, error) {
	out, err := runner.Run(ctx, projectRoot, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, nil // not a git repo or git not available
	}
	items := parseWorktreePorcelain(string(out))

	// Filter out worktrees whose directory no longer exists.
	// Uses runner.Exists which works for both local (os.Stat) and SSH (test -d).
	result := items[:0]
	for _, item := range items {
		exists, existsErr := runner.Exists(ctx, item.Path)
		if existsErr == nil && exists {
			result = append(result, item)
		}
	}
	return result, nil
}
