package gui

import (
	"fmt"

	"github.com/jesseduffield/gocui"
)

// Binding represents a key binding with display metadata.
type Binding struct {
	Key             gocui.Key
	Rune            rune // for character keys (e.g., 'j', 'k', 'y')
	Description     string
	Handler         func() error
	DisplayOnScreen bool        // show in action bar
	Style           ActionStyle // color style for action bar
}

// MatchesKey returns true if this binding matches the given key event.
func (b *Binding) MatchesKey(key gocui.Key, ch rune) bool {
	if b.Rune != 0 {
		return ch == b.Rune
	}
	return key == b.Key
}

// Label returns a human-readable label for the key.
func (b *Binding) Label() string {
	if b.Rune != 0 {
		return string(b.Rune)
	}
	return KeyLabel(b.Key)
}

// KeyLabel returns a human-readable name for a gocui key.
func KeyLabel(key gocui.Key) string {
	switch key {
	case gocui.KeyEnter:
		return "enter"
	case gocui.KeyEsc:
		return "Esc"
	case gocui.KeySpace:
		return "space"
	case gocui.KeyTab:
		return "tab"
	case gocui.KeyArrowUp:
		return "up"
	case gocui.KeyArrowDown:
		return "down"
	case gocui.KeyCtrlC:
		return "ctrl+c"
	case gocui.KeyCtrlX:
		return "ctrl+x"
	default:
		return "?"
	}
}

// setupGlobalKeybindings registers all keybindings using the app's keymap.
//
// Input priority per event:
//  1. Popup handlers (if popup showing)
//  2. Lazyclaude keymap actions (if key matches current context)
//  3. Forward to Claude Code (if full-screen + insert mode)
func (a *App) setupGlobalKeybindings() error {
	km := a.keyMap

	// Ctrl+C: always quit
	if err := a.gui.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return gocui.ErrQuit
	}); err != nil {
		return err
	}

	// q: quit, exit full-screen (normal mode), or forward (insert mode)
	if err := a.gui.SetKeybinding("", km.FirstRune(ActionQuit), gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			return nil
		}
		if a.fullScreen {
			if a.inputMode == ModeNormal {
				a.exitFullScreen()
			} else {
				a.forwardKey(km.FirstRune(ActionQuit))
			}
			return nil
		}
		if a.mode == ModeMain {
			return gocui.ErrQuit
		}
		return nil
	}); err != nil {
		return err
	}

	// Esc on popup view: cancel
	if err := a.gui.SetKeybinding(popupViewName, gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.dismissPopup(ChoiceCancel)
		return nil
	}); err != nil {
		return err
	}

	// Esc: dismiss popup, forward in full-screen, quit in popup mode
	if err := a.gui.SetKeybinding("", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissPopup(ChoiceCancel)
			return nil
		}
		if a.fullScreen && a.inputMode == ModeInsert {
			a.forwardSpecialKey("Escape")
			return nil
		}
		if a.mode == ModeDiff || a.mode == ModeTool {
			return gocui.ErrQuit
		}
		if a.contextMgr.Depth() > 1 {
			a.contextMgr.Pop()
		}
		return nil
	}); err != nil {
		return err
	}

	// Cursor/scroll handler factory: runeKey for j/k literal, tmuxSpecial for arrow keys
	makeCursorHandler := func(runeKey rune, tmuxSpecial string, isDown bool) func(*gocui.Gui, *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			if a.hasPopup() {
				if isDown && a.pendingTool.IsDiff() && a.popupDiffCache != nil {
					if a.popupScrollY < len(a.popupDiffCache)-1 {
						a.popupScrollY++
					}
				}
				if !isDown && a.pendingTool.IsDiff() && a.popupScrollY > 0 {
					a.popupScrollY--
				}
				return nil
			}
			if a.fullScreen {
				if a.inputMode == ModeInsert {
					if tmuxSpecial != "" {
						a.forwardSpecialKey(tmuxSpecial)
					} else {
						a.forwardKey(runeKey)
					}
				} else {
					// Normal mode: j/k scroll preview history
					if isDown {
						a.scrollDown()
					} else {
						a.scrollUp()
					}
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

	jDown := makeCursorHandler(km.FirstRune(ActionCursorDown), "", true)
	kUp := makeCursorHandler(km.FirstRune(ActionCursorUp), "", false)
	arrowDown := makeCursorHandler(0, "Down", true)
	arrowUp := makeCursorHandler(0, "Up", false)

	if err := a.gui.SetKeybinding("", km.FirstRune(ActionCursorDown), gocui.ModNone, jDown); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("", gocui.KeyArrowDown, gocui.ModNone, arrowDown); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("", km.FirstRune(ActionCursorUp), gocui.ModNone, kUp); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("", gocui.KeyArrowUp, gocui.ModNone, arrowUp); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, km.FirstRune(ActionCursorDown), gocui.ModNone, jDown); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, km.FirstRune(ActionCursorUp), gocui.ModNone, kUp); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, gocui.KeyArrowDown, gocui.ModNone, arrowDown); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, gocui.KeyArrowUp, gocui.ModNone, arrowUp); err != nil {
		return err
	}

	// n: create session, reject popup, or forward in full-screen
	if err := a.gui.SetKeybinding("", km.FirstRune(ActionNewSession), gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissPopup(ChoiceReject)
			return nil
		}
		if a.fullScreen {
			a.forwardKey(km.FirstRune(ActionNewSession))
			return nil
		}
		if a.mode != ModeMain || a.sessions == nil {
			return nil
		}
		if err := a.sessions.Create(".", ""); err != nil {
			a.setStatus(g, fmt.Sprintf("Error: %v", err))
			return nil
		}
		a.setStatus(g, "Session created")
		a.NotifySessionCreated()
		return nil
	}); err != nil {
		return err
	}

	// Popup choice handlers — bind on BOTH global ("") and popup view name.
	// jesseduffield/gocui dispatches view-specific bindings first when a view has focus.
	// Global bindings may not fire when the popup view is focused.
	popupAccept := func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissPopup(ChoiceAccept)
		} else if a.fullScreen {
			a.forwardKey(km.FirstRune(ActionPopupAccept))
		}
		return nil
	}
	popupAllow := func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissPopup(ChoiceAllow)
		} else if a.fullScreen {
			a.forwardKey(km.FirstRune(ActionPopupAllow))
		}
		return nil
	}
	popupReject := func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissPopup(ChoiceReject)
		}
		return nil
	}

	// y: accept
	if err := a.gui.SetKeybinding("", km.FirstRune(ActionPopupAccept), gocui.ModNone, popupAccept); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, km.FirstRune(ActionPopupAccept), gocui.ModNone, popupAccept); err != nil {
		return err
	}

	// a: allow always
	if err := a.gui.SetKeybinding("", km.FirstRune(ActionPopupAllow), gocui.ModNone, popupAllow); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, km.FirstRune(ActionPopupAllow), gocui.ModNone, popupAllow); err != nil {
		return err
	}

	// 1/2/3: direct number selection (same as y/a/n)
	if err := a.gui.SetKeybinding(popupViewName, '1', gocui.ModNone, popupAccept); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, '2', gocui.ModNone, popupAllow); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, '3', gocui.ModNone, popupReject); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, km.FirstRune(ActionPopupReject), gocui.ModNone, popupReject); err != nil {
		return err
	}

	// d: delete or forward in full-screen
	if err := a.gui.SetKeybinding("", km.FirstRune(ActionDeleteSession), gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			return nil
		}
		if a.fullScreen {
			a.forwardKey(km.FirstRune(ActionDeleteSession))
			return nil
		}
		if a.mode != ModeMain || a.sessions == nil {
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

	// enter: enter full-screen or forward
	if err := a.gui.SetKeybinding("", km.FirstKey(ActionEnterFull), gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			return nil
		}
		if a.fullScreen {
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
	if err := a.gui.SetKeybinding("", km.FirstKey(ActionExitFull), gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.fullScreen {
			a.exitFullScreen()
		}
		return nil
	}); err != nil {
		return err
	}

	// Ctrl+\: switch to normal mode (insert -> normal)
	// Cannot use Esc or Ctrl+[ because they are the same byte (0x1B)
	// and Claude Code uses Esc for chat:cancel, autocomplete:dismiss, etc.
	if err := a.gui.SetKeybinding("", km.FirstKey(ActionNormalMode), gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.fullScreen && a.inputMode == ModeInsert && !a.hasPopup() {
			a.inputMode = ModeNormal
		}
		return nil
	}); err != nil {
		return err
	}

	// i: switch to insert mode (normal -> insert)
	// In insert mode, 'i' is forwarded via inputEditor.Edit() on "main" view.
	if err := a.gui.SetKeybinding("", km.FirstRune(ActionInsertMode), gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.fullScreen && a.inputMode == ModeNormal && !a.hasPopup() {
			a.inputMode = ModeInsert
		}
		return nil
	}); err != nil {
		return err
	}

	// r: resume or forward in full-screen
	if err := a.gui.SetKeybinding("", 'r', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			return nil
		}
		if a.fullScreen {
			if a.inputMode == ModeInsert {
				a.forwardKey('r')
			}
			return nil
		}
		// r in preview mode: enter full-screen (same as Enter)
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

	// R: rename or forward in full-screen
	if err := a.gui.SetKeybinding("", 'R', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			return nil
		}
		if a.fullScreen {
			a.forwardKey('R')
			return nil
		}
		if a.mode != ModeMain || a.sessions == nil {
			return nil
		}
		// TODO: prompt for new name (requires input popup)
		// For now, append "-renamed" as placeholder
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

	// D: purge or forward in full-screen
	if err := a.gui.SetKeybinding("", 'D', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			return nil
		}
		if a.fullScreen {
			a.forwardKey('D')
			return nil
		}
		if a.mode != ModeMain || a.sessions == nil {
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

	// Full-screen insert mode: forward ALL unhandled keys via View.Editor.
	// No for-loop registration needed — the Editor catch-all handles any key
	// not matched by the keybindings above (KeyMap actions only).
	// This covers: printable ASCII, Unicode, Ctrl combos, F-keys, Home/End, etc.

	return nil
}
