package session

import (
	"strings"
	"time"
)

// Project groups sessions under a single git repository root.
// All sessions (including PM) are stored in the Sessions slice.
// PM sessions are identified by Role==RolePM.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Sessions []Session `json:"sessions,omitempty"`

	// Runtime state (not persisted)
	Expanded bool `json:"-"`
}

// FindPM returns the root PM session (Role==RolePM, ParentID=="") or nil.
// Returns a copy; modifications do not affect the project.
func (p *Project) FindPM() *Session {
	for i := range p.Sessions {
		if p.Sessions[i].Role == RolePM && p.Sessions[i].ParentID == "" {
			s := p.Sessions[i]
			return &s
		}
	}
	return nil
}

// RootSessions returns sessions with no parent (ParentID=="").
func (p *Project) RootSessions() []Session {
	var result []Session
	for _, s := range p.Sessions {
		if s.ParentID == "" {
			result = append(result, s)
		}
	}
	return result
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
