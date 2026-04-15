package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePrompt_CustomReader_ReturnsRemoteContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	fakeReader := func(path string) ([]byte, error) {
		expected := filepath.Join(root, ".lazyclaude", "prompts", "pm.md")
		if path == expected {
			return []byte("remote custom: %s %s %s"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	got := resolvePrompt(root, "", "", "pm.md", "fallback", fakeReader)
	if got != "remote custom: %s %s %s" {
		t.Errorf("resolvePrompt with custom reader = %q, want remote content", got)
	}
}

func TestResolvePrompt_CustomReader_FallsBackOnError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	fakeReader := func(path string) ([]byte, error) {
		return nil, fmt.Errorf("ssh: connection refused")
	}

	got := resolvePrompt(root, "", "", "pm.md", "default-fallback", fakeReader)
	if got != "default-fallback" {
		t.Errorf("resolvePrompt on reader error = %q, want %q", got, "default-fallback")
	}
}

func TestResolvePrompt_CustomReader_FallsBackOnEmpty(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	fakeReader := func(path string) ([]byte, error) {
		return []byte(""), nil
	}

	got := resolvePrompt(root, "", "", "pm.md", "default-fallback", fakeReader)
	if got != "default-fallback" {
		t.Errorf("resolvePrompt on empty reader = %q, want %q", got, "default-fallback")
	}
}

func TestResolvePrompt_CustomReader_WorktreePriority(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	branch := "feat-x"
	wtPath := filepath.Join(root, WorktreePathSegment, branch)

	wtPromptPath := filepath.Join(root, ".lazyclaude", "worktree", branch, ".lazyclaude", "prompts", "worker.md")
	projectPromptPath := filepath.Join(root, ".lazyclaude", "prompts", "worker.md")

	fakeReader := func(path string) ([]byte, error) {
		switch path {
		case wtPromptPath:
			return []byte("worktree-level"), nil
		case projectPromptPath:
			return []byte("project-level"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	got := resolvePrompt(root, wtPath, "", "worker.md", "fallback", fakeReader)
	if got != "worktree-level" {
		t.Errorf("resolvePrompt worktree priority = %q, want %q", got, "worktree-level")
	}
}

func TestResolvePrompt_CustomReader_SkipsWorktreeFallsToProject(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	branch := "feat-y"
	wtPath := filepath.Join(root, WorktreePathSegment, branch)

	projectPromptPath := filepath.Join(root, ".lazyclaude", "prompts", "worker.md")

	fakeReader := func(path string) ([]byte, error) {
		if path == projectPromptPath {
			return []byte("project-level"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	got := resolvePrompt(root, wtPath, "", "worker.md", "fallback", fakeReader)
	if got != "project-level" {
		t.Errorf("resolvePrompt project fallback = %q, want %q", got, "project-level")
	}
}

func TestResolvePrompt_LocalFileReader(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	customDir := filepath.Join(root, ".lazyclaude", "prompts")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(customDir, "pm.md"), []byte("local-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolvePrompt(root, "", "", "pm.md", "fallback", localFileReader())
	if got != "local-content" {
		t.Errorf("resolvePrompt with localFileReader = %q, want %q", got, "local-content")
	}
}

// TestResolvePrompt_HomeDirLayer verifies that $HOME/.lazyclaude/prompts/ is
// searched as layer 3, after project-level but before the embedded default.
func TestResolvePrompt_HomeDirLayer(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	home := t.TempDir()

	homePromptPath := filepath.Join(home, ".lazyclaude", "prompts", "pm.md")

	fakeReader := func(path string) ([]byte, error) {
		if path == homePromptPath {
			return []byte("home-level"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	got := resolvePrompt(root, "", home, "pm.md", "fallback", fakeReader)
	if got != "home-level" {
		t.Errorf("resolvePrompt home layer = %q, want %q", got, "home-level")
	}
}

// TestResolvePrompt_ProjectOverridesHome verifies that project-level (layer 2)
// takes priority over home-level (layer 3).
func TestResolvePrompt_ProjectOverridesHome(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	home := t.TempDir()

	projectPromptPath := filepath.Join(root, ".lazyclaude", "prompts", "pm.md")
	homePromptPath := filepath.Join(home, ".lazyclaude", "prompts", "pm.md")

	fakeReader := func(path string) ([]byte, error) {
		switch path {
		case projectPromptPath:
			return []byte("project-level"), nil
		case homePromptPath:
			return []byte("home-level"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	got := resolvePrompt(root, "", home, "pm.md", "fallback", fakeReader)
	if got != "project-level" {
		t.Errorf("resolvePrompt project should override home = %q, want %q", got, "project-level")
	}
}

// TestResolvePrompt_HomeDirEmptySkipsLayer verifies that an empty homeDir
// does not cause a panic and skips layer 3.
func TestResolvePrompt_HomeDirEmptySkipsLayer(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	fakeReader := func(path string) ([]byte, error) {
		return nil, fmt.Errorf("not found")
	}

	got := resolvePrompt(root, "", "", "pm.md", "embedded-default", fakeReader)
	if got != "embedded-default" {
		t.Errorf("resolvePrompt empty homeDir = %q, want %q", got, "embedded-default")
	}
}

// TestResolvePrompt_HomePathTraversal verifies that filenames with path
// traversal sequences do not escape the home prompts root.
// A sentinel file is placed at home/secret.txt; traversal attacks that try to
// reach it via "../../secret.txt" or "../secret.txt" must be blocked.
func TestResolvePrompt_HomePathTraversal(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	root := t.TempDir()

	// Place sentinel at home/secret.txt — two levels above prompts root.
	sentinelPath := filepath.Join(home, "secret.txt")
	if err := os.WriteFile(sentinelPath, []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Traversal attack filenames that try to reach home/secret.txt.
	attackFilenames := []string{
		"../../secret.txt",
		"../secret.txt",
	}

	for _, filename := range attackFilenames {
		t.Run(filename, func(t *testing.T) {
			t.Parallel()
			// Use the real file reader; if traversal is blocked, the sentinel
			// cannot be read and "fallback" is returned.
			got := resolvePrompt(root, "", home, filename, "fallback", localFileReader())
			if got == "SECRET" {
				t.Errorf("resolvePrompt(%q): path traversal not blocked — sentinel content returned", filename)
			}
		})
	}
}
