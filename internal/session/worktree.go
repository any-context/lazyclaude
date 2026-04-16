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
//
// Deprecated: Use ValidateBranchName, which permits "/" in branch names.
// ValidateWorktreeName is retained for backward compatibility.
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

// ValidateBranchName checks if a branch name is valid according to git refname
// rules. Unlike ValidateWorktreeName, "/" is permitted so that hierarchical
// branch names like "feat/login" work. Rejected patterns:
//   - empty or whitespace-only
//   - "//", trailing "/", leading "/"
//   - ".." anywhere (refname traversal)
//   - "@{" (reflog syntax)
//   - control characters (0x00-0x1F, 0x7F), space (0x20)
//   - \, ~, ^, :, ?, *, [ (git-check-ref-format rejects these)
//   - leading "-"
//   - trailing ".lock", or any component ending with ".lock" (e.g. "foo.lock/bar")
//   - trailing "."
//   - the literal name "HEAD"
//   - path component starting with "." (e.g. ".hidden/x" or "a/.b")
func ValidateBranchName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("branch name cannot be empty")
	}
	if strings.Contains(name, "//") {
		return fmt.Errorf("branch name cannot contain %q", "//")
	}
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("branch name cannot start with %q", "/")
	}
	if strings.HasSuffix(name, "/") {
		return fmt.Errorf("branch name cannot end with %q", "/")
	}
	if strings.HasSuffix(name, ".") {
		return fmt.Errorf("branch name cannot end with '.'")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("branch name cannot contain %q", "..")
	}
	if strings.Contains(name, "@{") {
		return fmt.Errorf("branch name cannot contain %q", "@{")
	}
	for _, ch := range name {
		if ch <= 0x20 || ch == 0x7F {
			return fmt.Errorf("branch name cannot contain control characters or spaces")
		}
	}
	for _, ch := range []string{"\\", "~", "^", ":", "?", "*", "["} {
		if strings.Contains(name, ch) {
			return fmt.Errorf("branch name cannot contain %q", ch)
		}
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("branch name cannot start with '-'")
	}
	if strings.HasSuffix(name, ".lock") {
		return fmt.Errorf("branch name cannot end with '.lock'")
	}
	// Reject the literal name "HEAD" (reserved by git).
	if name == "HEAD" {
		return fmt.Errorf("branch name cannot be %q", "HEAD")
	}
	// Reject path components starting with "." or ending with ".lock".
	for _, component := range strings.Split(name, "/") {
		if strings.HasPrefix(component, ".") {
			return fmt.Errorf("branch name component cannot start with '.'")
		}
		if strings.HasSuffix(component, ".lock") {
			return fmt.Errorf("branch name component cannot end with '.lock'")
		}
	}
	return nil
}

// DirNameFromBranch converts a branch name to a flattened directory name by
// replacing "/" with "-". The result is safe for use as a filesystem directory
// name under .lazyclaude/worktrees/.
func DirNameFromBranch(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// BuildWorktreePrompt returns the system isolation instructions for a worktree.
// This is appended to Claude's system prompt via --append-system-prompt.
// The user's task description is passed separately as a positional argument.
func BuildWorktreePrompt(worktreePath, projectRoot string) string {
	return fmt.Sprintf(worktreeSystemPrompt, worktreePath, projectRoot)
}

// WorktreeInfo describes an existing git worktree under .lazyclaude/worktrees/.
type WorktreeInfo struct {
	Name   string // branch name (e.g. "feat/login"); falls back to filepath.Base(path) for detached HEAD
	Path   string // full path to worktree directory
	Branch string // branch name without refs/heads/ prefix (same as Name when branch line exists)
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
		// Use branch name for Name; fall back to directory base name for
		// detached HEAD (no branch line in porcelain output).
		name := branch
		if name == "" {
			name = filepath.Base(path)
		}
		items = append(items, WorktreeInfo{
			Name:   name,
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
