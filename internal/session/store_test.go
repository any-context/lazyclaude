package session_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/session"
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

func TestSession_TmuxTarget(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		sess session.Session
		want string
	}{
		{
			name: "local with TmuxWindow ID",
			sess: session.Session{ID: "0123456789abcdef", TmuxWindow: "@42"},
			want: "lazyclaude:@42",
		},
		{
			name: "local with TmuxWindow already prefixed",
			sess: session.Session{ID: "0123456789abcdef", TmuxWindow: "lazyclaude:@42"},
			want: "lazyclaude:@42",
		},
		{
			name: "local fallback (empty TmuxWindow)",
			sess: session.Session{ID: "0123456789abcdef"},
			want: "lazyclaude:lc-01234567",
		},
		{
			name: "remote mirror with TmuxWindow name",
			sess: session.Session{ID: "0123456789abcdef", Host: "AERO", TmuxWindow: "rm-01234567"},
			want: "lazyclaude:rm-01234567",
		},
		{
			name: "remote fallback (empty TmuxWindow, desync)",
			sess: session.Session{ID: "0123456789abcdef", Host: "AERO"},
			want: "lazyclaude:rm-01234567",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.sess.TmuxTarget())
		})
	}
}

func TestStore_SaveAndLoad(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s1 := session.NewStore(path)
	s1.Add(newTestSession("id-1", "my-app", "/home/user/my-app"), "")
	s1.Add(newTestSession("id-2", "my-lib", "/home/user/my-lib"), "")
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
	s.Add(newTestSession("id-1", "my-app", "/path"), "")

	found := s.FindByID("id-1")
	require.NotNil(t, found)
	assert.Equal(t, "my-app", found.Name)

	notFound := s.FindByID("nonexistent")
	assert.Nil(t, notFound)
}

func TestStore_FindByName(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("id-1", "my-app", "/path"), "")

	found := s.FindByName("my-app")
	require.NotNil(t, found)
	assert.Equal(t, "id-1", found.ID)

	notFound := s.FindByName("nonexistent")
	assert.Nil(t, notFound)
}

func TestStore_Remove(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("id-1", "my-app", "/path"), "")
	s.Add(newTestSession("id-2", "my-lib", "/path2"), "")

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
	s.Add(newTestSession("id-1", "my-app", "/path"), "")

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

	name := s.GenerateName("/home/user/my-app")
	assert.Equal(t, "my-app", name)
}

func TestStore_GenerateName_Dedup(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("id-1", "my-app", "/path1"), "")

	name := s.GenerateName("/other/path/my-app")
	assert.Equal(t, "my-app-2", name)
}

func TestStore_GenerateName_Dedup_Multiple(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("id-1", "my-app", "/path1"), "")
	s.Add(newTestSession("id-2", "my-app-2", "/path2"), "")

	name := s.GenerateName("/other/my-app")
	assert.Equal(t, "my-app-3", name)
}

func TestStore_GenerateName_PathOnly(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	name := s.GenerateName("/home/user/work")
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
	s.Add(newTestSession("550e8400-aaa", "my-app", "/path1"), "")   // will match
	s.Add(newTestSession("6ba7b810-bbb", "my-lib", "/path2"), "")   // will match (dead)
	s.Add(newTestSession("cccccccc-ddd", "orphaned", "/path3"), "") // no tmux window

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
	store.Add(sess, "")
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
	store.Add(sess, "")
	require.NoError(t, store.Save())

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	// Current format: {"version":3,"projects":[...]}
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
	sess := newTestSession("id-w", "worker-1", "/project/.lazyclaude/worktrees/feat-x")
	sess.Role = session.RoleWorker
	store.Add(sess, "")
	require.NoError(t, store.Save())

	store2 := session.NewStore(path)
	require.NoError(t, store2.Load())

	all := store2.All()
	require.Len(t, all, 1)
	assert.Equal(t, session.RoleWorker, all[0].Role)
}

// --- Profile field tests ---

func TestStore_Load_MissingProfileFieldDefaultsToEmpty(t *testing.T) {
	t.Parallel()
	// v2 state.json written before the Profile field existed must load
	// via migration without error, and sessions must have Profile == "".
	v2Data := `{
  "version": 2,
  "projects": [{
    "id": "proj-1",
    "name": "project",
    "path": "/tmp/project",
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z",
    "sessions": [{
      "id": "abc-123",
      "name": "test",
      "path": "/tmp/project",
      "created_at": "2024-01-01T00:00:00Z",
      "updated_at": "2024-01-01T00:00:00Z"
    }]
  }]
}`
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	require.NoError(t, os.WriteFile(path, []byte(v2Data), 0o600))

	store := session.NewStore(path)
	require.NoError(t, store.Load())

	all := store.All()
	require.Len(t, all, 1)
	assert.Equal(t, "", all[0].Profile)
}

func TestStore_ProfileRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	store := session.NewStore(path)
	sess := newTestSession("id-p", "with-profile", "/project")
	sess.Profile = "opus"
	store.Add(sess, "")
	require.NoError(t, store.Save())

	store2 := session.NewStore(path)
	require.NoError(t, store2.Load())

	all := store2.All()
	require.Len(t, all, 1)
	assert.Equal(t, "opus", all[0].Profile)
}

func TestStore_ProfileOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	// Profile="" must not emit "profile" key in JSON (omitempty).
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	store := session.NewStore(path)
	sess := newTestSession("id-1", "default-profile", "/project")
	// Profile left as zero value
	store.Add(sess, "")
	require.NoError(t, store.Save())

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var sf map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &sf))
	var projects []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(sf["projects"], &projects))
	require.Len(t, projects, 1)
	var sessions []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(projects[0]["sessions"], &sessions))
	require.Len(t, sessions, 1)
	_, hasProfile := sessions[0]["profile"]
	assert.False(t, hasProfile, "profile key should be absent when empty (omitempty)")
}

// --- FindProjectForSession tests ---

func TestStore_FindProjectForSession_RegularSession(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("id-1", "my-app", "/project/a"), "")
	s.Add(newTestSession("id-2", "my-lib", "/project/b"), "")

	p := s.FindProjectForSession("id-1")
	require.NotNil(t, p)
	assert.Equal(t, "/project/a", p.Path)
}

func TestStore_FindProjectForSession_PMSession(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	pmSess := newTestSession("pm-id", "pm", "/project")
	pmSess.Role = session.RolePM
	s.Add(pmSess, "")

	p := s.FindProjectForSession("pm-id")
	require.NotNil(t, p)
	assert.Equal(t, "/project", p.Path)
}

func TestStore_FindProjectForSession_NotFound(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("id-1", "my-app", "/project"), "")

	p := s.FindProjectForSession("nonexistent")
	assert.Nil(t, p)
}

func TestStore_FindProjectForSession_MultipleProjects(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("id-a", "app-a", "/project/a"), "")
	s.Add(newTestSession("id-b", "app-b", "/project/b"), "")

	p := s.FindProjectForSession("id-b")
	require.NotNil(t, p)
	assert.Equal(t, "/project/b", p.Path)
}

func TestStore_FindProjectForSession_WorkerInWorktree(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	// Worktree path -> InferProjectRoot maps to /project
	workerSess := newTestSession("w-id", "feat-x", "/project/.lazyclaude/worktrees/feat-x")
	workerSess.Role = session.RoleWorker
	s.Add(workerSess, "")

	p := s.FindProjectForSession("w-id")
	require.NotNil(t, p)
	assert.Equal(t, "/project", p.Path)
}

func TestStore_SyncWithTmux_Detached(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("aabbccdd-eee", "detached-app", "/path"), "")

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

func TestStore_SyncWithTmux_PrefersAlivePaneOverDead(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("aabbccdd-eee", "my-app", "/path"), "")

	windows := []tmux.WindowInfo{
		{ID: "@5", Name: "lc-aabbccdd", Session: "lazyclaude"},
	}
	// Multiple panes in same window: dead pane listed first, alive pane second.
	// With remain-on-exit=on a dead pane can coexist with a respawned one.
	panes := []tmux.PaneInfo{
		{ID: "%10", Window: "@5", PID: 0, Dead: true},
		{ID: "%11", Window: "@5", PID: 9999, Dead: false},
	}

	s.SyncWithTmux(windows, panes)

	all := s.All()
	require.Len(t, all, 1)
	assert.Equal(t, session.StatusRunning, all[0].Status, "alive pane should take precedence over dead pane")
	assert.Equal(t, 9999, all[0].PID)
}

func TestStore_SyncWithTmux_PrefersAlivePaneOverDead_ReverseOrder(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("aabbccdd-eee", "my-app", "/path"), "")

	windows := []tmux.WindowInfo{
		{ID: "@5", Name: "lc-aabbccdd", Session: "lazyclaude"},
	}
	// Alive pane listed first, dead pane second — alive should still win.
	panes := []tmux.PaneInfo{
		{ID: "%11", Window: "@5", PID: 9999, Dead: false},
		{ID: "%10", Window: "@5", PID: 0, Dead: true},
	}

	s.SyncWithTmux(windows, panes)

	all := s.All()
	require.Len(t, all, 1)
	assert.Equal(t, session.StatusRunning, all[0].Status, "alive pane should take precedence over dead pane")
	assert.Equal(t, 9999, all[0].PID)
}

// --- Host-aware project matching tests ---

func newTestSessionWithHost(id, name, path, host string) session.Session {
	return session.Session{
		ID:        id,
		Name:      name,
		Path:      path,
		Host:      host,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func TestStore_Add_SamePathDifferentHosts_CreatesSeparateProjects(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	// Local session
	s.Add(newTestSession("local-1", "local-app", "/home/user/project"), "")
	// SSH session with same path but different host
	s.Add(newTestSessionWithHost("ssh-1", "remote-app", "/home/user/project", "user@srv1"), "")

	projects := s.Projects()
	require.Len(t, projects, 2, "same path + different host should create separate projects")

	// Verify each project has exactly one session
	assert.Len(t, projects[0].Sessions, 1)
	assert.Len(t, projects[1].Sessions, 1)
}

func TestStore_Add_SamePathSameHost_SameProject(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	s.Add(newTestSessionWithHost("ssh-1", "app-1", "/home/user/project", "user@srv1"), "")
	s.Add(newTestSessionWithHost("ssh-2", "app-2", "/home/user/project", "user@srv1"), "")

	projects := s.Projects()
	require.Len(t, projects, 1, "same path + same host should group into one project")
	assert.Len(t, projects[0].Sessions, 2)
}

func TestStore_Add_PMHostMatchesWorkerHost(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	// Add PM first
	pmSess := newTestSessionWithHost("pm-1", "pm", "/remote/project", "user@srv1")
	pmSess.Role = session.RolePM
	s.Add(pmSess, "")

	// Add worker with same host -- should match the PM's project
	s.Add(newTestSessionWithHost("w-1", "worker-1", "/remote/project", "user@srv1"), "")

	projects := s.Projects()
	require.Len(t, projects, 1)
	pmFound := projects[0].FindPM()
	require.NotNil(t, pmFound)
	assert.Equal(t, "pm-1", pmFound.ID)
	// PM + worker = 2 sessions
	assert.Len(t, projects[0].Sessions, 2)
}

func TestStore_Add_WorkerHostMatchesNewPM(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	// Add worker first (no PM yet)
	s.Add(newTestSessionWithHost("w-1", "worker-1", "/remote/project", "user@srv1"), "")

	// Add PM with same host -- should match the worker's project
	pmSess := newTestSessionWithHost("pm-1", "pm", "/remote/project", "user@srv1")
	pmSess.Role = session.RolePM
	s.Add(pmSess, "")

	projects := s.Projects()
	require.Len(t, projects, 1)
	pmFound := projects[0].FindPM()
	require.NotNil(t, pmFound)
	// worker + PM = 2 sessions
	assert.Len(t, projects[0].Sessions, 2)
}

func TestStore_Add_LocalDoesNotMatchSSH(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	// SSH session
	s.Add(newTestSessionWithHost("ssh-1", "remote-app", "/home/user/project", "user@srv1"), "")
	// Local session with same path
	s.Add(newTestSession("local-1", "local-app", "/home/user/project"), "")

	projects := s.Projects()
	require.Len(t, projects, 2, "local and SSH with same path should be separate projects")
}

// --- Explicit projectRoot tests ---

func TestStore_Add_ExplicitProjectRoot_OverridesInference(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	// Create project with path "."
	s.Add(newTestSessionWithHost("pm-1", "pm", ".", "user@host"), ".")

	// Resume worktree: git worktree list returns absolute path, but we pass
	// the explicit project root "." so it matches the existing project.
	s.Add(newTestSessionWithHost("w-1", "feat", "/home/user/project/.lazyclaude/worktrees/feat", "user@host"), ".")

	projects := s.Projects()
	require.Len(t, projects, 1, "explicit projectRoot should match existing project")
	assert.Len(t, projects[0].Sessions, 2)
}

// --- v2→v3 migration tests ---

func TestStore_MigrateV2ToV3_PMPresent(t *testing.T) {
	t.Parallel()
	// v2 format: PM is a separate field on the project.
	v2Data := `{
  "version": 2,
  "projects": [{
    "id": "proj-1",
    "name": "my-project",
    "path": "/home/user/project",
    "created_at": "2024-06-01T00:00:00Z",
    "updated_at": "2024-06-01T00:00:00Z",
    "pm": {
      "id": "pm-id-1",
      "name": "pm",
      "path": "/home/user/project",
      "role": "pm",
      "created_at": "2024-06-01T00:00:00Z",
      "updated_at": "2024-06-01T00:00:00Z"
    },
    "sessions": [{
      "id": "w-id-1",
      "name": "feat-x",
      "path": "/home/user/project/.lazyclaude/worktrees/feat-x",
      "role": "worker",
      "created_at": "2024-06-01T00:00:00Z",
      "updated_at": "2024-06-01T00:00:00Z"
    }]
  }]
}`
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	require.NoError(t, os.WriteFile(path, []byte(v2Data), 0o600))

	store := session.NewStore(path)
	require.NoError(t, store.Load())

	projects := store.Projects()
	require.Len(t, projects, 1)

	// PM should be merged into Sessions (PM first, then workers).
	require.Len(t, projects[0].Sessions, 2, "v2 PM + 1 worker = 2 sessions in v3")

	// Verify PM is findable via FindPM.
	pmSess := projects[0].FindPM()
	require.NotNil(t, pmSess)
	assert.Equal(t, "pm-id-1", pmSess.ID)
	assert.Equal(t, session.RolePM, pmSess.Role)

	// Verify worker is present.
	var foundWorker bool
	for _, s := range projects[0].Sessions {
		if s.ID == "w-id-1" {
			assert.Equal(t, session.RoleWorker, s.Role)
			foundWorker = true
		}
	}
	assert.True(t, foundWorker, "worker session should be in v3 Sessions")
}

func TestStore_MigrateV2ToV3_NoPM(t *testing.T) {
	t.Parallel()
	// v2 format with no PM field.
	v2Data := `{
  "version": 2,
  "projects": [{
    "id": "proj-1",
    "name": "solo",
    "path": "/home/user/solo",
    "created_at": "2024-06-01T00:00:00Z",
    "updated_at": "2024-06-01T00:00:00Z",
    "sessions": [{
      "id": "s-1",
      "name": "main",
      "path": "/home/user/solo",
      "created_at": "2024-06-01T00:00:00Z",
      "updated_at": "2024-06-01T00:00:00Z"
    }]
  }]
}`
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	require.NoError(t, os.WriteFile(path, []byte(v2Data), 0o600))

	store := session.NewStore(path)
	require.NoError(t, store.Load())

	projects := store.Projects()
	require.Len(t, projects, 1)
	require.Len(t, projects[0].Sessions, 1)
	assert.Nil(t, projects[0].FindPM())
}

func TestStore_MigrateV2ToV3_RoundTrip(t *testing.T) {
	t.Parallel()
	// v2 data → load (migrate) → save → reload as v3.
	v2Data := `{
  "version": 2,
  "projects": [{
    "id": "proj-rt",
    "name": "round-trip",
    "path": "/home/user/rt",
    "created_at": "2024-06-01T10:00:00Z",
    "updated_at": "2024-06-01T10:00:00Z",
    "pm": {
      "id": "pm-rt",
      "name": "pm",
      "path": "/home/user/rt",
      "role": "pm",
      "created_at": "2024-06-01T10:00:00Z",
      "updated_at": "2024-06-01T10:00:00Z"
    },
    "sessions": [
      {
        "id": "w-rt-1",
        "name": "feat-a",
        "path": "/home/user/rt/.lazyclaude/worktrees/feat-a",
        "role": "worker",
        "created_at": "2024-06-01T10:00:00Z",
        "updated_at": "2024-06-01T10:00:00Z"
      },
      {
        "id": "w-rt-2",
        "name": "feat-b",
        "path": "/home/user/rt/.lazyclaude/worktrees/feat-b",
        "role": "worker",
        "created_at": "2024-06-01T10:00:00Z",
        "updated_at": "2024-06-01T10:00:00Z"
      }
    ]
  }]
}`
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	require.NoError(t, os.WriteFile(path, []byte(v2Data), 0o600))

	// Load (triggers v2→v3 migration).
	store1 := session.NewStore(path)
	require.NoError(t, store1.Load())

	// Save as v3.
	require.NoError(t, store1.Save())

	// Reload from disk — must parse as v3 (no migration needed).
	store2 := session.NewStore(path)
	require.NoError(t, store2.Load())

	projects := store2.Projects()
	require.Len(t, projects, 1)
	assert.Equal(t, "round-trip", projects[0].Name)

	// PM + 2 workers = 3 sessions.
	require.Len(t, projects[0].Sessions, 3)

	pmSess := projects[0].FindPM()
	require.NotNil(t, pmSess)
	assert.Equal(t, "pm-rt", pmSess.ID)

	// Verify saved format is v3 (version field == 3, no "pm" key on project).
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	var version int
	require.NoError(t, json.Unmarshal(raw["version"], &version))
	assert.Equal(t, 3, version, "saved format should be v3")
}

func TestStore_MigrateV2ToV3_ParentIDPreserved(t *testing.T) {
	t.Parallel()
	// v2 format with a session that already has parent_id (hypothetical
	// forward-compatible write). Migration should preserve it.
	v2Data := `{
  "version": 2,
  "projects": [{
    "id": "proj-pid",
    "name": "parent-test",
    "path": "/home/user/pt",
    "created_at": "2024-06-01T00:00:00Z",
    "updated_at": "2024-06-01T00:00:00Z",
    "sessions": [{
      "id": "s-child",
      "name": "child",
      "path": "/home/user/pt/.lazyclaude/worktrees/child",
      "role": "worker",
      "parent_id": "some-parent-id",
      "created_at": "2024-06-01T00:00:00Z",
      "updated_at": "2024-06-01T00:00:00Z"
    }]
  }]
}`
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	require.NoError(t, os.WriteFile(path, []byte(v2Data), 0o600))

	store := session.NewStore(path)
	require.NoError(t, store.Load())

	all := store.All()
	require.Len(t, all, 1)
	assert.Equal(t, "some-parent-id", all[0].ParentID, "ParentID should survive migration")
}

// --- ChildrenOf tests ---

func TestStore_ChildrenOf_ReturnsChildren(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	pm := newTestSession("pm-1", "pm", "/project")
	pm.Role = session.RolePM
	s.Add(pm, "")

	child1 := newTestSession("w-1", "feat-a", "/project/.lazyclaude/worktrees/feat-a")
	child1.Role = session.RoleWorker
	child1.ParentID = "pm-1"
	s.Add(child1, "")

	child2 := newTestSession("w-2", "feat-b", "/project/.lazyclaude/worktrees/feat-b")
	child2.Role = session.RoleWorker
	child2.ParentID = "pm-1"
	s.Add(child2, "")

	// Unrelated session (no parent).
	s.Add(newTestSession("s-3", "standalone", "/other"), "")

	children := s.ChildrenOf("pm-1")
	require.Len(t, children, 2)
	ids := []string{children[0].ID, children[1].ID}
	assert.Contains(t, ids, "w-1")
	assert.Contains(t, ids, "w-2")
}

func TestStore_ChildrenOf_EmptyParent_MatchesRootSessions(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("s-1", "app", "/project"), "")

	// ChildrenOf("") matches sessions where ParentID=="", i.e. root-level sessions.
	children := s.ChildrenOf("")
	require.Len(t, children, 1)
	assert.Equal(t, "s-1", children[0].ID)
}

func TestStore_ChildrenOf_NoMatch(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	child := newTestSession("w-1", "feat", "/project/.lazyclaude/worktrees/feat")
	child.ParentID = "pm-x"
	s.Add(child, "")

	children := s.ChildrenOf("nonexistent")
	assert.Empty(t, children)
}

func TestStore_ChildrenOf_CrossProject(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	// Project A
	pmA := newTestSession("pm-a", "pm-a", "/project-a")
	pmA.Role = session.RolePM
	s.Add(pmA, "")

	childA := newTestSession("w-a", "feat-a", "/project-a/.lazyclaude/worktrees/feat-a")
	childA.ParentID = "pm-a"
	s.Add(childA, "")

	// Project B
	childB := newTestSession("w-b", "feat-b", "/project-b/.lazyclaude/worktrees/feat-b")
	childB.ParentID = "pm-b"
	s.Add(childB, "")

	childrenA := s.ChildrenOf("pm-a")
	require.Len(t, childrenA, 1)
	assert.Equal(t, "w-a", childrenA[0].ID)
}

// --- ParentID round-trip tests ---

func TestStore_ParentID_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	store1 := session.NewStore(path)
	sess := newTestSession("child-1", "child", "/project/.lazyclaude/worktrees/child")
	sess.ParentID = "parent-pm-id"
	store1.Add(sess, "")
	require.NoError(t, store1.Save())

	store2 := session.NewStore(path)
	require.NoError(t, store2.Load())

	found := store2.FindByID("child-1")
	require.NotNil(t, found)
	assert.Equal(t, "parent-pm-id", found.ParentID)
}

func TestStore_ParentID_OmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	store := session.NewStore(path)
	sess := newTestSession("no-parent", "sess", "/project")
	// ParentID left as zero value.
	store.Add(sess, "")
	require.NoError(t, store.Save())

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var sf map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &sf))
	var projects []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(sf["projects"], &projects))
	require.Len(t, projects, 1)
	var sessions []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(projects[0]["sessions"], &sessions))
	require.Len(t, sessions, 1)
	_, hasParentID := sessions[0]["parent_id"]
	assert.False(t, hasParentID, "parent_id key should be absent when empty (omitempty)")
}

func TestStore_Add_ExplicitProjectRoot_SymlinkMismatch(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	// Project stored with logical path
	s.Add(newTestSessionWithHost("pm-1", "pm", "/home/user/project", "user@host"), "")

	// Worktree path has resolved symlink, but explicit projectRoot uses
	// the stored logical path.
	s.Add(newTestSessionWithHost("w-1", "feat",
		"/data/home/user/project/.lazyclaude/worktrees/feat", "user@host"),
		"/home/user/project")

	projects := s.Projects()
	require.Len(t, projects, 1, "explicit projectRoot should prevent symlink mismatch")
	assert.Len(t, projects[0].Sessions, 2)
}
