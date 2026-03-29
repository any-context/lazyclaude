// Copyright 2020 The gocui Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gocui

import (
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
)

const (
	// pasteAccumTimeout is the maximum time to wait for more paste content
	// before flushing a partial paste. Handles very large pastes that might
	// stall if we waited indefinitely.
	pasteAccumTimeout = 500 * time.Millisecond

	// escSeqTimeout is how long to wait after Esc to determine if it's the
	// start of a bracketed paste marker or a standalone Esc press.
	escSeqTimeout = 10 * time.Millisecond

	// pasteStartMarker is the suffix after ESC for bracketed paste start.
	pasteStartMarker = "[200~"

	// pasteEndMarker is the suffix after ESC for bracketed paste end.
	pasteEndMarker = "[201~"
)

// We probably don't want this being a global variable for YOLO for now
var Screen tcell.Screen

// oldStyle is a representation of how a cell would be styled when we were using termbox
type oldStyle struct {
	fg         Attribute
	bg         Attribute
	outputMode OutputMode
}

var runeReplacements = map[rune]string{
	'┌': "+",
	'┐': "+",
	'└': "+",
	'┘': "+",
	'╭': "+",
	'╮': "+",
	'╰': "+",
	'╯': "+",
	'─': "-",
	'═': "-",
	'║': "|",
	'╔': "+",
	'╗': "+",
	'╚': "+",
	'╝': "+",

	// using a hyphen here actually looks weird.
	// We see these characters when in portrait mode
	'╶': " ",
	'╴': " ",

	'┴': "+",
	'┬': "+",
	'╷': "|",
	'├': "+",
	'│': "|",
	'▼': "v",
	'►': ">",
	'▲': "^",
	'◄': "<",
}

// tcellInit initializes tcell screen for use.
func (g *Gui) tcellInit(runeReplacements map[rune]string) error {
	tcell.SetEncodingFallback(tcell.EncodingFallbackASCII)

	if s, e := tcell.NewScreen(); e != nil {
		return e
	} else if e = s.Init(); e != nil {
		return e
	} else {
		registerRuneFallbacks(s, runeReplacements)

		g.screen = s
		Screen = s
		return nil
	}
}

func registerRuneFallbacks(s tcell.Screen, additional map[rune]string) {
	for before, after := range runeReplacements {
		s.RegisterRuneFallback(before, after)
	}

	for before, after := range additional {
		s.RegisterRuneFallback(before, after)
	}
}

// tcellInitSimulation initializes tcell screen for use.
func (g *Gui) tcellInitSimulation(width int, height int) error {
	s := tcell.NewSimulationScreen("")
	if e := s.Init(); e != nil {
		return e
	} else {
		g.screen = s
		Screen = s
		// setting to a larger value than the typical terminal size
		// so that during a test we're more likely to see an item to select in a view.
		s.SetSize(width, height)
		s.Sync()
		return nil
	}
}

// tcellSetCell sets the character cell at a given location to the given
// content (grapheme cluster) and attributes using provided OutputMode
func tcellSetCell(x, y int, ch string, fg, bg Attribute, outputMode OutputMode) {
	st := getTcellStyle(oldStyle{fg: fg, bg: bg, outputMode: outputMode})
	Screen.Put(x, y, ch, st)
}

// getTcellStyle creates tcell.Style from Attributes
func getTcellStyle(input oldStyle) tcell.Style {
	st := tcell.StyleDefault

	// extract colors and attributes
	if input.fg != ColorDefault {
		st = st.Foreground(getTcellColor(input.fg, input.outputMode))
		st = setTcellFontEffectStyle(st, input.fg)
	}
	if input.bg != ColorDefault {
		st = st.Background(getTcellColor(input.bg, input.outputMode))
		st = setTcellFontEffectStyle(st, input.bg)
	}

	return st
}

// setTcellFontEffectStyle add additional attributes to tcell.Style
func setTcellFontEffectStyle(st tcell.Style, attr Attribute) tcell.Style {
	if attr&AttrBold != 0 {
		st = st.Bold(true)
	}
	if attr&AttrUnderline != 0 {
		st = st.Underline(true)
	}
	if attr&AttrReverse != 0 {
		st = st.Reverse(true)
	}
	if attr&AttrBlink != 0 {
		st = st.Blink(true)
	}
	if attr&AttrDim != 0 {
		st = st.Dim(true)
	}
	if attr&AttrItalic != 0 {
		st = st.Italic(true)
	}
	if attr&AttrStrikeThrough != 0 {
		st = st.StrikeThrough(true)
	}
	return st
}

// gocuiEventType represents the type of event.
type gocuiEventType uint8

// GocuiEvent represents events like a keys, mouse actions, or window resize.
//
//	The 'Mod', 'Key' and 'Ch' fields are valid if 'Type' is 'eventKey'.
//	The 'MouseX' and 'MouseY' fields are valid if 'Type' is 'eventMouse'.
//	The 'Width' and 'Height' fields are valid if 'Type' is 'eventResize'.
//	The 'Focused' field is valid if 'Type' is 'eventFocus'.
//	The 'Start' field is valid if 'Type' is 'eventPaste'. It is true for the
//	  beginning of a paste operation, false for the end.
//	The 'PasteText' field is valid if 'Type' is 'eventPasteContent'.
//	The 'Err' field is valid if 'Type' is 'eventError'.
type GocuiEvent struct {
	Type      gocuiEventType
	Mod       Modifier
	Key       Key
	Ch        rune
	Width     int
	Height    int
	Err       error
	MouseX    int
	MouseY    int
	Focused   bool
	Start     bool
	N         int
	PasteText string
}

// Event types.
const (
	eventNone gocuiEventType = iota
	eventKey
	eventResize
	eventMouse
	eventMouseMove // only used when no button is down, otherwise it's eventMouse
	eventFocus
	eventPaste
	eventPasteContent // accumulated paste text from bracketed paste
	eventInterrupt
	eventError
	eventRaw
)

const (
	NOT_DRAGGING int = iota
	MAYBE_DRAGGING
	DRAGGING
)

var (
	lastMouseKey tcell.ButtonMask = tcell.ButtonNone
	lastMouseMod tcell.ModMask    = tcell.ModNone
	dragState    int              = NOT_DRAGGING
	lastX        int              = 0
	lastY        int              = 0
)

// this wrapper struct has public keys so we can easily serialize/deserialize to JSON
type TcellKeyEventWrapper struct {
	Timestamp int64
	Mod       tcell.ModMask
	Key       tcell.Key
	Ch        rune
}

func NewTcellKeyEventWrapper(event *tcell.EventKey, timestamp int64) *TcellKeyEventWrapper {
	return &TcellKeyEventWrapper{
		Timestamp: timestamp,
		Mod:       event.Modifiers(),
		Key:       event.Key(),
		Ch:        event.Rune(),
	}
}

func (wrapper TcellKeyEventWrapper) toTcellEvent() tcell.Event {
	return tcell.NewEventKey(wrapper.Key, wrapper.Ch, wrapper.Mod)
}

type TcellMouseEventWrapper struct {
	Timestamp  int64
	X          int
	Y          int
	ButtonMask tcell.ButtonMask
	ModMask    tcell.ModMask
}

func NewTcellMouseEventWrapper(event *tcell.EventMouse, timestamp int64) *TcellMouseEventWrapper {
	x, y := event.Position()
	return &TcellMouseEventWrapper{
		Timestamp:  timestamp,
		X:          x,
		Y:          y,
		ButtonMask: event.Buttons(),
		ModMask:    event.Modifiers(),
	}
}

func (wrapper TcellMouseEventWrapper) toTcellEvent() tcell.Event {
	return tcell.NewEventMouse(wrapper.X, wrapper.Y, wrapper.ButtonMask, wrapper.ModMask)
}

type TcellResizeEventWrapper struct {
	Timestamp int64
	Width     int
	Height    int
}

func NewTcellResizeEventWrapper(event *tcell.EventResize, timestamp int64) *TcellResizeEventWrapper {
	w, h := event.Size()

	return &TcellResizeEventWrapper{
		Timestamp: timestamp,
		Width:     w,
		Height:    h,
	}
}

func (wrapper TcellResizeEventWrapper) toTcellEvent() tcell.Event {
	return tcell.NewEventResize(wrapper.Width, wrapper.Height)
}

// pollEvent get tcell.Event and transform it into gocuiEvent
func (g *Gui) pollEvent() GocuiEvent {
	var tev tcell.Event
	if g.playRecording {
		select {
		case ev := <-g.ReplayedEvents.Keys:
			tev = (ev).toTcellEvent()
		case ev := <-g.ReplayedEvents.Resizes:
			tev = (ev).toTcellEvent()
		case ev := <-g.ReplayedEvents.MouseEvents:
			tev = (ev).toTcellEvent()
		}
	} else {
		tev = Screen.PollEvent()
	}

	switch tev := tev.(type) {
	case *tcell.EventInterrupt:
		return GocuiEvent{Type: eventInterrupt}
	case *tcell.EventResize:
		w, h := tev.Size()
		return GocuiEvent{Type: eventResize, Width: w, Height: h}
	case *tcell.EventKey:
		k := tev.Key()
		ch := rune(0)
		if k == tcell.KeyRune {
			k = 0 // if rune remove key (so it can match rune instead of key)
			ch = tev.Rune()
			if ch == ' ' {
				// special handling for spacebar
				k = 32 // tcell keys ends at 31 or starts at 256
				ch = rune(0)
			}
		}
		mod := tev.Modifiers()
		// remove control modifier and setup special handling of ctrl+spacebar, etc.
		if mod == tcell.ModCtrl && k == 32 {
			mod = 0
			ch = rune(0)
			k = tcell.KeyCtrlSpace
		} else if mod == tcell.ModShift && k == tcell.KeyUp {
			mod = 0
			ch = rune(0)
			k = tcell.KeyF62
		} else if mod == tcell.ModShift && k == tcell.KeyDown {
			mod = 0
			ch = rune(0)
			k = tcell.KeyF63
		} else if mod == tcell.ModCtrl || mod == tcell.ModShift {
			// remove Ctrl or Shift if specified
			// - shift - will be translated to the final code of rune
			// - ctrl  - is translated in the key
			mod = 0
		} else if mod == tcell.ModAlt && k == tcell.KeyEnter {
			// for the sake of convenience I'm having a KeyAltEnter key. I will likely
			// regret this laziness in the future. We're arbitrarily mapping that to tcell's
			// KeyF64.
			mod = 0
			k = tcell.KeyF64
		}

		return GocuiEvent{
			Type: eventKey,
			Key:  Key(k),
			Ch:   ch,
			Mod:  Modifier(mod),
		}
	case *tcell.EventMouse:
		x, y := tev.Position()
		button := tev.Buttons()
		mouseKey := MouseRelease
		mouseMod := ModNone
		// process mouse wheel
		if button&tcell.WheelUp != 0 {
			mouseKey = MouseWheelUp
		}
		if button&tcell.WheelDown != 0 {
			mouseKey = MouseWheelDown
		}
		if button&tcell.WheelLeft != 0 {
			mouseKey = MouseWheelLeft
		}
		if button&tcell.WheelRight != 0 {
			mouseKey = MouseWheelRight
		}

		wheeling := mouseKey == MouseWheelUp || mouseKey == MouseWheelDown || mouseKey == MouseWheelLeft || mouseKey == MouseWheelRight

		// process button events (not wheel events)
		button &= tcell.ButtonMask(0xff)
		if button != tcell.ButtonNone && lastMouseKey == tcell.ButtonNone {
			lastMouseKey = button
			lastMouseMod = tev.Modifiers()
			switch button {
			case tcell.ButtonPrimary:
				mouseKey = MouseLeft
				dragState = MAYBE_DRAGGING
				lastX = x
				lastY = y
			case tcell.ButtonSecondary:
				mouseKey = MouseRight
			case tcell.ButtonMiddle:
				mouseKey = MouseMiddle
			default:
			}
		}

		switch tev.Buttons() {
		case tcell.ButtonNone:
			if lastMouseKey != tcell.ButtonNone {
				switch lastMouseKey {
				case tcell.ButtonPrimary:
					dragState = NOT_DRAGGING
				case tcell.ButtonSecondary:
				case tcell.ButtonMiddle:
				default:
				}
				mouseMod = Modifier(lastMouseMod)
				lastMouseMod = tcell.ModNone
				lastMouseKey = tcell.ButtonNone
			}
		default:
		}

		if !wheeling {
			switch dragState {
			case NOT_DRAGGING:
				return GocuiEvent{
					Type:   eventMouseMove,
					MouseX: x,
					MouseY: y,
				}
			// if we haven't released the left mouse button and we've moved the cursor then we're dragging
			case MAYBE_DRAGGING:
				if x != lastX || y != lastY {
					dragState = DRAGGING
				}
			case DRAGGING:
				mouseMod = ModMotion
				mouseKey = MouseLeft
			}
		}

		return GocuiEvent{
			Type:   eventMouse,
			MouseX: x,
			MouseY: y,
			Key:    mouseKey,
			Ch:     0,
			Mod:    mouseMod,
		}
	case *tcell.EventFocus:
		return GocuiEvent{
			Type:    eventFocus,
			Focused: tev.Focused,
		}
	case *tcell.EventPaste:
		if tev.Start() {
			// Accumulate all paste content in this goroutine before sending
			// to gEvents. This prevents the 20-slot gEvents channel from
			// overflowing on large pastes.
			text := g.accumulatePasteNative()
			if text != "" {
				return GocuiEvent{Type: eventPasteContent, PasteText: text}
			}
			return GocuiEvent{Type: eventNone}
		}
		// Stray paste-end without start: ignore.
		return GocuiEvent{Type: eventNone}
	default:
		return GocuiEvent{Type: eventNone}
	}
}

// accumulatePasteNative reads tcell events until EventPaste{End} and returns
// the accumulated text. Called from the pollEvent goroutine when
// EventPaste{Start} is received (tcell properly detected bracketed paste).
//
// Uses a goroutine to poll events asynchronously so the timeout is
// effective even when PollEvent is blocking.
func (g *Gui) accumulatePasteNative() string {
	var buf strings.Builder
	timer := time.NewTimer(pasteAccumTimeout)
	defer timer.Stop()

	// Channel for async PollEvent results.
	evCh := make(chan tcell.Event, 1)

	for {
		// Launch async poll.
		go func() {
			evCh <- Screen.PollEvent()
		}()

		select {
		case <-timer.C:
			// Timeout: flush what we have. The goroutine still blocked on
			// PollEvent will eventually return and re-inject the event.
			go func() {
				ev := <-evCh
				Screen.PostEventWait(ev)
			}()
			return buf.String()
		case <-g.stop:
			return buf.String()
		case tev := <-evCh:
			switch tev := tev.(type) {
			case *tcell.EventPaste:
				if !tev.Start() {
					return buf.String()
				}
			case *tcell.EventKey:
				appendTcellKeyToBuilder(&buf, tev)
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(pasteAccumTimeout)
			case *tcell.EventInterrupt:
				return buf.String()
			default:
				// Ignore resize, mouse, focus during paste.
			}
		}
	}
}

// accumulatePasteFromESC reads tcell EventKey events until ESC[201~ (paste end
// marker) and returns the accumulated text. Called from the pollEvent goroutine
// when ESC[200~ is detected as raw EventKey events (tmux display-popup bug
// where tcell doesn't receive EventPaste).
//
// Uses a goroutine to poll events asynchronously so the timeout is
// effective even when PollEvent is blocking.
func (g *Gui) accumulatePasteFromESC() string {
	var buf strings.Builder
	var escBuf []rune
	inEsc := false
	timer := time.NewTimer(pasteAccumTimeout)
	defer timer.Stop()

	evCh := make(chan tcell.Event, 1)

	for {
		go func() {
			evCh <- Screen.PollEvent()
		}()

		select {
		case <-timer.C:
			if inEsc {
				buf.WriteRune('\x1b')
				for _, r := range escBuf {
					buf.WriteRune(r)
				}
			}
			go func() {
				ev := <-evCh
				Screen.PostEventWait(ev)
			}()
			return buf.String()
		case <-g.stop:
			return buf.String()
		case tev := <-evCh:
			switch tev := tev.(type) {
			case *tcell.EventKey:
				k := tev.Key()
				ch := tev.Rune()

				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(pasteAccumTimeout)

				if k == tcell.KeyEscape {
					if inEsc {
						buf.WriteRune('\x1b')
						for _, r := range escBuf {
							buf.WriteRune(r)
						}
					}
					escBuf = escBuf[:0]
					inEsc = true
					continue
				}

				if inEsc {
					if k == tcell.KeyRune {
						escBuf = append(escBuf, ch)
						seq := string(escBuf)
						if seq == pasteEndMarker {
							return buf.String()
						}
						if len(seq) < len(pasteEndMarker) && pasteEndMarker[:len(seq)] == seq {
							continue
						}
						buf.WriteRune('\x1b')
						for _, r := range escBuf {
							buf.WriteRune(r)
						}
						escBuf = escBuf[:0]
						inEsc = false
					} else {
						buf.WriteRune('\x1b')
						for _, r := range escBuf {
							buf.WriteRune(r)
						}
						escBuf = escBuf[:0]
						inEsc = false
						appendTcellKeyToBuilder(&buf, tev)
					}
					continue
				}

				appendTcellKeyToBuilder(&buf, tev)

			case *tcell.EventPaste:
				if !tev.Start() {
					return buf.String()
				}
			case *tcell.EventInterrupt:
				return buf.String()
			default:
			}
		}
	}
}

// appendTcellKeyToBuilder appends the character representation of a tcell key
// event to a strings.Builder.
func appendTcellKeyToBuilder(buf *strings.Builder, ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyRune:
		buf.WriteRune(ev.Rune())
	case tcell.KeyEnter:
		buf.WriteRune('\n')
	case tcell.KeyTab:
		buf.WriteRune('\t')
	case tcell.KeyEscape:
		buf.WriteRune('\x1b')
	default:
		// Control keys, function keys, etc. inside paste are rare.
		// Best effort: if the key maps to a rune, include it.
		if ev.Rune() != 0 {
			buf.WriteRune(ev.Rune())
		}
	}
}
