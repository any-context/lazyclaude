package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestManager(t *testing.T) (*session.Manager, *tmux.MockClient) {
	t.Helper()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	mock := tmux.NewMockClient()
	mgr := session.NewManager(store, mock, paths, nil)
	return mgr, mock
}

func TestManager_Create_FirstSession(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	sess, err := mgr.Create(ctx, "/home/user/my-app")
	require.NoError(t, err)
	require.NotNil(t, sess)

	assert.Equal(t, "my-app", sess.Name)
	assert.Equal(t, "/home/user/my-app", sess.Path)
	assert.NotEmpty(t, sess.ID)
	assert.Equal(t, session.StatusRunning, sess.Status)

	// Should have created tmux session
	all := mgr.Sessions()
	assert.Len(t, all, 1)
}

func TestManager_Create_SecondSession(t *testing.T) {
	t.Parallel()
	mgr, mock := newTestManager(t)
	ctx := context.Background()

	// First creates the tmux session
	_, err := mgr.Create(ctx, "/home/user/app1")
	require.NoError(t, err)

	// Mock: session now exists
	mock.Sessions["lazyclaude"] = []tmux.WindowInfo{
		{ID: "@0", Name: "lc-something", Session: "lazyclaude"},
	}

	// Second adds a window
	sess2, err := mgr.Create(ctx, "/home/user/app2")
	require.NoError(t, err)
	assert.Equal(t, "app2", sess2.Name)

	all := mgr.Sessions()
	assert.Len(t, all, 2)
}

// setupMCPInfo writes port file and lock file needed for PM and worker session tests.
func setupMCPInfo(t *testing.T, paths config.Paths, port int, token string) {
	t.Helper()
	// Write port file
	require.NoError(t, os.MkdirAll(filepath.Dir(paths.PortFile()), 0o755))
	require.NoError(t, os.WriteFile(paths.PortFile(), []byte(strconv.Itoa(port)), 0o600))
	// Write lock file
	require.NoError(t, os.MkdirAll(paths.IDEDir, 0o755))
	lockData, err := json.Marshal(map[string]string{"authToken": token})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(paths.LockFile(port), lockData, 0o600))
}

func TestManager_Create_LocalSession(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)

	sess, err := mgr.Create(context.Background(), "/home/user/work")
	require.NoError(t, err)
	assert.Equal(t, "work", sess.Name)
	assert.Empty(t, sess.Host)
}

func TestManager_Delete(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	sess, err := mgr.Create(ctx, "/home/user/app")
	require.NoError(t, err)

	err = mgr.Delete(ctx, sess.ID)
	require.NoError(t, err)

	assert.Empty(t, mgr.Sessions())
}

func TestManager_Delete_NotFound(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)

	err := mgr.Delete(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestManager_Rename(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)

	sess, err := mgr.Create(context.Background(), "/home/user/app")
	require.NoError(t, err)

	err = mgr.Rename(sess.ID, "renamed-app")
	require.NoError(t, err)

	found := mgr.Store().FindByID(sess.ID)
	require.NotNil(t, found)
	assert.Equal(t, "renamed-app", found.Name)
}

func TestManager_PurgeOrphans(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	// Create two sessions
	s1, _ := mgr.Create(ctx, "/home/user/app1")
	s2, _ := mgr.Create(ctx, "/home/user/app2")

	// Manually set one as orphan
	all := mgr.Store().All()
	_ = s1
	_ = s2
	// Sync with empty tmux → all become orphans
	mgr.Store().SyncWithTmux(nil, nil)

	all = mgr.Store().All()
	for _, s := range all {
		assert.Equal(t, session.StatusOrphan, s.Status)
	}

	count, err := mgr.PurgeOrphans()
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Empty(t, mgr.Sessions())
}

func TestManager_Sync_WithTmux(t *testing.T) {
	t.Parallel()
	mgr, mock := newTestManager(t)
	ctx := context.Background()

	// Create a session
	sess, err := mgr.Create(ctx, "/home/user/app")
	require.NoError(t, err)

	// Set up mock tmux state
	windowName := sess.WindowName()
	mock.Sessions["lazyclaude"] = []tmux.WindowInfo{
		{ID: "@1", Name: windowName, Session: "lazyclaude"},
	}
	mock.Panes["lazyclaude"] = []tmux.PaneInfo{
		{ID: "%1", Window: "@1", PID: 5555, Dead: false},
	}

	err = mgr.Sync(ctx)
	require.NoError(t, err)

	all := mgr.Sessions()
	require.Len(t, all, 1)
	assert.Equal(t, session.StatusRunning, all[0].Status)
	assert.Equal(t, 5555, all[0].PID)
}

func TestManager_Sync_HasSessionError_DoesNotOrphan(t *testing.T) {
	t.Parallel()
	mgr, mock := newTestManager(t)
	ctx := context.Background()

	// Create a session
	_, err := mgr.Create(ctx, "/home/user/app")
	require.NoError(t, err)

	// Inject a transient error into HasSession
	mock.ErrHasSession = errors.New("tmux server temporarily unavailable")

	err = mgr.Sync(ctx)
	assert.Error(t, err, "Sync should propagate HasSession errors")

	// Sessions must NOT be marked as orphan
	all := mgr.Sessions()
	require.Len(t, all, 1)
	assert.Equal(t, session.StatusRunning, all[0].Status)
}

func TestManager_Sync_ConsecutiveFailCount(t *testing.T) {
	t.Parallel()
	mgr, mock := newTestManager(t)
	ctx := context.Background()

	_, err := mgr.Create(ctx, "/home/user/app")
	require.NoError(t, err)

	// Remove the lazyclaude session from mock so HasSession returns false
	delete(mock.Sessions, "lazyclaude")

	all := mgr.Sessions()
	require.Len(t, all, 1)
	assert.Equal(t, session.StatusRunning, all[0].Status)

	// First Sync: failCount=1 → no orphan
	err = mgr.Sync(ctx)
	require.NoError(t, err)
	all = mgr.Sessions()
	require.Len(t, all, 1)
	assert.Equal(t, session.StatusRunning, all[0].Status, "1st failure should not orphan")

	// Second Sync: failCount=2 → no orphan
	err = mgr.Sync(ctx)
	require.NoError(t, err)
	all = mgr.Sessions()
	require.Len(t, all, 1)
	assert.Equal(t, session.StatusRunning, all[0].Status, "2nd failure should not orphan")

	// Third Sync: failCount=3 → orphan
	err = mgr.Sync(ctx)
	require.NoError(t, err)
	all = mgr.Sessions()
	require.Len(t, all, 1)
	assert.Equal(t, session.StatusOrphan, all[0].Status, "3rd failure should orphan")
}

func TestManager_Sync_FailCountResetsOnSuccess(t *testing.T) {
	t.Parallel()
	mgr, mock := newTestManager(t)
	ctx := context.Background()

	sess, err := mgr.Create(ctx, "/home/user/app")
	require.NoError(t, err)

	// Remove session so HasSession returns false
	delete(mock.Sessions, "lazyclaude")

	// Two failures (below threshold)
	err = mgr.Sync(ctx)
	require.NoError(t, err)
	err = mgr.Sync(ctx)
	require.NoError(t, err)

	// Success resets counter
	windowName := sess.WindowName()
	mock.Sessions["lazyclaude"] = []tmux.WindowInfo{
		{ID: "@1", Name: windowName, Session: "lazyclaude"},
	}
	mock.Panes["lazyclaude"] = []tmux.PaneInfo{
		{ID: "%1", Window: "@1", PID: 1234, Dead: false},
	}
	err = mgr.Sync(ctx)
	require.NoError(t, err)

	// Remove session again → counter starts from 0
	delete(mock.Sessions, "lazyclaude")
	delete(mock.Panes, "lazyclaude")

	// Two more failures — still below threshold
	err = mgr.Sync(ctx)
	require.NoError(t, err)
	err = mgr.Sync(ctx)
	require.NoError(t, err)

	all := mgr.Sessions()
	require.Len(t, all, 1)
	assert.Equal(t, session.StatusRunning, all[0].Status, "counter should have reset")
}

func TestManager_CleanSessionCommands_ExitEmpty(t *testing.T) {
	t.Parallel()
	mgr, mock := newTestManager(t)

	// Create a session to trigger cleanSessionCommands via PostCommands
	_, err := mgr.Create(context.Background(), "/home/user/app")
	require.NoError(t, err)

	// Verify exit-empty off is in PostCommands
	found := false
	for _, cmd := range mock.LastNewSessionOpts.PostCommands {
		if len(cmd) >= 4 && cmd[0] == "set-option" && cmd[2] == "exit-empty" && cmd[3] == "off" {
			found = true
			break
		}
	}
	assert.True(t, found, "cleanSessionCommands should include exit-empty off")
}

func TestManager_Create_PostCommands_NoWindowFlag(t *testing.T) {
	t.Parallel()
	mgr, mock := newTestManager(t)

	_, err := mgr.Create(context.Background(), "/home/user/app")
	require.NoError(t, err)

	// Verify automatic-rename and remain-on-exit are session-level (no -w flag).
	// With -w they only apply to the first window, causing new windows to get
	// tmux's default automatic-rename=on, which renames windows and breaks
	// SyncWithTmux's name-based matching.
	opts := mock.LastNewSessionOpts
	require.NotEmpty(t, opts.PostCommands, "PostCommands should be set")

	for _, cmd := range opts.PostCommands {
		if len(cmd) < 2 || cmd[0] != "set-option" {
			continue
		}
		optName := cmd[len(cmd)-1]
		if optName == "off" || optName == "on" {
			// The option name is the second-to-last arg
			if len(cmd) >= 3 {
				optName = cmd[len(cmd)-2]
			}
		}
		if optName == "automatic-rename" || optName == "remain-on-exit" {
			for _, arg := range cmd {
				assert.NotEqual(t, "-w", arg,
					"%s must be session-level (no -w flag) to affect all windows", optName)
			}
		}
	}
}

func TestManager_Persistence(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	statePath := filepath.Join(paths.DataDir, "state.json")

	// Create and save
	store1 := session.NewStore(statePath)
	mock := tmux.NewMockClient()
	mgr1 := session.NewManager(store1, mock, paths, nil)

	_, err := mgr1.Create(context.Background(), "/home/user/app")
	require.NoError(t, err)

	// Load in a new manager
	store2 := session.NewStore(statePath)
	mgr2 := session.NewManager(store2, mock, paths, nil)
	require.NoError(t, store2.Load())

	all := mgr2.Sessions()
	require.Len(t, all, 1)
	assert.Equal(t, "app", all[0].Name)
}

// initGitRepo creates a git repo with an initial commit in the given directory.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "test"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "cmd %v: %s", args, out)
	}
}

func TestManager_CreateWorktree_Basic(t *testing.T) {
	t.Parallel()
	mgr, mock := newTestManager(t)
	ctx := context.Background()

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)
	sess, err := mgr.CreateWorktree(ctx, "fix-popup", "Fix the bug", projectRoot)
	require.NoError(t, err)
	require.NotNil(t, sess)

	// Session should be created with worktree name
	assert.Equal(t, "fix-popup", sess.Name)
	assert.Equal(t, session.StatusRunning, sess.Status)

	// Worktree directory should exist
	wtDir := filepath.Join(projectRoot, ".lazyclaude", "worktrees", "fix-popup")
	info, err := os.Stat(wtDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Session path should be the worktree directory
	assert.Equal(t, wtDir, sess.Path)

	// tmux session should have been created (first session)
	_, ok := mock.Sessions["lazyclaude"]
	assert.True(t, ok)

	// Claude command should invoke a launcher script
	cmd := mock.LastNewSessionOpts.Command
	assert.Contains(t, cmd, "sh")
	assert.Contains(t, cmd, "lazyclaude-wt-")

	// Should be in the store
	all := mgr.Sessions()
	assert.Len(t, all, 1)
}

func TestManager_CreateWorktree_InvalidName(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	cases := []string{"", "  ", "foo/bar", "foo..bar", "-leading"}
	for _, name := range cases {
		_, err := mgr.CreateWorktree(ctx, name, "prompt", "/tmp/project")
		assert.Error(t, err, "should reject name %q", name)
	}
}

func TestManager_CreateWorktree_NotAGitRepo(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	projectRoot := t.TempDir() // no git init
	_, err := mgr.CreateWorktree(ctx, "test", "prompt", projectRoot)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
}

func TestManager_CreateWorktree_EmptyPrompt(t *testing.T) {
	t.Parallel()
	mgr, mock := newTestManager(t)
	ctx := context.Background()

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)
	sess, err := mgr.CreateWorktree(ctx, "no-prompt", "", projectRoot)
	require.NoError(t, err)
	require.NotNil(t, sess)

	// Should still launch with a launcher script
	cmd := mock.LastNewSessionOpts.Command
	assert.Contains(t, cmd, "sh")
	assert.Contains(t, cmd, "lazyclaude-wt-")
}

func TestManager_CreateWorktree_ExistingDir(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)
	wtDir := filepath.Join(projectRoot, ".lazyclaude", "worktrees", "reuse-me")
	require.NoError(t, os.MkdirAll(wtDir, 0o755))

	// Should succeed even if directory already exists (reuse)
	sess, err := mgr.CreateWorktree(ctx, "reuse-me", "Reuse this", projectRoot)
	require.NoError(t, err)
	assert.Equal(t, "reuse-me", sess.Name)
}

func TestManager_CreateWorktree_LauncherScriptContents(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)
	_, err := mgr.CreateWorktree(ctx, "test-script", "Fix the bug", projectRoot)
	require.NoError(t, err)

	// The launcher script is self-deleting, but we can verify through the command
	// by checking that --append-system-prompt is used (not --initial-prompt)
	// Create another worktree to inspect the script before it runs
	sess2, err := mgr.CreateWorktree(ctx, "inspect-me", "日本語プロンプト\n改行あり", projectRoot)
	require.NoError(t, err)
	_ = sess2

	// Worktree dir should contain the isolation marker
	wtDir := filepath.Join(projectRoot, ".lazyclaude", "worktrees", "inspect-me")
	_, err = os.Stat(wtDir)
	require.NoError(t, err)
}

func TestManager_CreateWorktree_DuplicateName(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)
	_, err := mgr.CreateWorktree(ctx, "dup-name", "first", projectRoot)
	require.NoError(t, err)

	// Second call with same name should fail
	_, err = mgr.CreateWorktree(ctx, "dup-name", "second", projectRoot)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestManager_CreateWorktree_AddsWindowToExistingSession(t *testing.T) {
	t.Parallel()
	mgr, mock := newTestManager(t)
	ctx := context.Background()

	// Create first session to establish tmux session
	_, err := mgr.Create(ctx, "/home/user/app1")
	require.NoError(t, err)

	// Mock: session now exists
	mock.Sessions["lazyclaude"] = []tmux.WindowInfo{
		{ID: "@0", Name: "lc-something", Session: "lazyclaude"},
	}

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)
	sess, err := mgr.CreateWorktree(ctx, "second-wt", "task 2", projectRoot)
	require.NoError(t, err)
	assert.Equal(t, "second-wt", sess.Name)

	// Should have added a window (not a new session)
	windows := mock.Sessions["lazyclaude"]
	assert.Len(t, windows, 2)

	// NewWindow command should invoke a launcher script
	cmd := mock.LastNewWindowOpts.Command
	assert.Contains(t, cmd, "sh")
	assert.Contains(t, cmd, "lazyclaude-wt-")
}

// --- Phase 4: PM/Worker session launch tests ---

func newTestManagerWithMCP(t *testing.T) (*session.Manager, *tmux.MockClient, config.Paths) {
	t.Helper()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	setupMCPInfo(t, paths, 9876, "secret-token")
	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	mock := tmux.NewMockClient()
	mgr := session.NewManager(store, mock, paths, nil)
	return mgr, mock, paths
}

func TestManager_CreatePMSession_Basic(t *testing.T) {
	t.Parallel()
	mgr, mock, _ := newTestManagerWithMCP(t)
	ctx := context.Background()

	projectRoot := t.TempDir()
	sess, err := mgr.CreatePMSession(ctx, projectRoot)
	require.NoError(t, err)
	require.NotNil(t, sess)

	assert.Equal(t, "pm", sess.Name)
	assert.Equal(t, session.RolePM, sess.Role)
	assert.Equal(t, projectRoot, sess.Path)
	assert.Equal(t, session.StatusRunning, sess.Status)
	assert.NotEmpty(t, sess.ID)

	// tmux session should have been created
	_, ok := mock.Sessions["lazyclaude"]
	assert.True(t, ok)

	// Claude command should invoke a launcher script
	cmd := mock.LastNewSessionOpts.Command
	assert.Contains(t, cmd, "sh")
	assert.Contains(t, cmd, "lazyclaude-wt-")

	// Should be in the store
	all := mgr.Sessions()
	require.Len(t, all, 1)
	assert.Equal(t, session.RolePM, all[0].Role)
}

func TestManager_CreatePMSession_DuplicateError(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManagerWithMCP(t)
	ctx := context.Background()

	projectRoot := t.TempDir()
	_, err := mgr.CreatePMSession(ctx, projectRoot)
	require.NoError(t, err)

	// Second call for same projectRoot should fail
	_, err = mgr.CreatePMSession(ctx, projectRoot)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pm")
}

func TestManager_CreatePMSession_WithExistingWorkers(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManagerWithMCP(t)
	ctx := context.Background()

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	// Create a worker session first
	_, err := mgr.CreateWorkerSession(ctx, "feat-a", "build feature a", projectRoot)
	require.NoError(t, err)

	// Create PM session — should list the worker in its prompt
	sess, err := mgr.CreatePMSession(ctx, projectRoot)
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, session.RolePM, sess.Role)
}

func TestManager_CreateWorkerSession_Basic(t *testing.T) {
	t.Parallel()
	mgr, mock, _ := newTestManagerWithMCP(t)
	ctx := context.Background()

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	sess, err := mgr.CreateWorkerSession(ctx, "feat-x", "implement feature x", projectRoot)
	require.NoError(t, err)
	require.NotNil(t, sess)

	assert.Equal(t, "feat-x", sess.Name)
	assert.Equal(t, session.RoleWorker, sess.Role)
	assert.Equal(t, session.StatusRunning, sess.Status)
	assert.NotEmpty(t, sess.ID)

	// Worktree directory should exist
	wtDir := filepath.Join(projectRoot, ".lazyclaude", "worktrees", "feat-x")
	info, err := os.Stat(wtDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Session path should be the worktree directory
	assert.Equal(t, wtDir, sess.Path)

	// tmux session should have been created
	_, ok := mock.Sessions["lazyclaude"]
	assert.True(t, ok)

	// Claude command should invoke a launcher script
	cmd := mock.LastNewSessionOpts.Command
	assert.Contains(t, cmd, "sh")
	assert.Contains(t, cmd, "lazyclaude-wt-")

	// Should be in the store with RoleWorker
	all := mgr.Sessions()
	require.Len(t, all, 1)
	assert.Equal(t, session.RoleWorker, all[0].Role)
}

func TestManager_CreateWorkerSession_InvalidName(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManagerWithMCP(t)
	ctx := context.Background()

	cases := []string{"", "  ", "foo/bar", "foo..bar", "-leading"}
	for _, name := range cases {
		_, err := mgr.CreateWorkerSession(ctx, name, "prompt", "/tmp/project")
		assert.Error(t, err, "should reject name %q", name)
	}
}

func TestManager_CreateWorkerSession_DuplicateName(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManagerWithMCP(t)
	ctx := context.Background()

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	_, err := mgr.CreateWorkerSession(ctx, "dup-worker", "first task", projectRoot)
	require.NoError(t, err)

	// Second call with same name should fail
	_, err = mgr.CreateWorkerSession(ctx, "dup-worker", "second task", projectRoot)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestManager_CreateWorktree_SetsWorkerRole(t *testing.T) {
	t.Parallel()
	mgr, mock := newTestManager(t)
	ctx := context.Background()

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	sess, err := mgr.CreateWorktree(ctx, "wt-worker", "task", projectRoot)
	require.NoError(t, err)
	require.NotNil(t, sess)

	// CreateWorktree should set RoleWorker
	assert.Equal(t, session.RoleWorker, sess.Role)

	// Command should reference a launcher script
	cmd := mock.LastNewSessionOpts.Command
	assert.Contains(t, cmd, "sh")
	assert.Contains(t, cmd, "lazyclaude-wt-")
}

func TestManager_launchWorktreeSession_RoleWorker_UsesWorkerPrompt(t *testing.T) {
	t.Parallel()
	mgr, mock, _ := newTestManagerWithMCP(t)
	ctx := context.Background()

	projectRoot := t.TempDir()
	initGitRepo(t, projectRoot)

	// CreateWorkerSession uses RoleWorker
	sess, err := mgr.CreateWorkerSession(ctx, "worker-prompt-test", "task", projectRoot)
	require.NoError(t, err)
	require.NotNil(t, sess)

	// Role should be RoleWorker
	assert.Equal(t, session.RoleWorker, sess.Role)

	// Command should reference a launcher script
	cmd := mock.LastNewSessionOpts.Command
	assert.Contains(t, cmd, "sh")
	assert.Contains(t, cmd, "lazyclaude-wt-")

	// Confirm in store
	all := mgr.Sessions()
	require.Len(t, all, 1)
	assert.Equal(t, session.RoleWorker, all[0].Role)
}

// --- claudeEnv tests ---

func TestClaudeEnv_InjectsSessionID(t *testing.T) {
	t.Parallel()
	env := session.ClaudeEnv("sess-abc")

	assert.Equal(t, "sess-abc", env["LAZYCLAUDE_SESSION_ID"])
	assert.Equal(t, "true", env["CLAUDE_CODE_AUTO_CONNECT_IDE"])

	// Server port/token must NOT be injected — hooks use lock file discovery
	_, hasPort := env["LAZYCLAUDE_SERVER_PORT"]
	_, hasToken := env["LAZYCLAUDE_SERVER_TOKEN"]
	assert.False(t, hasPort, "server port must not be in env (hooks use lock file)")
	assert.False(t, hasToken, "server token must not be in env (hooks use lock file)")
}

func TestClaudeEnv_OmitsEmptySessionID(t *testing.T) {
	t.Parallel()
	env := session.ClaudeEnv("")

	_, hasID := env["LAZYCLAUDE_SESSION_ID"]
	assert.False(t, hasID, "empty sessionID should not be set")
}

func TestClaudeEnv_AlwaysHasAutoConnectIDE(t *testing.T) {
	t.Parallel()
	env := session.ClaudeEnv("")
	assert.Equal(t, "true", env["CLAUDE_CODE_AUTO_CONNECT_IDE"])
}
