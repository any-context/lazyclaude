package gui

import "github.com/KEMSHlM/lazyclaude/internal/notify"

// Thin delegation from App to PopupController.
// These methods maintain backward compatibility with existing callers.

func (a *App) hasPopup() bool                          { return a.popups.HasVisible() }
func (a *App) popupCount() int                         { return a.popups.Count() }
func (a *App) visiblePopupCount() int                  { return a.popups.VisibleCount() }
func (a *App) activePopup() *notify.ToolNotification   { return a.popups.ActiveNotification() }
func (a *App) activeEntry() *popupEntry                { return a.popups.ActiveEntry() }
func (a *App) pushPopup(n *notify.ToolNotification)    { a.popups.Push(n) }
func (a *App) dismissActivePopup()                     { a.popups.DismissActive(ChoiceCancel) }
func (a *App) popupFocusNext()                         { a.popups.FocusNext() }
func (a *App) popupFocusPrev()                         { a.popups.FocusPrev() }
func (a *App) suspendAllPopups()                       { a.popups.SuspendAll() }
func (a *App) unsuspendAll()                           { a.popups.UnsuspendAll() }
func (a *App) visibleIndexOf(stackIdx int) int         { return a.popups.VisibleIndexOf(stackIdx) }

// popupCascadeOffset returns the top-left position for a cascaded popup.
func popupCascadeOffset(baseX, baseY, index int) (int, int) {
	return baseX + index*2, baseY + index
}
