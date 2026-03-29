package keyhandler

import (
	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/jesseduffield/gocui"
)

// PluginsPanel handles keys for the plugins view (middle-left).
// Stateless: all state (including tab index) is managed by App.
// Tab switching ([/]) is handled by GlobalHandler as a generic panel operation.
type PluginsPanel struct{}

func (p *PluginsPanel) Name() string  { return "plugins" }
func (p *PluginsPanel) Label() string { return "Plugins" }

func (p *PluginsPanel) HandleKey(ev KeyEvent, actions AppActions) HandlerResult {
	switch {
	case ev.Rune == 'j' || ev.Key == gocui.KeyArrowDown:
		actions.PluginCursorDown()
		return Handled
	case ev.Rune == 'k' || ev.Key == gocui.KeyArrowUp:
		actions.PluginCursorUp()
		return Handled
	case ev.Rune == 'i':
		actions.PluginInstall()
		return Handled
	case ev.Rune == 'd':
		actions.PluginUninstall()
		return Handled
	case ev.Rune == 'e':
		actions.PluginToggleEnabled()
		return Handled
	case ev.Rune == 'u':
		actions.PluginUpdate()
		return Handled
	case ev.Rune == 'r':
		actions.PluginRefresh()
		return Handled
	}
	return Unhandled
}

// OptionsBarForTab returns the options bar for the given tab.
// Tab 0 = Installed, Tab 1 = Marketplace.
func (p *PluginsPanel) OptionsBarForTab(tabIdx int) string {
	if tabIdx == 1 {
		return " " +
			presentation.StyledKey("i", "install") + "  " +
			presentation.StyledKey("r", "refresh") + "  " +
			presentation.StyledKey("[/]", "tab") + "  " +
			presentation.StyledKey("q", "quit")
	}
	return " " +
		presentation.StyledKey("e", "toggle") + "  " +
		presentation.StyledKey("d", "uninstall") + "  " +
		presentation.StyledKey("u", "update") + "  " +
		presentation.StyledKey("r", "refresh") + "  " +
		presentation.StyledKey("[/]", "tab") + "  " +
		presentation.StyledKey("q", "quit")
}

func (p *PluginsPanel) TabCount() int       { return 2 }
func (p *PluginsPanel) TabLabels() []string { return []string{"Installed", "Marketplace"} }
