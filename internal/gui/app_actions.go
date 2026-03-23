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

// --- Session cursor ---

func (a *App) MoveCursorDown() {
	if a.sessions == nil {
		return
	}
	if a.cursor < len(a.sessions.Sessions())-1 {
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
	items := a.sessions.Sessions()
	if a.cursor < 0 || a.cursor >= len(items) {
		return
	}
	if err := a.sessions.Delete(items[a.cursor].ID); err != nil {
		a.gui.Update(func(g *gocui.Gui) error {
			a.setStatus(g, fmt.Sprintf("Error: %v", err))
			return nil
		})
		return
	}
	if a.cursor > 0 && a.cursor >= len(a.sessions.Sessions()) {
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
	items := a.sessions.Sessions()
	if a.cursor < 0 || a.cursor >= len(items) {
		return
	}
	sess := items[a.cursor]
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
	items := a.sessions.Sessions()
	if a.cursor < 0 || a.cursor >= len(items) {
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
	attachErr := a.sessions.AttachSession(items[a.cursor].ID)
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
	items := a.sessions.Sessions()
	if a.cursor >= 0 && a.cursor < len(items) {
		a.enterFullScreen(items[a.cursor].ID)
	}
}

func (a *App) StartRename() {
	if a.sessions == nil || a.renameSessionID != "" {
		return
	}
	items := a.sessions.Sessions()
	if a.cursor < 0 || a.cursor >= len(items) {
		return
	}
	a.renameSessionID = items[a.cursor].ID
	a.gui.Update(func(g *gocui.Gui) error {
		if !a.showRenameInput(g, items[a.cursor].Name) {
			a.renameSessionID = ""
		}
		return nil
	})
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
