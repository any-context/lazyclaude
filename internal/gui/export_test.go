package gui

import (
	"github.com/KEMSHlM/lazyclaude/internal/core/model"
	"github.com/jesseduffield/gocui"
)

// TestLayout exposes layout for testing. Not for production use.
func (a *App) TestLayout(g *gocui.Gui) error {
	return a.layout(g)
}

// ShowToolPopupForTest exposes showToolPopup for testing.
func (a *App) ShowToolPopupForTest(n *model.ToolNotification) {
	a.showToolPopup(n)
}

// DismissPopupForTest exposes dismissPopup for testing.
func (a *App) DismissPopupForTest(choice Choice) {
	a.dismissPopup(choice)
}

// HasPopupForTest exposes hasPopup for testing.
func (a *App) HasPopupForTest() bool {
	return a.hasPopup()
}

// CursorForTest returns the current cursor position for testing.
func (a *App) CursorForTest() int {
	return a.cursor
}

// EnterFullScreenForTest enters full-screen mode for testing.
func (a *App) EnterFullScreenForTest(sessionID string) {
	a.enterFullScreen(sessionID)
}

// ExitFullScreenForTest exits full-screen mode for testing.
func (a *App) ExitFullScreenForTest() {
	a.exitFullScreen()
}

// IsFullScreenForTest returns full-screen state for testing.
func (a *App) IsFullScreenForTest() bool {
	return a.fullscreen.IsActive()
}

// StateForTest returns the current AppState.
func (a *App) StateForTest() AppState {
	if a.fullscreen.IsActive() {
		return StateFullScreen
	}
	return StateMain
}

// SetStateForTest sets the AppState for testing.
func (a *App) SetStateForTest(s AppState) {
	if s == StateFullScreen {
		a.fullscreen.Enter("test-session")
	} else {
		a.fullscreen.Exit()
	}
}

// ForwardKeyForTest simulates forwarding a key in full-screen mode.
// Drains the key queue synchronously so the test can assert immediately.
func (a *App) ForwardKeyForTest(ch rune) {
	a.forwardKey(ch)
	a.fullscreen.DrainQueue()
}

// ForwardSpecialKeyForTest simulates forwarding a special key in full-screen mode.
func (a *App) ForwardSpecialKeyForTest(tmuxKey string) {
	a.forwardSpecialKey(tmuxKey)
	a.fullscreen.DrainQueue()
}

// PollNotificationForTest simulates what the ticker does: check for pending
// notifications and show popup. For testing without running the event loop.
func (a *App) PollNotificationForTest() {
	if a.sessions != nil {
		for _, n := range a.sessions.PendingNotifications() {
			a.showToolPopup(n)
		}
	}
}

// DrainBrokerForTest drains any pending events from the notify broker subscription
// and calls showToolPopup for each one. Simulates what the ticker goroutine does
// when the broker channel has events, without needing to run the event loop.
func (a *App) DrainBrokerForTest() {
	ch := a.notify.BrokerCh()
	if ch == nil {
		return
	}
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if ev.Notification != nil {
				a.showToolPopup(ev.Notification)
			}
		default:
			return
		}
	}
}
