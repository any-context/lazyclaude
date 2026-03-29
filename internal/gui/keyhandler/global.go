package keyhandler

import "github.com/jesseduffield/gocui"

// GlobalHandler handles keys that apply regardless of focused panel.
type GlobalHandler struct {
	panels *PanelManager
}

// NewGlobalHandler creates a GlobalHandler.
func NewGlobalHandler(pm *PanelManager) *GlobalHandler {
	return &GlobalHandler{panels: pm}
}

func (h *GlobalHandler) HandleKey(ev KeyEvent, actions AppActions) HandlerResult {
	// Ctrl+C: always quit
	if ev.Key == gocui.KeyCtrlC {
		actions.Quit()
		return Handled
	}

	// Skip global keys in non-main modes
	if actions.Mode() != 0 {
		// Esc quits in Diff/Tool mode
		if ev.Key == gocui.KeyEsc {
			actions.Quit()
			return Handled
		}
		return Unhandled
	}

	switch {
	case ev.Rune == 'q':
		actions.Quit()
		return Handled
	case ev.Key == gocui.KeyCtrlBackslash:
		actions.Quit()
		return Handled
	case ev.Key == gocui.KeyTab:
		h.panels.FocusNext()
		return Handled
	case ev.Key == gocui.KeyBacktab:
		h.panels.FocusPrev()
		return Handled
	case ev.Rune == 'p':
		actions.UnsuspendPopups()
		return Handled
	case ev.Rune == ']':
		actions.PanelNextTab()
		return Handled
	case ev.Rune == '[':
		actions.PanelPrevTab()
		return Handled
	}
	return Unhandled
}
