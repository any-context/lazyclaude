package keyhandler

import (
	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/jesseduffield/gocui"
)

// SessionsPanel handles keys for the sessions list (upper-left).
type SessionsPanel struct{}

func (p *SessionsPanel) Name() string  { return "sessions" }
func (p *SessionsPanel) Label() string { return "Sessions" }

func (p *SessionsPanel) HandleKey(ev KeyEvent, actions AppActions) HandlerResult {
	switch {
	case ev.Rune == 'j' || ev.Key == gocui.KeyArrowDown:
		actions.MoveCursorDown()
		return Handled
	case ev.Rune == 'k' || ev.Key == gocui.KeyArrowUp:
		actions.MoveCursorUp()
		return Handled
	case ev.Rune == 'n':
		actions.CreateSession()
		return Handled
	case ev.Rune == 'd':
		actions.DeleteSession()
		return Handled
	case ev.Rune == 'a':
		actions.AttachSession()
		return Handled
	case ev.Rune == 'g':
		actions.LaunchLazygit()
		return Handled
	case ev.Key == gocui.KeyEnter:
		if actions.CursorIsProject() {
			actions.ToggleProjectExpanded()
		} else {
			actions.EnterFullScreen()
		}
		return Handled
	case ev.Rune == 'r':
		actions.EnterFullScreen()
		return Handled
	case ev.Rune == 'R':
		actions.StartRename()
		return Handled
	case ev.Rune == 'w':
		actions.StartWorktreeInput()
		return Handled
	case ev.Rune == 'W':
		actions.SelectWorktree()
		return Handled
	case ev.Rune == 'D':
		actions.PurgeOrphans()
		return Handled
	case ev.Rune == 'P':
		actions.StartPMSession()
		return Handled
	case ev.Rune == '1' || ev.Rune == '2' || ev.Rune == '3':
		actions.SendKeyToPane(string(ev.Rune))
		return Handled
	}
	return Unhandled
}

func (p *SessionsPanel) OptionsBarForTab(_ int) string {
	return " " +
		presentation.StyledKey("n", "new") + "  " +
		presentation.StyledKey("d", "del") + "  " +
		presentation.StyledKey("enter", "full") + "  " +
		presentation.StyledKey("a", "attach") + "  " +
		presentation.StyledKey("g", "lazygit") + "  " +
		presentation.StyledKey("1/2/3", "send") + "  " +
		presentation.StyledKey("R", "rename") + "  " +
		presentation.StyledKey("w", "worktree") + "  " +
		presentation.StyledKey("W", "select") + "  " +
		presentation.StyledKey("P", "pm") + "  " +
		presentation.StyledKey("q", "quit")
}

func (p *SessionsPanel) TabCount() int       { return 1 }
func (p *SessionsPanel) TabLabels() []string { return []string{"Sessions"} }
