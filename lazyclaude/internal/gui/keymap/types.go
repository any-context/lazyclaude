package keymap

import "github.com/jesseduffield/gocui"

// AppState represents the current UI state of the application.
// Popup visibility is orthogonal to AppState (overlay, not a state).
type AppState int

const (
	StateMain       AppState = iota // session list + preview
	StateFullScreen                 // full-screen, all keys forwarded to Claude Code
)

// IsFullScreen returns true if the state is full-screen mode.
func (s AppState) IsFullScreen() bool {
	return s == StateFullScreen
}

// KeyAction identifies a logical action in the keymap.
type KeyAction string

const (
	ActionQuit          KeyAction = "quit"
	ActionEnterFull     KeyAction = "enter_fullscreen"
	ActionExitFull      KeyAction = "exit_fullscreen"
	ActionNormalMode    KeyAction = "normal_mode"  // retained for registry lookup (returns empty)
	ActionInsertMode    KeyAction = "insert_mode"  // retained for registry lookup (returns empty)
	ActionCursorUp      KeyAction = "cursor_up"
	ActionCursorDown    KeyAction = "cursor_down"
	ActionNewSession    KeyAction = "new_session"
	ActionDeleteSession KeyAction = "delete_session"
	ActionPopupAccept   KeyAction = "popup_accept"
	ActionPopupAllow    KeyAction = "popup_allow"
	ActionPopupReject   KeyAction = "popup_reject"
	ActionPopupCancel   KeyAction = "popup_cancel"
)

// KeyBinding maps a physical key to an action.
type KeyBinding struct {
	Key  gocui.Key
	Rune rune
	Mod  gocui.Modifier
}

// Matches returns true if the given key event matches this binding.
func (kb KeyBinding) Matches(key gocui.Key, ch rune, mod gocui.Modifier) bool {
	if mod != kb.Mod {
		return false
	}
	if kb.Rune != 0 {
		return ch == kb.Rune
	}
	return key == kb.Key
}
