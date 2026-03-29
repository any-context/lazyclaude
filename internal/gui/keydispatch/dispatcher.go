package keydispatch

import "github.com/KEMSHlM/lazyclaude/internal/gui/keyhandler"

// Dispatcher routes key events through a priority chain:
//  1. Popup (highest priority, consumes ALL keys)
//  2. FullScreen special keys
//  3. Active Panel
//  4. Global (q, Ctrl+C, Tab, Shift+Tab, p)
type Dispatcher struct {
	popup      *keyhandler.PopupHandler
	fullscreen *keyhandler.FullScreenHandler
	panels     *keyhandler.PanelManager
	global     *keyhandler.GlobalHandler
}

// New creates a Dispatcher with the given PanelManager.
func New(pm *keyhandler.PanelManager) *Dispatcher {
	return &Dispatcher{
		popup:      &keyhandler.PopupHandler{},
		fullscreen: &keyhandler.FullScreenHandler{},
		panels:     pm,
		global:     keyhandler.NewGlobalHandler(pm),
	}
}

// Dispatch routes a key event through the priority chain.
func (d *Dispatcher) Dispatch(ev keyhandler.KeyEvent, actions keyhandler.AppActions) keyhandler.HandlerResult {
	// 1. Popup — highest priority, consumes ALL keys
	if r := d.popup.HandleKey(ev, actions); r == keyhandler.Handled {
		return keyhandler.Handled
	}

	// 2. FullScreen special keys
	if r := d.fullscreen.HandleKey(ev, actions); r == keyhandler.Handled {
		return keyhandler.Handled
	}

	// 3. Active panel (only in main mode, not fullscreen)
	if !actions.IsFullScreen() && actions.Mode() == 0 {
		panel := d.panels.ActivePanel()
		if panel != nil {
			if r := panel.HandleKey(ev, actions); r == keyhandler.Handled {
				return keyhandler.Handled
			}
		}
	}

	// 4. Global keys
	if r := d.global.HandleKey(ev, actions); r == keyhandler.Handled {
		return keyhandler.Handled
	}

	return keyhandler.Unhandled
}

// ActiveOptionsBar returns the options bar text for the current focus.
func (d *Dispatcher) ActiveOptionsBar(actions keyhandler.AppActions) string {
	if actions.HasPopup() || actions.IsFullScreen() {
		return ""
	}
	panel := d.panels.ActivePanel()
	if panel == nil {
		return ""
	}
	return panel.OptionsBarForTab(actions.ActivePanelTabIndex())
}

// PanelManager returns the underlying PanelManager.
func (d *Dispatcher) PanelManager() *keyhandler.PanelManager {
	return d.panels
}
