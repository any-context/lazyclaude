package keyhandler

import (
	"github.com/KEMSHlM/lazyclaude/internal/gui/keymap"
	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
)

// LogsPanel handles keys for the logs view (lower-left).
type LogsPanel struct {
	reg *keymap.Registry
}

// NewLogsPanel creates a LogsPanel with injected registry.
func NewLogsPanel(reg *keymap.Registry) *LogsPanel {
	return &LogsPanel{reg: reg}
}

func (p *LogsPanel) Name() string  { return "logs" }
func (p *LogsPanel) Label() string { return "Logs" }

func (p *LogsPanel) HandleKey(ev KeyEvent, actions AppActions) HandlerResult {
	def, ok := p.reg.Match(ev.Rune, ev.Key, ev.Mod, keymap.ScopeLog)
	if !ok {
		return Unhandled
	}

	switch def.Action {
	case keymap.ActionLogsCursorDown:
		actions.LogsCursorDown()
	case keymap.ActionLogsCursorUp:
		actions.LogsCursorUp()
	case keymap.ActionLogsCursorToEnd:
		actions.LogsCursorToEnd()
	case keymap.ActionLogsCursorToTop:
		actions.LogsCursorToTop()
	case keymap.ActionLogsToggleSelect:
		actions.LogsToggleSelect()
	case keymap.ActionLogsCopySelection:
		actions.LogsCopySelection()
	case keymap.ActionStartSearch:
		actions.StartSearch()
	default:
		return Unhandled
	}
	return Handled
}

func (p *LogsPanel) OptionsBarForTab(_ int) string {
	hints := p.reg.HintsForScope(keymap.ScopeLog)
	defs := make([]presentation.HintDef, 0, len(hints))
	for _, d := range hints {
		defs = append(defs, presentation.HintDef{
			Key:   d.HintKeyLabel(),
			Label: d.HintLabel,
		})
	}
	return presentation.BuildOptionsBar(defs)
}

func (p *LogsPanel) TabCount() int       { return 1 }
func (p *LogsPanel) TabLabels() []string { return []string{"Logs"} }
