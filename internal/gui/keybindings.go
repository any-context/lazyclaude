package gui

import (
	"fmt"
	"strings"

	"github.com/KEMSHlM/lazyclaude/internal/gui/keyhandler"
	"github.com/jesseduffield/gocui"
)

// dispatchRune creates a gocui handler that dispatches a rune key through the Dispatcher.
func (a *App) dispatchRune(ch rune) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		ev := keyhandler.KeyEvent{Rune: ch}
		a.dispatcher.Dispatch(ev, a)
		if a.quitRequested {
			a.quitRequested = false
			return gocui.ErrQuit
		}
		return nil
	}
}

// dispatchKey creates a gocui handler that dispatches a special key through the Dispatcher.
func (a *App) dispatchKey(key gocui.Key) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		ev := keyhandler.KeyEvent{Key: key}
		a.dispatcher.Dispatch(ev, a)
		if a.quitRequested {
			a.quitRequested = false
			return gocui.ErrQuit
		}
		return nil
	}
}

// setupGlobalKeybindings registers physical keys and delegates to the Dispatcher.
func (a *App) setupGlobalKeybindings() error {
	// 1. Rune keys dispatched through the chain
	runes := []rune{'j', 'k', 'n', 'd', 'r', 'R', 'D', 'q', 'p', 'y', 'a', 'Y', 'g', 'G', 'v', '1', '2', '3'}
	for _, ch := range runes {
		if err := a.gui.SetKeybinding("", ch, gocui.ModNone, a.dispatchRune(ch)); err != nil {
			return err
		}
	}

	// 2. Special keys dispatched through the chain
	specials := []gocui.Key{
		gocui.KeyEnter, gocui.KeyEsc, gocui.KeyCtrlC, gocui.KeyCtrlD,
		gocui.KeyCtrlBackslash, gocui.KeyTab, gocui.KeyBacktab,
		gocui.KeyArrowUp, gocui.KeyArrowDown,
	}
	for _, key := range specials {
		if err := a.gui.SetKeybinding("", key, gocui.ModNone, a.dispatchKey(key)); err != nil {
			return err
		}
	}



	// 3. Popup view bindings (gocui may skip global bindings when popup has focus)
	popupRunes := []rune{'j', 'k', 'y', 'a', 'n', 'Y', '1', '2', '3'}
	for _, ch := range popupRunes {
		if err := a.gui.SetKeybinding(popupViewName, ch, gocui.ModNone, a.dispatchRune(ch)); err != nil {
			return err
		}
	}
	popupSpecials := []gocui.Key{gocui.KeyArrowUp, gocui.KeyArrowDown, gocui.KeyEsc}
	for _, key := range popupSpecials {
		if err := a.gui.SetKeybinding(popupViewName, key, gocui.ModNone, a.dispatchKey(key)); err != nil {
			return err
		}
	}

	// 4. Mouse scroll (not dispatched — simple inline handlers)
	if err := a.gui.SetKeybinding("", gocui.MouseWheelUp, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.fullscreen.IsActive() && a.fullscreen.ScrollY() > 0 {
			a.fullscreen.ScrollUp()
		}
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("", gocui.MouseWheelDown, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.fullscreen.IsActive() {
			a.fullscreen.ScrollDown()
		}
		return nil
	}); err != nil {
		return err
	}

	// 5. Rename input (view-specific, outside dispatcher)
	if err := a.gui.SetKeybinding("rename-input", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		newName := strings.TrimSpace(v.TextArea.GetContent())
		if newName != "" && a.renameSessionID != "" {
			if err := a.sessions.Rename(a.renameSessionID, newName); err != nil {
				a.setStatus(g, fmt.Sprintf("Error: %v", err))
			} else {
				a.setStatus(g, "Renamed to "+newName)
			}
		}
		a.closeRenameInput(g)
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("rename-input", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.closeRenameInput(g)
		return nil
	}); err != nil {
		return err
	}

	return nil
}
