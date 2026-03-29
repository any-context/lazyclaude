package keyhandler

import (
	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/jesseduffield/gocui"
)

// LogsPanel handles keys for the logs view (lower-left).
type LogsPanel struct{}

func (p *LogsPanel) Name() string  { return "logs" }
func (p *LogsPanel) Label() string { return "Logs" }

func (p *LogsPanel) HandleKey(ev KeyEvent, actions AppActions) HandlerResult {
	switch {
	case ev.Rune == 'j' || ev.Key == gocui.KeyArrowDown:
		actions.LogsCursorDown()
		return Handled
	case ev.Rune == 'k' || ev.Key == gocui.KeyArrowUp:
		actions.LogsCursorUp()
		return Handled
	case ev.Rune == 'G':
		actions.LogsCursorToEnd()
		return Handled
	case ev.Rune == 'g':
		actions.LogsCursorToTop()
		return Handled
	case ev.Rune == 'v':
		actions.LogsToggleSelect()
		return Handled
	case ev.Rune == 'y':
		actions.LogsCopySelection()
		return Handled
	}
	return Unhandled
}

func (p *LogsPanel) OptionsBarForTab(_ int) string {
	return " " +
		presentation.StyledKey("j/k", "scroll") + "  " +
		presentation.StyledKey("v", "select") + "  " +
		presentation.StyledKey("y", "copy") + "  " +
		presentation.StyledKey("G", "end") + "  " +
		presentation.StyledKey("q", "quit")
}

func (p *LogsPanel) TabCount() int       { return 1 }
func (p *LogsPanel) TabLabels() []string { return []string{"Logs"} }
