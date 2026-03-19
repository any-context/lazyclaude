package session_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/KEMSHlM/lazyclaude/internal/session"
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

	sess, err := mgr.Create(ctx, "/home/user/my-app", "")
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
	_, err := mgr.Create(ctx, "/home/user/app1", "")
	require.NoError(t, err)

	// Mock: session now exists
	mock.Sessions["lazyclaude"] = []tmux.WindowInfo{
		{ID: "@0", Name: "lc-something", Session: "lazyclaude"},
	}

	// Second adds a window
	sess2, err := mgr.Create(ctx, "/home/user/app2", "")
	require.NoError(t, err)
	assert.Equal(t, "app2", sess2.Name)

	all := mgr.Sessions()
	assert.Len(t, all, 2)
}

// setupMCPInfo writes port file and lock file so Manager.Create can build SSH commands.
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

func TestManager_Create_RemoteSession(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	setupMCPInfo(t, paths, 12345, "test-token")

	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	mock := tmux.NewMockClient()
	mgr := session.NewManager(store, mock, paths, nil)

	sess, err := mgr.Create(context.Background(), "/home/user/work", "srv1")
	require.NoError(t, err)
	assert.Equal(t, "srv1:work", sess.Name)
	assert.Equal(t, "srv1", sess.Host)

	// Verify SSH command was passed to tmux
	lastCmd := mock.LastNewSessionOpts.Command
	assert.Contains(t, lastCmd, "ssh")
	assert.Contains(t, lastCmd, "srv1")
	assert.Contains(t, lastCmd, "-R")
	assert.Contains(t, lastCmd, "claude")
}

func TestManager_Delete(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)
	ctx := context.Background()

	sess, err := mgr.Create(ctx, "/home/user/app", "")
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

	sess, err := mgr.Create(context.Background(), "/home/user/app", "")
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
	s1, _ := mgr.Create(ctx, "/home/user/app1", "")
	s2, _ := mgr.Create(ctx, "/home/user/app2", "")

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
	sess, err := mgr.Create(ctx, "/home/user/app", "")
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

func TestManager_Create_PostCommands_NoWindowFlag(t *testing.T) {
	t.Parallel()
	mgr, mock := newTestManager(t)

	_, err := mgr.Create(context.Background(), "/home/user/app", "")
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

	_, err := mgr1.Create(context.Background(), "/home/user/app", "")
	require.NoError(t, err)

	// Load in a new manager
	store2 := session.NewStore(statePath)
	mgr2 := session.NewManager(store2, mock, paths, nil)
	require.NoError(t, store2.Load())

	all := mgr2.Sessions()
	require.Len(t, all, 1)
	assert.Equal(t, "app", all[0].Name)
}
