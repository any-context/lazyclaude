package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorktreePathSegment is the relative path segment identifying worktree directories.
// Used for both path construction and detection.
const WorktreePathSegment = ".claude/worktrees"

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

// BuildWorktreePrompt combines the system isolation prompt with the user's prompt.
func BuildWorktreePrompt(worktreePath, projectRoot, userPrompt string) string {
	system := fmt.Sprintf(worktreeSystemPrompt, worktreePath, projectRoot)
	if strings.TrimSpace(userPrompt) == "" {
		return system
	}
	return system + "\n\n---\n\n" + userPrompt
}

// WritePromptFile writes the prompt to a temporary file and returns the path.
// The caller is responsible for cleanup (or relying on OS temp cleanup).
func WritePromptFile(prompt string) (string, error) {
	f, err := os.CreateTemp("", "lazyclaude-prompt-*.txt")
	if err != nil {
		return "", fmt.Errorf("create prompt file: %w", err)
	}
	if _, err := f.WriteString(prompt); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("write prompt file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("close prompt file: %w", err)
	}
	return f.Name(), nil
}

// WorktreePath returns the absolute path for a worktree directory.
func WorktreePath(projectRoot, name string) string {
	return filepath.Join(projectRoot, WorktreePathSegment, name)
}
