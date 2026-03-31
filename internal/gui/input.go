package gui

import (
	"context"
	"sync"

	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/jesseduffield/gocui"
)

// --- Input forwarding interface & implementations ---

// InputForwarder sends keystrokes to a tmux pane.
type InputForwarder interface {
	// ForwardKey sends a tmux key name (e.g., "Enter", "Space").
	ForwardKey(target string, key string) error
	// ForwardLiteral sends text literally (not interpreted as key names).
	ForwardLiteral(target string, text string) error
	// ForwardPaste sends text as a bracketed paste to the target pane.
	ForwardPaste(target string, text string) error
}

// TmuxInputForwarder forwards keys via tmux send-keys.
type TmuxInputForwarder struct {
	client tmux.Client
}

// NewTmuxInputForwarder creates a forwarder backed by a tmux client.
func NewTmuxInputForwarder(client tmux.Client) *TmuxInputForwarder {
	return &TmuxInputForwarder{client: client}
}

func (f *TmuxInputForwarder) ForwardKey(target string, key string) error {
	return f.client.SendKeys(context.Background(), target, key)
}

func (f *TmuxInputForwarder) ForwardLiteral(target string, text string) error {
	return f.client.SendKeysLiteral(context.Background(), target, text)
}

func (f *TmuxInputForwarder) ForwardPaste(target string, text string) error {
	return f.client.PasteToPane(context.Background(), target, text)
}

// MockInputForwarder records forwarded keys for testing.
type MockInputForwarder struct {
	mu       sync.Mutex
	keys     []string
	literals []string // keys sent via ForwardLiteral
	pastes   []string // text sent via ForwardPaste
	target   string
}

func (f *MockInputForwarder) ForwardKey(target string, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.target = target
	f.keys = append(f.keys, key)
	return nil
}

func (f *MockInputForwarder) ForwardLiteral(target string, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.target = target
	f.keys = append(f.keys, text)
	f.literals = append(f.literals, text)
	return nil
}

func (f *MockInputForwarder) ForwardPaste(target string, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.target = target
	f.keys = append(f.keys, text)
	f.pastes = append(f.pastes, text)
	return nil
}

// Literals returns a copy of the literal-only keys sent via ForwardLiteral.
func (f *MockInputForwarder) Literals() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]string, len(f.literals))
	copy(result, f.literals)
	return result
}

// Pastes returns a copy of the paste texts sent via ForwardPaste.
func (f *MockInputForwarder) Pastes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]string, len(f.pastes))
	copy(result, f.pastes)
	return result
}

func (f *MockInputForwarder) Keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]string, len(f.keys))
	copy(result, f.keys)
	return result
}

func (f *MockInputForwarder) LastTarget() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.target
}

// RuneToLiteral converts a rune to a tmux send-keys compatible string.
func RuneToLiteral(ch rune) string {
	return string(ch)
}

// --- gocui Editor for full-screen key forwarding ---

// inputEditor implements gocui.Editor to forward all keys
// to the Claude Code pane in full-screen mode.
//
// Paste handling is done at the gocui layer (pollEvent / filterPasteEvents)
// which accumulates the entire paste text before it reaches the event loop.
// The inputEditor only handles individual key forwarding.
type inputEditor struct {
	app *App
}

// specialKeyMap maps gocui Key constants to tmux send-keys names.
var specialKeyMap = map[gocui.Key]string{
	gocui.KeySpace:      "Space",
	gocui.KeyTab:        "Tab",
	gocui.KeyBacktab:    "BTab",
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
	gocui.KeyCtrlE:      "C-e",
	gocui.KeyCtrlF:      "C-f",
	gocui.KeyCtrlG:      "C-g",
	gocui.KeyCtrlH:      "C-h",
	gocui.KeyCtrlJ:      "C-j",
	gocui.KeyCtrlK:      "C-k",
	gocui.KeyCtrlL:      "C-l",
	gocui.KeyCtrlN:      "C-n",
	gocui.KeyCtrlO:      "C-o",
	gocui.KeyCtrlP:      "C-p",
	gocui.KeyCtrlQ:      "C-q",
	gocui.KeyCtrlR:      "C-r",
	gocui.KeyCtrlS:      "C-s",
	gocui.KeyCtrlT:      "C-t",
	gocui.KeyCtrlU:      "C-u",
	gocui.KeyCtrlV:      "C-v",
	gocui.KeyCtrlW:      "C-w",
	gocui.KeyCtrlX:      "C-x",
	gocui.KeyCtrlY:      "C-y",
	gocui.KeyCtrlZ:      "C-z",
}

// Edit is called by gocui for every keypress when the view is Editable.
// In full-screen mode, keys are forwarded to the Claude Code pane.
// Paste is handled separately by gocui's OnPasteContent callback —
// individual characters from a paste never reach this method.
func (e *inputEditor) Edit(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
	if !e.app.fullscreen.IsActive() || e.app.hasPopup() {
		return false
	}
	if e.app.scroll.IsActive() {
		return false
	}
	return e.forwardAny(key, ch, mod)
}

func (e *inputEditor) forwardAny(key gocui.Key, ch rune, mod gocui.Modifier) bool {
	if key == gocui.KeyEnter && mod != 0 {
		e.app.forwardSpecialKey("Enter")
		return true
	}
	if ch != 0 {
		e.app.forwardKey(ch)
		return true
	}
	if key == gocui.KeyEsc {
		e.app.forwardSpecialKey("Escape")
		return true
	}
	// Ctrl+V is reserved for scroll mode entry — do not forward.
	if key == gocui.KeyCtrlV {
		return false
	}
	if tmuxKey, ok := specialKeyMap[key]; ok {
		e.app.forwardSpecialKey(tmuxKey)
		return true
	}
	return false
}
