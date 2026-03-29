package keyhandler

// Panel represents a focusable area in the TUI.
// Each panel manages its own key handling and options bar.
// Panels optionally support tabs (sub-content switching within a panel).
// Tab state is managed externally by App (panels are stateless).
type Panel interface {
	Name() string  // gocui view name ("sessions", "logs", "plugins")
	Label() string // display label
	HandleKey(ev KeyEvent, actions AppActions) HandlerResult

	// OptionsBarForTab returns the options bar text for the given tab index.
	// Single-tab panels ignore tabIdx and return a fixed bar.
	OptionsBarForTab(tabIdx int) string

	// Tab support. TabCount returns 1 for single-tab panels.
	TabCount() int
	TabLabels() []string
}

// PanelManager tracks focus across registered panels.
// Tab/Shift+Tab cycles focusIdx.
type PanelManager struct {
	panels   []Panel
	focusIdx int
}

// NewPanelManager creates a PanelManager with the given panels.
func NewPanelManager(panels ...Panel) *PanelManager {
	return &PanelManager{panels: panels}
}

// ActivePanel returns the currently focused panel.
func (pm *PanelManager) ActivePanel() Panel {
	if len(pm.panels) == 0 {
		return nil
	}
	return pm.panels[pm.focusIdx]
}

// FocusNext advances focus to the next panel (wrapping).
func (pm *PanelManager) FocusNext() {
	if len(pm.panels) == 0 {
		return
	}
	pm.focusIdx = (pm.focusIdx + 1) % len(pm.panels)
}

// FocusPrev moves focus to the previous panel (wrapping).
func (pm *PanelManager) FocusPrev() {
	if len(pm.panels) == 0 {
		return
	}
	pm.focusIdx = (pm.focusIdx - 1 + len(pm.panels)) % len(pm.panels)
}

// Panels returns all registered panels.
func (pm *PanelManager) Panels() []Panel {
	return pm.panels
}

// FocusIdx returns the current focus index.
func (pm *PanelManager) FocusIdx() int {
	return pm.focusIdx
}

// PanelCount returns the number of registered panels.
func (pm *PanelManager) PanelCount() int {
	return len(pm.panels)
}
