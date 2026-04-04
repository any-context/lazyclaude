package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// WorktreePathSegment is the relative path segment identifying worktree directories.
// Used for both path construction and detection.
const WorktreePathSegment = ".lazyclaude/worktrees"

// worktreeSystemPrompt is the isolation instruction prepended to the user's prompt.
const worktreeSystemPrompt = `You are working in an isolated worktree at %s.
Your task is scoped to this directory only.
NEVER modify files outside this worktree — %s must remain untouched.
Be careful that any commands you run do not interfere with other worktrees.`

// IsWorktreePath returns true if the path belongs to a worktree directory.
func IsWorktreePath(path string) bool {
	return strings.Contains(path, "/"+WorktreePathSegment+"/")
}

// ValidateWorktreeName checks if a worktree name is valid.
// Rejects empty, whitespace-only, path traversal, and git-invalid characters.
func ValidateWorktreeName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("worktree name cannot be empty")
	}
	for _, ch := range []string{"/", "\\", "..", "~", "^", ":", "?", "*", "["} {
		if strings.Contains(name, ch) {
			return fmt.Errorf("worktree name cannot contain %q", ch)
		}
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("worktree name cannot start with '-'")
	}
	if strings.HasSuffix(name, ".lock") {
		return fmt.Errorf("worktree name cannot end with '.lock'")
	}
	return nil
}

// BuildWorktreePrompt returns the system isolation instructions for a worktree.
// This is appended to Claude's system prompt via --append-system-prompt.
// The user's task description is passed separately as a positional argument.
func BuildWorktreePrompt(worktreePath, projectRoot string) string {
	return fmt.Sprintf(worktreeSystemPrompt, worktreePath, projectRoot)
}

// WorktreeInfo describes an existing git worktree under .lazyclaude/worktrees/.
type WorktreeInfo struct {
	Name   string // last path segment (e.g. "fix-popup")
	Path   string // full path to worktree directory
	Branch string // branch name without refs/heads/ prefix
}

// ListWorktrees returns existing git worktrees under .lazyclaude/worktrees/.
// Returns nil (not error) if projectRoot is not a git repo.
func ListWorktrees(ctx context.Context, projectRoot string) ([]WorktreeInfo, error) {
	return ListWorktreesWithRunner(ctx, &LocalRunner{}, projectRoot)
}

// parseWorktreePorcelain parses `git worktree list --porcelain` output
// and returns only entries under .lazyclaude/worktrees/.
func parseWorktreePorcelain(output string) []WorktreeInfo {
	var items []WorktreeInfo
	blocks := strings.Split(strings.TrimSpace(output), "\n\n")
	for _, block := range blocks {
		if block == "" {
			continue
		}
		var path, branch string
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "worktree ") {
				path = strings.TrimPrefix(line, "worktree ")
			}
			if strings.HasPrefix(line, "branch refs/heads/") {
				branch = strings.TrimPrefix(line, "branch refs/heads/")
			}
		}
		if path == "" || !IsWorktreePath(path) {
			continue
		}
		items = append(items, WorktreeInfo{
			Name:   filepath.Base(path),
			Path:   path,
			Branch: branch,
		})
	}
	return items
}

// WorktreePath returns the absolute path for a worktree directory.
func WorktreePath(projectRoot, name string) string {
	return filepath.Join(projectRoot, WorktreePathSegment, name)
}
