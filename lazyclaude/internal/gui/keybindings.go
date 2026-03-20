package gui

import (
	"fmt"

	"github.com/jesseduffield/gocui"
)

// setupGlobalKeybindings registers all keybindings using the app's keymap.
//
// Input priority per event:
//  1. Popup handlers (if popup showing)
//  2. Lazyclaude keymap actions (if key matches current context)
//  3. Forward to Claude Code (if full-screen mode)
func (a *App) setupGlobalKeybindings() error {
	km := a.keyRegistry

	// Ctrl+C: always quit
	if err := a.gui.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return gocui.ErrQuit
	}); err != nil {
		return err
	}

	// q: quit (fullScreen keys handled by Editor, not here)
	if err := a.gui.SetKeybinding("", km.FirstRune(ActionQuit), gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() || a.state.IsFullScreen() {
			return nil
		}
		if a.mode == ModeMain {
			return gocui.ErrQuit
		}
		return nil
	}); err != nil {
		return err
	}

	// Esc on popup view: suspend (hide, reopenable with 'p')
	if err := a.gui.SetKeybinding(popupViewName, gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.suspendAllPopups()
		return nil
	}); err != nil {
		return err
	}

	// Esc: suspend popup, forward in full-screen, quit in popup mode
	if err := a.gui.SetKeybinding("", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.suspendAllPopups()
			return nil
		}
		if a.state.IsFullScreen() {
			a.forwardSpecialKey("Escape")
			return nil
		}
		if a.mode == ModeDiff || a.mode == ModeTool {
			return gocui.ErrQuit
		}
		return nil
	}); err != nil {
		return err
	}

	// Cursor/scroll handler factory: tmuxSpecial for arrow keys, empty for j/k
	makeCursorHandler := func(tmuxSpecial string, isDown bool) func(*gocui.Gui, *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			if a.hasPopup() {
				if tmuxSpecial != "" {
					if isDown {
						a.popupFocusNext()
					} else {
						a.popupFocusPrev()
					}
				} else {
					p := a.popups.ActivePopup()
					if p != nil && p.IsDiff() {
						if isDown && p.ScrollY() < maxScrollFor(len(p.ContentLines()), 20) {
							p.SetScrollY(p.ScrollY() + 1)
						}
						if !isDown && p.ScrollY() > 0 {
							p.SetScrollY(p.ScrollY() - 1)
						}
					}
				}
				return nil
			}
			if a.state.IsFullScreen() {
				if tmuxSpecial != "" {
					a.forwardSpecialKey(tmuxSpecial)
				}
				return nil
			}
			if a.mode != ModeMain {
				return nil
			}
			if isDown && a.sessions != nil {
				if a.cursor < len(a.sessions.Sessions())-1 {
					a.cursor++
				}
			}
			if !isDown && a.cursor > 0 {
				a.cursor--
			}
			return nil
		}
	}

	jDown := makeCursorHandler("", true)
	kUp := makeCursorHandler("", false)
	arrowDown := makeCursorHandler("Down", true)
	arrowUp := makeCursorHandler("Up", false)

	for _, b := range []struct {
		view string
		key  interface{} // rune or gocui.Key
		fn   func(*gocui.Gui, *gocui.View) error
	}{
		{"", km.FirstRune(ActionCursorDown), jDown},
		{"", gocui.KeyArrowDown, arrowDown},
		{"", km.FirstRune(ActionCursorUp), kUp},
		{"", gocui.KeyArrowUp, arrowUp},
		{popupViewName, km.FirstRune(ActionCursorDown), jDown},
		{popupViewName, km.FirstRune(ActionCursorUp), kUp},
		{popupViewName, gocui.KeyArrowDown, arrowDown},
		{popupViewName, gocui.KeyArrowUp, arrowUp},
	} {
		var err error
		switch k := b.key.(type) {
		case rune:
			err = a.gui.SetKeybinding(b.view, k, gocui.ModNone, b.fn)
		case gocui.Key:
			err = a.gui.SetKeybinding(b.view, k, gocui.ModNone, b.fn)
		}
		if err != nil {
			return err
		}
	}

	// n: new session or reject popup (fullScreen handled by Editor)
	if err := a.gui.SetKeybinding("", km.FirstRune(ActionNewSession), gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissPopup(ChoiceReject)
			return nil
		}
		if a.state.IsFullScreen() || a.mode != ModeMain || a.sessions == nil {
			return nil
		}
		if err := a.sessions.Create(".", ""); err != nil {
			a.setStatus(g, fmt.Sprintf("Error: %v", err))
			return nil
		}
		a.setStatus(g, "Session created")
		return nil
	}); err != nil {
		return err
	}

	// 'p': unsuspend (reopen) suspended popups
	if err := a.gui.SetKeybinding("", 'p', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.popupCount() > 0 && !a.hasPopup() {
			a.unsuspendAll()
		}
		return nil
	}); err != nil {
		return err
	}

	// Popup choice handlers — bind on BOTH global ("") and popup view name.
	popupAccept := func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissPopup(ChoiceAccept)
		}
		return nil
	}
	popupAllow := func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissPopup(ChoiceAllow)
		}
		return nil
	}
	popupReject := func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissPopup(ChoiceReject)
		}
		return nil
	}
	popupAcceptAll := func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissAllPopups(ChoiceAccept)
		}
		return nil
	}

	for _, b := range []struct {
		view string
		ch   rune
		fn   func(*gocui.Gui, *gocui.View) error
	}{
		{"", 'Y', popupAcceptAll},
		{popupViewName, 'Y', popupAcceptAll},
		{"", km.FirstRune(ActionPopupAccept), popupAccept},
		{popupViewName, km.FirstRune(ActionPopupAccept), popupAccept},
		{"", km.FirstRune(ActionPopupAllow), popupAllow},
		{popupViewName, km.FirstRune(ActionPopupAllow), popupAllow},
		{popupViewName, '1', popupAccept},
		{popupViewName, '2', popupAllow},
		{popupViewName, '3', popupReject},
		{popupViewName, km.FirstRune(ActionPopupReject), popupReject},
	} {
		if err := a.gui.SetKeybinding(b.view, b.ch, gocui.ModNone, b.fn); err != nil {
			return err
		}
	}

	// d: delete session (main mode only)
	if err := a.gui.SetKeybinding("", km.FirstRune(ActionDeleteSession), gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() || a.state.IsFullScreen() || a.mode != ModeMain || a.sessions == nil {
			return nil
		}
		items := a.sessions.Sessions()
		if a.cursor >= 0 && a.cursor < len(items) {
			if err := a.sessions.Delete(items[a.cursor].ID); err != nil {
				a.setStatus(g, fmt.Sprintf("Error: %v", err))
				return nil
			}
			if a.cursor > 0 && a.cursor >= len(a.sessions.Sessions()) {
				a.cursor--
			}
			a.setStatus(g, "Session deleted")
		}
		return nil
	}); err != nil {
		return err
	}

	// Enter: enter full-screen or forward
	if err := a.gui.SetKeybinding("", km.FirstKey(ActionEnterFull), gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			return nil
		}
		if a.state.IsFullScreen() {
			a.forwardSpecialKey("Enter")
			return nil
		}
		if a.mode != ModeMain || a.sessions == nil {
			return nil
		}
		items := a.sessions.Sessions()
		if a.cursor >= 0 && a.cursor < len(items) {
			a.enterFullScreen(items[a.cursor].ID)
		}
		return nil
	}); err != nil {
		return err
	}

	// Ctrl+D: exit full-screen mode
	if err := a.gui.SetKeybinding("", gocui.KeyCtrlD, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.state.IsFullScreen() {
			a.exitFullScreen()
		}
		return nil
	}); err != nil {
		return err
	}

	// Ctrl+\: exit full-screen mode (no popup when popup is showing)
	if err := a.gui.SetKeybinding("", gocui.KeyCtrlBackslash, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.state.IsFullScreen() && !a.hasPopup() {
			a.exitFullScreen()
		}
		return nil
	}); err != nil {
		return err
	}

	// Mouse scroll in full-screen
	if err := a.gui.SetKeybinding("", gocui.MouseWheelUp, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.state.IsFullScreen() && a.fullScreenScrollY > 0 {
			a.fullScreenScrollY--
		}
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("", gocui.MouseWheelDown, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.state.IsFullScreen() {
			a.fullScreenScrollY++
		}
		return nil
	}); err != nil {
		return err
	}

	// r: resume (enter full-screen, same as Enter)
	if err := a.gui.SetKeybinding("", 'r', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() || a.state.IsFullScreen() {
			return nil
		}
		if a.mode == ModeMain && a.sessions != nil {
			items := a.sessions.Sessions()
			if a.cursor >= 0 && a.cursor < len(items) {
				a.enterFullScreen(items[a.cursor].ID)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// R: rename
	if err := a.gui.SetKeybinding("", 'R', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() || a.state.IsFullScreen() || a.mode != ModeMain || a.sessions == nil {
			return nil
		}
		items := a.sessions.Sessions()
		if a.cursor >= 0 && a.cursor < len(items) {
			newName := items[a.cursor].Name + "-renamed"
			if err := a.sessions.Rename(items[a.cursor].ID, newName); err != nil {
				a.setStatus(g, fmt.Sprintf("Error: %v", err))
				return nil
			}
			a.setStatus(g, "Renamed to "+newName)
		}
		return nil
	}); err != nil {
		return err
	}

	// D: purge orphans
	if err := a.gui.SetKeybinding("", 'D', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() || a.state.IsFullScreen() || a.mode != ModeMain || a.sessions == nil {
			return nil
		}
		count, err := a.sessions.PurgeOrphans()
		if err != nil {
			a.setStatus(g, fmt.Sprintf("Error: %v", err))
			return nil
		}
		a.setStatus(g, fmt.Sprintf("Purged %d orphans", count))
		return nil
	}); err != nil {
		return err
	}

	return nil
}
