package gui

import "github.com/jesseduffield/gocui"

// KeyAction identifies a logical action in the keymap.
type KeyAction string

const (
	ActionQuit          KeyAction = "quit"
	ActionEnterFull     KeyAction = "enter_fullscreen"
	ActionExitFull      KeyAction = "exit_fullscreen"
	ActionNormalMode    KeyAction = "normal_mode"
	ActionInsertMode    KeyAction = "insert_mode"
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
