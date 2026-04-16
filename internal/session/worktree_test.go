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
	if items[1].Name != "feat/auth" {
		t.Errorf("items[1].Name = %q, want %q", items[1].Name, "feat/auth")
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
	path, err := writeLauncher(launcherOpts{
		Spec:         LaunchSpec{Command: "claude"},
		SessionID:    "test-uuid-1234",
		RuntimeDir:   t.TempDir(),
		SystemPrompt: "system prompt here",
		UserPrompt:   "user task",
	})
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
	if !strings.Contains(content, "exec 'claude'") {
		t.Error("should exec quoted claude")
	}
	if !strings.Contains(content, "--session-id 'test-uuid-1234'") {
		t.Error("should contain --session-id")
	}
}

func TestWriteWorktreeLauncher_EmptyUserPrompt(t *testing.T) {
	path, err := writeLauncher(launcherOpts{
		Spec:         LaunchSpec{Command: "claude"},
		SessionID:    "test-uuid-empty",
		RuntimeDir:   t.TempDir(),
		SystemPrompt: "system only",
	})
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

// --- ValidateBranchName tests ---

func TestValidateBranchName_Valid(t *testing.T) {
	t.Parallel()
	valid := []string{
		"fix-popup",
		"feature-123",
		"my_branch",
		"a",
		"feat/x",
		"feat/sub/deep",
		"user/john/fix-bug",
	}
	for _, name := range valid {
		if err := ValidateBranchName(name); err != nil {
			t.Errorf("ValidateBranchName(%q) = %v, want nil", name, err)
		}
	}
}

func TestValidateBranchName_Invalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		label string
		name  string
		want  string
	}{
		{"empty", "", "empty"},
		{"whitespace-only", "   ", "empty"},
		{"double-slash", "feat//x", "//"},
		{"leading-slash", "/leading", "start with \"/\""},
		{"trailing-slash", "trailing/", "end with \"/\""},
		{"trailing-dot", "feat/x.", "end with '.'"},
		{"double-dot", "foo..bar", ".."},
		{"reflog-syntax", "foo@{bar", "@{"},
		{"backslash", "foo\\bar", "\\"},
		{"tilde", "foo~bar", "~"},
		{"caret", "foo^bar", "^"},
		{"colon", "foo:bar", ":"},
		{"question-mark", "foo?bar", "?"},
		{"asterisk", "foo*bar", "*"},
		{"bracket", "foo[bar", "["},
		{"leading-dash", "-leading", "start with '-'"},
		{"dot-lock-suffix", "foo.lock", ".lock"},
		{"dot-component", ".hidden", "start with '.'"},
		{"dot-component-nested", "feat/.hidden", "start with '.'"},
		{"dot-component-deep", "a/.config/b", "start with '.'"},
		{"control-null", "feat/\x00bad", "control"},
		{"control-0x1f", "feat/\x1fbad", "control"},
		{"space", "feat bar", "control characters or spaces"},
		{"HEAD-reserved", "HEAD", "HEAD"},
		{"component-lock", "foo.lock/bar", "component cannot end with '.lock'"},
		{"nested-component-lock", "x/y.lock", ".lock"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()
			err := ValidateBranchName(tc.name)
			if err == nil {
				t.Errorf("ValidateBranchName(%q) = nil, want error containing %q", tc.name, tc.want)
				return
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ValidateBranchName(%q) = %q, want error containing %q", tc.name, err.Error(), tc.want)
			}
		})
	}
}

// --- DirNameFromBranch tests ---

func TestDirNameFromBranch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		branch string
		want   string
	}{
		{"fix-popup", "fix-popup"},
		{"feat/x", "feat-x"},
		{"feat/sub/deep", "feat-sub-deep"},
		{"no-slash", "no-slash"},
	}
	for _, tc := range cases {
		got := DirNameFromBranch(tc.branch)
		if got != tc.want {
			t.Errorf("DirNameFromBranch(%q) = %q, want %q", tc.branch, got, tc.want)
		}
	}
}

// --- parseWorktreePorcelain branch-based Name tests ---

func TestParseWorktreePorcelain_DetachedHeadFallback(t *testing.T) {
	t.Parallel()
	porcelain := `worktree /project/.lazyclaude/worktrees/detached-wt
HEAD abc123
detached

`
	items := parseWorktreePorcelain(porcelain)
	if len(items) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(items))
	}
	if items[0].Name != "detached-wt" {
		t.Errorf("Name = %q, want %q (filepath.Base fallback for detached HEAD)", items[0].Name, "detached-wt")
	}
	if items[0].Branch != "" {
		t.Errorf("Branch = %q, want empty for detached HEAD", items[0].Branch)
	}
}

func TestParseWorktreePorcelain_SlashBranchName(t *testing.T) {
	t.Parallel()
	porcelain := `worktree /project/.lazyclaude/worktrees/feat-login
HEAD abc123
branch refs/heads/feat/login

`
	items := parseWorktreePorcelain(porcelain)
	if len(items) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(items))
	}
	if items[0].Name != "feat/login" {
		t.Errorf("Name = %q, want %q", items[0].Name, "feat/login")
	}
	if items[0].Branch != "feat/login" {
		t.Errorf("Branch = %q, want %q", items[0].Branch, "feat/login")
	}
	if items[0].Path != "/project/.lazyclaude/worktrees/feat-login" {
		t.Errorf("Path = %q, want flattened directory path", items[0].Path)
	}
}

// --- DirNameFromBranch invariant test ---

func TestDirNameFromBranch_Invariant(t *testing.T) {
	t.Parallel()
	// For any valid branch name, DirNameFromBranch(name) must equal
	// filepath.Base of the worktree path constructed from that name.
	branches := []string{"fix-popup", "feat/x", "feat/sub/deep", "simple"}
	projectRoot := "/project"
	for _, branch := range branches {
		dirName := DirNameFromBranch(branch)
		wtPath := WorktreePath(projectRoot, dirName)
		base := filepath.Base(wtPath)
		if dirName != base {
			t.Errorf("DirNameFromBranch(%q) = %q, but filepath.Base(WorktreePath(..., %q)) = %q",
				branch, dirName, dirName, base)
		}
	}
}

func TestWriteWorktreeLauncher_SpecialChars(t *testing.T) {
	// Prompt with single quotes, newlines, and Japanese text
	system := "Don't modify /project"
	user := "日本語プロンプト\nwith 'quotes' and $vars"
	path, err := writeLauncher(launcherOpts{
		Spec:         LaunchSpec{Command: "claude"},
		SessionID:    "test-uuid-special",
		RuntimeDir:   t.TempDir(),
		SystemPrompt: system,
		UserPrompt:   user,
	})
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
