package gui

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/any-context/lazyclaude/internal/core/model"
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

type mockMCPProvider struct {
	mu              sync.Mutex
	setProjectCalls []string
	refreshCount    int
	toggleCalls     []string
	servers         []MCPItem
}

func (m *mockMCPProvider) SetProjectDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setProjectCalls = append(m.setProjectCalls, dir)
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

func (m *mockMCPProvider) setProjectCallsSnapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.setProjectCalls))
	copy(out, m.setProjectCalls)
	return out
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
func (m *miniSessionProvider) SendChoice(_ string, _ Choice) error              { return nil }
func (m *miniSessionProvider) AttachSession(_ string) error                     { return nil }
func (m *miniSessionProvider) LaunchLazygit(_ string) error                     { return nil }
func (m *miniSessionProvider) CreateWorktree(_, _, _ string) error              { return nil }
func (m *miniSessionProvider) ResumeWorktree(_, _, _ string) error              { return nil }
func (m *miniSessionProvider) ListWorktrees(_ string) ([]WorktreeInfo, error)   { return nil, nil }
func (m *miniSessionProvider) CreatePMSession(_ string) error                   { return nil }
func (m *miniSessionProvider) CreateWorkerSession(_, _, _ string) error         { return nil }

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

func TestSyncPluginProject_RemoteNode_SkipsRefreshAndFlipsFlag(t *testing.T) {
	app, mp, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	// Cursor on remote project (index 2 = ProjectNode(remote)).
	app.cursor = 2
	app.syncPluginProject()

	assert.True(t, app.pluginState.remoteDisabled, "plugin remoteDisabled must be set on remote cursor")
	assert.True(t, app.mcpState.remoteDisabled, "mcp remoteDisabled must be set on remote cursor")
	assert.Empty(t, mp.setProjectCallsSnapshot(), "plugin SetProjectDir must not be called on remote")
	assert.Empty(t, mm.setProjectCallsSnapshot(), "mcp SetProjectDir must not be called on remote")
	assert.Zero(t, mp.refreshCountSnapshot(), "plugin Refresh must not be called on remote")
	assert.Zero(t, mm.refreshCountSnapshot(), "mcp Refresh must not be called on remote")
	assert.Empty(t, app.pluginState.projectDir, "pluginState.projectDir must not be set on remote")
}

func TestSyncPluginProject_LocalNode_RefreshesAndClearsFlag(t *testing.T) {
	app, mp, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	// Simulate coming from a prior remote selection.
	app.pluginState.remoteDisabled = true
	app.mcpState.remoteDisabled = true

	// Cursor on local project (index 0).
	app.cursor = 0
	app.syncPluginProject()

	assert.False(t, app.pluginState.remoteDisabled, "plugin remoteDisabled must clear on local cursor")
	assert.False(t, app.mcpState.remoteDisabled, "mcp remoteDisabled must clear on local cursor")
	assert.Equal(t, []string{"/tmp/local"}, mp.setProjectCallsSnapshot())
	assert.Equal(t, []string{"/tmp/local"}, mm.setProjectCallsSnapshot())
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

func TestSyncPluginProject_RemoteThenNoNode_ResetsFlag(t *testing.T) {
	// Recovery path: closing the last remote session can leave the tree
	// empty, meaning currentNode() returns nil. The flag must clear so
	// the panels leave the placeholder state.
	app, _, _ := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	app.cursor = 2
	app.syncPluginProject()
	require.True(t, app.pluginState.remoteDisabled)

	// Drop all projects and rebuild the tree — cursor now points past end.
	attachProjectsAndRefresh(app, nil)
	app.cursor = 0
	require.Nil(t, app.currentNode(), "currentNode must be nil after projects drained")

	app.syncPluginProject()
	assert.False(t, app.pluginState.remoteDisabled, "plugin remoteDisabled must clear when no node")
	assert.False(t, app.mcpState.remoteDisabled, "mcp remoteDisabled must clear when no node")
}

func TestSyncPluginProject_FilterHidesRemote_FlagPreserved(t *testing.T) {
	// Edge case surfaced by codex review: an active sessions-panel
	// search filter can make currentNode() return nil even though the
	// underlying tree still contains a remote node. In that transient
	// state the remoteDisabled flag must NOT clear — otherwise the next
	// write handler falls through the guard and runs against the
	// preserved local provider state.
	app, _, _ := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	// Put the cursor on the remote node first so the flag is legitimately set.
	app.cursor = 2
	app.syncPluginProject()
	require.True(t, app.pluginState.remoteDisabled)
	require.True(t, app.mcpState.remoteDisabled)

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
	assert.True(t, app.mcpState.remoteDisabled,
		"mcp remoteDisabled must survive transient nil-node (filter edge case)")
}

func TestGuardRemoteOp_FlagSetButNoNode_StillGuards(t *testing.T) {
	// Defence in depth: even if syncPluginProject somehow left the
	// remoteDisabled flag set and the cursor then landed on nil, the
	// write guard must still return true so mutate-local-by-accident
	// cannot slip through.
	app, _, _ := newRemoteDisabledApp(t)
	app.pluginState.remoteDisabled = true
	require.Nil(t, app.currentNode())

	assert.True(t, app.guardRemoteOp("Plugin editing"),
		"flag-only state must keep the guard effective")
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
	app, mp, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())

	app.cursor = 2 // remote project
	app.syncPluginProjectOnce()

	assert.True(t, app.pluginState.remoteDisabled)
	assert.True(t, app.mcpState.remoteDisabled)
	assert.Empty(t, mp.setProjectCallsSnapshot(), "remote startup must not trigger plugin SetProjectDir")
	assert.Empty(t, mm.setProjectCallsSnapshot(), "remote startup must not trigger mcp SetProjectDir")
	assert.Zero(t, mp.refreshCountSnapshot(), "remote startup must not trigger plugin Refresh")
	assert.Zero(t, mm.refreshCountSnapshot(), "remote startup must not trigger mcp Refresh")
	assert.Empty(t, app.pluginState.projectDir, "projectDir must stay unset so we don't poison future sync")
}

func TestSyncPluginProjectOnce_NoNode_FallbackRefreshes(t *testing.T) {
	app, mp, mm := newRemoteDisabledApp(t)
	// No session provider — currentNode() returns nil.
	require.Nil(t, app.currentNode())

	app.syncPluginProjectOnce()

	assert.False(t, app.pluginState.remoteDisabled)
	assert.False(t, app.mcpState.remoteDisabled)
	assert.Equal(t, ".", app.pluginState.projectDir, "fallback must mark projectDir as initialised")
	// MCP SetProjectDir is called with an absolute CWD in the fallback.
	assert.Len(t, mm.setProjectCallsSnapshot(), 1, "fallback must call mcp SetProjectDir once")
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

func TestWriteGuards_RemoteNode_GuardReturnsTrue(t *testing.T) {
	// Directly exercise guardRemoteOp: if it returns true for a remote
	// cursor, the callers (PluginInstall et al.) cannot reach their
	// runPluginAsync / runMCPAsync invocations, so there is nothing
	// to wait for. This replaces an earlier variant that called each
	// entry point and slept for goroutines that should never exist.
	app, mp, mm := newRemoteDisabledApp(t)
	attachProjectsAndRefresh(app, remoteAndLocalProjects())
	app.cursor = 2 // remote project

	assert.True(t, app.guardRemoteOp("Plugin editing"),
		"guard must short-circuit on remote cursor")

	// Defence in depth: calling the entry points should also be a
	// no-op at the provider level because the guard runs before the
	// provider calls. These assertions are synchronous — if the guard
	// failed, the code path after it runs on the calling goroutine up
	// to the runPluginAsync launch, which itself invokes the provider
	// before the goroutine starts (SetProjectDir is synchronous in the
	// local branch). So counter != 0 would be visible immediately.
	app.pluginState.tabIdx = keymap.PluginTabMarketplace
	app.PluginInstall()
	app.pluginState.tabIdx = keymap.PluginTabPlugins
	app.PluginUninstall()
	app.PluginToggleEnabled()
	app.PluginUpdate()
	app.PluginRefresh()
	app.pluginState.tabIdx = keymap.PluginTabMCP
	app.MCPToggleDenied()
	app.MCPRefresh()

	assert.Zero(t, mp.refreshCountSnapshot(), "plugin Refresh must not run on remote guard")
	assert.Zero(t, mm.refreshCountSnapshot(), "mcp Refresh must not run on remote guard")
	assert.Empty(t, mp.installCalls, "plugin Install must not run on remote guard")
	assert.Empty(t, mp.uninstallCalls, "plugin Uninstall must not run on remote guard")
	assert.Empty(t, mp.toggleCalls, "plugin ToggleEnabled must not run on remote guard")
	assert.Empty(t, mp.updateCalls, "plugin Update must not run on remote guard")
	assert.Empty(t, mm.toggleCalls, "mcp ToggleDenied must not run on remote guard")
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

func TestRenderMCPList_RemoteDisabled_ShowsPlaceholder(t *testing.T) {
	app, _, _ := newRemoteDisabledApp(t)
	app.mcpState.remoteDisabled = true

	v := makeTestView(app, "mcp-list")
	app.renderMCPList(v, 80, false)

	buf := stripANSI(v.Buffer())
	assert.Contains(t, buf, "MCP editing on remote hosts is not supported")
	assert.Contains(t, buf, "Switch cursor to a local session")
}

func TestRenderMCPPreview_RemoteDisabled_ShowsPlaceholder(t *testing.T) {
	app, _, _ := newRemoteDisabledApp(t)
	app.mcpState.remoteDisabled = true

	v := makeTestView(app, "mcp-preview")
	app.renderMCPPreview(v)

	buf := stripANSI(v.Buffer())
	assert.Contains(t, buf, "Remote session")
	assert.Contains(t, buf, "MCP editing not supported")
}

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
