package gui

import (
	"testing"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeNotif(tool, window string) *model.ToolNotification {
	return &model.ToolNotification{ToolName: tool, Window: window}
}

func newTestApp() *App {
	return &App{
		popups:         NewPopupController(),
		windowActivity: make(map[string]WindowActivityEntry),
	}
}

// --- App-level popup tests ---

func TestApp_HasPopup(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	assert.False(t, app.hasPopup())

	app.showToolPopup(makeNotif("Bash", "lc-test"))
	assert.True(t, app.hasPopup())
}

func TestApp_DismissPopup_RemovesFocusedOnly(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.showToolPopup(&model.ToolNotification{ToolName: "Bash", Window: "lc-1", Timestamp: time.Now()})
	app.showToolPopup(&model.ToolNotification{ToolName: "Write", Window: "lc-2", Timestamp: time.Now()})

	app.dismissPopup(ChoiceAccept)
	assert.True(t, app.hasPopup())
	assert.Equal(t, 1, app.popupCount())
	assert.Equal(t, "Bash", app.activePopup().ToolName)

	app.dismissPopup(ChoiceReject)
	assert.False(t, app.hasPopup())
}

func TestApp_DismissPopup_SetsActivityRunning(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.showToolPopup(&model.ToolNotification{ToolName: "Bash", Window: "lc-1", Timestamp: time.Now()})
	app.setWindowActivity("lc-1", WindowActivityEntry{State: model.ActivityNeedsInput})

	app.dismissPopup(ChoiceAccept)
	entry := app.windowActivity["lc-1"]
	assert.Equal(t, model.ActivityRunning, entry.State)
}

func TestApp_DismissAllPopups_SetsActivityRunning(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.showToolPopup(&model.ToolNotification{ToolName: "Bash", Window: "lc-1", Timestamp: time.Now()})
	app.showToolPopup(&model.ToolNotification{ToolName: "Write", Window: "lc-2", Timestamp: time.Now()})
	app.setWindowActivity("lc-1", WindowActivityEntry{State: model.ActivityNeedsInput})
	app.setWindowActivity("lc-2", WindowActivityEntry{State: model.ActivityNeedsInput})

	app.dismissAllPopups(ChoiceAccept)
	assert.Equal(t, model.ActivityRunning, app.windowActivity["lc-1"].State)
	assert.Equal(t, model.ActivityRunning, app.windowActivity["lc-2"].State)
}

func TestApp_DismissPopup_NopWhenNoPopup(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.dismissPopup(ChoiceCancel)
	assert.False(t, app.hasPopup())
}

func TestApp_ShowToolPopup_SetsFields(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	n := &model.ToolNotification{ToolName: "Edit", Input: `{"file_path":"/tmp/test.go"}`, CWD: "/home/user", Window: "lc-abc"}
	app.showToolPopup(n)

	active := app.activePopup()
	assert.Equal(t, "Edit", active.ToolName)
	assert.Equal(t, "lc-abc", active.Window)
}

// --- Popup stack delegation tests ---

func TestPopup_PushAndCount(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	assert.Equal(t, 0, app.popupCount())

	app.pushPopup(makeNotif("Bash", "@0"))
	assert.Equal(t, 1, app.popupCount())
	assert.True(t, app.hasPopup())

	app.pushPopup(makeNotif("Write", "@1"))
	assert.Equal(t, 2, app.popupCount())
}

func TestPopup_ActivePopup(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.pushPopup(makeNotif("Bash", "@0"))
	app.pushPopup(makeNotif("Write", "@1"))

	require.NotNil(t, app.activePopup())
	assert.Equal(t, "Write", app.activePopup().ToolName)
}

func TestPopup_DismissRemovesActive(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.pushPopup(makeNotif("Bash", "@0"))
	app.pushPopup(makeNotif("Write", "@1"))

	app.dismissActivePopup()
	assert.Equal(t, 1, app.popupCount())
	assert.Equal(t, "Bash", app.activePopup().ToolName)
}

func TestPopup_FocusCycle(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.pushPopup(makeNotif("Bash", "@0"))
	app.pushPopup(makeNotif("Write", "@1"))
	app.pushPopup(makeNotif("Edit", "@2"))

	assert.Equal(t, "Edit", app.activePopup().ToolName)
	app.popupFocusPrev()
	assert.Equal(t, "Write", app.activePopup().ToolName)
	app.popupFocusPrev()
	assert.Equal(t, "Bash", app.activePopup().ToolName)
	app.popupFocusPrev()
	assert.Equal(t, "Edit", app.activePopup().ToolName)
	app.popupFocusNext()
	assert.Equal(t, "Bash", app.activePopup().ToolName)
}

func TestPopup_SuspendAll(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.pushPopup(makeNotif("Bash", "@0"))
	app.suspendAllPopups()

	assert.False(t, app.hasPopup())
	assert.Equal(t, 1, app.popupCount())
}

func TestPopup_Unsuspend(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.pushPopup(makeNotif("Bash", "@0"))
	app.suspendAllPopups()
	app.unsuspendAll()

	assert.True(t, app.hasPopup())
	assert.Equal(t, "Bash", app.activePopup().ToolName)
}

func TestPopup_DismissOnEmpty(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.dismissActivePopup()
	assert.Equal(t, 0, app.popupCount())
}

func TestPopup_CascadeOffset(t *testing.T) {
	t.Parallel()
	x0, y0 := 10, 5
	for i := 0; i < 3; i++ {
		cx, cy := popupCascadeOffset(x0, y0, i)
		assert.Equal(t, x0+i*2, cx)
		assert.Equal(t, y0+i, cy)
	}
}

func TestPopup_ActiveEntry(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.pushPopup(makeNotif("Bash", "@0"))

	entry := app.activeEntry()
	require.NotNil(t, entry)
	assert.Equal(t, 0, entry.popup.ScrollY())
	entry.popup.SetScrollY(5)
	assert.Equal(t, 5, app.activeEntry().popup.ScrollY())
}
