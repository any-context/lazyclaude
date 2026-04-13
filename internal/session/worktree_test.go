package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateWorktreeName_Valid(t *testing.T) {
	valid := []string{"fix-popup", "feature-123", "my_branch", "a"}
	for _, name := range valid {
		if err := ValidateWorktreeName(name); err != nil {
			t.Errorf("ValidateWorktreeName(%q) = %v, want nil", name, err)
		}
	}
}

func TestValidateWorktreeName_Invalid(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"", "empty"},
		{"   ", "empty"},
		{"foo/bar", "/"},
		{"foo\\bar", "\\"},
		{"foo..bar", ".."},
		{"foo~bar", "~"},
		{"foo^bar", "^"},
		{"foo:bar", ":"},
		{"foo?bar", "?"},
		{"foo*bar", "*"},
		{"foo[bar", "["},
		{"-leading", "start with"},
		{"foo.lock", ".lock"},
	}
	for _, tc := range cases {
		err := ValidateWorktreeName(tc.name)
		if err == nil {
			t.Errorf("ValidateWorktreeName(%q) = nil, want error containing %q", tc.name, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ValidateWorktreeName(%q) = %q, want error containing %q", tc.name, err.Error(), tc.want)
		}
	}
}

func TestBuildWorktreePrompt(t *testing.T) {
	prompt := BuildWorktreePrompt("/project/.lazyclaude/worktrees/fix", "/project")
	if !strings.Contains(prompt, "/project/.lazyclaude/worktrees/fix") {
		t.Error("should contain worktree path")
	}
	if !strings.Contains(prompt, "/project") {
		t.Error("should contain project root")
	}
	if !strings.Contains(prompt, "NEVER modify") {
		t.Error("should contain isolation instruction")
	}
}

func TestWorktreePath(t *testing.T) {
	got := WorktreePath("/home/user/project", "fix-popup")
	want := filepath.Join("/home/user/project", ".lazyclaude", "worktrees", "fix-popup")
	if got != want {
		t.Errorf("WorktreePath = %q, want %q", got, want)
	}
}

func TestIsWorktreePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/project/.lazyclaude/worktrees/fix-popup", true},
		{"/project/.lazyclaude/worktrees/x/sub", true},
		{"/project/src/main.go", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsWorktreePath(tc.path); got != tc.want {
			t.Errorf("IsWorktreePath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestListWorktrees_ParsesPorcelainOutput(t *testing.T) {
	t.Parallel()
	porcelain := `worktree /project
HEAD abc123
branch refs/heads/main

worktree /project/.lazyclaude/worktrees/fix-popup
HEAD def456
branch refs/heads/fix-popup

worktree /project/.lazyclaude/worktrees/feat-auth
HEAD 789abc
branch refs/heads/feat/auth

worktree /other/path
HEAD 000000
branch refs/heads/other

`
	items := parseWorktreePorcelain(porcelain)
	if len(items) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(items))
	}
	if items[0].Name != "fix-popup" {
		t.Errorf("items[0].Name = %q, want %q", items[0].Name, "fix-popup")
	}
	if items[0].Branch != "fix-popup" {
		t.Errorf("items[0].Branch = %q, want %q", items[0].Branch, "fix-popup")
	}
	if items[0].Path != "/project/.lazyclaude/worktrees/fix-popup" {
		t.Errorf("items[0].Path = %q", items[0].Path)
	}
	if items[1].Name != "feat-auth" {
		t.Errorf("items[1].Name = %q, want %q", items[1].Name, "feat-auth")
	}
	if items[1].Branch != "feat/auth" {
		t.Errorf("items[1].Branch = %q, want %q", items[1].Branch, "feat/auth")
	}
}

func TestListWorktrees_EmptyOutput(t *testing.T) {
	t.Parallel()
	items := parseWorktreePorcelain("")
	if len(items) != 0 {
		t.Errorf("expected 0 worktrees, got %d", len(items))
	}
}

func TestListWorktrees_NoClaude(t *testing.T) {
	t.Parallel()
	porcelain := `worktree /project
HEAD abc123
branch refs/heads/main

`
	items := parseWorktreePorcelain(porcelain)
	if len(items) != 0 {
		t.Errorf("expected 0 worktrees, got %d", len(items))
	}
}

func TestWriteWorktreeLauncher_BasicContent(t *testing.T) {
	path, err := writeWorktreeLauncher("system prompt here", "user task", t.TempDir(), "test-uuid-1234", false)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.HasPrefix(content, "#!/bin/sh\n") {
		t.Error("should start with shebang")
	}
	if !strings.Contains(content, "rm -f \"$0\"") {
		t.Error("should self-delete")
	}
	if !strings.Contains(content, "--append-system-prompt") {
		t.Error("should use --append-system-prompt")
	}
	if !strings.Contains(content, "system prompt here") {
		t.Error("should contain system prompt")
	}
	if !strings.Contains(content, "user task") {
		t.Error("should contain user prompt")
	}
	if !strings.Contains(content, "exec claude") {
		t.Error("should exec claude")
	}
	if !strings.Contains(content, "--session-id 'test-uuid-1234'") {
		t.Error("should contain --session-id")
	}
}

func TestWriteWorktreeLauncher_EmptyUserPrompt(t *testing.T) {
	path, err := writeWorktreeLauncher("system only", "", t.TempDir(), "test-uuid-empty", false)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "--append-system-prompt") {
		t.Error("should use --append-system-prompt")
	}
	// Should NOT have a trailing positional argument after --append-system-prompt.
	// The last line should end with the system prompt quote, not an extra argument.
	lines := strings.Split(strings.TrimSpace(content), "\n")
	lastLine := lines[len(lines)-1]
	// Find the position of --append-system-prompt and check nothing follows the system prompt
	idx := strings.Index(lastLine, "--append-system-prompt")
	if idx < 0 {
		t.Fatal("expected --append-system-prompt in last line")
	}
	afterAppend := lastLine[idx+len("--append-system-prompt"):]
	// afterAppend should be: " 'system only'\n" — exactly one quoted arg
	// Count quoted segments after --append-system-prompt
	quotedArgs := strings.Count(afterAppend, "'") / 2 // pairs of quotes
	if quotedArgs > 1 {
		t.Error("should not include user prompt argument when empty")
	}
}

func TestWriteWorktreeLauncher_SpecialChars(t *testing.T) {
	// Prompt with single quotes, newlines, and Japanese text
	system := "Don't modify /project"
	user := "日本語プロンプト\nwith 'quotes' and $vars"
	path, err := writeWorktreeLauncher(system, user, t.TempDir(), "test-uuid-special", false)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "Don") {
		t.Error("should contain system prompt text")
	}
	if !strings.Contains(content, "日本語") {
		t.Error("should contain Japanese text in user prompt")
	}
}
