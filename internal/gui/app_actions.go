package gui

import (
	"fmt"
	"path/filepath"

	"github.com/KEMSHlM/lazyclaude/internal/core/choice"
	"github.com/KEMSHlM/lazyclaude/internal/gui/keyhandler"
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
}

func (a *App) MoveCursorUp() {
	if a.cursor > 0 {
		a.cursor--
	}
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
		err = a.sessions.CreatePMSession(abs)
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
		items, err := a.sessions.ListWorktrees(abs)
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
	text := a.logs.CopyText(readLogLines())
	if text != "" {
		copyToClipboard(text)
	}
	a.logs.ClearSelection()
}

// --- Application ---

func (a *App) Quit() { a.quitRequested = true }
