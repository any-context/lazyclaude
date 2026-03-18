package gui

import "github.com/jesseduffield/gocui"

// inputEditor implements gocui.Editor to forward unhandled keys
// to the Claude Code pane in full-screen insert mode.
// Only active when the "main" view has Editable=true.
//
// Key dispatch order:
//   1. gocui keybindings (KeyMap actions: q, j, k, n, d, etc.)
//   2. Editor.Edit() — this catch-all (only for unmatched keys)
//
// This eliminates the need to register individual keybindings for
// every printable ASCII, Ctrl combo, F-key, etc.
type inputEditor struct {
	app *App
}

// specialKeyMap maps gocui Key constants to tmux send-keys names.
var specialKeyMap = map[gocui.Key]string{
	gocui.KeyTab:        "Tab",
	gocui.KeyBackspace:  "BSpace",
	gocui.KeyBackspace2: "BSpace",
	gocui.KeyArrowUp:    "Up",
	gocui.KeyArrowDown:  "Down",
	gocui.KeyArrowLeft:  "Left",
	gocui.KeyArrowRight: "Right",
	gocui.KeyHome:       "Home",
	gocui.KeyEnd:        "End",
	gocui.KeyPgup:       "PageUp",
	gocui.KeyPgdn:       "PageDown",
	gocui.KeyDelete:     "DC",
	gocui.KeyInsert:     "IC",
	gocui.KeyF1:         "F1",
	gocui.KeyF2:         "F2",
	gocui.KeyF3:         "F3",
	gocui.KeyF4:         "F4",
	gocui.KeyF5:         "F5",
	gocui.KeyF6:         "F6",
	gocui.KeyF7:         "F7",
	gocui.KeyF8:         "F8",
	gocui.KeyF9:         "F9",
	gocui.KeyF10:        "F10",
	gocui.KeyF11:        "F11",
	gocui.KeyF12:        "F12",
	gocui.KeyCtrlA:      "C-a",
	gocui.KeyCtrlB:      "C-b",
	// Ctrl+C is reserved (hardcoded interrupt)
	// Ctrl+D is reserved (exit full-screen)
	gocui.KeyCtrlE: "C-e",
	gocui.KeyCtrlF: "C-f",
	gocui.KeyCtrlG: "C-g",
	gocui.KeyCtrlH: "C-h",
	// Ctrl+I = Tab (handled above)
	gocui.KeyCtrlJ: "C-j",
	gocui.KeyCtrlK: "C-k",
	gocui.KeyCtrlL: "C-l",
	// Ctrl+M = Enter (handled as KeyEnter)
	gocui.KeyCtrlN: "C-n",
	gocui.KeyCtrlO: "C-o",
	gocui.KeyCtrlP: "C-p",
	gocui.KeyCtrlQ: "C-q",
	gocui.KeyCtrlR: "C-r",
	gocui.KeyCtrlS: "C-s",
	gocui.KeyCtrlT: "C-t",
	gocui.KeyCtrlU: "C-u",
	gocui.KeyCtrlV: "C-v",
	gocui.KeyCtrlW: "C-w",
	gocui.KeyCtrlX: "C-x",
	gocui.KeyCtrlY: "C-y",
	gocui.KeyCtrlZ: "C-z",
}

// Edit is called by gocui for every keypress not handled by keybindings.
func (e *inputEditor) Edit(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
	if !e.app.fullScreen || e.app.inputMode != ModeInsert || e.app.hasPopup() {
		return false
	}

	// Printable rune (including Unicode)
	if ch != 0 {
		e.app.forwardKey(ch)
		return false // don't modify the view buffer
	}

	// Special key
	if tmuxKey, ok := specialKeyMap[key]; ok {
		e.app.forwardSpecialKey(tmuxKey)
		return false
	}

	return false
}
