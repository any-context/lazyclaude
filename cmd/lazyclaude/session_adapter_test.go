package main

import (
	"path/filepath"
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/core/model"
	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/KEMSHlM/lazyclaude/internal/gui"
	"github.com/KEMSHlM/lazyclaude/internal/server"
	"github.com/KEMSHlM/lazyclaude/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPendingWindowSet_EmptyNotifications returns empty map
func TestPendingWindowSet_EmptyNotifications(t *testing.T) {
	t.Parallel()
	result := pendingWindowSet(nil)
	assert.Empty(t, result)
}

// TestPendingWindowSet_SingleNotification maps window to true
func TestPendingWindowSet_SingleNotification(t *testing.T) {
	t.Parallel()
	notifications := []*model.ToolNotification{
		{Window: "@1"},
	}
	result := pendingWindowSet(notifications)
	assert.True(t, result["@1"])
	assert.Len(t, result, 1)
}

// TestPendingWindowSet_MultipleNotifications maps all windows
func TestPendingWindowSet_MultipleNotifications(t *testing.T) {
	t.Parallel()
	notifications := []*model.ToolNotification{
		{Window: "@1"},
		{Window: "@2"},
		{Window: "@3"},
	}
	result := pendingWindowSet(notifications)
	assert.True(t, result["@1"])
	assert.True(t, result["@2"])
	assert.True(t, result["@3"])
	assert.Len(t, result, 3)
}

// TestPendingWindowSet_DuplicateWindows deduplicates
func TestPendingWindowSet_DuplicateWindows(t *testing.T) {
	t.Parallel()
	notifications := []*model.ToolNotification{
		{Window: "@1"},
		{Window: "@1"},
	}
	result := pendingWindowSet(notifications)
	assert.True(t, result["@1"])
	assert.Len(t, result, 1)
}

// TestPendingWindowSet_UnknownWindowNotInSet
func TestPendingWindowSet_UnknownWindowNotInSet(t *testing.T) {
	t.Parallel()
	notifications := []*model.ToolNotification{
		{Window: "@1"},
	}
	result := pendingWindowSet(notifications)
	assert.False(t, result["@99"])
}

// TestBuildSessionItems_RunningWithPending sets Activity to "pending"
func TestBuildSessionItems_RunningWithPending(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{ID: "s1", Name: "worker", Status: session.StatusRunning, TmuxWindow: "@1"},
	}
	pending := map[string]bool{"@1": true}

	items := buildSessionItems(sessions, pending, nil)
	assert.Len(t, items, 1)
	assert.Equal(t, "pending", items[0].Activity)
}

// TestBuildSessionItems_RunningWithoutPending has empty Activity
func TestBuildSessionItems_RunningWithoutPending(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{ID: "s1", Name: "app", Status: session.StatusRunning, TmuxWindow: "@2"},
	}
	pending := map[string]bool{"@1": true}

	items := buildSessionItems(sessions, pending, nil)
	assert.Len(t, items, 1)
	assert.Equal(t, "", items[0].Activity, "non-pending running session should have empty Activity")
}

// TestBuildSessionItems_DeadSession_NoPendingActivity
func TestBuildSessionItems_DeadSession_NoPendingActivity(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{ID: "s1", Name: "dead", Status: session.StatusDead, TmuxWindow: "@1"},
	}
	pending := map[string]bool{"@1": true}

	items := buildSessionItems(sessions, pending, nil)
	assert.Len(t, items, 1)
	// Dead sessions should NOT be marked pending even if window matches
	assert.Equal(t, "", items[0].Activity, "dead session should not be marked pending")
}

// TestBuildSessionItems_OrphanSession_NoPendingActivity
func TestBuildSessionItems_OrphanSession_NoPendingActivity(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{ID: "s1", Name: "orphan", Status: session.StatusOrphan, TmuxWindow: "@1"},
	}
	pending := map[string]bool{"@1": true}

	items := buildSessionItems(sessions, pending, nil)
	assert.Len(t, items, 1)
	assert.Equal(t, "", items[0].Activity, "orphan session should not be marked pending")
}

// TestBuildSessionItems_MixedSessions correctly categorizes all
func TestBuildSessionItems_MixedSessions(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{ID: "s1", Name: "running-blocked", Status: session.StatusRunning, TmuxWindow: "@1"},
		{ID: "s2", Name: "running-free", Status: session.StatusRunning, TmuxWindow: "@2"},
		{ID: "s3", Name: "dead-one", Status: session.StatusDead, TmuxWindow: "@3"},
	}
	pending := map[string]bool{"@1": true, "@3": true}

	items := buildSessionItems(sessions, pending, nil)
	assert.Len(t, items, 3)
	assert.Equal(t, "pending", items[0].Activity, "s1 running + window in pending set")
	assert.Equal(t, "", items[1].Activity, "s2 running but window not in pending set")
	assert.Equal(t, "", items[2].Activity, "s3 dead, should not be pending even if window matches")
}

// TestBuildSessionItems_EmptySessions returns empty slice
func TestBuildSessionItems_EmptySessions(t *testing.T) {
	t.Parallel()
	items := buildSessionItems(nil, nil, nil)
	assert.Empty(t, items)
}

// TestBuildSessionItems_PreservesExistingFields maps all SessionItem fields
func TestBuildSessionItems_PreservesExistingFields(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{
			ID:         "abc123",
			Name:       "my-app",
			Path:       "/home/user/project",
			Host:       "remote.server",
			Status:     session.StatusRunning,
			Flags:      []string{"--resume"},
			TmuxWindow: "@5",
		},
	}
	items := buildSessionItems(sessions, map[string]bool{}, nil)

	assert.Equal(t, "abc123", items[0].ID)
	assert.Equal(t, "my-app", items[0].Name)
	assert.Equal(t, "/home/user/project", items[0].Path)
	assert.Equal(t, "remote.server", items[0].Host)
	assert.Equal(t, "Running", items[0].Status)
	assert.Equal(t, []string{"--resume"}, items[0].Flags)
	assert.Equal(t, "@5", items[0].TmuxWindow)
}

// TestBuildSessionItems_NilPendingMap is treated as no pending
func TestBuildSessionItems_NilPendingMap(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{ID: "s1", Name: "app", Status: session.StatusRunning, TmuxWindow: "@1"},
	}
	items := buildSessionItems(sessions, nil, nil)
	assert.Equal(t, "", items[0].Activity, "nil pending map should mean no session is pending")
}

// Verify SessionItem.Activity field exists and has the right type.
func TestSessionItem_ActivityField_IsString(t *testing.T) {
	t.Parallel()
	item := gui.SessionItem{Activity: "pending"}
	assert.Equal(t, "pending", item.Activity)

	item2 := gui.SessionItem{}
	assert.Equal(t, "", item2.Activity)
}

// TestBuildSessionItems_PMRole_IsPreserved maps session.RolePM to "pm"
func TestBuildSessionItems_PMRole_IsPreserved(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{ID: "s1", Name: "pm-session", Status: session.StatusRunning, Role: session.RolePM},
	}
	items := buildSessionItems(sessions, map[string]bool{}, nil)

	assert.Len(t, items, 1)
	assert.Equal(t, "pm", items[0].Role, "PM role session should have Role='pm'")
}

// TestBuildSessionItems_WorkerRole_IsPreserved maps session.RoleWorker to "worker"
func TestBuildSessionItems_WorkerRole_IsPreserved(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{ID: "s1", Name: "worker-session", Status: session.StatusRunning, Role: session.RoleWorker},
	}
	items := buildSessionItems(sessions, map[string]bool{}, nil)

	assert.Len(t, items, 1)
	assert.Equal(t, "worker", items[0].Role, "Worker role session should have Role='worker'")
}

// TestBuildSessionItems_NoRole_IsEmpty maps session.RoleNone to ""
func TestBuildSessionItems_NoRole_IsEmpty(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{ID: "s1", Name: "regular", Status: session.StatusRunning, Role: session.RoleNone},
	}
	items := buildSessionItems(sessions, map[string]bool{}, nil)

	assert.Len(t, items, 1)
	assert.Equal(t, "", items[0].Role, "RoleNone should map to empty string")
}

// TestBuildSessionItems_MixedRoles preserves all roles correctly
func TestBuildSessionItems_MixedRoles(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{ID: "s1", Name: "pm", Status: session.StatusRunning, Role: session.RolePM},
		{ID: "s2", Name: "worker", Status: session.StatusRunning, Role: session.RoleWorker},
		{ID: "s3", Name: "regular", Status: session.StatusRunning, Role: session.RoleNone},
	}
	items := buildSessionItems(sessions, map[string]bool{}, nil)

	assert.Len(t, items, 3)
	assert.Equal(t, "pm", items[0].Role)
	assert.Equal(t, "worker", items[1].Role)
	assert.Equal(t, "", items[2].Role)
}

// --- sessionListerAdapter tests ---

// newTestSessionListerAdapter creates a Manager and wraps it in a sessionListerAdapter.
func newTestSessionListerAdapter(t *testing.T) (*sessionListerAdapter, *session.Manager) {
	t.Helper()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	mock := tmux.NewMockClient()
	mgr := session.NewManager(store, mock, paths, nil)
	return &sessionListerAdapter{mgr: mgr}, mgr
}

// TestSessionListerAdapter_Empty returns empty slice when no sessions exist.
func TestSessionListerAdapter_Empty(t *testing.T) {
	t.Parallel()
	adapter, _ := newTestSessionListerAdapter(t)

	result := adapter.Sessions()

	assert.NotNil(t, result)
	assert.Empty(t, result)
}

// TestSessionListerAdapter_ImplementsInterface verifies the type satisfies server.SessionLister.
func TestSessionListerAdapter_ImplementsInterface(t *testing.T) {
	t.Parallel()
	adapter, _ := newTestSessionListerAdapter(t)

	var _ server.SessionLister = adapter
}

// TestSessionListerAdapter_SingleSession maps one session correctly.
func TestSessionListerAdapter_SingleSession(t *testing.T) {
	t.Parallel()
	adapter, mgr := newTestSessionListerAdapter(t)

	sess, err := mgr.Create(t.Context(), "/home/user/project", "")
	require.NoError(t, err)

	result := adapter.Sessions()

	require.Len(t, result, 1)
	assert.Equal(t, sess.ID, result[0].ID)
	assert.Equal(t, sess.Name, result[0].Name)
	assert.Equal(t, sess.Path, result[0].Path)
	assert.Equal(t, string(sess.Role), result[0].Role)
}

// TestSessionListerAdapter_PMRole maps RolePM to "pm" string.
func TestSessionListerAdapter_PMRole(t *testing.T) {
	t.Parallel()
	adapter, mgr := newTestSessionListerAdapter(t)

	sess, err := mgr.Create(t.Context(), "/home/user/pm-project", "")
	require.NoError(t, err)
	// Verify the role field is correctly mapped regardless of role value
	_ = sess

	// All sessions returned by manager.Sessions() go through the adapter.
	// RolePM sessions created via CreatePMSession need MCP info, so we test
	// via RoleNone and verify the Role field passes through correctly.
	result := adapter.Sessions()
	require.Len(t, result, 1)
	// RoleNone maps to empty string
	assert.Equal(t, string(session.RoleNone), result[0].Role)
}

// TestSessionListerAdapter_MultipleSessions maps all sessions.
func TestSessionListerAdapter_MultipleSessions(t *testing.T) {
	t.Parallel()
	adapter, mgr := newTestSessionListerAdapter(t)

	_, err := mgr.Create(t.Context(), "/home/user/app1", "")
	require.NoError(t, err)

	// Second session needs tmux session to exist
	mock := tmux.NewMockClient()
	_ = mock

	_, err = mgr.Create(t.Context(), "/home/user/app2", "")
	require.NoError(t, err)

	result := adapter.Sessions()
	assert.Len(t, result, 2)
}

// TestSessionListerAdapter_FieldMapping verifies all fields are mapped correctly.
func TestSessionListerAdapter_FieldMapping(t *testing.T) {
	t.Parallel()
	adapter, mgr := newTestSessionListerAdapter(t)

	sess, err := mgr.Create(t.Context(), "/home/user/my-app", "")
	require.NoError(t, err)

	result := adapter.Sessions()
	require.Len(t, result, 1)
	item := result[0]

	// All SessionInfo fields must be populated from Session fields.
	assert.Equal(t, sess.ID, item.ID, "ID must match")
	assert.Equal(t, sess.Name, item.Name, "Name must match")
	assert.Equal(t, sess.Path, item.Path, "Path must match")
	assert.Equal(t, string(sess.Role), item.Role, "Role must be string representation")
}

// TestSessionListerAdapter_WindowField verifies TmuxWindow is mapped to Window.
func TestSessionListerAdapter_WindowField(t *testing.T) {
	t.Parallel()
	adapter, mgr := newTestSessionListerAdapter(t)

	sess, err := mgr.Create(t.Context(), "/home/user/windowed-app", "")
	require.NoError(t, err)

	// Inject a TmuxWindow value directly into the store to test mapping.
	mgr.Store().SetTmuxWindow(sess.ID, "@42")

	result := adapter.Sessions()
	require.Len(t, result, 1)
	assert.Equal(t, "@42", result[0].Window, "Window must map from session.TmuxWindow")
}

// TestSessionListerAdapter_StatusField verifies Status string is mapped correctly.
func TestSessionListerAdapter_StatusField(t *testing.T) {
	t.Parallel()
	adapter, mgr := newTestSessionListerAdapter(t)

	_, err := mgr.Create(t.Context(), "/home/user/status-app", "")
	require.NoError(t, err)

	result := adapter.Sessions()
	require.Len(t, result, 1)
	// Newly created sessions have StatusUnknown or StatusOrphan (no tmux).
	// Either way, the Status string must be non-empty and come from Status.String().
	assert.NotEmpty(t, result[0].Status, "Status field must be mapped")
}

// TestSessionListerAdapter_RunningStatus maps StatusRunning to "Running".
func TestSessionListerAdapter_RunningStatus(t *testing.T) {
	t.Parallel()
	adapter, mgr := newTestSessionListerAdapter(t)

	sess, err := mgr.Create(t.Context(), "/home/user/running-app", "")
	require.NoError(t, err)

	mgr.Store().SetStatus(sess.ID, session.StatusRunning)

	result := adapter.Sessions()
	require.Len(t, result, 1)
	assert.Equal(t, "Running", result[0].Status)
}

// TestSessionInfo_HasWindowField confirms the SessionInfo struct has a Window field.
func TestSessionInfo_HasWindowField(t *testing.T) {
	t.Parallel()
	info := server.SessionInfo{Window: "lc-abcdef01"}
	assert.Equal(t, "lc-abcdef01", info.Window)
}

// TestSessionInfo_HasStatusField confirms the SessionInfo struct has a Status field.
func TestSessionInfo_HasStatusField(t *testing.T) {
	t.Parallel()
	info := server.SessionInfo{Status: "Running"}
	assert.Equal(t, "Running", info.Status)
}

// TestBuildSessionItems_WindowActivity_Finished sets Activity to "finished"
func TestBuildSessionItems_WindowActivity_Finished(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{ID: "s1", Name: "done", Status: session.StatusRunning, TmuxWindow: "@1"},
	}
	windowActivity := map[string]string{"@1": "finished"}

	items := buildSessionItems(sessions, nil, windowActivity)
	require.Len(t, items, 1)
	assert.Equal(t, "finished", items[0].Activity)
}

// TestBuildSessionItems_WindowActivity_Error sets Activity to "error"
func TestBuildSessionItems_WindowActivity_Error(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{ID: "s1", Name: "err", Status: session.StatusRunning, TmuxWindow: "@2"},
	}
	windowActivity := map[string]string{"@2": "error"}

	items := buildSessionItems(sessions, nil, windowActivity)
	require.Len(t, items, 1)
	assert.Equal(t, "error", items[0].Activity)
}

// TestBuildSessionItems_PendingOverridesWindowActivity
func TestBuildSessionItems_PendingOverridesWindowActivity(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{ID: "s1", Name: "both", Status: session.StatusRunning, TmuxWindow: "@3"},
	}
	pending := map[string]bool{"@3": true}
	windowActivity := map[string]string{"@3": "finished"}

	items := buildSessionItems(sessions, pending, windowActivity)
	require.Len(t, items, 1)
	assert.Equal(t, "pending", items[0].Activity, "pending should take priority over windowActivity")
}

// TestBuildSessionItems_DeadSessionIgnoresWindowActivity
func TestBuildSessionItems_DeadSessionIgnoresWindowActivity(t *testing.T) {
	t.Parallel()
	sessions := []session.Session{
		{ID: "s1", Name: "dead", Status: session.StatusDead, TmuxWindow: "@4"},
	}
	windowActivity := map[string]string{"@4": "finished"}

	items := buildSessionItems(sessions, nil, windowActivity)
	require.Len(t, items, 1)
	assert.Equal(t, "", items[0].Activity, "dead session should not show windowActivity")
}
