package gui

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/gui/chooser"
	"github.com/any-context/lazyclaude/internal/gui/keymap"
	"github.com/jesseduffield/gocui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file exercises the Phase 1 "remote MCP/plugin disabled" behaviour.
// It lives in package gui (not gui_test) so assertions can inspect the
// private remoteDisabled flags on pluginState / mcpState.
//
// Mock scope: the tests rely on three stubs defined below
// (mockPluginProvider, mockMCPProvider, miniSessionProvider). The stubs
// exist only because the *_test.go files in the sibling gui_test package
// (e.g. app_integration_test.go's mockSessionProvider) cannot be imported
// across the _test package boundary.

// --- mock providers ---

// Compile-time interface checks. Guarantees the mocks stay in sync
// with the PluginProvider / MCPProvider interfaces — a future
// interface extension fails the build here instead of silently
// returning zero values at runtime.
var (
	_ PluginProvider = (*mockPluginProvider)(nil)
	_ MCPProvider    = (*mockMCPProvider)(nil)
)

type mockPluginProvider struct {
	mu              sync.Mutex
	setProjectCalls []string
	refreshCount    int
	installCalls    []string
	uninstallCalls  []string
	toggleCalls     []string
	updateCalls     []string
	installed       []PluginItem
	available       []AvailablePluginItem
}

func (m *mockPluginProvider) SetProjectDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setProjectCalls = append(m.setProjectCalls, dir)
}

func (m *mockPluginProvider) Refresh(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshCount++
	return nil
}

// Installed / Available return a copy under the mutex. The render
// layer pulls these during async refresh while the test goroutine may
// still be seeding the slice — the lock plus copy keeps the race
// detector happy even when a test has background goroutines in flight.
func (m *mockPluginProvider) Installed() []PluginItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PluginItem, len(m.installed))
	copy(out, m.installed)
	return out
}

func (m *mockPluginProvider) Available() []AvailablePluginItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]AvailablePluginItem, len(m.available))
	copy(out, m.available)
	return out
}

func (m *mockPluginProvider) Install(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.installCalls = append(m.installCalls, id)
	return nil
}

func (m *mockPluginProvider) Uninstall(_ context.Context, id, scope string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uninstallCalls = append(m.uninstallCalls, id+":"+scope)
	return nil
}

func (m *mockPluginProvider) ToggleEnabled(_ context.Context, id, scope string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toggleCalls = append(m.toggleCalls, id+":"+scope)
	return nil
}

func (m *mockPluginProvider) Update(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalls = append(m.updateCalls, id)
	return nil
}

func (m *mockPluginProvider) refreshCountSnapshot() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refreshCount
}

func (m *mockPluginProvider) setProjectCallsSnapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.setProjectCalls))
	copy(out, m.setProjectCalls)
	return out
}

// mockRemoteCall captures one SetRemote invocation so tests can
// assert both fields moved together.
type mockRemoteCall struct {
	host       string
	projectDir string
}

type mockMCPProvider struct {
	mu              sync.Mutex
	setRemoteCalls  []mockRemoteCall
	refreshCount    int
	toggleCalls     []string
	servers         []MCPItem
}

func (m *mockMCPProvider) SetRemote(host, projectDir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setRemoteCalls = append(m.setRemoteCalls, mockRemoteCall{host: host, projectDir: projectDir})
}

func (m *mockMCPProvider) Refresh(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshCount++
	return nil
}

// Servers returns a copy under the mutex, matching the rationale in
// mockPluginProvider.Installed above.
func (m *mockMCPProvider) Servers() []MCPItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MCPItem, len(m.servers))
	copy(out, m.servers)
	return out
}

func (m *mockMCPProvider) ToggleDenied(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toggleCalls = append(m.toggleCalls, name)
	return nil
}

func (m *mockMCPProvider) refreshCountSnapshot() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refreshCount
}

func (m *mockMCPProvider) setRemoteCallsSnapshot() []mockRemoteCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]mockRemoteCall, len(m.setRemoteCalls))
	copy(out, m.setRemoteCalls)
	return out
}

// lastSetRemote returns the most recent SetRemote call or a zero
// value when none has been recorded. Tests use this when only the
// terminal state matters.
func (m *mockMCPProvider) lastSetRemote() mockRemoteCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.setRemoteCalls) == 0 {
		return mockRemoteCall{}
	}
	return m.setRemoteCalls[len(m.setRemoteCalls)-1]
}

// miniSessionProvider is a minimal SessionProvider that exposes a caller-
// configurable project list. Every other method is a benign no-op so the
// App can be constructed without panics during tests that only care about
// the tree cursor.
type miniSessionProvider struct {
	projects []ProjectItem
}

func (m *miniSessionProvider) Sessions() []SessionItem         { return nil }
func (m *miniSessionProvider) Projects() []ProjectItem         { return m.projects }
func (m *miniSessionProvider) ToggleProjectExpanded(_ string)  {}
func (m *miniSessionProvider) Create(_ string) error           { return nil }
func (m *miniSessionProvider) CreateAtPaneCWD() error          { return nil }
func (m *miniSessionProvider) Delete(_ string) error           { return nil }
func (m *miniSessionProvider) Rename(_, _ string) error        { return nil }
func (m *miniSessionProvider) PurgeOrphans() (int, error)      { return 0, nil }
func (m *miniSessionProvider) CapturePreview(_ string, _, _ int) (PreviewResult, error) {
	return PreviewResult{}, nil
}
func (m *miniSessionProvider) CaptureScrollback(_ string, _, _, _ int) (PreviewResult, error) {
	return PreviewResult{}, nil
}
func (m *miniSessionProvider) HistorySize(_ string) (int, error) { return 0, nil }
func (m *miniSessionProvider) PendingNotifications() []*model.ToolNotification {
	return nil
}
func (m *miniSessionProvider) SendChoice(_ string, _ Choice) error                    { return nil }
func (m *miniSessionProvider) AttachSession(_ string) error                           { return nil }
func (m *miniSessionProvider) LaunchLazygit(_ string) error                           { return nil }
func (m *miniSessionProvider) CreateWorktree(_, _, _ string) error                    { return nil }
func (m *miniSessionProvider) CreateWorktreeWithOpts(_, _, _, _, _ string) error      { return nil }
func (m *miniSessionProvider) ResumeWorktree(_, _, _ string) error                    { return nil }
func (m *miniSessionProvider) ResumeWorktreeWithOpts(_, _, _, _, _ string) error      { return nil }
func (m *miniSessionProvider) ListWorktrees(_ string) ([]WorktreeInfo, error)         { return nil, nil }
func (m *miniSessionProvider) CreatePMSession(_ string) error                         { return nil }
func (m *miniSessionProvider) CreatePMSessionWithOpts(_, _, _ string) error           { return nil }
func (m *miniSessionProvider) CreateWorkerSession(_, _, _ string) error               { return nil }
func (m *miniSessionProvider) CreateWithOpts(_, _, _ string) error                    { return nil }
func (m *miniSessionProvider) CreateAtPaneCWDWithOpts(_, _ string) error              { return nil }
func (m *miniSessionProvider) ProfileItems() []chooser.Item                           { return nil }

// --- test helpers ---

// newRemoteDisabledApp builds a headless App wired with mock providers.
// The returned App has an empty tree; callers attach a session provider
// and call rebuildTree to position the cursor.
func newRemoteDisabledApp(t *testing.T) (*App, *mockPluginProvider, *mockMCPProvider) {
	t.Helper()
	app, err := NewAppHeadless(ModeMain, 120, 40)
	require.NoError(t, err)
	mp := &mockPluginProvider{}
	mm := &mockMCPProvider{}
	app.plugins = mp
	app.mcpServers = mm
	return app, mp, mm
}

// attachProjectsAndRefresh wires a miniSessionProvider with the given
// projects and rebuilds the tree cache so currentNode() returns the
// desired entry.
func attachProjectsAndRefresh(app *App, projects []ProjectItem) {
	sp := &miniSessionProvider{projects: projects}
	app.sessions = sp
	app.refreshTreeNodes()
}

// remoteAndLocalProjects returns a local and remote project (in that
// order). Both are expanded and carry one session each.
func remoteAndLocalProjects() []ProjectItem {
	return []ProjectItem{
		{
			ID:       "local",
			Name:     "local",
			Path:     "/tmp/local",
			Host:     "",
			Expanded: true,
			Sessions: []SessionItem{{ID: "ls1", Name: "ls1", Host: "", Path: "/tmp/local/ls1"}},
		},
		{
			ID:       "remote",
			Name:     "remote",
			Path:     "/remote/path",
			Host:     "ssh-host",
			Expanded: true,
			Sessions: []SessionItem{{ID: "rs1", Name: "rs1", Host: "ssh-host", Path: "/remote/path/rs1"}},
		},
	}
}

// waitFor polls fn until it returns true or the timeout elapses. Fails
// the test if the timeout is reached.
func waitFor(t *testing.T, fn func() bool, msg string) {
	t.Helper()
	require.Eventually(t, fn, time.Second, 5*time.Millisecond, msg)
}

// --- Step 2/3: syncPluginProject / syncPluginProjectOnce ---

func TestSyncPluginProject_RemoteNode_PluginDisabledMCPRouted(t *testing.T) {
	// Phase 2: on a remote node the plugin panel stays disabled
	// (Phase 3 territory) while the MCP panel is routed through the
	// SSH code path. The test asserts the split:
	//   - plugin.remoteDisabled set, plugin provider untouched
	//   - mcp.remoteDisabled CLEARED, host+projectDir forwarded,
	//     Refresh kicked off
	app, mp, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	// Cursor on remote project (index 2 = ProjectNode(remote)).
	app.cursor = 2
	app.syncPluginProject()

	// Plugin: still disabled, provider untouched.
	assert.True(t, app.pluginState.remoteDisabled, "plugin remoteDisabled must be set on remote cursor")
	assert.Empty(t, mp.setProjectCallsSnapshot(), "plugin SetProjectDir must not be called on remote")
	assert.Zero(t, mp.refreshCountSnapshot(), "plugin Refresh must not be called on remote")
	assert.Empty(t, app.pluginState.projectDir, "pluginState.projectDir must not be set on remote")

	// MCP: routed to SSH. SetRemote must atomically carry both the
	// host and the remote project path in a single call.
	assert.False(t, app.mcpState.remoteDisabled, "mcp remoteDisabled must clear on remote cursor (Phase 2)")
	assert.Equal(t,
		[]mockRemoteCall{{host: "ssh-host", projectDir: "/remote/path"}},
		mm.setRemoteCallsSnapshot(),
		"mcp SetRemote must fire once with (remote host, remote project path)")
	waitFor(t, func() bool { return mm.refreshCountSnapshot() >= 1 }, "mcp Refresh must run on remote")
}

func TestSyncPluginProject_RemoteNode_DedupesOnRepeatedSync(t *testing.T) {
	// syncPluginProject is called from every cursor movement. On a
	// remote node the dedupe key (host|projectDir) must prevent
	// repeat SSH refreshes when the underlying selection has not
	// actually changed.
	app, _, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	app.cursor = 2
	app.syncPluginProject()
	waitFor(t, func() bool { return mm.refreshCountSnapshot() >= 1 }, "first refresh")
	first := mm.refreshCountSnapshot()

	for i := 0; i < 5; i++ {
		app.syncPluginProject()
	}
	assert.Equal(t, first, mm.refreshCountSnapshot(),
		"repeated sync on the same remote node must NOT re-spawn Refresh")
	assert.Len(t, mm.setRemoteCallsSnapshot(), 1,
		"repeated sync on the same remote node must NOT re-call SetRemote")
}

func TestSyncPluginProject_LocalNode_RefreshesAndClearsFlag(t *testing.T) {
	app, mp, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	// Simulate coming from a prior remote selection.
	app.pluginState.remoteDisabled = true
	app.mcpState.remoteKey = "ssh-host|/remote/path"

	// Cursor on local project (index 0).
	app.cursor = 0
	app.syncPluginProject()

	assert.False(t, app.pluginState.remoteDisabled, "plugin remoteDisabled must clear on local cursor")
	assert.False(t, app.mcpState.remoteDisabled, "mcp remoteDisabled must stay clear on local cursor")
	assert.Equal(t, []string{"/tmp/local"}, mp.setProjectCallsSnapshot())
	// SetRemote("", local) must fire so the provider atomically
	// restores the local code path AND installs the local project
	// dir in one lock acquisition.
	assert.Equal(t,
		[]mockRemoteCall{{host: "", projectDir: "/tmp/local"}},
		mm.setRemoteCallsSnapshot(),
		"mcp SetRemote(\"\", local) must fire on remote->local transition")
	assert.Empty(t, app.mcpState.remoteKey, "remoteKey dedupe must clear on local transition")
	waitFor(t, func() bool { return mp.refreshCountSnapshot() >= 1 }, "plugin Refresh must run on local")
	waitFor(t, func() bool { return mm.refreshCountSnapshot() >= 1 }, "mcp Refresh must run on local")
}

func TestSyncPluginProject_RemoteThenLocal_ResetsFlag(t *testing.T) {
	app, _, _ := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	app.cursor = 2 // remote
	app.syncPluginProject()
	require.True(t, app.pluginState.remoteDisabled)

	app.cursor = 0 // local
	app.syncPluginProject()
	assert.False(t, app.pluginState.remoteDisabled, "plugin remoteDisabled must reset on remote->local")
	assert.False(t, app.mcpState.remoteDisabled, "mcp remoteDisabled must reset on remote->local")
}

func TestSyncPluginProject_TreeEmptiesAfterLocal_ResetsToCWD(t *testing.T) {
	// Regression for codex P1 on a25ed88: after selecting local A,
	// moving to remote, then an out-of-band tree rebuild emptying the
	// tree, the recovery path must reset pluginState.projectDir to
	// the CWD fallback instead of leaving it pointing at A. Otherwise
	// a plugin/MCP write after the tree empties would mutate A.
	app, mp, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	// Select local A first (establishes projectDir=/tmp/local).
	app.cursor = 0
	app.syncPluginProject()
	require.Equal(t, "/tmp/local", app.pluginState.projectDir)
	// Drain the refresh-on-select baseline so subsequent asserts
	// only see calls made by the recovery path.
	waitFor(t, func() bool { return mp.refreshCountSnapshot() >= 1 }, "baseline plugin refresh")
	waitFor(t, func() bool { return mm.refreshCountSnapshot() >= 1 }, "baseline mcp refresh")

	// Move to remote (flag flips, projectDir untouched).
	app.cursor = 2
	app.syncPluginProject()
	require.True(t, app.pluginState.remoteDisabled)
	require.Equal(t, "/tmp/local", app.pluginState.projectDir,
		"precondition: projectDir unchanged by remote selection")

	// Simulate out-of-band tree empty (background GC).
	attachProjectsAndRefresh(app, nil)
	app.cursor = 0
	require.Nil(t, app.currentNode())
	require.Empty(t, app.cachedNodes)

	app.syncPluginProject()

	// Flag cleared AND projectDir is reset to something other than
	// the old local path. Use NotEqual rather than matching an
	// absolute path because filepath.Abs(".") is environment-dependent.
	assert.False(t, app.pluginState.remoteDisabled,
		"empty-tree recovery must clear remoteDisabled")
	assert.False(t, app.mcpState.remoteDisabled)
	assert.NotEqual(t, "/tmp/local", app.pluginState.projectDir,
		"empty-tree recovery must not leave projectDir pointing at the last local project")
	assert.NotEmpty(t, app.pluginState.projectDir,
		"empty-tree recovery must set projectDir to the CWD fallback")
}

func TestSyncPluginProject_TreeEmpties_ClearsFlagEvenWhenProjectDirIsCWD(t *testing.T) {
	// Regression for codex P1 on 7b5c6ea: when the prior local
	// selection happens to equal the CWD fallback, the earlier
	// `projectDir != cwd` guard skipped the recovery entirely and
	// left remoteDisabled stuck true. The flag clear must happen
	// independently of that refresh-gating check.
	//
	// Phase 2 Codex follow-up: even when projectDir==cwd already,
	// the empty-tree recovery MUST still issue SetRemote("", cwd)
	// unconditionally. The manager may be holding (remoteHost,
	// remoteDir) from the preceding remote selection — skipping
	// the atomic reset would let a subsequent MCP write target the
	// old remote host through the nil-node guardRemoteOp fallback.
	app, _, mm := newRemoteDisabledApp(t)
	cwd, err := filepath.Abs(".")
	require.NoError(t, err)

	// Construct a project tree whose local project Path equals the
	// process CWD, so the later empty-tree branch sees projectDir == cwd.
	projects := []ProjectItem{
		{
			ID:       "local-cwd",
			Name:     "cwd",
			Path:     cwd,
			Host:     "",
			Expanded: true,
			Sessions: []SessionItem{{ID: "ls1", Name: "ls1", Path: cwd}},
		},
		{
			ID:       "remote",
			Name:     "remote",
			Path:     "/remote/path",
			Host:     "ssh-host",
			Expanded: true,
			Sessions: []SessionItem{{ID: "rs1", Name: "rs1", Host: "ssh-host", Path: "/remote/path/rs1"}},
		},
	}
	attachProjectsAndRefresh(app, projects)

	// Local → remote → tree empties.
	app.cursor = 0
	app.syncPluginProject()
	require.Equal(t, cwd, app.pluginState.projectDir)

	app.cursor = 2
	app.syncPluginProject()
	require.True(t, app.pluginState.remoteDisabled)

	attachProjectsAndRefresh(app, nil)
	app.cursor = 0
	require.Nil(t, app.currentNode())
	require.Empty(t, app.cachedNodes)

	app.syncPluginProject()

	assert.False(t, app.pluginState.remoteDisabled,
		"flag must clear on empty-tree recovery even when projectDir already equals CWD")
	assert.False(t, app.mcpState.remoteDisabled)

	// The atomic (host, projectDir) reset must run even though the
	// refresh-gating `projectDir != cwd` check is false.
	last := mm.lastSetRemote()
	assert.Empty(t, last.host,
		"empty-tree recovery must atomically clear the manager's host "+
			"even when projectDir==cwd short-circuits the refresh")
	assert.Equal(t, cwd, last.projectDir,
		"empty-tree recovery must install the CWD fallback via SetRemote")
}

func TestSyncPluginProject_EmptyTreeReset_ResetsPanelCursors(t *testing.T) {
	// Regression for codex P2 on 7b5c6ea: when the empty-tree
	// recovery swaps to the CWD fallback, the panel cursors must
	// zero, otherwise an out-of-range cursor silently blocks write
	// handlers (PluginUninstall / PluginToggleEnabled / MCPToggleDenied)
	// via their own `cursor >= len(...)` early returns.
	app, _, _ := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	// Select local A and park the panel cursors deep.
	app.cursor = 0
	app.syncPluginProject()
	app.pluginState.installedCursor = 99
	app.pluginState.marketCursor = 99
	app.mcpState.cursor = 99

	// Trigger the empty-tree recovery.
	attachProjectsAndRefresh(app, nil)
	app.cursor = 0
	app.syncPluginProject()

	assert.Zero(t, app.pluginState.installedCursor,
		"installedCursor must reset on empty-tree recovery")
	assert.Zero(t, app.pluginState.marketCursor,
		"marketCursor must reset on empty-tree recovery")
	assert.Zero(t, app.mcpState.cursor,
		"mcpState.cursor must reset on empty-tree recovery")
}

func TestSyncPluginProject_EmptyTreeReset_IsIdempotent(t *testing.T) {
	// The empty-tree recovery is triggered from the layout loop, so
	// it must be idempotent — otherwise it would spawn Refresh on
	// every frame. Once projectDir matches the CWD fallback the
	// subsequent calls must be no-ops at the provider level.
	app, mp, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, nil)

	// First call: triggers the reset and one Refresh each.
	app.syncPluginProject()
	waitFor(t, func() bool { return mp.refreshCountSnapshot() >= 1 }, "first refresh")
	waitFor(t, func() bool { return mm.refreshCountSnapshot() >= 1 }, "first refresh")
	firstPlugin := mp.refreshCountSnapshot()
	firstMCP := mm.refreshCountSnapshot()

	// Subsequent calls on the same empty tree must NOT spawn more.
	for i := 0; i < 5; i++ {
		app.syncPluginProject()
	}
	assert.Equal(t, firstPlugin, mp.refreshCountSnapshot(),
		"plugin refresh must not re-spawn on repeated empty-tree sync")
	assert.Equal(t, firstMCP, mm.refreshCountSnapshot(),
		"mcp refresh must not re-spawn on repeated empty-tree sync")
}

func TestSyncPluginProject_FilterHidesRemote_FlagPreserved(t *testing.T) {
	// Edge case surfaced by codex review: an active sessions-panel
	// search filter can make currentNode() return nil even though the
	// underlying tree still contains a remote node. In that transient
	// state the pluginState.remoteDisabled flag must NOT clear —
	// otherwise the next plugin write falls through the guard and runs
	// against the preserved local provider state.
	//
	// Phase 2: the MCP panel is no longer "disabled" on remote, but
	// the dedupe key (remoteKey) must survive the transient so that
	// returning to the same remote selection does not re-spawn a
	// wasteful SSH refresh.
	app, _, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	// Put the cursor on the remote node first so the flag is legitimately set.
	app.cursor = 2
	app.syncPluginProject()
	require.True(t, app.pluginState.remoteDisabled)
	waitFor(t, func() bool { return mm.refreshCountSnapshot() >= 1 }, "baseline refresh")
	require.Equal(t, "ssh-host|/remote/path", app.mcpState.remoteKey,
		"precondition: remoteKey must be set after remote sync")

	// Simulate filter yielding zero rows: keep cachedNodes non-empty
	// but force currentNode() to return nil by parking the cursor out
	// of range (the real code does this via filteredTreeNodes when the
	// search query matches nothing).
	originalNodes := len(app.cachedNodes)
	require.Greater(t, originalNodes, 0, "precondition: cachedNodes must be non-empty")
	app.cursor = originalNodes + 10
	require.Nil(t, app.currentNode(), "cursor out of range must produce nil node")

	app.syncPluginProject()

	assert.True(t, app.pluginState.remoteDisabled,
		"plugin remoteDisabled must survive transient nil-node (filter edge case)")
	assert.Equal(t, "ssh-host|/remote/path", app.mcpState.remoteKey,
		"mcpState.remoteKey must survive transient nil-node so dedupe still short-circuits")
	// SetRemote must NOT have been re-called (no second entry beyond
	// the initial remote sync).
	assert.Len(t, mm.setRemoteCallsSnapshot(), 1,
		"transient nil-node must not re-invoke SetRemote")
}

func TestGuardRemoteOp_FilterHidesRemote_FlagFallbackBlocks(t *testing.T) {
	// Edge case: sessions-panel filter yields no matches while the
	// underlying tree still holds a remote node, so cachedNodes is
	// non-empty but currentNode() returns nil. syncPluginProject
	// preserves the remoteDisabled flag in that case (len(cachedNodes)
	// > 0), and the guard must fall back to the flag so writes stay
	// blocked.
	app, _, _ := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	// Legitimately set the flag by selecting the remote node first.
	app.cursor = 2
	app.syncPluginProject()
	require.True(t, app.pluginState.remoteDisabled)

	// Park cursor out of range (simulates filter that hides every row).
	app.cursor = len(app.cachedNodes) + 10
	require.Nil(t, app.currentNode())

	assert.True(t, app.guardRemoteOp("Plugin editing"),
		"nil-node + non-empty tree must fall back to the flag and block")
}

func TestGuardRemoteOp_AllClear_ReturnsFalse(t *testing.T) {
	// Regression sanity check: the flag fallback must not cause the
	// guard to false-positive when everything is local.
	app, _, _ := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())
	app.cursor = 0 // local project
	require.NotNil(t, app.currentNode())

	assert.False(t, app.guardRemoteOp("Plugin editing"),
		"local cursor with cleared flag must not trigger the guard")
}

func TestGuardRemoteOp_StaleFlagOnLocalNode_LiveNodeWins(t *testing.T) {
	// Regression for the codex P2 follow-up: if some cursor-moving path
	// (applySearchFilter, cursor clamp in layout, moveCursorToLastSession)
	// lands the cursor on a LIVE LOCAL node without calling
	// syncPluginProject, the cached remoteDisabled flag may still be
	// set. The guard must trust the live node and allow the operation.
	// OR-ing the flag unconditionally would wrongly block local writes.
	app, _, _ := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	// Cursor lands on the local project (index 0) directly, without
	// syncPluginProject being called first. The flag is artificially
	// left in the stale "remote" state.
	app.cursor = 0
	require.NotNil(t, app.currentNode())
	app.pluginState.remoteDisabled = true
	app.mcpState.remoteDisabled = true

	assert.False(t, app.guardRemoteOp("Plugin editing"),
		"live local node must override a stale remoteDisabled flag")
}

func TestMoveCursorToLastSession_SyncsPluginPanel(t *testing.T) {
	// Regression: moveCursorToLastSession moves a.cursor after a
	// session create/delete but historically did not re-sync the
	// plugin/MCP panels. Without the re-sync, a local→remote cursor
	// jump would leave the plugin write guard thinking it was still
	// on the local selection.
	//
	// remoteAndLocalProjects() places the remote project second,
	// so the "last session" resolves to the remote-s1 SessionNode.
	// We start on the local project and expect moveCursorToLastSession
	// to flip the panels over to the remote selection.
	app, _, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	app.cursor = 0 // local project
	app.syncPluginProject()
	require.False(t, app.pluginState.remoteDisabled)

	app.moveCursorToLastSession()

	n := app.currentNode()
	require.NotNil(t, n)
	require.Equal(t, SessionNode, n.Kind)
	require.Equal(t, "ssh-host", n.Session.Host,
		"precondition: last session must be the remote one")

	assert.True(t, app.pluginState.remoteDisabled,
		"moveCursorToLastSession must re-sync plugin panel to remote")
	// Phase 2: mcp is routed to SSH (not disabled). Assert the
	// (host, projectDir) atomically forwarded through SetRemote.
	last := mm.lastSetRemote()
	assert.Equal(t, "ssh-host", last.host,
		"moveCursorToLastSession must re-sync mcp panel to the remote host")
	assert.Equal(t, "/remote/path/rs1", last.projectDir,
		"moveCursorToLastSession must re-sync mcp panel to the remote session path")
}

func TestCloseSearch_EscRestore_ReSyncsPluginPanel(t *testing.T) {
	// Regression for codex P2 follow-up: when Esc cancels a sessions
	// search, the cursor snaps back to the pre-search row but
	// closeSearch historically did not call syncPluginProject. After
	// "start on remote → type local query → Esc" the plugin/MCP
	// panels would keep showing the local project (from applySearchFilter)
	// even though the cursor was back on the remote row.
	app, _, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	// Start on remote, establish the expected flag state.
	app.cursor = 2
	app.syncPluginProject()
	require.True(t, app.pluginState.remoteDisabled)

	// Simulate the search flow: dialog opens, query types, filter
	// snaps to local, Esc restores the original cursor.
	app.dialog.Kind = DialogSearch
	app.dialog.SearchPanel = "sessions"
	app.dialog.SearchPreCursor = 2
	app.dialog.SearchQuery = "local"
	app.applySearchFilter()
	require.False(t, app.pluginState.remoteDisabled,
		"precondition: applySearchFilter moved the panels to local")

	// Esc — closeSearch restores the pre-search cursor and must
	// re-sync the plugin/MCP panels back to the remote selection.
	app.closeSearch(app.gui, true)

	assert.True(t, app.pluginState.remoteDisabled,
		"Esc restore must re-sync plugin panel back to remote")
	// Phase 2: MCP must be re-routed to the remote host+path rather
	// than flagged as disabled.
	last := mm.lastSetRemote()
	assert.Equal(t, "ssh-host", last.host,
		"Esc restore must re-point MCP panel at the remote host")
	assert.Equal(t, "/remote/path", last.projectDir,
		"Esc restore must re-point MCP panel at the remote project dir")
}

func TestApplySearchFilter_SessionsPanel_ReSyncsPluginPanel(t *testing.T) {
	// Regression for the codex P2 follow-up: applySearchFilter moves
	// a.cursor when the user searches, but historically did not invoke
	// syncPluginProject. That left the plugin/MCP panels rendering the
	// stale remote placeholder after a user searched from a remote
	// selection into a local match. The fix calls syncPluginProject
	// from applySearchFilter so both the flag and the cached project
	// path are refreshed along with the cursor.
	app, mp, _ := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	// Start on the remote node so the flag gets set.
	app.cursor = 2
	app.syncPluginProject()
	require.True(t, app.pluginState.remoteDisabled)

	// Simulate the user searching for "local" — in the real code the
	// editor updates dialog.SearchQuery and calls applySearchFilter.
	app.dialog.Kind = DialogSearch
	app.dialog.SearchPanel = "sessions"
	app.dialog.SearchQuery = "local"
	app.applySearchFilter()

	// After the filter snaps the cursor to the first match (the local
	// project), the panels must observe the local selection.
	assert.False(t, app.pluginState.remoteDisabled,
		"applySearchFilter must re-sync plugin panel when cursor lands on local")
	assert.False(t, app.mcpState.remoteDisabled,
		"applySearchFilter must re-sync MCP panel when cursor lands on local")
	assert.Equal(t, "/tmp/local", app.pluginState.projectDir,
		"applySearchFilter must flow the new projectDir into plugin state")
	assert.Equal(t, []string{"/tmp/local"}, mp.setProjectCallsSnapshot())
}

func TestSyncPluginProject_InitialNoNode_FlagStaysFalse(t *testing.T) {
	app, _, _ := newRemoteDisabledApp(t)
	// No session provider attached — currentNode() returns nil.
	require.Nil(t, app.currentNode())

	app.syncPluginProject()
	assert.False(t, app.pluginState.remoteDisabled)
	assert.False(t, app.mcpState.remoteDisabled)
}

func TestSyncPluginProjectOnce_RemoteStartup_SkipsCWDFallback(t *testing.T) {
	// Phase 2: on a remote startup the plugin panel stays disabled
	// (projectDir untouched), but the MCP panel is routed to the
	// remote host. The Once path must short-circuit BEFORE the CWD
	// fallback so the local CWD plugin refresh is not emitted.
	app, mp, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	app.cursor = 2 // remote project
	app.syncPluginProjectOnce()

	assert.True(t, app.pluginState.remoteDisabled)
	assert.False(t, app.mcpState.remoteDisabled, "MCP is enabled on remote in Phase 2")

	// Plugin provider untouched.
	assert.Empty(t, mp.setProjectCallsSnapshot(), "remote startup must not trigger plugin SetProjectDir")
	assert.Zero(t, mp.refreshCountSnapshot(), "remote startup must not trigger plugin Refresh")
	assert.Empty(t, app.pluginState.projectDir, "projectDir must stay unset so we don't poison future sync")

	// MCP provider forwarded to the remote host atomically.
	assert.Equal(t,
		[]mockRemoteCall{{host: "ssh-host", projectDir: "/remote/path"}},
		mm.setRemoteCallsSnapshot(),
		"remote startup must forward (host, projectDir) via SetRemote")
	waitFor(t, func() bool { return mm.refreshCountSnapshot() >= 1 },
		"remote startup must trigger a single mcp Refresh")
}

func TestSyncPluginProjectOnce_NoNode_FallbackRefreshes(t *testing.T) {
	app, mp, mm := newRemoteDisabledApp(t)
	// No session provider — currentNode() returns nil.
	require.Nil(t, app.currentNode())

	app.syncPluginProjectOnce()

	assert.False(t, app.pluginState.remoteDisabled)
	assert.False(t, app.mcpState.remoteDisabled)
	assert.Equal(t, ".", app.pluginState.projectDir, "fallback must mark projectDir as initialised")
	// MCP SetRemote is called once with ("", abs(CWD)) in the fallback.
	calls := mm.setRemoteCallsSnapshot()
	require.Len(t, calls, 1, "fallback must call mcp SetRemote once")
	assert.Empty(t, calls[0].host, "fallback SetRemote must install the local code path")
	assert.NotEmpty(t, calls[0].projectDir, "fallback SetRemote must carry the CWD projectDir")
	waitFor(t, func() bool { return mp.refreshCountSnapshot() >= 1 }, "fallback must call plugin Refresh")
	waitFor(t, func() bool { return mm.refreshCountSnapshot() >= 1 }, "fallback must call mcp Refresh")
}

func TestSyncPluginProjectOnce_FallbackResetsPriorRemoteFlag(t *testing.T) {
	app, _, _ := newRemoteDisabledApp(t)

	// Simulate prior remote selection that left the flag set.
	app.pluginState.remoteDisabled = true
	app.mcpState.remoteDisabled = true

	// No session provider — currentNode() returns nil, so the fallback runs.
	app.syncPluginProjectOnce()

	assert.False(t, app.pluginState.remoteDisabled, "fallback must clear plugin remoteDisabled")
	assert.False(t, app.mcpState.remoteDisabled, "fallback must clear mcp remoteDisabled")
}

// --- Step 5: write entry point guards ---

func TestWriteGuards_RemoteNode_PluginBlockedMCPAllowed(t *testing.T) {
	// Phase 2: the plugin write guards still block on a remote
	// cursor (Phase 3 territory), but MCP writes are NO LONGER
	// guarded — they run through the SSH code path.
	app, mp, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())
	app.cursor = 2 // remote project

	assert.True(t, app.guardRemoteOp("Plugin editing"),
		"plugin guard must short-circuit on remote cursor")

	// Calling plugin entry points must be a no-op at the provider
	// level because the guard runs before the provider calls.
	app.pluginState.tabIdx = keymap.PluginTabMarketplace
	app.PluginInstall()
	app.pluginState.tabIdx = keymap.PluginTabPlugins
	app.PluginUninstall()
	app.PluginToggleEnabled()
	app.PluginUpdate()
	app.PluginRefresh()

	assert.Zero(t, mp.refreshCountSnapshot(), "plugin Refresh must not run on remote guard")
	assert.Empty(t, mp.installCalls, "plugin Install must not run on remote guard")
	assert.Empty(t, mp.uninstallCalls, "plugin Uninstall must not run on remote guard")
	assert.Empty(t, mp.toggleCalls, "plugin ToggleEnabled must not run on remote guard")
	assert.Empty(t, mp.updateCalls, "plugin Update must not run on remote guard")

	// MCP entry points are unguarded in Phase 2. MCPRefresh reaches
	// the provider even on a remote cursor.
	app.pluginState.tabIdx = keymap.PluginTabMCP
	app.MCPRefresh()
	waitFor(t, func() bool { return mm.refreshCountSnapshot() >= 1 },
		"MCPRefresh must reach the provider on remote cursor (Phase 2)")
}

func TestWriteGuards_LocalNode_ProviderCalled(t *testing.T) {
	app, mp, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	// Seed one installed plugin and one marketplace plugin so the
	// non-guard paths reach the provider.
	mp.installed = []PluginItem{{ID: "alpha@1.0.0", Version: "1.0.0", Scope: "project", Enabled: true}}
	mp.available = []AvailablePluginItem{{PluginID: "beta", Name: "beta"}}
	mm.servers = []MCPItem{{Name: "srv1", Type: "stdio", Scope: "project"}}

	// Cursor on local project.
	app.cursor = 0
	// PluginRefresh works regardless of tab.
	app.PluginRefresh()
	waitFor(t, func() bool { return mp.refreshCountSnapshot() >= 1 }, "local PluginRefresh must reach provider")

	// MCPRefresh works regardless of tab.
	app.MCPRefresh()
	waitFor(t, func() bool { return mm.refreshCountSnapshot() >= 1 }, "local MCPRefresh must reach provider")
}

// --- Step 6: render layer placeholders ---

// makeTestView allocates a gocui view suitable for render assertions.
// SetView returns ErrUnknownView on first call but the view pointer is
// valid, so the error is intentionally ignored.
func makeTestView(app *App, name string) *gocui.View {
	v, _ := app.gui.SetView(name, 0, 0, 80, 20, 0)
	v.Clear()
	return v
}

func TestRenderPluginPanel_RemoteDisabled_ShowsPlaceholder_PluginsTab(t *testing.T) {
	app, _, _ := newRemoteDisabledApp(t)
	app.pluginState.tabIdx = keymap.PluginTabPlugins
	app.pluginState.remoteDisabled = true

	v := makeTestView(app, "plugins")
	app.renderPluginPanel(v, 80)

	buf := stripANSI(v.Buffer())
	assert.Contains(t, buf, "Plugin editing on remote hosts is not supported")
	assert.Contains(t, buf, "Switch cursor to a local session")
}

func TestRenderPluginPanel_RemoteDisabled_ShowsPlaceholder_MarketplaceTab(t *testing.T) {
	app, _, _ := newRemoteDisabledApp(t)
	app.pluginState.tabIdx = keymap.PluginTabMarketplace
	app.pluginState.remoteDisabled = true

	v := makeTestView(app, "plugins-mkt")
	app.renderPluginPanel(v, 80)

	buf := stripANSI(v.Buffer())
	assert.Contains(t, buf, "Plugin editing on remote hosts is not supported")
}

func TestRenderPluginPanel_MCPTab_DispatchesToMCPRenderer(t *testing.T) {
	// Critical: when tabIdx == MCP, renderPluginPanel must NOT emit the
	// plugin placeholder text. It must delegate to renderMCPList, which
	// then honours mcpState.remoteDisabled independently.
	app, _, _ := newRemoteDisabledApp(t)
	app.pluginState.tabIdx = keymap.PluginTabMCP
	// Set ONLY the plugin flag to prove the MCP tab bypass works.
	app.pluginState.remoteDisabled = true
	app.mcpState.remoteDisabled = false

	v := makeTestView(app, "plugins-mcp-bypass")
	app.renderPluginPanel(v, 80)

	buf := stripANSI(v.Buffer())
	assert.NotContains(t, buf, "Plugin editing on remote hosts is not supported",
		"MCP tab must not surface the plugin-flavoured placeholder")
}

func TestRenderRemoteDisabledPlaceholder_ResetsOrigin(t *testing.T) {
	// Codex P3 finding: the placeholder was not resetting v.SetOrigin.
	// If the plugin/MCP list was previously scrolled (origin y > 0),
	// the new placeholder would render off-screen. Simulate the scrolled
	// state, render, then assert the origin is back at (0, 0).
	app, _, _ := newRemoteDisabledApp(t)
	v := makeTestView(app, "origin-reset")

	// Park the view on a non-zero origin to simulate "user had scrolled
	// down through a long plugin list before switching to remote".
	v.SetOrigin(0, 5)
	v.SetCursor(0, 5)
	_, oy := v.Origin()
	require.Equal(t, 5, oy, "precondition: origin must be non-zero before render")

	renderRemoteDisabledPlaceholder(v, "remote disabled")

	ox, oy := v.Origin()
	assert.Equal(t, 0, ox, "placeholder must reset origin x")
	assert.Equal(t, 0, oy, "placeholder must reset origin y so text is visible")
}

// Phase 2 removed the remote-disabled placeholder for MCP — MCP now
// runs through the SSH code path. The render layer falls through to
// the normal MCPProvider output, so the previous placeholder tests
// no longer have a feature to exercise.

func TestRenderPluginPreview_RemoteDisabled_ShowsPlaceholder(t *testing.T) {
	cases := []struct {
		name string
		tab  int
	}{
		{"plugins tab", keymap.PluginTabPlugins},
		{"marketplace tab", keymap.PluginTabMarketplace},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			app, _, _ := newRemoteDisabledApp(t)
			app.pluginState.tabIdx = tc.tab
			app.pluginState.remoteDisabled = true

			v := makeTestView(app, "plugins-preview-"+tc.name)
			app.renderPluginPreview(v)

			buf := stripANSI(v.Buffer())
			assert.Contains(t, buf, "Remote session")
			assert.Contains(t, buf, "plugin editing not supported")
		})
	}
}

// Note: stripANSI is defined in app_actions.go and reused here.
