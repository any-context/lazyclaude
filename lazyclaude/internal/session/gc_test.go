package session_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/KEMSHlM/lazyclaude/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGC_RemovesDeadSessions(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	mock := tmux.NewMockClient()
	mgr := session.NewManager(store, mock, paths)

	// Create a session and backdate it past the grace period
	sess, err := mgr.Create(context.Background(), "/home/user/app", "")
	require.NoError(t, err)
	store.SetCreatedAt(sess.ID, time.Now().Add(-1*time.Minute))

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
	mgr := session.NewManager(store, mock, paths)

	// Create a session and backdate it past the grace period
	sess, err := mgr.Create(context.Background(), "/home/user/app", "")
	require.NoError(t, err)
	store.SetCreatedAt(sess.ID, time.Now().Add(-1*time.Minute))

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
	mgr := session.NewManager(store, mock, paths)

	sess, err := mgr.Create(context.Background(), "/home/user/app", "")
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

func TestGC_GracePeriod_SkipsRecentSessions(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	mock := tmux.NewMockClient()
	mgr := session.NewManager(store, mock, paths)

	// Create a session (just now)
	sess, err := mgr.Create(context.Background(), "/home/user/app", "")
	require.NoError(t, err)

	// Mock: pane is dead (but session was just created)
	windowName := sess.WindowName()
	mock.Sessions["lazyclaude"] = []tmux.WindowInfo{
		{ID: "@1", Name: windowName, Session: "lazyclaude"},
	}
	mock.Panes["lazyclaude"] = []tmux.PaneInfo{
		{ID: "%1", Window: "@1", PID: 0, Dead: true},
	}

	gc := session.NewGC(mgr, 50*time.Millisecond)
	gc.Start()

	// Wait a few GC cycles
	time.Sleep(300 * time.Millisecond)

	// Session should NOT be deleted (grace period = 10s)
	assert.Len(t, mgr.Sessions(), 1, "recently created session should not be GC'd")

	gc.Stop()
}

func TestGC_StopIsIdempotent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	mock := tmux.NewMockClient()
	mgr := session.NewManager(store, mock, paths)

	gc := session.NewGC(mgr, 50*time.Millisecond)
	gc.Start()
	gc.Stop()
	gc.Stop() // should not panic
}
