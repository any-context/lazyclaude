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

	got := resolvePrompt(root, "", "pm.md", "fallback", fakeReader)
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

	got := resolvePrompt(root, "", "pm.md", "default-fallback", fakeReader)
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

	got := resolvePrompt(root, "", "pm.md", "default-fallback", fakeReader)
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

	got := resolvePrompt(root, wtPath, "worker.md", "fallback", fakeReader)
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

	got := resolvePrompt(root, wtPath, "worker.md", "fallback", fakeReader)
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

	got := resolvePrompt(root, "", "pm.md", "fallback", localFileReader())
	if got != "local-content" {
		t.Errorf("resolvePrompt with localFileReader = %q, want %q", got, "local-content")
	}
}
