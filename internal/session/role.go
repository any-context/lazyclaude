package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/any-context/lazyclaude/prompts"
)

// Role identifies the operational role of a session.
type Role string

const (
	// RoleNone is the zero value; represents a regular session with no PM/Worker role.
	// Used for backward compatibility with existing state.json files.
	RoleNone Role = ""

	// RolePM represents a Project Manager session that reviews Worker PRs.
	RolePM Role = "pm"

	// RoleWorker represents a Worker session that operates within a git worktree.
	RoleWorker Role = "worker"
)

// String returns a human-readable name. RoleNone returns "none".
func (r Role) String() string {
	if r == RoleNone {
		return "none"
	}
	return string(r)
}

// IsValid reports whether r is one of the defined Role constants.
func (r Role) IsValid() bool {
	return r == RoleNone || r == RolePM || r == RoleWorker
}

// resolvePrompt searches for a custom prompt file in priority order and falls
// back to the embedded default. The search order is:
//
//  1. {projectRoot}/.claude/worktree/{branch}/.lazyclaude/prompts/{filename}
//  2. {projectRoot}/.lazyclaude/prompts/{filename}
//  3. Embedded default (compiled into the binary)
//
// worktreePath may be empty (e.g. for PM sessions that run in the project root).
// When empty, the worktree-level search is skipped.
// projectRoot must be an absolute path; relative paths fall back to the default.
func resolvePrompt(projectRoot, worktreePath, filename, fallback string) string {
	if !filepath.IsAbs(projectRoot) {
		return fallback
	}

	cleanRoot := filepath.Clean(projectRoot) + string(os.PathSeparator)
	var candidates []string

	if worktreePath != "" {
		branch := branchFromWorktreePath(projectRoot, worktreePath)
		if branch != "" {
			candidate := filepath.Join(projectRoot, ".claude", "worktree", branch, ".lazyclaude", "prompts", filename)
			if strings.HasPrefix(candidate, cleanRoot) {
				candidates = append(candidates, candidate)
			}
		}
	}

	projectCandidate := filepath.Join(projectRoot, ".lazyclaude", "prompts", filename)
	if strings.HasPrefix(projectCandidate, cleanRoot) {
		candidates = append(candidates, projectCandidate)
	}

	for _, candidate := range candidates {
		data, err := os.ReadFile(candidate)
		if err == nil && len(data) > 0 {
			return string(data)
		}
	}

	return fallback
}

// branchFromWorktreePath extracts the branch name from a worktree path by
// computing the path relative to {projectRoot}/.claude/worktrees/.
// Returns empty string if the path does not match the expected pattern.
func branchFromWorktreePath(projectRoot, wtPath string) string {
	base := filepath.Join(projectRoot, ".claude", "worktrees") + string(os.PathSeparator)
	if !strings.HasPrefix(wtPath, base) {
		return ""
	}
	rel := strings.TrimPrefix(wtPath, base)
	parts := strings.SplitN(rel, string(os.PathSeparator), 2)
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	branch := parts[0]
	if strings.Contains(branch, "..") {
		return ""
	}
	return branch
}

// BuildPMPrompt generates the system prompt injected into a PM session at launch.
// It searches for a custom pm.md in the project before falling back to the
// embedded default. projectRoot is used for custom prompt discovery.
func BuildPMPrompt(projectRoot, sessionID, workerList string) string {
	tmpl := resolvePrompt(projectRoot, "", "pm.md", prompts.DefaultPM())
	return fmt.Sprintf(tmpl,
		sessionID, // Session ID line
		sessionID, // msg send --from
		sessionID, // msg send --from (spawn)
		workerList,
	)
}

// BuildWorkerPrompt generates the system prompt injected into a Worker session at launch.
// It searches for a custom worker.md in the project/worktree before falling back
// to the embedded default.
func BuildWorkerPrompt(worktreePath, projectRoot, sessionID string) string {
	tmpl := resolvePrompt(projectRoot, worktreePath, "worker.md", prompts.DefaultWorker())
	return fmt.Sprintf(tmpl,
		projectRoot,  // NEVER modify ... must remain untouched
		worktreePath, // Worktree path line
		sessionID,    // Session ID line
		sessionID,    // msg send --from (review_request)
		sessionID,    // msg send --from (spawn)
	)
}
