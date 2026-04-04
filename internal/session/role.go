package session

import (
	"context"
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

// fileReader abstracts file reading so resolvePrompt can be tested.
type fileReader func(path string) ([]byte, error)

// localFileReader returns a fileReader that reads from the local filesystem.
func localFileReader() fileReader {
	return func(path string) ([]byte, error) { return os.ReadFile(path) }
}

// resolvePrompt searches for a custom prompt file in priority order and falls
// back to the embedded default. The search order is:
//
//  1. {projectRoot}/.lazyclaude/worktree/{branch}/.lazyclaude/prompts/{filename}
//  2. {projectRoot}/.lazyclaude/prompts/{filename}
//  3. Embedded default (compiled into the binary)
//
// Note: the worktree custom config path uses "worktree" (singular) for per-branch
// configuration, which is distinct from the "worktrees" (plural) directory where
// git worktree checkouts reside (WorktreePathSegment).
//
// worktreePath may be empty (e.g. for PM sessions that run in the project root).
// When empty, the worktree-level search is skipped.
// projectRoot must be an absolute path; relative paths fall back to the default.
func resolvePrompt(projectRoot, worktreePath, filename, fallback string, read fileReader) string {
	if !filepath.IsAbs(projectRoot) {
		return fallback
	}

	cleanRoot := filepath.Clean(projectRoot) + string(os.PathSeparator)
	var candidates []string

	if worktreePath != "" {
		branch := branchFromWorktreePath(projectRoot, worktreePath)
		if branch != "" {
			candidate := filepath.Join(projectRoot, ".lazyclaude", "worktree", branch, ".lazyclaude", "prompts", filename)
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
		data, err := read(candidate)
		if err == nil && len(data) > 0 {
			return string(data)
		}
	}

	return fallback
}

// branchFromWorktreePath extracts the branch name from a worktree path by
// computing the path relative to {projectRoot}/{WorktreePathSegment}/.
// Returns empty string if the path does not match the expected pattern.
func branchFromWorktreePath(projectRoot, wtPath string) string {
	base := filepath.Join(projectRoot, WorktreePathSegment) + string(os.PathSeparator)
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
// The final prompt is composed of pm.md (role-specific, custom-searchable) +
// base.md (shared communication reference, always embedded).
func BuildPMPrompt(ctx context.Context, projectRoot, sessionID, workerList string) string {
	roleTmpl := resolvePrompt(projectRoot, "", "pm.md", prompts.DefaultPM(), localFileReader())
	baseTmpl := prompts.DefaultBase()

	role := fmt.Sprintf(roleTmpl,
		sessionID, // Session ID line
		sessionID, // msg send --from (review_response)
		workerList,
	)
	base := fmt.Sprintf(baseTmpl,
		sessionID, // msg create --from (spawn)
	)
	return role + "\n\n" + base
}

// BuildWorkerPrompt generates the system prompt injected into a Worker session at launch.
// The final prompt is composed of worker.md (role-specific, custom-searchable) +
// base.md (shared communication reference, always embedded).
func BuildWorkerPrompt(ctx context.Context, worktreePath, projectRoot, sessionID string) string {
	roleTmpl := resolvePrompt(projectRoot, worktreePath, "worker.md", prompts.DefaultWorker(), localFileReader())
	baseTmpl := prompts.DefaultBase()

	role := fmt.Sprintf(roleTmpl,
		projectRoot,  // NEVER modify ... must remain untouched
		worktreePath, // Worktree path line
		sessionID,    // Session ID line
		sessionID,    // msg send --from (review_request)
	)
	base := fmt.Sprintf(baseTmpl,
		sessionID, // msg create --from (spawn)
	)
	return role + "\n\n" + base
}
