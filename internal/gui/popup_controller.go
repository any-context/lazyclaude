package gui

import (
	"github.com/KEMSHlM/lazyclaude/internal/core/model"
)

// PopupManager abstracts popup stack operations.
type PopupManager interface {
	HasVisible() bool
	Count() int
	VisibleCount() int
	PushPopup(p Popup)
	DismissActive(choice Choice) string
	DismissAll(choice Choice) []string
	ActivePopup() Popup
	ActiveEntry() *popupEntry
	ActiveNotification() *model.ToolNotification
	FocusNext()
	FocusPrev()
	FocusIndex() int
	SuspendAll()
	UnsuspendAll()
	Stack() []popupEntry
	VisibleIndexOf(stackIdx int) int
}

// PopupController manages the popup stack independently from App.
type PopupController struct {
	stack    []popupEntry
	focusIdx int
}

// popupEntry represents a single popup in the stack.
type popupEntry struct {
	popup     Popup
	suspended bool
}

// NewPopupController creates a popup controller.
func NewPopupController() *PopupController {
	return &PopupController{}
}

// PushPopup adds a Popup directly to the stack and focuses it.
func (pc *PopupController) PushPopup(p Popup) {
	pc.stack = append(pc.stack, popupEntry{popup: p})
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
// This is a backward-compatibility helper for code that still uses ToolNotification.
func (pc *PopupController) ActiveNotification() *model.ToolNotification {
	e := pc.ActiveEntry()
	if e == nil {
		return nil
	}
	return notificationFromPopup(e.popup)
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

// ActivePopup returns the focused Popup, or nil.
func (pc *PopupController) ActivePopup() Popup {
	e := pc.ActiveEntry()
	if e == nil {
		return nil
	}
	return e.popup
}

// DismissActive removes the focused popup from the stack.
// Returns the window ID so the caller can send the choice.
func (pc *PopupController) DismissActive(choice Choice) string {
	if len(pc.stack) == 0 || pc.focusIdx < 0 || pc.focusIdx >= len(pc.stack) {
		return ""
	}
	window := pc.stack[pc.focusIdx].popup.Window()
	pc.stack = append(pc.stack[:pc.focusIdx], pc.stack[pc.focusIdx+1:]...)
	if pc.focusIdx >= len(pc.stack) {
		pc.focusIdx = len(pc.stack) - 1
	}
	if len(pc.stack) > 0 && pc.focusIdx >= 0 && pc.stack[pc.focusIdx].suspended {
		pc.FocusNext()
	}
	return window
}

// DismissAll removes all popups from the stack.
// Returns the windows so the caller can send choices.
func (pc *PopupController) DismissAll(choice Choice) []string {
	entries := make([]popupEntry, len(pc.stack))
	copy(entries, pc.stack)
	pc.stack = nil
	pc.focusIdx = 0
	windows := make([]string, len(entries))
	for i, e := range entries {
		windows[i] = e.popup.Window()
	}
	return windows
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

// notificationFromPopup extracts the ToolNotification from a Popup,
// returning nil if the Popup type does not wrap a ToolNotification.
func notificationFromPopup(p Popup) *model.ToolNotification {
	switch v := p.(type) {
	case *ToolPopup:
		return v.Notification()
	case *DiffPopup:
		return v.Notification()
	default:
		return nil
	}
}
