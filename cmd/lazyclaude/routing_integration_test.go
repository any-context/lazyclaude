package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/daemon"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file contains end-to-end behaviour tests for command routing. Unlike
// routing_test.go (which asserts on arguments passed to a sessionCommander
// mock), these tests wire up the real SessionCommandService stack — real
// session.Manager, real CompositeProvider, real MirrorManager, real
// RemoteHostManager — and verify the final state of the session.Store
// after each command runs. The only fakes are the network-facing pieces
// (fakeRemoteSessionAPI for n/N remote plain-create, fakeSessionProvider
// for w/W/P/worker/g remote) and tmux.MockClient.
//
// The tests MUST NOT call t.Parallel(): chdirTemp mutates the process cwd to
// control the "no cursor, no pending" local-fallback path (which resolves
// via filepath.Abs(".")), and parallel execution would race on os.Chdir.

// --- fakeRemoteSessionAPI: n/N remote plain-create ---------------------------

// fakeRemoteSessionAPI replaces the SSH-backed *daemon.RemoteProvider that
// SessionCommandService.completeRemoteCreate invokes via remoteProviderFn.
// It only covers the plain-create path (n, N) plus d/R's remote delete and
// rename, which bypass the CompositeProvider and go through this type
// directly.
type fakeRemoteSessionAPI struct {
	// nameStore drives session name generation so the fake matches
	// production's behaviour (Manager.Create calls Store.GenerateName).
	// The store is private to the fake and never persisted — it exists
	// only so we can call GenerateName without hard-coding names.
	nameStore *session.Store

	// createCalls records the remote path passed to CreateSession so that
	// n/N tests can assert what was sent to the remote daemon after
	// resolveRemotePath translation.
	createCalls []string
	// deleteCalls records session ids passed to Delete for d remote.
	deleteCalls []string
	// renameCalls records the (id, newName) pairs passed to Rename for
	// R remote.
	renameCalls []renameCall
}

func (f *fakeRemoteSessionAPI) CreateSession(path string) (*daemon.SessionCreateResponse, error) {
	f.createCalls = append(f.createCalls, path)
	id := uuid.New().String()
	return &daemon.SessionCreateResponse{
		ID:         id,
		Name:       f.nameStore.GenerateName(path),
		Path:       path,
		TmuxWindow: "lc-" + id[:8],
	}, nil
}

func (f *fakeRemoteSessionAPI) Delete(id string) error {
	f.deleteCalls = append(f.deleteCalls, id)
	return nil
}

func (f *fakeRemoteSessionAPI) Rename(id, newName string) error {
	f.renameCalls = append(f.renameCalls, renameCall{ID: id, NewName: newName})
	return nil
}

var _ remoteSessionAPI = (*fakeRemoteSessionAPI)(nil)

// --- fakeSessionProvider: remote w/W/P/worker/g -------------------------------

// fakeSessionProvider satisfies daemon.SessionProvider and CWDQuerier. It is
// registered in CompositeProvider as the remote backend for host="AERO".
// Every role-session / worktree method records its arguments and then calls
// the PostCreateHook (= MirrorManager.CreateMirror) so that the integration
// test can observe the resulting remote mirror session in the local store.
// This mirrors how *daemon.RemoteProvider behaves in production.
type fakeSessionProvider struct {
	host       string
	postCreate daemon.PostCreateHook
	remoteCWD  string

	// Call records exposed to tests.
	worktreeCalls  []worktreeCall  // CreateWorktree
	resumeWTCalls  []worktreeCall  // ResumeWorktree
	listWTCalls    []string        // ListWorktrees projectRoot
	pmCalls        []string        // CreatePMSession projectRoot
	workerCalls    []worktreeCall  // CreateWorkerSession
	lazygitCalls   []string        // LaunchLazygit path
}

// --- daemon.SessionLister ---

func (f *fakeSessionProvider) HasSession(_ string) bool                { return false }
func (f *fakeSessionProvider) LocalSessionHost(_ string) (string, bool) { return "", false }
func (f *fakeSessionProvider) Host() string                            { return f.host }
func (f *fakeSessionProvider) Sessions() ([]daemon.SessionInfo, error)  { return nil, nil }

// --- daemon.SessionMutator ---

func (f *fakeSessionProvider) Create(_ string) error     { return nil }
func (f *fakeSessionProvider) Delete(_ string) error     { return nil }
func (f *fakeSessionProvider) Rename(_, _ string) error  { return nil }
func (f *fakeSessionProvider) PurgeOrphans() (int, error) { return 0, nil }

// --- daemon.PreviewProvider (stubs) ---

func (f *fakeSessionProvider) CapturePreview(_ string, _, _ int) (*daemon.PreviewResponse, error) {
	return &daemon.PreviewResponse{}, nil
}
func (f *fakeSessionProvider) CaptureScrollback(_ string, _, _, _ int) (*daemon.ScrollbackResponse, error) {
	return &daemon.ScrollbackResponse{}, nil
}
func (f *fakeSessionProvider) HistorySize(_ string) (int, error) { return 0, nil }

// --- daemon.SessionActioner ---

func (f *fakeSessionProvider) SendChoice(_ string, _ int) error { return nil }
func (f *fakeSessionProvider) AttachSession(_ string) error     { return nil }

func (f *fakeSessionProvider) LaunchLazygit(path string) error {
	f.lazygitCalls = append(f.lazygitCalls, path)
	return nil
}

// --- daemon.WorktreeProvider ---

func (f *fakeSessionProvider) CreateWorktree(name, prompt, projectRoot string) error {
	f.worktreeCalls = append(f.worktreeCalls, worktreeCall{
		Target: OperationTarget{Host: f.host, ProjectRoot: projectRoot},
		Name:   name,
		Prompt: prompt,
	})
	resp := f.newResponse(name, filepath.Join(projectRoot, session.WorktreePathSegment, name), "worker")
	return f.postCreate(f.host, projectRoot, resp)
}

func (f *fakeSessionProvider) ResumeWorktree(worktreePath, prompt, projectRoot string) error {
	f.resumeWTCalls = append(f.resumeWTCalls, worktreeCall{
		Target: OperationTarget{Host: f.host, ProjectRoot: projectRoot},
		Name:   worktreePath,
		Prompt: prompt,
	})
	resp := f.newResponse(filepath.Base(worktreePath), worktreePath, "worker")
	return f.postCreate(f.host, projectRoot, resp)
}

func (f *fakeSessionProvider) ListWorktrees(projectRoot string) ([]daemon.WorktreeInfo, error) {
	f.listWTCalls = append(f.listWTCalls, projectRoot)
	return nil, nil
}

// --- daemon.RoleSessionProvider ---

func (f *fakeSessionProvider) CreatePMSession(projectRoot string) error {
	f.pmCalls = append(f.pmCalls, projectRoot)
	resp := f.newResponse("pm", projectRoot, "pm")
	return f.postCreate(f.host, projectRoot, resp)
}

func (f *fakeSessionProvider) CreateWorkerSession(name, prompt, projectRoot string) error {
	f.workerCalls = append(f.workerCalls, worktreeCall{
		Target: OperationTarget{Host: f.host, ProjectRoot: projectRoot},
		Name:   name,
		Prompt: prompt,
	})
	resp := f.newResponse(name, filepath.Join(projectRoot, session.WorktreePathSegment, name), "worker")
	return f.postCreate(f.host, projectRoot, resp)
}

// --- daemon.ConnectionAware ---

func (f *fakeSessionProvider) ConnectionState() daemon.ConnectionState {
	return daemon.Connected
}

// --- daemon.CWDQuerier ---

// QueryCWD returns the fake's configured remote working directory. Called
// from guiCompositeAdapter.resolveRemotePath when the caller passes "." or
// the local project root as the project path.
func (f *fakeSessionProvider) QueryCWD(_ context.Context) (string, error) {
	return f.remoteCWD, nil
}

// newResponse builds a synthetic SessionCreateResponse with the given name,
// path, and role. Used by CreateWorktree / CreatePMSession / etc. so that
// MirrorManager.CreateMirror has everything it needs to insert a session
// into the local store.
func (f *fakeSessionProvider) newResponse(name, path, role string) *daemon.SessionCreateResponse {
	id := uuid.New().String()
	return &daemon.SessionCreateResponse{
		ID:         id,
		Name:       name,
		Path:       path,
		TmuxWindow: "lc-" + id[:8],
		Role:       role,
	}
}

// Compile-time interface checks.
var (
	_ daemon.SessionProvider = (*fakeSessionProvider)(nil)
	_ daemon.CWDQuerier      = (*fakeSessionProvider)(nil)
)

// --- fakeIntegrationMirrorCreator -------------------------------------------
//
// The real MirrorManager.CreateMirror builds an SSH command and issues
// tmux.NewSession / NewWindow through the mock tmux client, which is fine
// for the store-state assertions. No extra fake is needed here; we use the
// real MirrorManager wired with tmux.MockClient.

// --- helpers -----------------------------------------------------------------

const integrationRemoteHost = "AERO"

// initGitRepo creates a git repo with an initial empty commit. Mirrors the
// helper in internal/session/manager_test.go so that the local w / worker
// tests can invoke real `git worktree add`. Uses plain `git init` (no
// --initial-branch flag) to stay compatible with older Git versions in CI.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "test"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "cmd %v: %s", args, out)
	}
}

// chdirTemp switches the process cwd to dir and restores the previous cwd
// via t.Cleanup. Integration tests call this so that filepath.Abs(".") and
// any other cwd-dependent lookups are deterministic. Because this mutates
// process-global state, tests using it MUST NOT call t.Parallel(). A
// failed restore is reported as a test error so that a subsequent test
// does not silently run in the wrong directory and produce a confusing
// failure far from the root cause.
func chdirTemp(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("chdirTemp restore to %q failed: %v", orig, err)
		}
	})
}

// setupMCPFiles writes the port file and lock file that session.Manager
// expects when creating PM/worker sessions. Without these, the launcher
// script generation succeeds but the session-integration path that reads
// MCP credentials would fail (claudeEnv lookup). Only strictly required by
// PM and worker tests; cheap enough to write unconditionally.
func setupMCPFiles(t *testing.T, paths config.Paths) {
	t.Helper()
	const port = 19876
	const token = "integration-test-token"
	require.NoError(t, os.MkdirAll(filepath.Dir(paths.PortFile()), 0o755))
	require.NoError(t, os.WriteFile(paths.PortFile(), []byte(strconv.Itoa(port)), 0o600))
	require.NoError(t, os.MkdirAll(paths.IDEDir, 0o755))
	lockData, err := json.Marshal(map[string]string{"authToken": token})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(paths.LockFile(port), lockData, 0o600))
}

// waitUntilStoreSettled blocks until the background completeRemoteCreate
// goroutine (spawned by SessionCommandService.Create for remote hosts) has
// removed the "connecting..." placeholder and converged on the expected
// session count. A 5 second ceiling is generous — the goroutine does no
// real IO in these tests.
func waitUntilStoreSettled(t *testing.T, mgr *session.Manager, expected int) {
	t.Helper()
	require.Eventually(t, func() bool {
		sessions := mgr.Sessions()
		for _, s := range sessions {
			if s.Name == "connecting..." {
				return false
			}
		}
		return len(sessions) == expected
	}, 5*time.Second, 20*time.Millisecond, "store did not settle to %d sessions", expected)
}

// recordingTmux wraps tmux.MockClient and records every target passed to
// KillWindow. tmux.MockClient does not track KillWindow arguments, so
// without this wrapper the d tests cannot verify that the right window
// prefix (lc- for local, rm- for remote mirror) was killed.
type recordingTmux struct {
	*tmux.MockClient
	killedWindows []string
}

func (r *recordingTmux) KillWindow(ctx context.Context, target string) error {
	r.killedWindows = append(r.killedWindows, target)
	return r.MockClient.KillWindow(ctx, target)
}

// integrationStack holds every wired component the tests need. Built by
// newIntegrationStack for each test and torn down via t.TempDir / t.Cleanup.
type integrationStack struct {
	adapter    *guiCompositeAdapter
	svc        *SessionCommandService
	mgr        *session.Manager
	composite  *daemon.CompositeProvider
	mirrorMgr  *MirrorManager
	hostMgr    *RemoteHostManager
	fakeRemote *fakeRemoteSessionAPI
	fakeRP     *fakeSessionProvider
	tmuxMock   *recordingTmux

	localProj string // absolute path of the local git project (also the cwd)
}

// newIntegrationStack builds the full real stack (session.Manager,
// CompositeProvider, MirrorManager, SessionCommandService,
// guiCompositeAdapter) with tmux.MockClient and a fakeSessionProvider
// registered for integrationRemoteHost. The caller supplies the adapter's
// cursor/pendingHost state.
//
// Side effect: chdirs into the local project for the duration of the test.
func newIntegrationStack(t *testing.T, cursor cursorState, pendingHost string) *integrationStack {
	t.Helper()

	tmp := t.TempDir()
	paths := config.TestPaths(tmp)
	setupMCPFiles(t, paths)

	// Local project lives under the test's tmpdir so that it has a unique
	// absolute path per test and so filepath.Abs(".") is deterministic once
	// chdirTemp runs. On macOS, t.TempDir() returns a path under /var/
	// while the kernel reports the canonical /private/var/ after chdir —
	// capture os.Getwd() post-chdir so that expected and observed paths
	// agree.
	localProj := filepath.Join(tmp, "proj")
	require.NoError(t, os.MkdirAll(localProj, 0o755))
	initGitRepo(t, localProj)
	chdirTemp(t, localProj)
	canonProj, err := os.Getwd()
	require.NoError(t, err)
	localProj = canonProj

	store := session.NewStore(filepath.Join(paths.DataDir, "state.json"))
	tmuxMock := &recordingTmux{MockClient: tmux.NewMockClient()}
	mgr := session.NewManager(store, tmuxMock, paths, nil)

	localProv := &localDaemonProvider{mgr: mgr, tmux: tmuxMock}
	composite := daemon.NewCompositeProvider(localProv, nil)

	mirrorMgr := &MirrorManager{
		tmux:  tmuxMock,
		store: store,
	}

	// A no-op connectFn makes EnsureConnected a no-op for the remote host.
	// MarkConnected pre-populates the lazyConn entry so even the once.Do
	// is skipped. This keeps the host manager exercising its real routing
	// code without touching SSH.
	hostMgr := NewRemoteHostManager(func(_ string) error { return nil })
	hostMgr.MarkConnected(integrationRemoteHost)

	fakeRemote := &fakeRemoteSessionAPI{
		nameStore: session.NewStore(""),
	}
	// Post-create hook mirrors production: every remote CreateWorktree /
	// CreatePMSession / CreateWorkerSession must funnel through
	// MirrorManager.CreateMirror so that the resulting mirror session
	// lands in the local store where tests can observe it. We wire the
	// hook at construction (rather than assigning it afterwards) to
	// prevent a future reorder from registering the fake in
	// CompositeProvider before the hook is set and causing a nil panic.
	fakeRP := &fakeSessionProvider{
		host:      integrationRemoteHost,
		remoteCWD: "/remote/cwd",
		postCreate: func(host, path string, resp *daemon.SessionCreateResponse) error {
			return mirrorMgr.CreateMirror(host, path, resp)
		},
	}

	// Build the adapter first so its resolveRemotePath method can be
	// referenced by SessionCommandService.resolveRemotePathFn and its
	// readPendingHost method can be read by CreateAtPaneCWD.
	adapter := &guiCompositeAdapter{
		cp:               composite,
		localMgr:         mgr,
		paths:            paths,
		pendingHost:      pendingHost,
		localProjectRoot: localProj,
	}
	adapter.cachedHost = cursor.Host
	adapter.cachedOnNode = cursor.OnNode

	svc := &SessionCommandService{
		localMgr:            mgr,
		cp:                  composite,
		mirrors:             mirrorMgr,
		tmux:                tmuxMock,
		ensureConnectedFn:   hostMgr.EnsureConnected,
		resolveRemotePathFn: adapter.resolveRemotePath,
		remoteProviderFn: func(host string) remoteSessionAPI {
			if host == integrationRemoteHost {
				return fakeRemote
			}
			return nil
		},
	}
	adapter.commands = svc

	// Register the fake as the AERO remote. resolveRemotePath's CWDQuerier
	// lookup and CompositeProvider.providerForHost both read this map.
	composite.AddRemote(integrationRemoteHost, fakeRP)

	return &integrationStack{
		adapter:    adapter,
		svc:        svc,
		mgr:        mgr,
		composite:  composite,
		mirrorMgr:  mirrorMgr,
		hostMgr:    hostMgr,
		fakeRemote: fakeRemote,
		fakeRP:     fakeRP,
		tmuxMock:   tmuxMock,
		localProj:  localProj,
	}
}

// findProjectByPathHost returns the project with the given path+host tuple.
// Host is derived from the project's sessions (PM or first worker) because
// session.Project has no Host field of its own.
func findProjectByPathHost(mgr *session.Manager, path, host string) *session.Project {
	for _, p := range mgr.Projects() {
		if p.Path != path {
			continue
		}
		if projectHostOf(p) == host {
			return &p
		}
	}
	return nil
}

// projectHostOf inspects a project's sessions and returns the host they
// carry. Mirrors session.projectHost (unexported in the session package).
func projectHostOf(p session.Project) string {
	if p.PM != nil && p.PM.Host != "" {
		return p.PM.Host
	}
	for _, s := range p.Sessions {
		if s.Host != "" {
			return s.Host
		}
	}
	return ""
}

// assertSingleNonPMSession verifies that the store contains exactly one
// session with the expected Path/Host/Role, and that this session lives
// inside a project keyed by (expectProjectPath, expectHost). Use this
// helper for n, N, w, worker cases (anything that is NOT a PM session).
//
// The linkage check (session ID matches project.Sessions[0].ID) guards
// against regressions where the session lands in the store but ends up
// attached to the wrong project tuple — a failure mode that a looser
// "non-nil project" assertion would miss.
func assertSingleNonPMSession(t *testing.T, mgr *session.Manager,
	expectProjectPath, expectSessionPath, expectHost string, expectRole session.Role,
) {
	t.Helper()
	sessions := mgr.Sessions()
	require.Len(t, sessions, 1, "expected exactly one session in the store")
	sess := sessions[0]
	assert.Equal(t, expectSessionPath, sess.Path, "session.Path")
	assert.Equal(t, expectHost, sess.Host, "session.Host")
	assert.Equal(t, expectRole, sess.Role, "session.Role")

	proj := findProjectByPathHost(mgr, expectProjectPath, expectHost)
	require.NotNil(t, proj, "project (%q, %q) must exist", expectProjectPath, expectHost)
	require.Len(t, proj.Sessions, 1, "project must contain exactly one session")
	assert.Equal(t, sess.ID, proj.Sessions[0].ID, "session must be linked to project.Sessions")
	assert.Nil(t, proj.PM, "non-PM routing must not populate project.PM")
}

// assertSinglePMSession verifies that the store contains exactly one
// session with Role=RolePM at the expected project, and that the PM is
// attached as project.PM (not project.Sessions). Use this helper for P
// cases.
func assertSinglePMSession(t *testing.T, mgr *session.Manager,
	expectProjectPath, expectSessionPath, expectHost string,
) {
	t.Helper()
	sessions := mgr.Sessions()
	require.Len(t, sessions, 1, "expected exactly one session in the store")
	sess := sessions[0]
	assert.Equal(t, expectSessionPath, sess.Path, "PM session.Path")
	assert.Equal(t, expectHost, sess.Host, "PM session.Host")
	assert.Equal(t, session.RolePM, sess.Role, "PM session.Role")

	proj := findProjectByPathHost(mgr, expectProjectPath, expectHost)
	require.NotNil(t, proj, "project (%q, %q) must exist", expectProjectPath, expectHost)
	require.NotNil(t, proj.PM, "P routing must populate project.PM")
	assert.Equal(t, sess.ID, proj.PM.ID, "PM session must be linked to project.PM")
	assert.Empty(t, proj.Sessions, "P routing must not populate project.Sessions")
}

// --- n (CreateSession) ×4 ----------------------------------------------------

// 1. n, cursor on local node, pendingHost=AERO → stays local (plan table row 1)
func TestIntegration_n_LocalCursor_StaysLocal(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: "", OnNode: true}, integrationRemoteHost)

	require.NoError(t, s.adapter.Create(s.localProj))

	assert.Empty(t, s.fakeRemote.createCalls, "local cursor must not invoke remote API")
	assertSingleNonPMSession(t, s.mgr, s.localProj, s.localProj, "", session.RoleNone)
}

// 2. n, cursor on remote node (/remote/proj), pendingHost=AERO → routes to AERO
func TestIntegration_n_RemoteCursor_RoutesToRemote(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: integrationRemoteHost, OnNode: true}, integrationRemoteHost)

	const remoteProj = "/remote/proj"
	require.NoError(t, s.adapter.Create(remoteProj))
	waitUntilStoreSettled(t, s.mgr, 1)

	assert.Equal(t, []string{remoteProj}, s.fakeRemote.createCalls,
		"remote CreateSession must receive the cursor-provided remote path verbatim")
	// For n (plain create) the mirror session's Path matches the remote
	// project root and its Role is RoleNone.
	assertSingleNonPMSession(t, s.mgr, remoteProj, remoteProj, integrationRemoteHost, session.RoleNone)
}

// 3. n, no cursor, pendingHost=AERO → path "." resolved via remoteCWD
func TestIntegration_n_NoCursor_FallsBackToPending(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, integrationRemoteHost)

	// App layer would pass filepath.Abs("."); "." also matches localProj
	// because chdirTemp put us there. Either form exercises the CWDQuerier
	// path — we pick "." to match the most common runtime case.
	require.NoError(t, s.adapter.Create("."))
	waitUntilStoreSettled(t, s.mgr, 1)

	assert.Equal(t, []string{s.fakeRP.remoteCWD}, s.fakeRemote.createCalls,
		"remote CreateSession must receive the remote CWD returned by QueryCWD")
	assertSingleNonPMSession(t, s.mgr, s.fakeRP.remoteCWD, s.fakeRP.remoteCWD, integrationRemoteHost, session.RoleNone)
}

// 4. n, no cursor, pendingHost="" → stays local at the cwd project path
func TestIntegration_n_NoCursor_NoPending_StaysLocal(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, "")

	require.NoError(t, s.adapter.Create(s.localProj))

	assert.Empty(t, s.fakeRemote.createCalls)
	assertSingleNonPMSession(t, s.mgr, s.localProj, s.localProj, "", session.RoleNone)
}

// --- N (CreateAtPaneCWD) ×4 --------------------------------------------------

// 5. N, cursor on local node, pendingHost=AERO → still routes to pending remote
func TestIntegration_N_LocalCursor_RoutesToPending(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: "", OnNode: true}, integrationRemoteHost)

	require.NoError(t, s.adapter.CreateAtPaneCWD())
	waitUntilStoreSettled(t, s.mgr, 1)

	assert.Equal(t, []string{s.fakeRP.remoteCWD}, s.fakeRemote.createCalls,
		"N must bypass resolveHost() and route to pendingHost even with a local cursor")
	assertSingleNonPMSession(t, s.mgr, s.fakeRP.remoteCWD, s.fakeRP.remoteCWD, integrationRemoteHost, session.RoleNone)
}

// 6. N, cursor on remote node, pendingHost=AERO → pendingHost + remoteCWD
func TestIntegration_N_RemoteCursor_RoutesToPending(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: integrationRemoteHost, OnNode: true}, integrationRemoteHost)

	require.NoError(t, s.adapter.CreateAtPaneCWD())
	waitUntilStoreSettled(t, s.mgr, 1)

	assert.Equal(t, []string{s.fakeRP.remoteCWD}, s.fakeRemote.createCalls)
	assertSingleNonPMSession(t, s.mgr, s.fakeRP.remoteCWD, s.fakeRP.remoteCWD, integrationRemoteHost, session.RoleNone)
}

// 7. N, no cursor, pendingHost=AERO → pendingHost + remoteCWD
func TestIntegration_N_NoCursor_RoutesToPending(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, integrationRemoteHost)

	require.NoError(t, s.adapter.CreateAtPaneCWD())
	waitUntilStoreSettled(t, s.mgr, 1)

	assert.Equal(t, []string{s.fakeRP.remoteCWD}, s.fakeRemote.createCalls)
	assertSingleNonPMSession(t, s.mgr, s.fakeRP.remoteCWD, s.fakeRP.remoteCWD, integrationRemoteHost, session.RoleNone)
}

// 8. N, no cursor, pendingHost="" → stays local at the pane cwd
func TestIntegration_N_NoCursor_NoPending_StaysLocal(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, "")

	require.NoError(t, s.adapter.CreateAtPaneCWD())

	assert.Empty(t, s.fakeRemote.createCalls)
	// localDaemonProvider.Create translates "." to filepath.Abs(".") =
	// localProj, so the resulting session's Path is localProj.
	assertSingleNonPMSession(t, s.mgr, s.localProj, s.localProj, "", session.RoleNone)
}

// --- w (CreateWorktree) ×4 ---------------------------------------------------

// 9. w, cursor on local node → git worktree add under localProj
func TestIntegration_w_LocalCursor_StaysLocal(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: "", OnNode: true}, integrationRemoteHost)

	require.NoError(t, s.adapter.CreateWorktree("feat-local", "do it", s.localProj))

	assert.Empty(t, s.fakeRP.worktreeCalls, "remote provider must not be invoked")
	wtPath := filepath.Join(s.localProj, session.WorktreePathSegment, "feat-local")
	info, err := os.Stat(wtPath)
	require.NoError(t, err, "git worktree add must have created the directory")
	assert.True(t, info.IsDir())

	assertSingleNonPMSession(t, s.mgr, s.localProj, wtPath, "", session.RoleWorker)
}

// 10. w, cursor on remote node → fake CreateWorktree with the remote project path
func TestIntegration_w_RemoteCursor_RoutesToRemote(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: integrationRemoteHost, OnNode: true}, integrationRemoteHost)

	const remoteProj = "/remote/proj"
	require.NoError(t, s.adapter.CreateWorktree("feat-r", "do it", remoteProj))

	require.Len(t, s.fakeRP.worktreeCalls, 1)
	assert.Equal(t, remoteProj, s.fakeRP.worktreeCalls[0].Target.ProjectRoot)
	assert.Equal(t, "feat-r", s.fakeRP.worktreeCalls[0].Name)
	// fakeSessionProvider.CreateWorktree builds a synthetic
	// SessionCreateResponse whose Path is projectRoot/.lazyclaude/worktrees/<name>
	// and Role="worker"; the PostCreateHook then routes it through
	// MirrorManager.CreateMirror into the local store.
	wtPath := filepath.Join(remoteProj, session.WorktreePathSegment, "feat-r")
	assertSingleNonPMSession(t, s.mgr, remoteProj, wtPath, integrationRemoteHost, session.RoleWorker)
}

// 11. w, no cursor, pendingHost=AERO → resolveRemotePath("." → remoteCWD), fake receives remoteCWD
func TestIntegration_w_NoCursor_FallsBackToPending(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, integrationRemoteHost)

	require.NoError(t, s.adapter.CreateWorktree("feat-cwd", "do it", "."))

	require.Len(t, s.fakeRP.worktreeCalls, 1)
	assert.Equal(t, s.fakeRP.remoteCWD, s.fakeRP.worktreeCalls[0].Target.ProjectRoot,
		"remote worktree projectRoot must be the resolved remote CWD")
	wtPath := filepath.Join(s.fakeRP.remoteCWD, session.WorktreePathSegment, "feat-cwd")
	assertSingleNonPMSession(t, s.mgr, s.fakeRP.remoteCWD, wtPath, integrationRemoteHost, session.RoleWorker)
}

// 12. w, no cursor, no pending → local git worktree add at localProj
func TestIntegration_w_NoCursor_NoPending_StaysLocal(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, "")

	require.NoError(t, s.adapter.CreateWorktree("feat-12", "do it", s.localProj))

	assert.Empty(t, s.fakeRP.worktreeCalls)
	wtPath := filepath.Join(s.localProj, session.WorktreePathSegment, "feat-12")
	_, err := os.Stat(wtPath)
	require.NoError(t, err)
	assertSingleNonPMSession(t, s.mgr, s.localProj, wtPath, "", session.RoleWorker)
}

// --- W (ListWorktrees) ×4 ----------------------------------------------------

// 13. W, cursor on local node → real git worktree list on localProj (empty)
func TestIntegration_W_LocalCursor_StaysLocal(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: "", OnNode: true}, integrationRemoteHost)

	items, err := s.adapter.ListWorktrees(s.localProj)
	require.NoError(t, err)

	assert.Empty(t, s.fakeRP.listWTCalls, "local W must not route through fake remote provider")
	// A fresh git repo has no worktrees under .lazyclaude/worktrees; the
	// parser filters to that prefix and so returns an empty slice.
	assert.Empty(t, items)
}

// 14. W, cursor on remote node → fake.listWTCalls carries the remote project path
func TestIntegration_W_RemoteCursor_RoutesToRemote(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: integrationRemoteHost, OnNode: true}, integrationRemoteHost)

	const remoteProj = "/remote/proj"
	_, err := s.adapter.ListWorktrees(remoteProj)
	require.NoError(t, err)

	assert.Equal(t, []string{remoteProj}, s.fakeRP.listWTCalls)
}

// 15. W, no cursor, pendingHost=AERO → resolveRemotePath translates "." → remoteCWD
func TestIntegration_W_NoCursor_FallsBackToPending(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, integrationRemoteHost)

	_, err := s.adapter.ListWorktrees(".")
	require.NoError(t, err)

	assert.Equal(t, []string{s.fakeRP.remoteCWD}, s.fakeRP.listWTCalls)
}

// 16. W, no cursor, no pending → real git worktree list (empty on fresh repo)
func TestIntegration_W_NoCursor_NoPending_StaysLocal(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, "")

	items, err := s.adapter.ListWorktrees(s.localProj)
	require.NoError(t, err)

	assert.Empty(t, s.fakeRP.listWTCalls)
	assert.Empty(t, items)
}

// --- P (CreatePMSession) ×4 --------------------------------------------------

// 17. P, cursor on local node → localDaemonProvider.CreatePMSession
func TestIntegration_P_LocalCursor_StaysLocal(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: "", OnNode: true}, integrationRemoteHost)

	require.NoError(t, s.adapter.CreatePMSession(s.localProj))

	assert.Empty(t, s.fakeRP.pmCalls)
	// session.Manager.CreatePMSession sets sess.Path = projectRoot, so the
	// session path matches the project path for PM sessions.
	assertSinglePMSession(t, s.mgr, s.localProj, s.localProj, "")
}

// 18. P, cursor on remote node → fake.pmCalls[0] == /remote/proj, mirror stored
func TestIntegration_P_RemoteCursor_RoutesToRemote(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: integrationRemoteHost, OnNode: true}, integrationRemoteHost)

	const remoteProj = "/remote/proj"
	require.NoError(t, s.adapter.CreatePMSession(remoteProj))

	assert.Equal(t, []string{remoteProj}, s.fakeRP.pmCalls)
	// fakeSessionProvider.CreatePMSession builds a synthetic response
	// whose Path is the projectRoot and Role is "pm"; the PostCreateHook
	// routes it through MirrorManager.CreateMirror into the local store
	// as project.PM.
	assertSinglePMSession(t, s.mgr, remoteProj, remoteProj, integrationRemoteHost)
}

// 19. P, no cursor, pendingHost=AERO → remoteCWD resolution + mirror stored
func TestIntegration_P_NoCursor_FallsBackToPending(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, integrationRemoteHost)

	require.NoError(t, s.adapter.CreatePMSession("."))

	assert.Equal(t, []string{s.fakeRP.remoteCWD}, s.fakeRP.pmCalls)
	assertSinglePMSession(t, s.mgr, s.fakeRP.remoteCWD, s.fakeRP.remoteCWD, integrationRemoteHost)
}

// 20. P, no cursor, no pending → local PM at localProj
func TestIntegration_P_NoCursor_NoPending_StaysLocal(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, "")

	require.NoError(t, s.adapter.CreatePMSession(s.localProj))

	assert.Empty(t, s.fakeRP.pmCalls)
	assertSinglePMSession(t, s.mgr, s.localProj, s.localProj, "")
}

// --- worker (CreateWorkerSession) ×4 -----------------------------------------

// 21. worker, cursor on local node → local worktree session with role=worker
func TestIntegration_worker_LocalCursor_StaysLocal(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: "", OnNode: true}, integrationRemoteHost)

	require.NoError(t, s.adapter.CreateWorkerSession("work-21", "task", s.localProj))

	assert.Empty(t, s.fakeRP.workerCalls)
	wtPath := filepath.Join(s.localProj, session.WorktreePathSegment, "work-21")
	assertSingleNonPMSession(t, s.mgr, s.localProj, wtPath, "", session.RoleWorker)
}

// 22. worker, cursor on remote node → fake.workerCalls[0] == /remote/proj
func TestIntegration_worker_RemoteCursor_RoutesToRemote(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: integrationRemoteHost, OnNode: true}, integrationRemoteHost)

	const remoteProj = "/remote/proj"
	require.NoError(t, s.adapter.CreateWorkerSession("work-22", "task", remoteProj))

	require.Len(t, s.fakeRP.workerCalls, 1)
	assert.Equal(t, remoteProj, s.fakeRP.workerCalls[0].Target.ProjectRoot)
	assert.Equal(t, "work-22", s.fakeRP.workerCalls[0].Name)
	wtPath := filepath.Join(remoteProj, session.WorktreePathSegment, "work-22")
	assertSingleNonPMSession(t, s.mgr, remoteProj, wtPath, integrationRemoteHost, session.RoleWorker)
}

// 23. worker, no cursor, pendingHost=AERO → resolveRemotePath translates "." → remoteCWD
func TestIntegration_worker_NoCursor_FallsBackToPending(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, integrationRemoteHost)

	require.NoError(t, s.adapter.CreateWorkerSession("work-23", "task", "."))

	require.Len(t, s.fakeRP.workerCalls, 1)
	assert.Equal(t, s.fakeRP.remoteCWD, s.fakeRP.workerCalls[0].Target.ProjectRoot)
	wtPath := filepath.Join(s.fakeRP.remoteCWD, session.WorktreePathSegment, "work-23")
	assertSingleNonPMSession(t, s.mgr, s.fakeRP.remoteCWD, wtPath, integrationRemoteHost, session.RoleWorker)
}

// 24. worker, no cursor, no pending → local worker at localProj
func TestIntegration_worker_NoCursor_NoPending_StaysLocal(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, "")

	require.NoError(t, s.adapter.CreateWorkerSession("work-24", "task", s.localProj))

	assert.Empty(t, s.fakeRP.workerCalls)
	wtPath := filepath.Join(s.localProj, session.WorktreePathSegment, "work-24")
	assertSingleNonPMSession(t, s.mgr, s.localProj, wtPath, "", session.RoleWorker)
}

// --- g (LaunchLazygit) ×2 — remote only --------------------------------------

// 25. g, cursor on remote node → fake.lazygitCalls[0] == /remote/proj
func TestIntegration_g_RemoteCursor_RoutesToRemote(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: integrationRemoteHost, OnNode: true}, integrationRemoteHost)

	const remoteProj = "/remote/proj"
	require.NoError(t, s.adapter.LaunchLazygit(remoteProj))

	assert.Equal(t, []string{remoteProj}, s.fakeRP.lazygitCalls)
}

// 26. g, no cursor, pendingHost=AERO → resolveRemotePath translates "." → remoteCWD
func TestIntegration_g_NoCursor_FallsBackToPending(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, integrationRemoteHost)

	require.NoError(t, s.adapter.LaunchLazygit("."))

	assert.Equal(t, []string{s.fakeRP.remoteCWD}, s.fakeRP.lazygitCalls)
}

// --- d (Delete) ×2 -----------------------------------------------------------

// 27. d, local session → store.Remove + tmux KillWindow(lc-), no remote API
func TestIntegration_d_LocalSession(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, "")

	// Create a local session to delete, and pre-verify the initial state.
	require.NoError(t, s.adapter.Create(s.localProj))
	assertSingleNonPMSession(t, s.mgr, s.localProj, s.localProj, "", session.RoleNone)
	sess := s.mgr.Sessions()[0]

	require.NoError(t, s.adapter.Delete(sess.ID))

	assert.Empty(t, s.fakeRemote.deleteCalls, "remote API must not be invoked for local session")
	assert.Empty(t, s.mgr.Sessions(), "local store must be empty after delete")
	assert.Nil(t, findProjectByPathHost(s.mgr, s.localProj, ""),
		"project must be removed once its last session is deleted")

	// tmux mock should have received a KillWindow for the local window.
	assertKillWindowContains(t, s.tmuxMock, "lc-")
}

// 28. d, remote session → store.Remove + fakeRemote.Delete + tmux KillWindow(rm-)
func TestIntegration_d_RemoteSession(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: integrationRemoteHost, OnNode: true}, integrationRemoteHost)

	// Create a remote session via the full stack so we have a mirror
	// window and a Host-bearing store entry.
	const remoteProj = "/remote/proj"
	require.NoError(t, s.adapter.Create(remoteProj))
	waitUntilStoreSettled(t, s.mgr, 1)
	assertSingleNonPMSession(t, s.mgr, remoteProj, remoteProj, integrationRemoteHost, session.RoleNone)
	sess := s.mgr.Sessions()[0]

	// Reset KillWindow history so we only see the delete-time kill.
	s.tmuxMock.killedWindows = nil

	require.NoError(t, s.adapter.Delete(sess.ID))

	assert.Equal(t, []string{sess.ID}, s.fakeRemote.deleteCalls,
		"remote API Delete must be called with the session id")
	assert.Empty(t, s.mgr.Sessions(), "local store must be empty after delete")
	assert.Nil(t, findProjectByPathHost(s.mgr, remoteProj, integrationRemoteHost),
		"remote project must be removed once its last session is deleted")

	// Mirror window prefix is rm- per session.MirrorWindowName.
	assertKillWindowContains(t, s.tmuxMock, "rm-")
}

// --- R (Rename) ×2 -----------------------------------------------------------

// 29. R, local session → store.Name updated, no remote API
func TestIntegration_R_LocalSession(t *testing.T) {
	s := newIntegrationStack(t, cursorState{}, "")

	require.NoError(t, s.adapter.Create(s.localProj))
	assertSingleNonPMSession(t, s.mgr, s.localProj, s.localProj, "", session.RoleNone)
	sess := s.mgr.Sessions()[0]

	require.NoError(t, s.adapter.Rename(sess.ID, "renamed-local"))

	assert.Empty(t, s.fakeRemote.renameCalls)
	updated := s.mgr.Store().FindByID(sess.ID)
	require.NotNil(t, updated)
	assert.Equal(t, "renamed-local", updated.Name)
	// Verify the rename did not relocate the session: Path/Host/Role/
	// project linkage must remain identical.
	assert.Equal(t, s.localProj, updated.Path)
	assert.Empty(t, updated.Host)
	assert.Equal(t, session.RoleNone, updated.Role)
	proj := findProjectByPathHost(s.mgr, s.localProj, "")
	require.NotNil(t, proj)
	require.Len(t, proj.Sessions, 1)
	assert.Equal(t, sess.ID, proj.Sessions[0].ID)
}

// 30. R, remote session → fakeRemote.Rename + store.Name updated
func TestIntegration_R_RemoteSession(t *testing.T) {
	s := newIntegrationStack(t, cursorState{Host: integrationRemoteHost, OnNode: true}, integrationRemoteHost)

	const remoteProj = "/remote/proj"
	require.NoError(t, s.adapter.Create(remoteProj))
	waitUntilStoreSettled(t, s.mgr, 1)
	assertSingleNonPMSession(t, s.mgr, remoteProj, remoteProj, integrationRemoteHost, session.RoleNone)
	sess := s.mgr.Sessions()[0]

	require.NoError(t, s.adapter.Rename(sess.ID, "renamed-remote"))

	assert.Equal(t, []renameCall{{ID: sess.ID, NewName: "renamed-remote"}}, s.fakeRemote.renameCalls)
	updated := s.mgr.Store().FindByID(sess.ID)
	require.NotNil(t, updated)
	assert.Equal(t, "renamed-remote", updated.Name)
	// Verify the rename preserved routing metadata.
	assert.Equal(t, remoteProj, updated.Path)
	assert.Equal(t, integrationRemoteHost, updated.Host)
	assert.Equal(t, session.RoleNone, updated.Role)
	proj := findProjectByPathHost(s.mgr, remoteProj, integrationRemoteHost)
	require.NotNil(t, proj)
	require.Len(t, proj.Sessions, 1)
	assert.Equal(t, sess.ID, proj.Sessions[0].ID)
}

// --- tmux assertion helper ---------------------------------------------------

// assertKillWindowContains asserts that recordingTmux recorded at least
// one KillWindow whose target contains the given substring. Used by d
// tests to verify that the correct window prefix (lc- vs rm-) was killed.
func assertKillWindowContains(t *testing.T, rec *recordingTmux, substr string) {
	t.Helper()
	for _, target := range rec.killedWindows {
		if strings.Contains(target, substr) {
			return
		}
	}
	t.Fatalf("expected KillWindow target containing %q, got %v", substr, rec.killedWindows)
}
