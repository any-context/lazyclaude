package gui

import (
	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/KEMSHlM/lazyclaude/internal/notify"
)

// ChoiceSender is called when a popup is dismissed to deliver the user's choice.
// Nil means no delivery (testing).
type ChoiceSender func(window string, choice Choice)

// PopupController manages the popup stack independently from App.
type PopupController struct {
	stack    []popupEntry
	focusIdx int
	sendFn   ChoiceSender
}

// popupEntry represents a single popup in the stack.
type popupEntry struct {
	notification *notify.ToolNotification
	scrollY      int
	diffCache    []string
	diffKinds    []presentation.DiffLineKind
	suspended    bool
}

// NewPopupController creates a popup controller with an optional choice sender.
func NewPopupController(sendFn ChoiceSender) *PopupController {
	return &PopupController{sendFn: sendFn}
}

// Push adds a notification to the popup stack and focuses it.
func (pc *PopupController) Push(n *notify.ToolNotification) {
	pc.stack = append(pc.stack, popupEntry{notification: n})
	pc.focusIdx = len(pc.stack) - 1
}

// Count returns total popups (including suspended).
func (pc *PopupController) Count() int {
	return len(pc.stack)
}

// VisibleCount returns non-suspended popup count.
func (pc *PopupController) VisibleCount() int {
	c := 0
	for _, e := range pc.stack {
		if !e.suspended {
			c++
		}
	}
	return c
}

// HasVisible returns true if any non-suspended popup exists.
func (pc *PopupController) HasVisible() bool {
	return pc.VisibleCount() > 0
}

// ActiveNotification returns the focused popup's notification, or nil.
func (pc *PopupController) ActiveNotification() *notify.ToolNotification {
	e := pc.ActiveEntry()
	if e == nil {
		return nil
	}
	return e.notification
}

// ActiveEntry returns a pointer to the focused popup entry, or nil.
func (pc *PopupController) ActiveEntry() *popupEntry {
	if len(pc.stack) == 0 || pc.focusIdx < 0 || pc.focusIdx >= len(pc.stack) {
		return nil
	}
	e := &pc.stack[pc.focusIdx]
	if e.suspended {
		return nil
	}
	return e
}

// DismissActive removes the focused popup and sends the choice.
func (pc *PopupController) DismissActive(choice Choice) {
	if len(pc.stack) == 0 || pc.focusIdx < 0 || pc.focusIdx >= len(pc.stack) {
		return
	}
	window := pc.stack[pc.focusIdx].notification.Window
	pc.stack = append(pc.stack[:pc.focusIdx], pc.stack[pc.focusIdx+1:]...)
	if pc.focusIdx >= len(pc.stack) {
		pc.focusIdx = len(pc.stack) - 1
	}
	if len(pc.stack) > 0 && pc.focusIdx >= 0 && pc.stack[pc.focusIdx].suspended {
		pc.FocusNext()
	}
	if pc.sendFn != nil {
		pc.sendFn(window, choice)
	}
}

// DismissAll sends the choice to all popups and clears the stack.
func (pc *PopupController) DismissAll(choice Choice) {
	entries := make([]popupEntry, len(pc.stack))
	copy(entries, pc.stack)
	pc.stack = nil
	pc.focusIdx = 0
	if pc.sendFn != nil {
		for _, e := range entries {
			pc.sendFn(e.notification.Window, choice)
		}
	}
}

// SuspendAll hides all popups without dismissing.
func (pc *PopupController) SuspendAll() {
	for i := range pc.stack {
		pc.stack[i].suspended = true
	}
}

// UnsuspendAll makes all suspended popups visible again.
func (pc *PopupController) UnsuspendAll() {
	for i := range pc.stack {
		pc.stack[i].suspended = false
	}
	if len(pc.stack) > 0 {
		pc.focusIdx = len(pc.stack) - 1
	}
}

// FocusNext moves focus to the next visible popup (wrapping).
func (pc *PopupController) FocusNext() {
	n := len(pc.stack)
	if n == 0 {
		return
	}
	for i := 0; i < n; i++ {
		next := (pc.focusIdx + 1 + i) % n
		if !pc.stack[next].suspended {
			pc.focusIdx = next
			return
		}
	}
}

// FocusPrev moves focus to the previous visible popup (wrapping).
func (pc *PopupController) FocusPrev() {
	n := len(pc.stack)
	if n == 0 {
		return
	}
	for i := 0; i < n; i++ {
		prev := (pc.focusIdx - 1 - i + n) % n
		if !pc.stack[prev].suspended {
			pc.focusIdx = prev
			return
		}
	}
}

// FocusIndex returns the current focus index.
func (pc *PopupController) FocusIndex() int {
	return pc.focusIdx
}

// Stack returns a read-only view of the popup stack.
func (pc *PopupController) Stack() []popupEntry {
	return pc.stack
}

// VisibleIndexOf returns the visible-only index for a stack index.
func (pc *PopupController) VisibleIndexOf(stackIdx int) int {
	idx := 0
	for i := 0; i < stackIdx && i < len(pc.stack); i++ {
		if !pc.stack[i].suspended {
			idx++
		}
	}
	return idx
}
