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

func TestBuildWorktreePrompt_WithUserPrompt(t *testing.T) {
	prompt := BuildWorktreePrompt("/project/.claude/worktrees/fix", "/project", "Fix the bug")
	if !strings.Contains(prompt, "/project/.claude/worktrees/fix") {
		t.Error("should contain worktree path")
	}
	if !strings.Contains(prompt, "/project") {
		t.Error("should contain project root")
	}
	if !strings.Contains(prompt, "NEVER modify") {
		t.Error("should contain isolation instruction")
	}
	if !strings.Contains(prompt, "---") {
		t.Error("should contain separator")
	}
	if !strings.Contains(prompt, "Fix the bug") {
		t.Error("should contain user prompt")
	}
}

func TestBuildWorktreePrompt_EmptyUserPrompt(t *testing.T) {
	prompt := BuildWorktreePrompt("/wt", "/proj", "")
	if strings.Contains(prompt, "---") {
		t.Error("should not contain separator when user prompt is empty")
	}
	if !strings.Contains(prompt, "NEVER modify") {
		t.Error("should still contain system prompt")
	}
}

func TestWorktreePath(t *testing.T) {
	got := WorktreePath("/home/user/project", "fix-popup")
	want := filepath.Join("/home/user/project", ".claude", "worktrees", "fix-popup")
	if got != want {
		t.Errorf("WorktreePath = %q, want %q", got, want)
	}
}

func TestIsWorktreePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/project/.claude/worktrees/fix-popup", true},
		{"/project/.claude/worktrees/x/sub", true},
		{"/project/src/main.go", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsWorktreePath(tc.path); got != tc.want {
			t.Errorf("IsWorktreePath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestWriteWorktreeLauncher_BasicContent(t *testing.T) {
	path, err := writeWorktreeLauncher("system prompt here", "user task")
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
}

func TestWriteWorktreeLauncher_EmptyUserPrompt(t *testing.T) {
	path, err := writeWorktreeLauncher("system only", "")
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
	// Should NOT have a trailing positional argument
	lines := strings.Split(strings.TrimSpace(content), "\n")
	lastLine := lines[len(lines)-1]
	if strings.Count(lastLine, "'") > 2 {
		// More than one single-quoted argument means user prompt was included
		t.Error("should not include user prompt argument when empty")
	}
}

func TestWriteWorktreeLauncher_SpecialChars(t *testing.T) {
	// Prompt with single quotes, newlines, and Japanese text
	system := "Don't modify /project"
	user := "日本語プロンプト\nwith 'quotes' and $vars"
	path, err := writeWorktreeLauncher(system, user)
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
