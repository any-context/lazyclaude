package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/daemon"
	"github.com/any-context/lazyclaude/internal/gui"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- sessionCommander mock (adapter-layer tests) -----------------------------

// mockCommands records arguments passed to sessionCommander methods so that
// adapter-level routing tests can assert the OperationTarget each command
// receives.
type mockCommands struct {
	createCalls         []OperationTarget
	createWorktreeCalls []worktreeCall
	resumeWorktreeCalls []worktreeCall
	listWorktreesCalls  []OperationTarget
	createPMCalls       []OperationTarget
	createWorkerCalls   []worktreeCall
	launchLazygitCalls  []OperationTarget
	deleteCalls         []string
	renameCalls         []renameCall
}

type worktreeCall struct {
	Target OperationTarget
	Name   string
	Prompt string
}

type renameCall struct {
	ID      string
	NewName string
}

func (m *mockCommands) Create(target OperationTarget) error {
	m.createCalls = append(m.createCalls, target)
	return nil
}

func (m *mockCommands) Delete(id string) error {
	m.deleteCalls = append(m.deleteCalls, id)
	return nil
}

func (m *mockCommands) Rename(id, newName string) error {
	m.renameCalls = append(m.renameCalls, renameCall{ID: id, NewName: newName})
	return nil
}

func (m *mockCommands) LaunchLazygit(target OperationTarget) error {
	m.launchLazygitCalls = append(m.launchLazygitCalls, target)
	return nil
}

func (m *mockCommands) CreateWorktree(target OperationTarget, name, prompt string) error {
	m.createWorktreeCalls = append(m.createWorktreeCalls, worktreeCall{Target: target, Name: name, Prompt: prompt})
	return nil
}

func (m *mockCommands) ResumeWorktree(target OperationTarget, wtPath, prompt string) error {
	m.resumeWorktreeCalls = append(m.resumeWorktreeCalls, worktreeCall{Target: target, Name: wtPath, Prompt: prompt})
	return nil
}

func (m *mockCommands) ListWorktrees(target OperationTarget) ([]gui.WorktreeInfo, error) {
	m.listWorktreesCalls = append(m.listWorktreesCalls, target)
	return nil, nil
}

func (m *mockCommands) CreatePMSession(target OperationTarget) error {
	m.createPMCalls = append(m.createPMCalls, target)
	return nil
}

func (m *mockCommands) CreateWorkerSession(target OperationTarget, name, prompt string) error {
	m.createWorkerCalls = append(m.createWorkerCalls, worktreeCall{Target: target, Name: name, Prompt: prompt})
	return nil
}

// Compile-time interface check.
var _ sessionCommander = (*mockCommands)(nil)

// --- shared helpers ----------------------------------------------------------

// cursorState mirrors App.CurrentSessionHost(): host plus an onNode flag
// that distinguishes "on a local node" (onNode=true, host="") from "no node
// selected" (onNode=false, host="").
type cursorState struct {
	Host   string
	OnNode bool
}

const (
	localProjPath  = "/Users/me/project"
	remoteProjPath = "/home/user/remote-project"
	remoteHost     = "AERO"
)

// adapterCase describes a single routing scenario used by the n/w/W/P/g
// adapter-level tests.
type adapterCase struct {
	name        string
	cursor      cursorState
	pendingHost string
	inputPath   string // path the app layer would forward to the adapter
	expectHost  string
	expectPath  string
}

// standardCursorCases is the 4-row matrix used by the cursor-based commands
// (n, w, W, P, g). Each row corresponds to one entry in the plan's routing
// table and the path column reflects what the app layer would compute via
// currentProjectRoot().
func standardCursorCases() []adapterCase {
	return []adapterCase{
		{
			name:        "cursor on local node stays local",
			cursor:      cursorState{Host: "", OnNode: true},
			pendingHost: remoteHost,
			inputPath:   localProjPath,
			expectHost:  "",
			expectPath:  localProjPath,
		},
		{
			name:        "cursor on remote node routes to that host",
			cursor:      cursorState{Host: remoteHost, OnNode: true},
			pendingHost: remoteHost,
			inputPath:   remoteProjPath,
			expectHost:  remoteHost,
			expectPath:  remoteProjPath,
		},
		{
			name:        "no node selected falls back to pending remote",
			cursor:      cursorState{Host: "", OnNode: false},
			pendingHost: remoteHost,
			inputPath:   ".",
			expectHost:  remoteHost,
			expectPath:  ".",
		},
		{
			name:        "no node selected and no pending host stays local",
			cursor:      cursorState{Host: "", OnNode: false},
			pendingHost: "",
			inputPath:   ".",
			expectHost:  "",
			expectPath:  ".",
		},
	}
}

// newRoutingAdapter constructs a minimally-wired guiCompositeAdapter with a
// mockCommands injected as the sessionCommander. Host caches are
// pre-populated to simulate the state Sessions() would leave after a layout
// cycle.
func newRoutingAdapter(t *testing.T, cursor cursorState, pendingHost string) (*guiCompositeAdapter, *mockCommands) {
	t.Helper()
	mock := &mockCommands{}
	a := &guiCompositeAdapter{
		commands:         mock,
		pendingHost:      pendingHost,
		localProjectRoot: localProjPath,
	}
	a.cachedHost = cursor.Host
	a.cachedOnNode = cursor.OnNode
	return a, mock
}

// --- n (CreateSession) -------------------------------------------------------

// TestRouting_n_Create verifies that `n` resolves host via the cursor:
// the app layer computes the path via currentProjectRoot(), and the adapter
// picks the host via resolveHost().
func TestRouting_n_Create(t *testing.T) {
	t.Parallel()
	for _, tc := range standardCursorCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, mock := newRoutingAdapter(t, tc.cursor, tc.pendingHost)
			require.NoError(t, a.Create(tc.inputPath))
			require.Len(t, mock.createCalls, 1)
			assert.Equal(t, OperationTarget{Host: tc.expectHost, ProjectRoot: tc.expectPath}, mock.createCalls[0])
		})
	}
}

// --- N (CreateSessionAtCWD) --------------------------------------------------

// TestRouting_N_CreateAtPaneCWD verifies that `N` is pane-based: it uses
// pendingHost regardless of cursor state, and the project path is always
// "." (the pane CWD is translated downstream by resolveRemotePathFn).
func TestRouting_N_CreateAtPaneCWD(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		cursor      cursorState
		pendingHost string
		expectHost  string
	}{
		{
			name:        "cursor on local node still routes to pending remote",
			cursor:      cursorState{Host: "", OnNode: true},
			pendingHost: remoteHost,
			expectHost:  remoteHost,
		},
		{
			name:        "cursor on remote node routes to pending remote",
			cursor:      cursorState{Host: remoteHost, OnNode: true},
			pendingHost: remoteHost,
			expectHost:  remoteHost,
		},
		{
			name:        "no node selected routes to pending remote",
			cursor:      cursorState{Host: "", OnNode: false},
			pendingHost: remoteHost,
			expectHost:  remoteHost,
		},
		{
			name:        "no node selected and no pending host stays local",
			cursor:      cursorState{Host: "", OnNode: false},
			pendingHost: "",
			expectHost:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, mock := newRoutingAdapter(t, tc.cursor, tc.pendingHost)
			require.NoError(t, a.CreateAtPaneCWD())
			require.Len(t, mock.createCalls, 1)
			assert.Equal(t, OperationTarget{Host: tc.expectHost, ProjectRoot: "."}, mock.createCalls[0])
		})
	}
}

// --- w (CreateWorktree) ------------------------------------------------------

// TestRouting_w_CreateWorktree verifies that `w` follows the cursor-based
// host rule and forwards name/prompt unchanged.
func TestRouting_w_CreateWorktree(t *testing.T) {
	t.Parallel()
	for _, tc := range standardCursorCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, mock := newRoutingAdapter(t, tc.cursor, tc.pendingHost)
			require.NoError(t, a.CreateWorktree("feature-x", "do stuff", tc.inputPath))
			require.Len(t, mock.createWorktreeCalls, 1)
			assert.Equal(t, OperationTarget{Host: tc.expectHost, ProjectRoot: tc.expectPath}, mock.createWorktreeCalls[0].Target)
			assert.Equal(t, "feature-x", mock.createWorktreeCalls[0].Name)
			assert.Equal(t, "do stuff", mock.createWorktreeCalls[0].Prompt)
		})
	}
}

// --- W (ListWorktrees / SelectWorktree) --------------------------------------

// TestRouting_W_ListWorktrees verifies that `W` (open the worktree chooser)
// follows the same cursor-based host rule as `w`.
func TestRouting_W_ListWorktrees(t *testing.T) {
	t.Parallel()
	for _, tc := range standardCursorCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, mock := newRoutingAdapter(t, tc.cursor, tc.pendingHost)
			_, err := a.ListWorktrees(tc.inputPath)
			require.NoError(t, err)
			require.Len(t, mock.listWorktreesCalls, 1)
			assert.Equal(t, OperationTarget{Host: tc.expectHost, ProjectRoot: tc.expectPath}, mock.listWorktreesCalls[0])
		})
	}
}

// --- P (CreatePMSession) -----------------------------------------------------

// TestRouting_P_CreatePMSession verifies that `P` follows the same
// cursor-based host rule as `n`.
func TestRouting_P_CreatePMSession(t *testing.T) {
	t.Parallel()
	for _, tc := range standardCursorCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, mock := newRoutingAdapter(t, tc.cursor, tc.pendingHost)
			require.NoError(t, a.CreatePMSession(tc.inputPath))
			require.Len(t, mock.createPMCalls, 1)
			assert.Equal(t, OperationTarget{Host: tc.expectHost, ProjectRoot: tc.expectPath}, mock.createPMCalls[0])
		})
	}
}

// --- g (LaunchLazygit) -------------------------------------------------------

// TestRouting_g_LaunchLazygit verifies that `g` follows the same
// cursor-based host rule as `n`.
func TestRouting_g_LaunchLazygit(t *testing.T) {
	t.Parallel()
	for _, tc := range standardCursorCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, mock := newRoutingAdapter(t, tc.cursor, tc.pendingHost)
			require.NoError(t, a.LaunchLazygit(tc.inputPath))
			require.Len(t, mock.launchLazygitCalls, 1)
			assert.Equal(t, OperationTarget{Host: tc.expectHost, ProjectRoot: tc.expectPath}, mock.launchLazygitCalls[0])
		})
	}
}

// --- fakeRemoteAPI for d/R ---------------------------------------------------

// fakeRemoteAPI records Delete / Rename calls and satisfies remoteSessionAPI.
// Used by d/R routing tests to observe whether SessionCommandService
// dispatched to the remote path. CreateSession is implemented to satisfy the
// interface but is not exercised by these tests.
type fakeRemoteAPI struct {
	deleteCalls []string
	renameCalls []renameCall
}

func (f *fakeRemoteAPI) CreateSession(_ string) (*daemon.SessionCreateResponse, error) {
	return &daemon.SessionCreateResponse{}, nil
}

func (f *fakeRemoteAPI) Delete(id string) error {
	f.deleteCalls = append(f.deleteCalls, id)
	return nil
}

func (f *fakeRemoteAPI) Rename(id, newName string) error {
	f.renameCalls = append(f.renameCalls, renameCall{ID: id, NewName: newName})
	return nil
}

var _ remoteSessionAPI = (*fakeRemoteAPI)(nil)

// fakeMirrorCreator records DeleteMirror calls. CreateMirror is implemented
// to satisfy the interface but is not exercised by these tests.
type fakeMirrorCreator struct {
	deleteCalls []string // session id per call
}

func (f *fakeMirrorCreator) CreateMirror(_, _ string, _ *daemon.SessionCreateResponse) error {
	return nil
}

func (f *fakeMirrorCreator) DeleteMirror(sessionID string) error {
	f.deleteCalls = append(f.deleteCalls, sessionID)
	return nil
}

var _ MirrorCreator = (*fakeMirrorCreator)(nil)

// sessionCmdFixture bundles the real SessionCommandService stack needed to
// test d (Delete) and R (Rename) routing. It uses a real session.Manager so
// that the local path exercises real store/tmux plumbing, and an injected
// fakeRemoteAPI so that the remote path is observable without SSH.
type sessionCmdFixture struct {
	svc    *SessionCommandService
	mgr    *session.Manager
	remote *fakeRemoteAPI
	mirror *fakeMirrorCreator
	mock   *tmux.MockClient
}

func newSessionCmdFixture(t *testing.T) *sessionCmdFixture {
	t.Helper()
	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	mock := tmux.NewMockClient()
	mgr := session.NewManager(store, mock, paths, nil)

	localProv := &localDaemonProvider{mgr: mgr, tmux: mock}
	composite := daemon.NewCompositeProvider(localProv, nil)

	remote := &fakeRemoteAPI{}
	mirror := &fakeMirrorCreator{}

	svc := &SessionCommandService{
		localMgr: mgr,
		cp:       composite,
		mirrors:  mirror,
		tmux:     mock,
		remoteProviderFn: func(host string) remoteSessionAPI {
			if host != remoteHost {
				return nil
			}
			return remote
		},
	}
	return &sessionCmdFixture{
		svc:    svc,
		mgr:    mgr,
		remote: remote,
		mirror: mirror,
		mock:   mock,
	}
}

// addSession inserts a session with the given ID and host into the store.
// Host="" makes it local; Host=remoteHost makes it remote.
func (f *sessionCmdFixture) addSession(id, host string) {
	now := time.Now()
	sess := session.Session{
		ID:        id,
		Name:      "test-" + id,
		Path:      localProjPath,
		Host:      host,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if host != "" {
		sess.Path = remoteProjPath
	}
	f.mgr.Store().Add(sess, "")
}

// --- d (Delete) --------------------------------------------------------------

// TestRouting_d_Delete_LocalSession verifies that deleting a local session
// forwards to the local CompositeProvider path (no remote API, no mirror
// teardown) and removes the session from the store.
func TestRouting_d_Delete_LocalSession(t *testing.T) {
	t.Parallel()
	f := newSessionCmdFixture(t)
	const id = "local-session-id"
	f.addSession(id, "")
	require.NotNil(t, f.mgr.Store().FindByID(id), "precondition: session exists")

	require.NoError(t, f.svc.Delete(id))

	assert.Empty(t, f.remote.deleteCalls, "remote API must NOT be called for a local session")
	assert.Empty(t, f.mirror.deleteCalls, "mirror teardown must NOT run for a local session")
	assert.Nil(t, f.mgr.Store().FindByID(id), "session must be removed from the local store")
}

// TestRouting_d_Delete_RemoteSession verifies that deleting a remote session
// calls the remote Delete API, tears down the local mirror window, and
// removes the placeholder from the local store.
func TestRouting_d_Delete_RemoteSession(t *testing.T) {
	t.Parallel()
	f := newSessionCmdFixture(t)
	const id = "remote-session-id"
	f.addSession(id, remoteHost)
	require.NotNil(t, f.mgr.Store().FindByID(id), "precondition: session exists")

	require.NoError(t, f.svc.Delete(id))

	assert.Equal(t, []string{id}, f.remote.deleteCalls, "remote API Delete must be called with the session id")
	assert.Equal(t, []string{id}, f.mirror.deleteCalls, "mirror teardown must run for a remote session")
	assert.Nil(t, f.mgr.Store().FindByID(id), "session must be removed from the local store")
}

// --- R (Rename) --------------------------------------------------------------

// TestRouting_R_Rename_LocalSession verifies that renaming a local session
// flows through the local CompositeProvider (no remote API).
func TestRouting_R_Rename_LocalSession(t *testing.T) {
	t.Parallel()
	f := newSessionCmdFixture(t)
	const id = "local-rename-id"
	f.addSession(id, "")

	require.NoError(t, f.svc.Rename(id, "new-local-name"))

	assert.Empty(t, f.remote.renameCalls, "remote API must NOT be called for a local session")

	sess := f.mgr.Store().FindByID(id)
	require.NotNil(t, sess)
	assert.Equal(t, "new-local-name", sess.Name, "local store must reflect the new name")
}

// TestRouting_R_Rename_RemoteSession verifies that renaming a remote session
// calls the remote Rename API and updates the local mirror's name.
func TestRouting_R_Rename_RemoteSession(t *testing.T) {
	t.Parallel()
	f := newSessionCmdFixture(t)
	const id = "remote-rename-id"
	f.addSession(id, remoteHost)

	require.NoError(t, f.svc.Rename(id, "new-remote-name"))

	assert.Equal(t, []renameCall{{ID: id, NewName: "new-remote-name"}}, f.remote.renameCalls,
		"remote API Rename must be called with the id and new name")

	sess := f.mgr.Store().FindByID(id)
	require.NotNil(t, sess)
	assert.Equal(t, "new-remote-name", sess.Name, "local store must reflect the new name")
}
