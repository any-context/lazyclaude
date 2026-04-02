package session

import (
	"strings"
	"time"
)

// Project groups sessions under a single git repository root.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	PM       *Session  `json:"pm,omitempty"`
	Sessions []Session `json:"sessions,omitempty"`

	// Runtime state (not persisted)
	Expanded bool `json:"-"`
}

// InferProjectRoot extracts the project root directory from a session path.
// For worktree paths, it strips the .lazyclaude/worktrees/<name> suffix.
// For non-worktree paths, it returns the path unchanged.
func InferProjectRoot(path string) string {
	if path == "" {
		return ""
	}
	idx := strings.Index(path, "/.lazyclaude/worktrees/")
	if idx >= 0 {
		return path[:idx]
	}
	return path
}
