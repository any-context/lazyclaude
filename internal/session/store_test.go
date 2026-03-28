package session_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/KEMSHlM/lazyclaude/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSession(id, name, path string) session.Session {
	return session.Session{
		ID:        id,
		Name:      name,
		Path:      path,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func TestStore_SaveAndLoad(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s1 := session.NewStore(path)
	s1.Add(newTestSession("id-1", "my-app", "/home/user/my-app"))
	s1.Add(newTestSession("id-2", "my-lib", "/home/user/my-lib"))
	require.NoError(t, s1.Save())

	s2 := session.NewStore(path)
	require.NoError(t, s2.Load())

	all := s2.All()
	require.Len(t, all, 2)
	assert.Equal(t, "my-app", all[0].Name)
	assert.Equal(t, "my-lib", all[1].Name)
	assert.Equal(t, "/home/user/my-app", all[0].Path)
}

func TestStore_Load_NonExistentFile(t *testing.T) {
	t.Parallel()
	s := session.NewStore("/tmp/lazyclaude-test-nonexistent/state.json")
	err := s.Load()
	require.NoError(t, err)
	assert.Empty(t, s.All())
}

func TestStore_FindByID(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("id-1", "my-app", "/path"))

	found := s.FindByID("id-1")
	require.NotNil(t, found)
	assert.Equal(t, "my-app", found.Name)

	notFound := s.FindByID("nonexistent")
	assert.Nil(t, notFound)
}

func TestStore_FindByName(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("id-1", "my-app", "/path"))

	found := s.FindByName("my-app")
	require.NotNil(t, found)
	assert.Equal(t, "id-1", found.ID)

	notFound := s.FindByName("nonexistent")
	assert.Nil(t, notFound)
}

func TestStore_Remove(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("id-1", "my-app", "/path"))
	s.Add(newTestSession("id-2", "my-lib", "/path2"))

	ok := s.Remove("id-1")
	assert.True(t, ok)
	assert.Len(t, s.All(), 1)
	assert.Equal(t, "my-lib", s.All()[0].Name)

	ok = s.Remove("nonexistent")
	assert.False(t, ok)
}

func TestStore_Rename(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("id-1", "my-app", "/path"))

	ok := s.Rename("id-1", "renamed-app")
	assert.True(t, ok)

	found := s.FindByID("id-1")
	require.NotNil(t, found)
	assert.Equal(t, "renamed-app", found.Name)

	ok = s.Rename("nonexistent", "foo")
	assert.False(t, ok)
}

func TestStore_GenerateName_Simple(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	name := s.GenerateName("/home/user/my-app", "")
	assert.Equal(t, "my-app", name)
}

func TestStore_GenerateName_Dedup(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("id-1", "my-app", "/path1"))

	name := s.GenerateName("/other/path/my-app", "")
	assert.Equal(t, "my-app-2", name)
}

func TestStore_GenerateName_Dedup_Multiple(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("id-1", "my-app", "/path1"))
	s.Add(newTestSession("id-2", "my-app-2", "/path2"))

	name := s.GenerateName("/other/my-app", "")
	assert.Equal(t, "my-app-3", name)
}

func TestStore_GenerateName_Remote(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	name := s.GenerateName("/home/user/work", "srv1")
	assert.Equal(t, "work", name)
}

func TestSession_WindowName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id   string
		want string
	}{
		{"550e8400-e29b-41d4-a716-446655440000", "lc-550e8400"},
		{"short", "lc-short"},
		{"12345678", "lc-12345678"},
	}
	for _, tt := range tests {
		s := session.Session{ID: tt.id}
		assert.Equal(t, tt.want, s.WindowName())
	}
}

func TestStatus_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Unknown", session.StatusUnknown.String())
	assert.Equal(t, "Detached", session.StatusDetached.String())
	assert.Equal(t, "Running", session.StatusRunning.String())
	assert.Equal(t, "Dead", session.StatusDead.String())
	assert.Equal(t, "Orphan", session.StatusOrphan.String())
}

func TestSession_InitialStatusIsUnknown(t *testing.T) {
	t.Parallel()
	s := session.Session{ID: "test"}
	assert.Equal(t, session.StatusUnknown, s.Status)
}

func TestStore_SyncWithTmux(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("550e8400-aaa", "my-app", "/path1"))   // will match
	s.Add(newTestSession("6ba7b810-bbb", "my-lib", "/path2"))   // will match (dead)
	s.Add(newTestSession("cccccccc-ddd", "orphaned", "/path3")) // no tmux window

	windows := []tmux.WindowInfo{
		{ID: "@1", Name: "lc-550e8400", Session: "lazyclaude"},
		{ID: "@2", Name: "lc-6ba7b810", Session: "lazyclaude"},
	}
	panes := []tmux.PaneInfo{
		{ID: "%1", Window: "@1", PID: 1001, Dead: false},
		{ID: "%2", Window: "@2", PID: 0, Dead: true},
	}

	s.SyncWithTmux(windows, panes)

	all := s.All()
	require.Len(t, all, 3)

	// my-app: running
	assert.Equal(t, session.StatusRunning, all[0].Status)
	assert.Equal(t, "@1", all[0].TmuxWindow)
	assert.Equal(t, 1001, all[0].PID)

	// my-lib: dead pane
	assert.Equal(t, session.StatusDead, all[1].Status)

	// orphaned: no tmux window
	assert.Equal(t, session.StatusOrphan, all[2].Status)
	assert.Equal(t, "", all[2].TmuxWindow)
}

// --- Role field backward-compatibility tests ---

func TestStore_BackwardCompat_NoRoleField(t *testing.T) {
	t.Parallel()
	// Legacy format (flat []Session) is reset to empty on load
	legacy := `[{"id":"id-1","name":"my-app","path":"/path","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}]`
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	require.NoError(t, os.WriteFile(path, []byte(legacy), 0o600))

	store := session.NewStore(path)
	require.NoError(t, store.Load())

	// Legacy format should be reset
	assert.Empty(t, store.All())
	assert.Empty(t, store.Projects())
}

func TestStore_RoleRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	store := session.NewStore(path)
	sess := newTestSession("id-pm", "pm-session", "/project")
	sess.Role = session.RolePM
	store.Add(sess)
	require.NoError(t, store.Save())

	store2 := session.NewStore(path)
	require.NoError(t, store2.Load())

	all := store2.All()
	require.Len(t, all, 1)
	assert.Equal(t, session.RolePM, all[0].Role)
}

func TestStore_RoleOmittedWhenNone(t *testing.T) {
	t.Parallel()
	// RoleNone sessions must not emit "role" key in JSON (omitempty)
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	store := session.NewStore(path)
	sess := newTestSession("id-1", "regular", "/project")
	// Role is zero value (RoleNone)
	store.Add(sess)
	require.NoError(t, store.Save())

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	// New format: {"version":2,"projects":[...]}
	var sf map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &sf))
	var projects []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(sf["projects"], &projects))
	require.Len(t, projects, 1)
	// Check the session inside the project
	var sessions []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(projects[0]["sessions"], &sessions))
	require.Len(t, sessions, 1)
	_, hasRole := sessions[0]["role"]
	assert.False(t, hasRole, "role key should be absent when RoleNone (omitempty)")
}

func TestStore_WorkerRoleRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	store := session.NewStore(path)
	sess := newTestSession("id-w", "worker-1", "/project/.claude/worktrees/feat-x")
	sess.Role = session.RoleWorker
	store.Add(sess)
	require.NoError(t, store.Save())

	store2 := session.NewStore(path)
	require.NoError(t, store2.Load())

	all := store2.All()
	require.Len(t, all, 1)
	assert.Equal(t, session.RoleWorker, all[0].Role)
}

func TestStore_SyncWithTmux_Detached(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("aabbccdd-eee", "detached-app", "/path"))

	windows := []tmux.WindowInfo{
		{ID: "@5", Name: "lc-aabbccdd", Session: "lazyclaude"},
	}
	// Window exists but no pane info
	panes := []tmux.PaneInfo{}

	s.SyncWithTmux(windows, panes)

	all := s.All()
	require.Len(t, all, 1)
	assert.Equal(t, session.StatusDetached, all[0].Status)
}