package gui

import "github.com/KEMSHlM/lazyclaude/internal/gui/keymap"

// Type aliases for backward compatibility with existing gui callers.
type AppState = keymap.AppState
type KeyAction = keymap.KeyAction
type KeyBinding = keymap.KeyBinding
type ActionDef = keymap.ActionDef
type KeyRegistry = keymap.Registry

const (
	StateMain       = keymap.StateMain
	StateFullScreen = keymap.StateFullScreen

	ActionQuit          = keymap.ActionQuit
	ActionEnterFull     = keymap.ActionEnterFull
	ActionExitFull      = keymap.ActionExitFull
	ActionNormalMode    = keymap.ActionNormalMode
	ActionInsertMode    = keymap.ActionInsertMode
	ActionCursorUp      = keymap.ActionCursorUp
	ActionCursorDown    = keymap.ActionCursorDown
	ActionNewSession    = keymap.ActionNewSession
	ActionDeleteSession = keymap.ActionDeleteSession
	ActionPopupAccept   = keymap.ActionPopupAccept
	ActionPopupAllow    = keymap.ActionPopupAllow
	ActionPopupReject   = keymap.ActionPopupReject
	ActionPopupCancel   = keymap.ActionPopupCancel
)

func DefaultKeyRegistry() *KeyRegistry { return keymap.Default() }
