package gui

import (
	"context"
	"strings"
	"sync"
	"time"

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

// escTimeout is how long to wait after Esc before treating it as standalone.
// Paste markers (ESC[200~) arrive in a burst within the same event loop cycle,
// so 10ms is generous. A standalone Esc press has no following bytes.
const escTimeout = 10 * time.Millisecond

// pasteWatchdogTimeout is how long the watchdog waits after paste starts
// before flushing the buffer. This handles tcell event channel overflow
// (256 slots) where EventPaste{End} is blocked and the event loop deadlocks.
const pasteWatchdogTimeout = 200 * time.Millisecond

// pasteStartSuffix is "[200~" — the bytes after ESC in a paste start marker.
const pasteStartSuffix = "[200~"

// pasteEndSuffix is "[201~" — the bytes after ESC in a paste end marker.
const pasteEndSuffix = "[201~"

// editEvent stores a deferred key event.
type editEvent struct {
	key gocui.Key
	ch  rune
	mod gocui.Modifier
}

// inputEditor implements gocui.Editor to forward all keys
// to the Claude Code pane in full-screen mode.
// Detects bracketed paste markers (ESC[200~ / ESC[201~) in the event
// stream — works even when tcell fails to parse them (tmux popup bug).
//
// Fields guarded by pasteMu (inPaste, nativePaste, pasteBuf) are shared
// between the gocui event-loop goroutine and the paste watchdog goroutine.
// All other fields (escBuf, escTimer, escGen) are accessed exclusively on
// the gocui event-loop goroutine — the watchdog never touches them.
type inputEditor struct {
	app         *App
	escBuf      []editEvent     // event-loop only: buffered escape sequence detection
	inPaste     bool            // guarded by pasteMu: between paste start and end markers
	nativePaste bool            // guarded by pasteMu: paste detected via tcell IsPasting
	pasteBuf    strings.Builder // guarded by pasteMu: accumulated paste content
	pasteMu     sync.Mutex      // guards inPaste, nativePaste, pasteBuf
	escTimer    *time.Timer     // event-loop only: fires to flush standalone Esc
	escGen      uint64          // event-loop only: generation counter for escTimer
	pasteNotify chan struct{}    // signals watchdog that paste started
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
// Bracketed paste markers (ESC[200~ / ESC[201~) are detected even when
// tcell fails to parse them (tmux display-popup bug #4431). When paste
// markers arrive as individual events (Esc, '[', '2', '0', '0', '~'),
// they are detected via pattern matching and the paste text is forwarded
// atomically via tmux paste-buffer.
func (e *inputEditor) Edit(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
	if !e.app.fullscreen.IsActive() || e.app.hasPopup() {
		return false
	}

	// If tcell detected bracketed paste natively (IsPasting), use it.
	if e.app.gui.IsPasting {
		e.pasteMu.Lock()
		alreadyInPaste := e.inPaste
		if !alreadyInPaste {
			e.inPaste = true
			e.nativePaste = true
		}
		e.pasteMu.Unlock()
		if !alreadyInPaste {
			e.cancelEscTimer()
			e.escBuf = e.escBuf[:0]
			// Notify watchdog to schedule a flush in case the paste is large
			// enough to overflow tcell's event channel (256 slots).
			e.notifyWatchdog()
		}
	}

	// Inside a paste: buffer everything until end marker.
	e.pasteMu.Lock()
	pasting := e.inPaste
	e.pasteMu.Unlock()
	if pasting {
		return e.handlePaste(key, ch, mod)
	}

	// Accumulating an escape sequence: check for paste marker pattern.
	if len(e.escBuf) > 0 {
		return e.handleEscSeq(key, ch, mod)
	}

	// Esc might be the start of a paste marker — buffer it.
	if key == gocui.KeyEsc && mod == 0 {
		e.escBuf = append(e.escBuf[:0], editEvent{key: key, ch: ch, mod: mod})
		e.startEscTimer()
		return true
	}

	return e.forwardAny(key, ch, mod)
}

// handleEscSeq processes events while detecting a potential paste marker.
// Expects escBuf[0] to be Esc. Subsequent chars are matched against "[200~".
func (e *inputEditor) handleEscSeq(key gocui.Key, ch rune, mod gocui.Modifier) bool {
	e.cancelEscTimer()
	e.escBuf = append(e.escBuf, editEvent{key: key, ch: ch, mod: mod})

	// Build the string from escBuf[1:] to compare with pasteStartSuffix.
	var seq []rune
	for _, ev := range e.escBuf[1:] {
		if ev.ch != 0 {
			seq = append(seq, ev.ch)
		} else if ev.key == gocui.KeyEsc {
			seq = append(seq, '\x1b')
		} else {
			// Non-rune, non-Esc key breaks the pattern.
			e.flushEscBuf()
			return true
		}
	}

	prefix := string(seq)

	// Check if it's a complete paste start marker.
	if prefix == pasteStartSuffix {
		e.pasteMu.Lock()
		e.inPaste = true
		e.pasteMu.Unlock()
		e.escBuf = e.escBuf[:0]
		e.notifyWatchdog()
		return true
	}

	// Check if it's still a valid prefix of the paste start marker.
	if len(prefix) < len(pasteStartSuffix) && pasteStartSuffix[:len(prefix)] == prefix {
		// Keep buffering — might still become a paste marker.
		return true
	}

	// Not a paste marker — flush buffered events as normal input.
	e.flushEscBuf()
	return true
}

// handlePaste buffers characters during a bracketed paste.
// Detects the end marker ESC[201~ to flush.
func (e *inputEditor) handlePaste(key gocui.Key, ch rune, mod gocui.Modifier) bool {
	// Check for end marker start (Esc).
	if key == gocui.KeyEsc && mod == 0 {
		e.escBuf = append(e.escBuf[:0], editEvent{key: key, ch: ch, mod: mod})
		return true
	}

	// If we have a pending Esc inside paste, check for end marker.
	if len(e.escBuf) > 0 {
		e.escBuf = append(e.escBuf, editEvent{key: key, ch: ch, mod: mod})

		// Build suffix string — handle both runes and Esc keys.
		var seq []rune
		for _, ev := range e.escBuf[1:] {
			if ev.ch != 0 {
				seq = append(seq, ev.ch)
			} else if ev.key == gocui.KeyEsc {
				// Another Esc mid-sequence: the original Esc+partial is paste content.
				// Flush them and restart detection from this new Esc.
				e.pasteMu.Lock()
				for _, old := range e.escBuf[:len(e.escBuf)-1] {
					e.appendPasteChar(old.key, old.ch)
				}
				e.pasteMu.Unlock()
				e.escBuf = e.escBuf[:1]
				e.escBuf[0] = editEvent{key: key, ch: ch, mod: mod}
				return true
			} else {
				// Non-rune, non-Esc key: not an end marker.
				e.pasteMu.Lock()
				for _, ev := range e.escBuf {
					e.appendPasteChar(ev.key, ev.ch)
				}
				e.pasteMu.Unlock()
				e.escBuf = e.escBuf[:0]
				return true
			}
		}
		prefix := string(seq)

		// Complete end marker.
		if prefix == pasteEndSuffix {
			e.escBuf = e.escBuf[:0]
			e.flushPaste()
			return true
		}

		// Still a valid prefix of end marker.
		if len(prefix) < len(pasteEndSuffix) && pasteEndSuffix[:len(prefix)] == prefix {
			return true
		}

		// Not an end marker — the Esc and following chars are part of paste content.
		e.pasteMu.Lock()
		for _, ev := range e.escBuf {
			e.appendPasteChar(ev.key, ev.ch)
		}
		e.pasteMu.Unlock()
		e.escBuf = e.escBuf[:0]
		return true
	}

	// Regular paste character.
	e.pasteMu.Lock()
	e.appendPasteChar(key, ch)
	e.pasteMu.Unlock()
	return true
}

// appendPasteChar adds a single character to the paste buffer.
func (e *inputEditor) appendPasteChar(key gocui.Key, ch rune) {
	if ch != 0 {
		e.pasteBuf.WriteRune(ch)
	} else if key == gocui.KeyEnter {
		e.pasteBuf.WriteRune('\n')
	} else if key == gocui.KeySpace {
		e.pasteBuf.WriteRune(' ')
	} else if key == gocui.KeyTab {
		e.pasteBuf.WriteRune('\t')
	} else if key == gocui.KeyEsc {
		e.pasteBuf.WriteRune('\x1b')
	}
}

// flushPaste extracts the paste buffer and forwards it via tmux paste-buffer.
// Thread-safe: called from both the gocui event loop (when paste end marker
// is detected) and the watchdog goroutine (when tcell's event channel
// overflows and the event loop is deadlocked).
func (e *inputEditor) flushPaste() {
	e.pasteMu.Lock()
	text := e.pasteBuf.String()
	e.pasteBuf.Reset()
	e.inPaste = false
	e.nativePaste = false
	e.pasteMu.Unlock()
	if text == "" {
		return
	}
	e.app.forwardPaste(text)
}

// notifyWatchdog signals the paste watchdog goroutine (non-blocking).
func (e *inputEditor) notifyWatchdog() {
	if e.pasteNotify != nil {
		select {
		case e.pasteNotify <- struct{}{}:
		default:
		}
	}
}

// flushEscBuf forwards all buffered escape events as normal input.
func (e *inputEditor) flushEscBuf() {
	buf := make([]editEvent, len(e.escBuf))
	copy(buf, e.escBuf)
	e.escBuf = e.escBuf[:0]
	for _, ev := range buf {
		e.forwardAny(ev.key, ev.ch, ev.mod)
	}
}

// startEscTimer starts a timer that flushes the escape buffer
// if no more events arrive (standalone Esc press).
//
// Thread safety: the generation counter (escGen) prevents stale timer
// callbacks from flushing a newly-started escape buffer. When a new Esc
// arrives, escGen is incremented and the old timer's captured generation
// no longer matches, so its callback is a no-op. All inputEditor fields
// are accessed on the gocui event-loop goroutine; the timer callback
// uses gui.Update() to re-enter that goroutine before touching any state.
func (e *inputEditor) startEscTimer() {
	e.cancelEscTimer()
	e.escGen++
	gen := e.escGen
	e.escTimer = time.AfterFunc(escTimeout, func() {
		e.app.gui.Update(func(g *gocui.Gui) error {
			// Only flush if this timer's generation still matches.
			if gen == e.escGen && len(e.escBuf) > 0 {
				e.flushEscBuf()
			}
			return nil
		})
	})
}

// cancelEscTimer stops the pending escape timer if any.
func (e *inputEditor) cancelEscTimer() {
	if e.escTimer != nil {
		e.escTimer.Stop()
		e.escTimer = nil
	}
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
	if tmuxKey, ok := specialKeyMap[key]; ok {
		e.app.forwardSpecialKey(tmuxKey)
		return true
	}
	return false
}
