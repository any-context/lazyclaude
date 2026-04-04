package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- LocalRunner tests ---

func TestLocalRunner_Run(t *testing.T) {
	t.Parallel()
	r := &LocalRunner{}
	out, err := r.Run(context.Background(), "", "echo", "hello")
	require.NoError(t, err)
	assert.Contains(t, string(out), "hello")
}

func TestLocalRunner_Run_WithDir(t *testing.T) {
	t.Parallel()
	r := &LocalRunner{}
	dir := t.TempDir()
	out, err := r.Run(context.Background(), dir, "pwd")
	require.NoError(t, err)
	assert.Contains(t, string(out), filepath.Base(dir))
}

func TestLocalRunner_Run_NoArgs(t *testing.T) {
	t.Parallel()
	r := &LocalRunner{}
	_, err := r.Run(context.Background(), "")
	assert.Error(t, err)
}

func TestLocalRunner_Exists(t *testing.T) {
	t.Parallel()
	r := &LocalRunner{}
	dir := t.TempDir()
	exists, err := r.Exists(context.Background(), dir)
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = r.Exists(context.Background(), filepath.Join(dir, "nonexistent"))
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestLocalRunner_MkdirAll(t *testing.T) {
	t.Parallel()
	r := &LocalRunner{}
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	err := r.MkdirAll(context.Background(), dir)
	require.NoError(t, err)

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

// --- LocalRunner factory tests ---

func TestLocalRunner_IsDefaultRunner(t *testing.T) {
	t.Parallel()
	r := &LocalRunner{}
	_, ok := interface{}(r).(GitRunner)
	assert.True(t, ok)
}

// --- CreateWorktreeWithRunner tests (local) ---

func TestCreateWorktreeWithRunner_Local(t *testing.T) {
	t.Parallel()
	r := &LocalRunner{}
	ctx := context.Background()

	projectRoot := t.TempDir()
	initGitRepoHelper(t, projectRoot)

	wtPath := filepath.Join(projectRoot, ".lazyclaude", "worktrees", "test-wt")
	err := CreateWorktreeWithRunner(ctx, r, projectRoot, wtPath, "test-wt")
	require.NoError(t, err)

	info, err := os.Stat(wtPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestCreateWorktreeWithRunner_Reuse(t *testing.T) {
	t.Parallel()
	r := &LocalRunner{}
	ctx := context.Background()

	projectRoot := t.TempDir()
	initGitRepoHelper(t, projectRoot)

	wtPath := filepath.Join(projectRoot, ".lazyclaude", "worktrees", "reuse-me")
	require.NoError(t, os.MkdirAll(wtPath, 0o755))

	err := CreateWorktreeWithRunner(ctx, r, projectRoot, wtPath, "reuse-me")
	require.NoError(t, err)
}

func TestCreateWorktreeWithRunner_NotGitRepo(t *testing.T) {
	t.Parallel()
	r := &LocalRunner{}
	ctx := context.Background()

	projectRoot := t.TempDir() // not a git repo
	wtPath := filepath.Join(projectRoot, ".lazyclaude", "worktrees", "test")
	err := CreateWorktreeWithRunner(ctx, r, projectRoot, wtPath, "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
}

// --- ListWorktreesWithRunner tests (local) ---

func TestListWorktreesWithRunner_Local(t *testing.T) {
	t.Parallel()
	r := &LocalRunner{}
	ctx := context.Background()

	projectRoot := t.TempDir()
	initGitRepoHelper(t, projectRoot)

	// Create a worktree
	wtPath := filepath.Join(projectRoot, ".lazyclaude", "worktrees", "list-test")
	err := CreateWorktreeWithRunner(ctx, r, projectRoot, wtPath, "list-test")
	require.NoError(t, err)

	items, err := ListWorktreesWithRunner(ctx, r, projectRoot)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "list-test", items[0].Name)
}

func TestListWorktreesWithRunner_NotGitRepo(t *testing.T) {
	t.Parallel()
	r := &LocalRunner{}
	items, err := ListWorktreesWithRunner(context.Background(), r, t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, items)
}

// initGitRepoHelper creates a bare git repo with an initial commit.
func initGitRepoHelper(t *testing.T, dir string) {
	t.Helper()
	r := &LocalRunner{}
	ctx := context.Background()
	_, err := r.Run(ctx, dir, "git", "init")
	require.NoError(t, err)
	_, err = r.Run(ctx, dir, "git", "commit", "--allow-empty", "-m", "init")
	require.NoError(t, err)
}
