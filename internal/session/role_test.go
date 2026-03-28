package session_test

import (
	"strings"
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/session"
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
	prompt := session.BuildPMPrompt("sess-abc123", 9876, "tok-secret", "worker-1, worker-2", "/tmp/lazyclaude-mcp.port", "/home/user/.claude/ide")

	cases := []struct {
		desc    string
		snippet string
	}{
		{"sessionID", "sess-abc123"},
		{"worker list", "worker-1, worker-2"},
		{"send endpoint", "/msg/send"},
		{"sessions endpoint", "/msg/sessions"},
		{"role description", "PM"},
		{"review criteria correctness", "correctness"},
		{"review criteria tests", "test"},
		{"review criteria security", "security"},
		{"auth header", "X-Auth-Token:"},
		{"push delivery notice", "delivered directly"},
		{"port file path", "/tmp/lazyclaude-mcp.port"},
		{"ide dir path", "/home/user/.claude/ide"},
		{"dynamic discovery", "$PORT"},
	}
	for _, tc := range cases {
		if !strings.Contains(prompt, tc.snippet) {
			t.Errorf("BuildPMPrompt missing %s: want %q in prompt", tc.desc, tc.snippet)
		}
	}
}

func TestBuildPMPrompt_NoPollInstructions(t *testing.T) {
	t.Parallel()
	prompt := session.BuildPMPrompt("sess-xyz", 8080, "tok-abc", "", "/tmp/mcp.port", "/tmp/ide")
	if strings.Contains(prompt, "/msg/poll") {
		t.Error("BuildPMPrompt should not contain /msg/poll (push-based, no polling needed)")
	}
}

func TestBuildPMPrompt_EmptyWorkerList(t *testing.T) {
	t.Parallel()
	prompt := session.BuildPMPrompt("sess-xyz", 8080, "tok-abc", "", "/tmp/mcp.port", "/tmp/ide")
	if !strings.Contains(prompt, "$PORT") {
		t.Error("BuildPMPrompt with empty worker list should still contain $PORT")
	}
}

func TestBuildWorkerPrompt_ContainsRequiredFields(t *testing.T) {
	t.Parallel()
	prompt := session.BuildWorkerPrompt(
		"/project/.claude/worktrees/feat-x",
		"/project",
		"sess-worker-99",
		9876,
		"tok-secret",
		"/tmp/lazyclaude-mcp.port",
		"/home/user/.claude/ide",
	)

	cases := []struct {
		desc    string
		snippet string
	}{
		{"worktree path", "/project/.claude/worktrees/feat-x"},
		{"project root", "/project"},
		{"sessionID", "sess-worker-99"},
		{"send endpoint", "/msg/send"},
		{"sessions endpoint", "/msg/sessions"},
		{"isolation instruction", "NEVER modify"},
		{"role description", "Worker"},
		{"review request instruction", "review"},
		{"auth header", "X-Auth-Token:"},
		{"push delivery notice", "delivered directly"},
		{"port file path", "/tmp/lazyclaude-mcp.port"},
		{"ide dir path", "/home/user/.claude/ide"},
		{"dynamic discovery", "$PORT"},
	}
	for _, tc := range cases {
		if !strings.Contains(prompt, tc.snippet) {
			t.Errorf("BuildWorkerPrompt missing %s: want %q in prompt", tc.desc, tc.snippet)
		}
	}
}

func TestBuildWorkerPrompt_NoPollInstructions(t *testing.T) {
	t.Parallel()
	prompt := session.BuildWorkerPrompt(
		"/project/.claude/worktrees/feat-x",
		"/project",
		"sess-worker-99",
		9876,
		"tok-secret",
		"/tmp/mcp.port",
		"/tmp/ide",
	)
	if strings.Contains(prompt, "/msg/poll") {
		t.Error("BuildWorkerPrompt should not contain /msg/poll (push-based, no polling needed)")
	}
}

func TestBuildWorkerPrompt_PathIsolation(t *testing.T) {
	t.Parallel()
	worktree := "/home/user/project/.claude/worktrees/my-task"
	root := "/home/user/project"
	prompt := session.BuildWorkerPrompt(worktree, root, "id-1", 8080, "t", "/tmp/mcp.port", "/tmp/ide")

	// Must mention both paths for isolation enforcement
	if !strings.Contains(prompt, worktree) {
		t.Errorf("BuildWorkerPrompt missing worktree path %q", worktree)
	}
	if !strings.Contains(prompt, root) {
		t.Errorf("BuildWorkerPrompt missing project root %q", root)
	}
}

// TestBuildWorkerPrompt_DynamicDiscovery verifies that curl commands use
// dynamic port/token discovery (cat portFile + python3 lockFile) instead of
// hardcoded values. This prevents stale connection after server restart.
func TestBuildWorkerPrompt_DynamicDiscovery(t *testing.T) {
	t.Parallel()
	prompt := session.BuildWorkerPrompt(
		"/project/.claude/worktrees/feat-x",
		"/project",
		"sess-worker-99",
		9876,
		"tok-secret",
		"/tmp/lazyclaude-mcp.port",
		"/home/user/.claude/ide",
	)

	// The prompt must NOT contain hardcoded "localhost:9876" — all curl
	// commands must use $PORT for dynamic server discovery.
	if strings.Contains(prompt, "localhost:9876") {
		t.Error("prompt must not contain hardcoded localhost:9876; curl should use $PORT")
	}

	// The prompt must NOT contain "X-Auth-Token: tok-secret" — all curl
	// commands must use $TOKEN for dynamic token discovery.
	if strings.Contains(prompt, "Token: tok-secret") {
		t.Error("prompt must not contain hardcoded token in curl; should use $TOKEN")
	}

	// Must use $PORT and $TOKEN variables in curl commands
	if !strings.Contains(prompt, "$PORT") {
		t.Error("prompt must contain $PORT variable for dynamic port")
	}
	if !strings.Contains(prompt, "$TOKEN") {
		t.Error("prompt must contain $TOKEN variable for dynamic token")
	}

	// Must contain dynamic discovery commands using portFile and ideDir
	if !strings.Contains(prompt, "$(cat") {
		t.Error("prompt must contain $(cat ...) for dynamic port discovery")
	}
}

// TestBuildPMPrompt_DynamicDiscovery verifies PM prompt also uses dynamic discovery.
func TestBuildPMPrompt_DynamicDiscovery(t *testing.T) {
	t.Parallel()
	prompt := session.BuildPMPrompt("sess-pm", 9876, "tok-secret", "", "/tmp/lazyclaude-mcp.port", "/home/user/.claude/ide")

	if strings.Contains(prompt, "localhost:9876") {
		t.Error("prompt must not contain hardcoded localhost:9876; curl should use $PORT")
	}
	if strings.Contains(prompt, "Token: tok-secret") {
		t.Error("prompt must not contain hardcoded token in curl; should use $TOKEN")
	}
	if !strings.Contains(prompt, "$PORT") {
		t.Error("prompt must contain $PORT variable")
	}
	if !strings.Contains(prompt, "$TOKEN") {
		t.Error("prompt must contain $TOKEN variable")
	}
}

func TestBuildPMPrompt_CurlExampleIsUsable(t *testing.T) {
	t.Parallel()
	// Verify the curl example uses correct HTTP method conventions
	prompt := session.BuildPMPrompt("id-pm", 1234, "mytoken", "", "/tmp/mcp.port", "/tmp/ide")

	// /msg/send requires POST; /msg/sessions requires GET
	if !strings.Contains(prompt, "/msg/send") {
		t.Fatal("prompt missing /msg/send")
	}
	if !strings.Contains(prompt, "/msg/sessions") {
		t.Fatal("prompt missing /msg/sessions")
	}

	// The prompt must mention POST (for /msg/send curl examples)
	if !strings.Contains(strings.ToUpper(prompt), "POST") {
		t.Error("prompt should mention POST for /msg/send")
	}
}
