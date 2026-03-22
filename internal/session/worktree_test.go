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

func TestWritePromptFile(t *testing.T) {
	path, err := WritePromptFile("hello world")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Errorf("content = %q, want %q", string(data), "hello world")
	}
}

func TestWritePromptFile_Unicode(t *testing.T) {
	prompt := "日本語のプロンプト\n改行あり"
	path, err := WritePromptFile(prompt)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != prompt {
		t.Errorf("content = %q, want %q", string(data), prompt)
	}
}

func TestWorktreePath(t *testing.T) {
	got := WorktreePath("/home/user/project", "fix-popup")
	want := filepath.Join("/home/user/project", ".claude", "worktrees", "fix-popup")
	if got != want {
		t.Errorf("WorktreePath = %q, want %q", got, want)
	}
}
