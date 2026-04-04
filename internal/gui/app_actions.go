package gui

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/any-context/lazyclaude/internal/core/choice"
	"github.com/any-context/lazyclaude/internal/gui/keyhandler"
	"github.com/any-context/lazyclaude/internal/gui/keymap"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/jesseduffield/gocui"
)

// Compile-time check: *App implements keyhandler.AppActions.
var _ keyhandler.AppActions = (*App)(nil)

// --- State queries ---

func (a *App) HasPopup() bool    { return a.hasPopup() }
func (a *App) IsFullScreen() bool { return a.fullscreen.IsActive() }

// --- Tree helpers ---

// refreshTreeNodes rebuilds the flat tree node list from projects.
// Called once per layout cycle to ensure consistency within a render pass.
func (a *App) refreshTreeNodes() {
	if a.sessions == nil {
		a.cachedNodes = nil
		return
	}
	a.cachedNodes = BuildTreeNodes(a.sessions.Projects())
}

// treeNodes returns the tree node list, filtered when search is active.
func (a *App) treeNodes() []TreeNode {
	return a.filteredTreeNodes()
}

// currentNode returns the tree node at the cursor, or nil if out of bounds.
func (a *App) currentNode() *TreeNode {
	nodes := a.filteredTreeNodes()
	if a.cursor < 0 || a.cursor >= len(nodes) {
		return nil
	}
	return &nodes[a.cursor]
}

// currentSession returns the session at the cursor, or nil if cursor is on a project.
func (a *App) currentSession() *SessionItem {
	node := a.currentNode()
	if node == nil || node.Kind != SessionNode {
		return nil
	}
	return node.Session
}

// --- Tree operations ---

func (a *App) ToggleProjectExpanded() {
	node := a.currentNode()
	if node == nil || node.Kind != ProjectNode {
		return
	}
	a.sessions.ToggleProjectExpanded(node.ProjectID)
	// Rebuild cache immediately so cursor bounds are correct.
	a.refreshTreeNodes()
}

func (a *App) CollapseProject() {
	node := a.currentNode()
	if node == nil || node.Kind != ProjectNode {
		return
	}
	if node.Project != nil && node.Project.Expanded {
		a.sessions.ToggleProjectExpanded(node.ProjectID)
		a.refreshTreeNodes()
	}
}

func (a *App) ExpandProject() {
	node := a.currentNode()
	if node == nil || node.Kind != ProjectNode {
		return
	}
	if node.Project != nil && !node.Project.Expanded {
		a.sessions.ToggleProjectExpanded(node.ProjectID)
		a.refreshTreeNodes()
	}
}

func (a *App) CursorIsProject() bool {
	node := a.currentNode()
	return node != nil && node.Kind == ProjectNode
}

// --- Session cursor ---

func (a *App) MoveCursorDown() {
	if a.sessions == nil {
		return
	}
	nodes := a.treeNodes()
	if a.cursor < len(nodes)-1 {
		a.cursor++
	}
	a.syncPluginProject()
}

func (a *App) MoveCursorUp() {
	if a.cursor > 0 {
		a.cursor--
	}
	a.syncPluginProject()
}

// syncPluginProjectOnce triggers the first plugin load during layout.
// Tries to derive the project path from the session tree; if no sessions
// exist yet, falls back to the process working directory so that installed
// plugins are displayed immediately.
func (a *App) syncPluginProjectOnce() {
	if a.plugins == nil || a.pluginState.projectDir != "" {
		return
	}

	// Try session tree first (preferred: project-scoped context).
	node := a.currentNode()
	if node != nil {
		a.syncPluginProject()
		if a.pluginState.projectDir != "" {
			return
		}
	}

	// Fallback: no sessions yet — use process CWD so plugins load immediately.
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.Refresh(ctx)
	})
	if a.mcpServers != nil {
		cwd, _ := filepath.Abs(".")
		a.mcpServers.SetProjectDir(cwd)
		a.runMCPAsync(func(ctx context.Context) error {
			return a.mcpServers.Refresh(ctx)
		})
	}
	a.pluginState.projectDir = "." // mark as initialised to prevent re-entry
}

// syncPluginProject updates the plugin panel's project context
// based on the currently selected session/project in the tree.
func (a *App) syncPluginProject() {
	if a.plugins == nil {
		return
	}
	node := a.currentNode()
	if node == nil {
		return
	}
	var projectPath string
	if node.Kind == ProjectNode && node.Project != nil {
		projectPath = node.Project.Path
	} else if node.Session != nil {
		projectPath = a.configDirForSession(node.Session)
	}
	if projectPath == "" || projectPath == a.pluginState.projectDir {
		return
	}
	a.pluginState.projectDir = projectPath
	a.pluginState.installedCursor = 0
	a.pluginState.marketCursor = 0
	a.plugins.SetProjectDir(projectPath)
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.Refresh(ctx)
	})

	if a.mcpServers != nil {
		a.mcpState.cursor = 0
		a.mcpServers.SetProjectDir(projectPath)
		a.runMCPAsync(func(ctx context.Context) error {
			return a.mcpServers.Refresh(ctx)
		})
	}
}

// --- Path helpers ---

// currentProjectRoot returns the project root path for the currently selected
// tree node. For ProjectNode, returns Project.Path directly. For SessionNode,
// looks up the parent project's stored Path (avoids InferProjectRoot which can
// mismatch on remote when paths differ from the stored project path).
// Falls back to filepath.Abs(".") when no node is selected.
func (a *App) currentProjectRoot() string {
	node := a.currentNode()
	if node != nil {
		switch node.Kind {
		case ProjectNode:
			if node.Project != nil && node.Project.Path != "" {
				return node.Project.Path
			}
		case SessionNode:
			// Look up the parent project's stored path instead of inferring
			// from the session's worktree path. This prevents mismatches when
			// the stored project path differs from InferProjectRoot output
			// (e.g. relative "." vs absolute, or symlink-resolved paths).
			if path := a.projectPathByID(node.ProjectID); path != "" {
				return path
			}
			if node.Session != nil && node.Session.Path != "" {
				return session.InferProjectRoot(node.Session.Path)
			}
		}
	}
	abs, err := filepath.Abs(".")
	if err != nil {
		return "."
	}
	return session.InferProjectRoot(abs)
}

// projectPathByID returns the stored Path for the project with the given ID.
// Returns "" if not found. Scans the current tree nodes which are already
// cached, so this is inexpensive.
func (a *App) projectPathByID(projectID string) string {
	if projectID == "" {
		return ""
	}
	for _, n := range a.treeNodes() {
		if n.Kind == ProjectNode && n.ProjectID == projectID && n.Project != nil {
			return n.Project.Path
		}
	}
	return ""
}

// configDirForSession returns the directory to use for configuration lookups
// (MCP servers, plugins, settings) for the given session.
// Worktree sessions use their worktree path directly so that configuration
// is localized to the worktree. Non-worktree sessions use InferProjectRoot
// to resolve back to the project root.
func (a *App) configDirForSession(s *SessionItem) string {
	if s == nil || s.Path == "" {
		return ""
	}
	if session.IsWorktreePath(s.Path) {
		return s.Path
	}
	return session.InferProjectRoot(s.Path)
}

// --- Session operations ---

func (a *App) CreateSession() { a.createSession(a.currentProjectRoot()) }
func (a *App) CreateSessionAtCWD() { a.createSession(".") }

// createSession is the shared implementation for CreateSession and CreateSessionAtCWD.
// localPath is the fallback directory for non-SSH sessions.
func (a *App) createSession(localPath string) {
	if a.sessions == nil {
		return
	}
	if err := a.sessions.Create(localPath); err != nil {
		a.gui.Update(func(g *gocui.Gui) error {
			a.showError(g, fmt.Sprintf("Error: %v", err))
			return nil
		})
		return
	}
	a.gui.Update(func(g *gocui.Gui) error {
		a.setStatus(g, "Session created")
		return nil
	})
}

func (a *App) DeleteSession() {
	if a.sessions == nil {
		return
	}
	sess := a.currentSession()
	if sess == nil {
		return
	}
	if err := a.sessions.Delete(sess.ID); err != nil {
		a.gui.Update(func(g *gocui.Gui) error {
			a.showError(g, fmt.Sprintf("Error: %v", err))
			return nil
		})
		return
	}
	nodes := a.treeNodes()
	if a.cursor > 0 && a.cursor >= len(nodes) {
		a.cursor--
	}
	a.gui.Update(func(g *gocui.Gui) error {
		a.setStatus(g, "Session deleted")
		return nil
	})
}

func (a *App) LaunchLazygit() {
	if a.sessions == nil {
		return
	}
	s := a.currentSession()
	if s == nil {
		return
	}
	sess := *s
	g := a.gui
	if err := g.Suspend(); err != nil {
		a.gui.Update(func(g *gocui.Gui) error {
			a.setStatus(g, fmt.Sprintf("Suspend error: %v", err))
			return nil
		})
		return
	}
	launchErr := a.sessions.LaunchLazygit(sess.Path)
	if err := g.Resume(); err != nil {
		return
	}
	if launchErr != nil {
		a.gui.Update(func(g *gocui.Gui) error {
			a.setStatus(g, fmt.Sprintf("lazygit error: %v", launchErr))
			return nil
		})
	}
}

func (a *App) AttachSession() {
	if a.sessions == nil {
		return
	}
	sess := a.currentSession()
	if sess == nil {
		return
	}
	g := a.gui
	if err := g.Suspend(); err != nil {
		a.gui.Update(func(g *gocui.Gui) error {
			a.setStatus(g, fmt.Sprintf("Suspend error: %v", err))
			return nil
		})
		return
	}
	attachErr := a.sessions.AttachSession(sess.ID)
	if err := g.Resume(); err != nil {
		return
	}
	if attachErr != nil {
		a.gui.Update(func(g *gocui.Gui) error {
			a.setStatus(g, fmt.Sprintf("Attach error: %v", attachErr))
			return nil
		})
	}
}

func (a *App) EnterFullScreen() {
	if a.sessions == nil {
		return
	}
	sess := a.currentSession()
	if sess == nil {
		return
	}
	a.enterFullScreen(sess.ID)
}

func (a *App) StartRename() {
	if a.sessions == nil || a.dialog.RenameID != "" {
		return
	}
	sess := a.currentSession()
	if sess == nil {
		return
	}
	a.dialog.RenameID = sess.ID
	name := sess.Name
	a.gui.Update(func(g *gocui.Gui) error {
		if !a.showRenameInput(g, name) {
			a.dialog.RenameID = ""
		}
		return nil
	})
}

func (a *App) StartPMSession() {
	if a.sessions == nil || a.HasActiveDialog() {
		return
	}
	projectRoot := a.currentProjectRoot()
	go func() {
		err := a.sessions.CreatePMSession(projectRoot)
		a.gui.Update(func(g *gocui.Gui) error {
			if err != nil {
				a.showError(g, fmt.Sprintf("PM error: %v", err))
			} else {
				a.setStatus(g, "PM session started")
			}
			return nil
		})
	}()
}

func (a *App) StartWorktreeInput() {
	if a.sessions == nil || a.HasActiveDialog() {
		return
	}
	a.gui.Update(func(g *gocui.Gui) error {
		if !a.showWorktreeDialog(g) {
			a.setStatus(g, "Error: could not open worktree dialog")
		}
		return nil
	})
}

func (a *App) SelectWorktree() {
	if a.sessions == nil || a.HasActiveDialog() {
		return
	}
	projectRoot := a.currentProjectRoot()
	go func() {
		items, err := a.sessions.ListWorktrees(projectRoot)
		a.gui.Update(func(g *gocui.Gui) error {
			if err != nil {
				a.showError(g, fmt.Sprintf("Error: %v", err))
				return nil
			}
			if len(items) == 0 {
				a.setStatus(g, "No worktrees found")
				return nil
			}
			wtItems := make([]WorktreeInfo, len(items))
			copy(wtItems, items)
			if !a.showWorktreeChooser(g, wtItems) {
				a.setStatus(g, "Error: could not open worktree chooser")
			}
			return nil
		})
	}()
}

func (a *App) ConnectRemote() {
	if a.HasActiveDialog() {
		return
	}
	a.gui.Update(func(g *gocui.Gui) error {
		if !a.showConnectDialog(g) {
			a.setStatus(g, "Error: could not open connect dialog")
		}
		return nil
	})
}

func (a *App) PurgeOrphans() {
	if a.sessions == nil {
		return
	}
	count, err := a.sessions.PurgeOrphans()
	if err != nil {
		a.gui.Update(func(g *gocui.Gui) error {
			a.showError(g, fmt.Sprintf("Error: %v", err))
			return nil
		})
		return
	}
	a.gui.Update(func(g *gocui.Gui) error {
		a.setStatus(g, fmt.Sprintf("Purged %d orphans", count))
		return nil
	})
}

// --- Popup ---

func (a *App) DismissPopup(c choice.Choice) {
	a.dismissPopup(Choice(c))
}

func (a *App) DismissAllPopups(c choice.Choice) {
	a.dismissAllPopups(Choice(c))
}

func (a *App) SuspendPopups()   { a.suspendAllPopups() }
func (a *App) UnsuspendPopups() {
	if a.popupCount() > 0 && !a.hasPopup() {
		a.unsuspendAll()
	}
}
func (a *App) PopupFocusNext() { a.popupFocusNext() }
func (a *App) PopupFocusPrev() { a.popupFocusPrev() }

func (a *App) PopupScrollDown() {
	p := a.popups.ActivePopup()
	if p != nil && p.IsDiff() {
		if p.ScrollY() < maxScrollFor(len(p.ContentLines()), 20) {
			p.SetScrollY(p.ScrollY() + 1)
		}
	}
}

func (a *App) PopupScrollUp() {
	p := a.popups.ActivePopup()
	if p != nil && p.IsDiff() {
		if p.ScrollY() > 0 {
			p.SetScrollY(p.ScrollY() - 1)
		}
	}
}

// --- FullScreen ---

func (a *App) ExitFullScreen() { a.exitFullScreen() }

func (a *App) ForwardSpecialKey(tmuxKey string) {
	a.forwardSpecialKey(tmuxKey)
}

// --- Send key to pane (works without fullscreen) ---

func (a *App) SendKeyToPane(key string) {
	target := a.resolveSessionTarget()
	if target == "" {
		return
	}
	if a.fullscreen.forwarder == nil {
		return
	}
	go func() {
		_ = a.fullscreen.forwarder.ForwardKey(target, key)
	}()
}

// --- Logs ---

func (a *App) LogsCursorDown()    { a.logs.CursorDown() }
func (a *App) LogsCursorUp()      { a.logs.CursorUp() }
func (a *App) LogsCursorToEnd()   { a.logs.ToEnd() }
func (a *App) LogsCursorToTop()   { a.logs.ToTop() }
func (a *App) LogsToggleSelect()  { a.logs.ToggleSelect() }

func (a *App) LogsCopySelection() {
	text := a.logs.CopyText(a.readLogLines())
	if text != "" {
		copyToClipboard(text)
	}
	a.logs.ClearSelection()
}

// --- Panel tab switching (generic) ---

func (a *App) PanelNextTab() {
	panel := a.panelManager.ActivePanel()
	if panel == nil || panel.TabCount() <= 1 {
		return
	}
	name := panel.Name()
	cur := a.panelTabs[name]
	if cur < panel.TabCount()-1 {
		newTab := cur + 1
		a.panelTabs[name] = newTab
		panel.OnTabChanged(newTab, a)
	}
}

func (a *App) PanelPrevTab() {
	panel := a.panelManager.ActivePanel()
	if panel == nil || panel.TabCount() <= 1 {
		return
	}
	name := panel.Name()
	cur := a.panelTabs[name]
	if cur > 0 {
		newTab := cur - 1
		a.panelTabs[name] = newTab
		panel.OnTabChanged(newTab, a)
	}
}

func (a *App) ActivePanelTabIndex() int {
	panel := a.panelManager.ActivePanel()
	if panel == nil {
		return 0
	}
	return a.panelTabs[panel.Name()]
}

// PluginSetTab sets the active plugin tab index.
// Called from PluginsPanel.OnTabChanged via AppActions interface.
func (a *App) PluginSetTab(tab int) {
	a.pluginState.tabIdx = tab
}

// --- Plugin panel ---

func (a *App) PluginCursorDown() {
	max := a.pluginItemCount() - 1
	cur := a.pluginState.Cursor()
	if cur < max {
		a.pluginState.SetCursor(cur + 1)
	}
}

func (a *App) PluginCursorUp() {
	cur := a.pluginState.Cursor()
	if cur > 0 {
		a.pluginState.SetCursor(cur - 1)
	}
}

func (a *App) PluginInstall() {
	if a.plugins == nil || a.pluginState.tabIdx != keymap.PluginTabMarketplace {
		return
	}
	avail := a.filteredAvailablePlugins()
	if a.pluginState.marketCursor >= len(avail) {
		return
	}
	pluginID := avail[a.pluginState.marketCursor].PluginID
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.Install(ctx, pluginID)
	})
}

func (a *App) PluginUninstall() {
	if a.plugins == nil || a.pluginState.tabIdx != keymap.PluginTabPlugins {
		return
	}
	installed := a.filteredInstalledPlugins()
	if a.pluginState.installedCursor >= len(installed) {
		return
	}
	p := installed[a.pluginState.installedCursor]
	if p.Scope != "project" {
		a.pluginState.errMsg = "only project-scoped plugins can be uninstalled"
		return
	}
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.Uninstall(ctx, p.ID, p.Scope)
	})
}

func (a *App) PluginToggleEnabled() {
	if a.plugins == nil || a.pluginState.tabIdx != keymap.PluginTabPlugins {
		return
	}
	installed := a.filteredInstalledPlugins()
	if a.pluginState.installedCursor >= len(installed) {
		return
	}
	p := installed[a.pluginState.installedCursor]
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.ToggleEnabled(ctx, p.ID, p.Scope)
	})
}

func (a *App) PluginUpdate() {
	if a.plugins == nil || a.pluginState.tabIdx != keymap.PluginTabPlugins {
		return
	}
	installed := a.filteredInstalledPlugins()
	if a.pluginState.installedCursor >= len(installed) {
		return
	}
	pluginID := installed[a.pluginState.installedCursor].ID
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.Update(ctx, pluginID)
	})
}

func (a *App) PluginRefresh() {
	if a.plugins == nil {
		return
	}
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.Refresh(ctx)
	})
}

// runPluginAsync runs a plugin operation asynchronously with loading state management.
func (a *App) runPluginAsync(fn func(ctx context.Context) error) {
	a.pluginState.loading = true
	a.pluginState.errMsg = ""
	go func() {
		err := fn(context.Background())
		a.gui.Update(func(g *gocui.Gui) error {
			a.pluginState.loading = false
			if err != nil {
				a.pluginState.errMsg = err.Error()
			}
			return nil
		})
	}()
}

// pluginItemCount returns the number of items in the active plugin tab,
// respecting any active search filter.
func (a *App) pluginItemCount() int {
	if a.plugins == nil {
		return 0
	}
	switch a.pluginState.tabIdx {
	case keymap.PluginTabMCP:
		return len(a.filteredMCPServers())
	case keymap.PluginTabMarketplace:
		return len(a.filteredAvailablePlugins())
	default:
		return len(a.filteredInstalledPlugins())
	}
}

// --- MCP panel ---

func (a *App) MCPCursorDown() {
	max := a.mcpItemCount() - 1
	if a.mcpState.cursor < max {
		a.mcpState.cursor++
	}
}

func (a *App) MCPCursorUp() {
	if a.mcpState.cursor > 0 {
		a.mcpState.cursor--
	}
}

func (a *App) MCPToggleDenied() {
	if a.mcpServers == nil || a.pluginState.tabIdx != keymap.PluginTabMCP {
		return
	}
	servers := a.filteredMCPServers()
	if a.mcpState.cursor >= len(servers) {
		return
	}
	name := servers[a.mcpState.cursor].Name
	a.runMCPAsync(func(ctx context.Context) error {
		return a.mcpServers.ToggleDenied(ctx, name)
	})
}

func (a *App) MCPRefresh() {
	if a.mcpServers == nil {
		return
	}
	a.runMCPAsync(func(ctx context.Context) error {
		return a.mcpServers.Refresh(ctx)
	})
}

func (a *App) runMCPAsync(fn func(ctx context.Context) error) {
	a.mcpState.loading = true
	a.mcpState.errMsg = ""
	go func() {
		err := fn(context.Background())
		a.gui.Update(func(g *gocui.Gui) error {
			a.mcpState.loading = false
			if err != nil {
				a.mcpState.errMsg = err.Error()
			}
			return nil
		})
	}()
}

func (a *App) mcpItemCount() int {
	if a.mcpServers == nil {
		return 0
	}
	return len(a.filteredMCPServers())
}

// --- Help ---

func (a *App) ShowKeybindHelp() {
	if a.HasActiveDialog() || a.fullscreen.IsActive() {
		return
	}

	scope := a.panelManager.ActivePanel().Scope()
	tab := a.ActivePanelTabIndex()

	items := a.keyRegistry.BindingsForScopeTab(scope, tab)
	items = append(items, a.keyRegistry.BindingsForScope(keymap.ScopeGlobal)...)

	a.dialog.Kind = DialogKeybindHelp
	a.dialog.HelpAllItems = items
	a.dialog.HelpItems = items
	a.dialog.HelpCursor = 0
	a.dialog.HelpFilter = ""
	a.dialog.HelpScrollY = 0
}


// --- Search ---

func (a *App) StartSearch() {
	if a.HasActiveDialog() || a.fullscreen.IsActive() {
		return
	}

	panel := a.panelManager.ActivePanel()
	if panel == nil {
		return
	}
	panelName := panel.Name()

	// Save pre-search cursor position for Esc restore.
	var preCursor int
	switch panelName {
	case "sessions":
		preCursor = a.cursor
	case "plugins":
		preCursor = a.pluginState.Cursor()
	case "logs":
		preCursor = a.logs.CursorY()
	default:
		return
	}

	// Clear any previous active filter for this panel so the user
	// starts fresh. The previous filter's results are discarded.
	a.clearActiveFilter(panelName)

	a.dialog.Kind = DialogSearch
	a.dialog.SearchQuery = ""
	a.dialog.SearchPanel = panelName
	a.dialog.SearchPreCursor = preCursor
}

// --- Scroll mode ---

func (a *App) IsScrollMode() bool { return a.scroll.IsActive() }

func (a *App) ScrollModeEnter() {
	viewH := a.scrollViewHeight()
	if viewH <= 0 {
		return
	}
	a.scroll.Enter(viewH)
	// Query history_size to set maxOffset so g/G work correctly.
	target := a.fullscreen.Target()
	if target != "" {
		if histSize, err := a.sessions.HistorySize(target); err == nil && histSize > 0 {
			a.scroll.SetMaxOffset(histSize)
		}
	}
	a.scroll.BumpGeneration()
	a.captureScrollbackAsync()
}

func (a *App) ScrollModeExit() {
	a.scroll.Exit()
	a.preview.Invalidate()
}

func (a *App) ScrollModeUp() {
	// vim-like: move cursor up; if at top of viewport, scroll up
	if a.scroll.CursorY() > 0 {
		a.scroll.CursorUp()
		return
	}
	// Cursor at top edge — scroll viewport up (show older content)
	a.scroll.ScrollUp(1)
	a.scroll.BumpGeneration()
	a.captureScrollbackAsync()
}

func (a *App) ScrollModeDown() {
	// vim-like: move cursor down; if at bottom of viewport, scroll down
	maxCursor := len(a.scroll.Lines()) - 1
	if maxCursor < 0 {
		maxCursor = a.scroll.ViewHeight() - 1
	}
	if a.scroll.CursorY() < maxCursor {
		a.scroll.CursorDown()
		return
	}
	// Cursor at bottom edge — scroll viewport down (show newer content)
	if a.scroll.ScrollOffset() > 0 {
		a.scroll.ScrollDown(1)
		a.scroll.BumpGeneration()
		a.captureScrollbackAsync()
	}
}

func (a *App) ScrollModeHalfUp() {
	half := a.scroll.ViewHeight() / 2
	a.scroll.ScrollUp(half)
	// Keep cursor at same relative position (vim Ctrl+U behavior)
	a.scroll.BumpGeneration()
	a.captureScrollbackAsync()
}

func (a *App) ScrollModeHalfDown() {
	half := a.scroll.ViewHeight() / 2
	a.scroll.ScrollDown(half)
	a.scroll.BumpGeneration()
	a.captureScrollbackAsync()
}

func (a *App) ScrollModeToTop() {
	// Re-query history_size since it grows while the session is active.
	target := a.fullscreen.Target()
	if target != "" {
		if histSize, err := a.sessions.HistorySize(target); err == nil && histSize > 0 {
			a.scroll.SetMaxOffset(histSize)
		}
	}
	a.scroll.ToTop()
	a.scroll.BumpGeneration()
	a.captureScrollbackAsync()
}

func (a *App) ScrollModeToBottom() {
	a.scroll.ToBottom()
	a.scroll.BumpGeneration()
	a.captureScrollbackAsync()
}

func (a *App) ScrollModeToggleSelect() {
	a.scroll.ToggleSelect()
}

func (a *App) ScrollModeCopy() {
	text := a.scroll.CopyText()
	if text != "" {
		copyToClipboard(stripANSI(text))
	}
	a.scroll.Exit()
	a.preview.Invalidate()
}

// mouseScrollLines is the number of lines scrolled per mouse wheel tick.
const mouseScrollLines = 3

// ScrollModeMouseUp handles mouse wheel up in fullscreen.
// Enters scroll mode if not already active, then scrolls the viewport up.
func (a *App) ScrollModeMouseUp() {
	if !a.scroll.IsActive() {
		a.ScrollModeEnter()
	}
	a.scroll.ScrollUp(mouseScrollLines)
	a.scroll.BumpGeneration()
	a.captureScrollbackAsync()
}

// ScrollModeMouseDown handles mouse wheel down in fullscreen.
// Scrolls the viewport down. Exits scroll mode when reaching live output.
func (a *App) ScrollModeMouseDown() {
	if !a.scroll.IsActive() {
		return
	}
	a.scroll.ScrollDown(mouseScrollLines)
	if a.scroll.ScrollOffset() == 0 {
		a.ScrollModeExit()
		return
	}
	a.scroll.BumpGeneration()
	a.captureScrollbackAsync()
}

// stripANSI removes ANSI escape sequences from text.
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}

// scrollViewHeight returns the inner height of the fullscreen view.
func (a *App) scrollViewHeight() int {
	v, err := a.gui.View("main")
	if err != nil {
		return 0
	}
	return v.InnerHeight()
}


// captureScrollbackAsync launches a goroutine to capture scrollback content.
func (a *App) captureScrollbackAsync() {
	target := a.fullscreen.Target()
	if target == "" {
		return
	}
	gen := a.scroll.Generation()
	startLine, endLine := a.scroll.CaptureRange()
	viewW := a.scrollViewWidth()

	go func() {
		result, err := a.sessions.CaptureScrollback(target, viewW, startLine, endLine)
		// Serialise state mutation through the gocui event loop to avoid
		// racing with BumpGeneration/Exit on the main goroutine.
		a.gui.Update(func(g *gocui.Gui) error {
			if err != nil || a.scroll.Generation() != gen {
				return nil
			}
			a.scroll.SetLines(splitLines(result.Content))
			return nil
		})
	}()
}

// scrollViewWidth returns the inner width of the fullscreen view.
func (a *App) scrollViewWidth() int {
	v, err := a.gui.View("main")
	if err != nil {
		return 0
	}
	return v.InnerWidth()
}

// splitLines splits a string into lines, preserving empty trailing lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// --- Application ---

func (a *App) Quit() { a.quitRequested = true }
