package session_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/any-context/lazyclaude/internal/session"
)

func TestRole_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		role session.Role
		want string
	}{
		{session.RoleNone, "none"},
		{session.RolePM, "pm"},
		{session.RoleWorker, "worker"},
		{session.Role("unknown"), "unknown"},
	}
	for _, tt := range tests {
		got := tt.role.String()
		if got != tt.want {
			t.Errorf("Role(%q).String() = %q, want %q", string(tt.role), got, tt.want)
		}
	}
}

func TestRole_IsValid(t *testing.T) {
	t.Parallel()
	valid := []session.Role{
		session.RoleNone,
		session.RolePM,
		session.RoleWorker,
	}
	for _, r := range valid {
		if !r.IsValid() {
			t.Errorf("Role(%q).IsValid() = false, want true", string(r))
		}
	}

	invalid := []session.Role{
		session.Role("admin"),
		session.Role("PM"),
		session.Role("Worker"),
		session.Role("unknown"),
	}
	for _, r := range invalid {
		if r.IsValid() {
			t.Errorf("Role(%q).IsValid() = true, want false", string(r))
		}
	}
}

func TestBuildPMPrompt_ContainsRequiredFields(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	prompt := session.BuildPMPrompt(context.Background(), root, "sess-abc123", "worker-1, worker-2", "")

	cases := []struct {
		desc    string
		snippet string
	}{
		{"sessionID", "sess-abc123"},
		{"worker list", "worker-1, worker-2"},
		{"sessions CLI (base)", "lazyclaude sessions"},
		{"msg send CLI", "lazyclaude msg send"},
		{"msg create CLI (base)", "lazyclaude msg create"},
		{"tmux fallback (base)", "tmux -L lazyclaude send-keys"},
		{"role description", "PM"},
		{"review criteria correctness", "correctness"},
		{"review criteria tests", "test"},
		{"review criteria security", "security"},
		{"push delivery notice", "delivered directly"},
	}
	for _, tc := range cases {
		if !strings.Contains(prompt, tc.snippet) {
			t.Errorf("BuildPMPrompt missing %s: want %q in prompt", tc.desc, tc.snippet)
		}
	}
}

func TestBuildPMPrompt_NoPollInstructions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	prompt := session.BuildPMPrompt(context.Background(), root, "sess-xyz", "", "")
	if strings.Contains(prompt, "/msg/poll") {
		t.Error("BuildPMPrompt should not contain /msg/poll (push-based, no polling needed)")
	}
}

func TestBuildPMPrompt_EmptyWorkerList(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	prompt := session.BuildPMPrompt(context.Background(), root, "sess-xyz", "", "")
	if !strings.Contains(prompt, "lazyclaude sessions") {
		t.Error("BuildPMPrompt with empty worker list should still contain lazyclaude sessions")
	}
}

func TestBuildPMPrompt_UsesCLINotCurl(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	prompt := session.BuildPMPrompt(context.Background(), root, "sess-pm", "", "")

	if strings.Contains(prompt, "curl -s") {
		t.Error("prompt should use lazyclaude CLI, not curl")
	}
	if strings.Contains(prompt, "$PORT") {
		t.Error("prompt should not contain $PORT (CLI handles discovery)")
	}
	if strings.Contains(prompt, "$TOKEN") {
		t.Error("prompt should not contain $TOKEN (CLI handles discovery)")
	}
}

func TestBuildWorkerPrompt_ContainsRequiredFields(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wtPath := filepath.Join(root, ".lazyclaude", "worktrees", "feat-x")
	prompt := session.BuildWorkerPrompt(context.Background(), wtPath, root, "sess-worker-99", "")

	cases := []struct {
		desc    string
		snippet string
	}{
		{"worktree path", wtPath},
		{"project root", root},
		{"sessionID", "sess-worker-99"},
		{"sessions CLI (base)", "lazyclaude sessions"},
		{"msg send CLI", "lazyclaude msg send"},
		{"msg create CLI (base)", "lazyclaude msg create"},
		{"tmux fallback (base)", "tmux -L lazyclaude send-keys"},
		{"isolation instruction", "NEVER modify"},
		{"role description", "Worker"},
		{"review request instruction", "review"},
		{"push delivery notice", "delivered directly"},
	}
	for _, tc := range cases {
		if !strings.Contains(prompt, tc.snippet) {
			t.Errorf("BuildWorkerPrompt missing %s: want %q in prompt", tc.desc, tc.snippet)
		}
	}
}

func TestBuildWorkerPrompt_NoPollInstructions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wtPath := filepath.Join(root, ".lazyclaude", "worktrees", "feat-x")
	prompt := session.BuildWorkerPrompt(context.Background(), wtPath, root, "sess-worker-99", "")
	if strings.Contains(prompt, "/msg/poll") {
		t.Error("BuildWorkerPrompt should not contain /msg/poll (push-based, no polling needed)")
	}
}

func TestBuildWorkerPrompt_PathIsolation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wtPath := filepath.Join(root, ".lazyclaude", "worktrees", "my-task")
	prompt := session.BuildWorkerPrompt(context.Background(), wtPath, root, "id-1", "")

	if !strings.Contains(prompt, wtPath) {
		t.Errorf("BuildWorkerPrompt missing worktree path %q", wtPath)
	}
	if !strings.Contains(prompt, root) {
		t.Errorf("BuildWorkerPrompt missing project root %q", root)
	}
}

func TestBuildWorkerPrompt_UsesCLINotCurl(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wtPath := filepath.Join(root, ".lazyclaude", "worktrees", "feat-x")
	prompt := session.BuildWorkerPrompt(context.Background(), wtPath, root, "sess-worker-99", "")

	if strings.Contains(prompt, "curl -s") {
		t.Error("prompt should use lazyclaude CLI, not curl")
	}
	if strings.Contains(prompt, "$PORT") {
		t.Error("prompt should not contain $PORT (CLI handles discovery)")
	}
	if strings.Contains(prompt, "$TOKEN") {
		t.Error("prompt should not contain $TOKEN (CLI handles discovery)")
	}
}

func TestBuildWorkerPrompt_SessionIDInFromFlag(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wtPath := filepath.Join(root, ".lazyclaude", "worktrees", "feat-x")
	prompt := session.BuildWorkerPrompt(context.Background(), wtPath, root, "sess-worker-99", "")

	if !strings.Contains(prompt, "--from sess-worker-99") {
		t.Error("worker prompt should contain --from <session-id> in msg send examples")
	}
}

func TestBuildPMPrompt_SessionIDInFromFlag(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	prompt := session.BuildPMPrompt(context.Background(), root, "sess-pm-42", "workers", "")

	if !strings.Contains(prompt, "--from sess-pm-42") {
		t.Error("PM prompt should contain --from <session-id> in msg send examples")
	}
}

// --- Custom prompt search tests ---

func TestBuildPMPrompt_UsesProjectCustomPrompt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	customDir := filepath.Join(root, ".lazyclaude", "prompts")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	customTemplate := "Custom PM prompt. Session: %s, From: %s, Workers: %s"
	if err := os.WriteFile(filepath.Join(customDir, "pm.md"), []byte(customTemplate), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt := session.BuildPMPrompt(context.Background(), root, "sid-1", "w1", "")
	if !strings.Contains(prompt, "Custom PM prompt") {
		t.Error("expected custom PM prompt to be used")
	}
	if !strings.Contains(prompt, "sid-1") {
		t.Error("expected session ID in custom prompt output")
	}
}

func TestBuildWorkerPrompt_UsesProjectCustomPrompt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	customDir := filepath.Join(root, ".lazyclaude", "prompts")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	customTemplate := "Custom Worker. Root: %s, WT: %s, Session: %s, From: %s"
	if err := os.WriteFile(filepath.Join(customDir, "worker.md"), []byte(customTemplate), 0o644); err != nil {
		t.Fatal(err)
	}

	wtPath := filepath.Join(root, ".lazyclaude", "worktrees", "feat-x")
	prompt := session.BuildWorkerPrompt(context.Background(), wtPath, root, "sid-2", "")
	if !strings.Contains(prompt, "Custom Worker") {
		t.Error("expected custom Worker prompt to be used")
	}
	if !strings.Contains(prompt, "sid-2") {
		t.Error("expected session ID in custom prompt output")
	}
}

func TestBuildWorkerPrompt_WorktreeCustomTakesPriority(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Set up project-level custom prompt
	projectDir := filepath.Join(root, ".lazyclaude", "prompts")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "worker.md"),
		[]byte("Project level: %s %s %s %s"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up worktree-level custom prompt (higher priority)
	branch := "feat-x"
	wtDir := filepath.Join(root, ".lazyclaude", "worktree", branch, ".lazyclaude", "prompts")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "worker.md"),
		[]byte("Worktree level: %s %s %s %s"), 0o644); err != nil {
		t.Fatal(err)
	}

	wtPath := filepath.Join(root, ".lazyclaude", "worktrees", branch)
	prompt := session.BuildWorkerPrompt(context.Background(), wtPath, root, "sid-3", "")
	if !strings.Contains(prompt, "Worktree level") {
		t.Error("expected worktree-level custom prompt to take priority over project-level")
	}
	if strings.Contains(prompt, "Project level") {
		t.Error("project-level prompt should not be used when worktree-level exists")
	}
}

func TestBuildPMPrompt_FallsBackToEmbedded(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	prompt := session.BuildPMPrompt(context.Background(), root, "sid-4", "w1", "")
	if !strings.Contains(prompt, "PM") {
		t.Error("expected embedded default prompt when no custom found")
	}
	if !strings.Contains(prompt, "lazyclaude sessions") {
		t.Error("expected embedded prompt CLI commands")
	}
}

func TestBuildWorkerPrompt_FallsBackToEmbedded(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wtPath := filepath.Join(root, ".lazyclaude", "worktrees", "feat-y")

	prompt := session.BuildWorkerPrompt(context.Background(), wtPath, root, "sid-5", "")
	if !strings.Contains(prompt, "Worker") {
		t.Error("expected embedded default prompt when no custom found")
	}
	if !strings.Contains(prompt, "NEVER modify") {
		t.Error("expected embedded prompt isolation instruction")
	}
}

func TestBuildWorkerPrompt_SkipsEmptyCustomFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	customDir := filepath.Join(root, ".lazyclaude", "prompts")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(customDir, "worker.md"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	wtPath := filepath.Join(root, ".lazyclaude", "worktrees", "feat-z")
	prompt := session.BuildWorkerPrompt(context.Background(), wtPath, root, "sid-6", "")
	if !strings.Contains(prompt, "Worker") {
		t.Error("expected fallback to embedded prompt for empty custom file")
	}
}

func TestResolvePrompt_RelativeRootFallsBack(t *testing.T) {
	t.Parallel()
	// Relative projectRoot should fall back to embedded default
	prompt := session.BuildPMPrompt(context.Background(), "relative/path", "sid-7", "w1", "")
	if !strings.Contains(prompt, "PM") {
		t.Error("expected embedded fallback for relative projectRoot")
	}
}

func TestBuildWorkerPrompt_PathTraversalInBranch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Create a file outside the project that would be reached by path traversal
	outsideDir := filepath.Join(root, "..", ".lazyclaude", "prompts")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideDir, "worker.md"),
		[]byte("Escaped: %s %s %s %s"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Craft a worktree path with ".." in the branch name
	wtPath := filepath.Join(root, ".lazyclaude", "worktrees", "../../..")
	prompt := session.BuildWorkerPrompt(context.Background(), wtPath, root, "sid-8", "")
	if strings.Contains(prompt, "Escaped") {
		t.Error("path traversal via branch name should be blocked")
	}
}
