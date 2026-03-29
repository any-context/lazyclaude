package gui

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/KEMSHlM/lazyclaude/internal/core/choice"
	"github.com/KEMSHlM/lazyclaude/internal/gui/keyhandler"
	"github.com/KEMSHlM/lazyclaude/internal/session"
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

// treeNodes returns the cached flat tree node list.
func (a *App) treeNodes() []TreeNode {
	return a.cachedNodes
}

// currentNode returns the tree node at the cursor, or nil if out of bounds.
func (a *App) currentNode() *TreeNode {
	nodes := a.cachedNodes
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
		projectPath = node.Session.Path
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
}

// --- Session operations ---

func (a *App) CreateSession() {
	if a.sessions == nil {
		return
	}
	host := DetectSSHHost()
	path := "."
	if host != "" {
		if rp := DetectRemotePath(); rp != "" {
			path = rp
		}
	}
	if err := a.sessions.Create(path, host); err != nil {
		a.gui.Update(func(g *gocui.Gui) error {
			a.setStatus(g, fmt.Sprintf("Error: %v", err))
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
			a.setStatus(g, fmt.Sprintf("Error: %v", err))
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
	launchErr := a.sessions.LaunchLazygit(sess.Path, sess.Host)
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
	go func() {
		abs, err := filepath.Abs(".")
		if err != nil {
			return
		}
		projectRoot := session.InferProjectRoot(abs)
		err = a.sessions.CreatePMSession(projectRoot)
		a.gui.Update(func(g *gocui.Gui) error {
			if err != nil {
				a.setStatus(g, fmt.Sprintf("PM error: %v", err))
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
	go func() {
		abs, err := filepath.Abs(".")
		if err != nil {
			return
		}
		projectRoot := session.InferProjectRoot(abs)
		items, err := a.sessions.ListWorktrees(projectRoot)
		a.gui.Update(func(g *gocui.Gui) error {
			if err != nil {
				a.setStatus(g, fmt.Sprintf("Error: %v", err))
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

func (a *App) PurgeOrphans() {
	if a.sessions == nil {
		return
	}
	count, err := a.sessions.PurgeOrphans()
	if err != nil {
		a.gui.Update(func(g *gocui.Gui) error {
			a.setStatus(g, fmt.Sprintf("Error: %v", err))
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
		a.panelTabs[name] = cur + 1
		a.onPanelTabChanged(name, cur+1)
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
		a.panelTabs[name] = cur - 1
		a.onPanelTabChanged(name, cur-1)
	}
}

func (a *App) ActivePanelTabIndex() int {
	panel := a.panelManager.ActivePanel()
	if panel == nil {
		return 0
	}
	return a.panelTabs[panel.Name()]
}

// onPanelTabChanged is called when a panel's tab changes.
// Panel-specific side effects (e.g. resetting cursors) go here.
func (a *App) onPanelTabChanged(panelName string, newTab int) {
	if panelName == "plugins" {
		a.pluginState.tabIdx = newTab
	}
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
	if a.plugins == nil || a.pluginState.tabIdx != 1 {
		return
	}
	avail := a.plugins.Available()
	if a.pluginState.marketCursor >= len(avail) {
		return
	}
	pluginID := avail[a.pluginState.marketCursor].PluginID
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.Install(ctx, pluginID)
	})
}

func (a *App) PluginUninstall() {
	if a.plugins == nil || a.pluginState.tabIdx != 0 {
		return
	}
	installed := a.plugins.Installed()
	if a.pluginState.installedCursor >= len(installed) {
		return
	}
	pluginID := installed[a.pluginState.installedCursor].ID
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.Uninstall(ctx, pluginID)
	})
}

func (a *App) PluginToggleEnabled() {
	if a.plugins == nil || a.pluginState.tabIdx != 0 {
		return
	}
	installed := a.plugins.Installed()
	if a.pluginState.installedCursor >= len(installed) {
		return
	}
	pluginID := installed[a.pluginState.installedCursor].ID
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.ToggleEnabled(ctx, pluginID)
	})
}

func (a *App) PluginUpdate() {
	if a.plugins == nil || a.pluginState.tabIdx != 0 {
		return
	}
	installed := a.plugins.Installed()
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

// pluginItemCount returns the number of items in the active plugin tab.
func (a *App) pluginItemCount() int {
	if a.plugins == nil {
		return 0
	}
	if a.pluginState.tabIdx == 0 {
		return len(a.plugins.Installed())
	}
	return len(a.plugins.Available())
}

// --- Application ---

func (a *App) Quit() { a.quitRequested = true }
