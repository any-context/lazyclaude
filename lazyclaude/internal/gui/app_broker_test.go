package gui_test

import (
	"testing"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/event"
	"github.com/KEMSHlM/lazyclaude/internal/gui"
	"github.com/KEMSHlM/lazyclaude/internal/notify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApp_SetNotifyBroker_NilBrokerIsNoop verifies that calling SetNotifyBroker
// with nil does not panic and the app continues to work normally.
func TestApp_SetNotifyBroker_NilBrokerIsNoop(t *testing.T) {
	app, err := newHeadlessApp()
	require.NoError(t, err)
	defer app.Gui().Close()

	// Passing nil must not panic.
	assert.NotPanics(t, func() {
		app.SetNotifyBroker(nil)
	})
}

// TestApp_SetNotifyBroker_AcceptsBroker verifies that SetNotifyBroker stores the
// broker without error.
func TestApp_SetNotifyBroker_AcceptsBroker(t *testing.T) {
	app, err := newHeadlessApp()
	require.NoError(t, err)
	defer app.Gui().Close()

	broker := event.NewBroker[notify.Event]()
	defer broker.Close()

	assert.NotPanics(t, func() {
		app.SetNotifyBroker(broker)
	})
}

// TestApp_DrainBrokerForTest_ShowsPopup verifies that DrainBrokerForTest simulates
// a broker event being received and calls showToolPopup, resulting in a popup.
func TestApp_DrainBrokerForTest_ShowsPopup(t *testing.T) {
	app, err := newHeadlessApp()
	require.NoError(t, err)
	defer app.Gui().Close()

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running", TmuxWindow: "@0"},
		},
	}
	app.SetSessions(mock)

	broker := event.NewBroker[notify.Event]()
	defer broker.Close()
	app.SetNotifyBroker(broker)

	// Publish an event to the broker.
	n := &notify.ToolNotification{
		ToolName: "Bash",
		Input:    `{"command":"ls"}`,
		Window:   "@0",
	}
	broker.Publish(notify.Event{Notification: n})

	// DrainBrokerForTest simulates the select in the ticker goroutine receiving
	// from the broker and calling showToolPopup.
	require.Eventually(t, func() bool {
		app.DrainBrokerForTest()
		return app.HasPopupForTest()
	}, time.Second, 5*time.Millisecond)
}

// TestApp_DrainBrokerForTest_NoBroker_IsNoop verifies that DrainBrokerForTest
// is safe to call when no broker is set (nil).
func TestApp_DrainBrokerForTest_NoBroker_IsNoop(t *testing.T) {
	app, err := newHeadlessApp()
	require.NoError(t, err)
	defer app.Gui().Close()

	// No broker set; must not panic.
	assert.NotPanics(t, func() {
		app.DrainBrokerForTest()
	})
	assert.False(t, app.HasPopupForTest())
}

// TestApp_BrokerEvent_PopupMode_Overlay verifies that broker events show popups
// regardless of popup mode (overlay mode).
func TestApp_BrokerEvent_PopupMode_Overlay(t *testing.T) {
	app, err := newHeadlessApp()
	require.NoError(t, err)
	defer app.Gui().Close()

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Status: "Running", TmuxWindow: "@0"},
		},
	}
	app.SetSessions(mock)

	broker := event.NewBroker[notify.Event]()
	defer broker.Close()
	app.SetNotifyBroker(broker)

	broker.Publish(notify.Event{Notification: &notify.ToolNotification{
		ToolName: "Write",
		Window:   "@0",
	}})

	require.Eventually(t, func() bool {
		app.DrainBrokerForTest()
		return app.HasPopupForTest()
	}, time.Second, 5*time.Millisecond)
}

// newHeadlessApp is a helper that creates a headless App for broker tests.
func newHeadlessApp() (*gui.App, error) {
	return gui.NewAppHeadless(gui.ModeMain, 80, 24)
}
