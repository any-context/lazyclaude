package session_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGC_RemovesDeadSessions(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	mock := tmux.NewMockClient()
	mgr := session.NewManager(store, mock, paths, nil)

	sess, err := mgr.Create(context.Background(), "/home/user/app")
	require.NoError(t, err)

	// Backdate past GC grace period
	store.BackdateForTest(sess.ID, 30*time.Second)

	// Set up tmux mock: session exists but pane is dead
	windowName := sess.WindowName()
	mock.Sessions["lazyclaude"] = []tmux.WindowInfo{
		{ID: "@1", Name: windowName, Session: "lazyclaude"},
	}
	mock.Panes["lazyclaude"] = []tmux.PaneInfo{
		{ID: "%1", Window: "@1", PID: 0, Dead: true},
	}

	// Run GC
	gc := session.NewGC(mgr, 50*time.Millisecond)
	gc.Start()

	// Wait for GC to run
	require.Eventually(t, func() bool {
		return len(mgr.Sessions()) == 0
	}, 2*time.Second, 50*time.Millisecond, "GC should have removed dead session")

	gc.Stop()
}

func TestGC_RemovesOrphanSessions(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	mock := tmux.NewMockClient()
	mgr := session.NewManager(store, mock, paths, nil)

	sess, err := mgr.Create(context.Background(), "/home/user/app")
	require.NoError(t, err)

	// Backdate past GC grace period
	store.BackdateForTest(sess.ID, 30*time.Second)

	// Mock: tmux session exists but has no matching window (orphan)
	mock.Sessions["lazyclaude"] = []tmux.WindowInfo{}
	mock.Panes["lazyclaude"] = []tmux.PaneInfo{}

	gc := session.NewGC(mgr, 50*time.Millisecond)
	gc.Start()

	require.Eventually(t, func() bool {
		return len(mgr.Sessions()) == 0
	}, 2*time.Second, 50*time.Millisecond)

	gc.Stop()
}

func TestGC_KeepsRunningSessions(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	mock := tmux.NewMockClient()
	mgr := session.NewManager(store, mock, paths, nil)

	sess, err := mgr.Create(context.Background(), "/home/user/app")
	require.NoError(t, err)

	// Mock: pane is alive
	windowName := sess.WindowName()
	mock.Sessions["lazyclaude"] = []tmux.WindowInfo{
		{ID: "@1", Name: windowName, Session: "lazyclaude"},
	}
	mock.Panes["lazyclaude"] = []tmux.PaneInfo{
		{ID: "%1", Window: "@1", PID: 1234, Dead: false},
	}

	gc := session.NewGC(mgr, 50*time.Millisecond)
	gc.Start()

	// Wait a few cycles
	time.Sleep(200 * time.Millisecond)

	assert.Len(t, mgr.Sessions(), 1, "running session should not be removed")

	gc.Stop()
}

func TestGC_GracePeriodSkipsNewSessions(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	mock := tmux.NewMockClient()
	mgr := session.NewManager(store, mock, paths, nil)

	// Create a session (CreatedAt = now, within grace period)
	sess, err := mgr.Create(context.Background(), "/home/user/app")
	require.NoError(t, err)

	// Mock: tmux session exists but window name doesn't match (simulating automatic-rename)
	mock.Sessions["lazyclaude"] = []tmux.WindowInfo{
		{ID: "@1", Name: "claude", Session: "lazyclaude"}, // renamed by tmux!
	}
	mock.Panes["lazyclaude"] = []tmux.PaneInfo{
		{ID: "%1", Window: "@1", PID: 1234, Dead: false},
	}

	// GC should skip this session because it was just created (grace period)
	gc := session.NewGC(mgr, 50*time.Millisecond)
	gc.Start()

	// Wait several GC cycles
	time.Sleep(300 * time.Millisecond)

	// Session should still exist despite being marked Orphan
	sessions := mgr.Sessions()
	assert.Len(t, sessions, 1, "GC should NOT delete session within grace period")
	assert.Equal(t, sess.ID, sessions[0].ID)

	gc.Stop()
}

func TestGC_DeletesAfterGracePeriod(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	mock := tmux.NewMockClient()
	mgr := session.NewManager(store, mock, paths, nil)

	// Create a session and backdate CreatedAt to be outside grace period
	_, err := mgr.Create(context.Background(), "/home/user/app")
	require.NoError(t, err)

	// Backdate the session to make it older than grace period
	all := store.All()
	require.Len(t, all, 1)
	store.BackdateForTest(all[0].ID, 30*time.Second)

	// Mock: tmux session exists but no matching window (orphan)
	mock.Sessions["lazyclaude"] = []tmux.WindowInfo{}
	mock.Panes["lazyclaude"] = []tmux.PaneInfo{}

	gc := session.NewGC(mgr, 50*time.Millisecond)
	gc.Start()

	require.Eventually(t, func() bool {
		return len(mgr.Sessions()) == 0
	}, 2*time.Second, 50*time.Millisecond, "GC should delete session after grace period")

	gc.Stop()
}

func TestGC_StopIsIdempotent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	mock := tmux.NewMockClient()
	mgr := session.NewManager(store, mock, paths, nil)

	gc := session.NewGC(mgr, 50*time.Millisecond)
	gc.Start()
	gc.Stop()
	gc.Stop() // should not panic
}
