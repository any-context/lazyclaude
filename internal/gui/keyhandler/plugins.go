package keyhandler

import (
	"github.com/KEMSHlM/lazyclaude/internal/gui/keymap"
	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
)

// PluginsPanel handles keys for the plugins view (middle-left).
// Stateless: all state (including tab index) is managed by App.
// Tab switching ([/]) is handled by GlobalHandler as a generic panel operation.
type PluginsPanel struct {
	reg *keymap.Registry
}

// NewPluginsPanel creates a PluginsPanel with injected registry.
func NewPluginsPanel(reg *keymap.Registry) *PluginsPanel {
	return &PluginsPanel{reg: reg}
}

func (p *PluginsPanel) Name() string  { return "plugins" }
func (p *PluginsPanel) Label() string { return "Plugins" }

func (p *PluginsPanel) HandleKey(ev KeyEvent, actions AppActions) HandlerResult {
	tab := actions.ActivePanelTabIndex()
	def, ok := p.reg.MatchTab(ev.Rune, ev.Key, ev.Mod, keymap.ScopePlugins, tab)
	if !ok {
		return Unhandled
	}

	switch def.Action {
	case keymap.ActionMCPCursorDown:
		actions.MCPCursorDown()
	case keymap.ActionMCPCursorUp:
		actions.MCPCursorUp()
	case keymap.ActionMCPToggleDenied:
		actions.MCPToggleDenied()
	case keymap.ActionMCPRefresh:
		actions.MCPRefresh()
	case keymap.ActionPluginCursorDown:
		actions.PluginCursorDown()
	case keymap.ActionPluginCursorUp:
		actions.PluginCursorUp()
	case keymap.ActionPluginInstall:
		actions.PluginInstall()
	case keymap.ActionPluginUninstall:
		actions.PluginUninstall()
	case keymap.ActionPluginToggleEnabled:
		actions.PluginToggleEnabled()
	case keymap.ActionPluginUpdate:
		actions.PluginUpdate()
	case keymap.ActionPluginRefresh:
		actions.PluginRefresh()
	case keymap.ActionStartSearch:
		actions.StartSearch()
	default:
		return Unhandled
	}
	return Handled
}

// OptionsBarForTab returns the options bar for the given tab.
func (p *PluginsPanel) OptionsBarForTab(tabIdx int) string {
	hints := p.reg.HintsForScopeTab(keymap.ScopePlugins, tabIdx)
	defs := make([]presentation.HintDef, 0, len(hints))
	for _, d := range hints {
		defs = append(defs, presentation.HintDef{
			Key:   d.HintKeyLabel(),
			Label: d.HintLabel,
		})
	}
	return presentation.BuildOptionsBar(defs)
}

func (p *PluginsPanel) TabCount() int       { return len(keymap.PluginTabLabels()) }
func (p *PluginsPanel) TabLabels() []string { return keymap.PluginTabLabels() }
