package gui

import (
	"github.com/KEMSHlM/lazyclaude/internal/notify"
	"github.com/jesseduffield/gocui"
)

// TestLayout exposes layout for testing. Not for production use.
func (a *App) TestLayout(g *gocui.Gui) error {
	return a.layout(g)
}

// ShowToolPopupForTest exposes showToolPopup for testing.
func (a *App) ShowToolPopupForTest(n *notify.ToolNotification) {
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
	return a.state.IsFullScreen()
}

// StateForTest returns the current AppState.
func (a *App) StateForTest() AppState {
	return a.state
}

// SetStateForTest sets the AppState for testing.
func (a *App) SetStateForTest(s AppState) {
	a.transition(s)
}

// ForwardKeyForTest simulates forwarding a key in full-screen mode.
// Drains the key queue synchronously so the test can assert immediately.
func (a *App) ForwardKeyForTest(ch rune) {
	a.forwardKey(ch)
	a.drainKeyQueue()
}

// ForwardSpecialKeyForTest simulates forwarding a special key in full-screen mode.
func (a *App) ForwardSpecialKeyForTest(tmuxKey string) {
	a.forwardSpecialKey(tmuxKey)
	a.drainKeyQueue()
}

// drainKeyQueue processes all pending keys synchronously (for testing).
func (a *App) drainKeyQueue() {
	for {
		select {
		case cmd := <-a.keyQueue:
			if a.inputForwarder != nil {
				a.inputForwarder.ForwardKey(cmd.target, cmd.key)
			}
		default:
			return
		}
	}
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
	if a.notifyBrokerSub == nil {
		return
	}
	for {
		select {
		case ev, ok := <-a.notifyBrokerSub.Ch():
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
