package keyhandler

import (
	"github.com/KEMSHlM/lazyclaude/internal/gui/keymap"
	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
)

// SessionsPanel handles keys for the sessions list (upper-left).
type SessionsPanel struct {
	reg *keymap.Registry
}

// NewSessionsPanel creates a SessionsPanel with injected registry.
func NewSessionsPanel(reg *keymap.Registry) *SessionsPanel {
	return &SessionsPanel{reg: reg}
}

func (p *SessionsPanel) Name() string  { return "sessions" }
func (p *SessionsPanel) Label() string { return "Sessions" }

func (p *SessionsPanel) HandleKey(ev KeyEvent, actions AppActions) HandlerResult {
	def, ok := p.reg.Match(ev.Rune, ev.Key, ev.Mod, keymap.ScopeSession)
	if !ok {
		return Unhandled
	}

	switch def.Action {
	case keymap.ActionCursorDown:
		actions.MoveCursorDown()
	case keymap.ActionCursorUp:
		actions.MoveCursorUp()
	case keymap.ActionCollapseProject:
		actions.CollapseProject()
	case keymap.ActionExpandProject:
		actions.ExpandProject()
	case keymap.ActionNewSession:
		actions.CreateSession()
	case keymap.ActionNewSessionCWD:
		actions.CreateSessionAtCWD()
	case keymap.ActionDeleteSession:
		actions.DeleteSession()
	case keymap.ActionAttachSession:
		actions.AttachSession()
	case keymap.ActionLaunchLazygit:
		actions.LaunchLazygit()
	case keymap.ActionEnterFull:
		if actions.CursorIsProject() {
			actions.ToggleProjectExpanded()
		} else {
			actions.EnterFullScreen()
		}
	case keymap.ActionEnterFullR:
		actions.EnterFullScreen()
	case keymap.ActionStartRename:
		actions.StartRename()
	case keymap.ActionStartWorktree:
		actions.StartWorktreeInput()
	case keymap.ActionSelectWorktree:
		actions.SelectWorktree()
	case keymap.ActionPurgeOrphans:
		actions.PurgeOrphans()
	case keymap.ActionStartPMSession:
		actions.StartPMSession()
	case keymap.ActionSendKey1:
		actions.SendKeyToPane("1")
	case keymap.ActionSendKey2:
		actions.SendKeyToPane("2")
	case keymap.ActionSendKey3:
		actions.SendKeyToPane("3")
	case keymap.ActionStartSearch:
		actions.StartSearch()
	default:
		return Unhandled
	}
	return Handled
}

func (p *SessionsPanel) OptionsBarForTab(_ int) string {
	hints := p.reg.HintsForScope(keymap.ScopeSession)
	defs := make([]presentation.HintDef, 0, len(hints))
	for _, d := range hints {
		defs = append(defs, presentation.HintDef{
			Key:   d.HintKeyLabel(),
			Label: d.HintLabel,
		})
	}
	return presentation.BuildOptionsBar(defs)
}

func (p *SessionsPanel) TabCount() int       { return 1 }
func (p *SessionsPanel) TabLabels() []string { return []string{"Sessions"} }
