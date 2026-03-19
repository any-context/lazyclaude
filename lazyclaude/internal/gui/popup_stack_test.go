package gui

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/notify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeNotif(tool, window string) *notify.ToolNotification {
	return &notify.ToolNotification{ToolName: tool, Window: window}
}

func newTestApp() *App {
	return &App{popups: NewPopupController(nil)}
}

func TestPopupStack_PushAndCount(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	assert.Equal(t, 0, app.popupCount())
	assert.False(t, app.hasPopup())

	app.pushPopup(makeNotif("Bash", "@0"))
	assert.Equal(t, 1, app.popupCount())
	assert.True(t, app.hasPopup())

	app.pushPopup(makeNotif("Write", "@1"))
	assert.Equal(t, 2, app.popupCount())
}

func TestPopupStack_ActivePopup(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.pushPopup(makeNotif("Bash", "@0"))
	app.pushPopup(makeNotif("Write", "@1"))

	active := app.activePopup()
	require.NotNil(t, active)
	assert.Equal(t, "Write", active.ToolName)
}

func TestPopupStack_DismissRemovesActive(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.pushPopup(makeNotif("Bash", "@0"))
	app.pushPopup(makeNotif("Write", "@1"))

	app.dismissActivePopup()
	assert.Equal(t, 1, app.popupCount())
	assert.Equal(t, "Bash", app.activePopup().ToolName)
}

func TestPopupStack_FocusCycle(t *testing.T) {
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

func TestPopupStack_SuspendAll_HasPopupFalse(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.pushPopup(makeNotif("Bash", "@0"))
	app.suspendAllPopups()

	assert.False(t, app.hasPopup())
	assert.Equal(t, 1, app.popupCount())
}

func TestPopupStack_Unsuspend(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.pushPopup(makeNotif("Bash", "@0"))
	app.suspendAllPopups()

	app.unsuspendAll()
	assert.True(t, app.hasPopup())
	assert.Equal(t, "Bash", app.activePopup().ToolName)
}

func TestPopupStack_DismissOnEmpty(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.dismissActivePopup()
	assert.Equal(t, 0, app.popupCount())
}

func TestPopupStack_CascadeOffset(t *testing.T) {
	t.Parallel()
	x0, y0 := 10, 5
	for i := 0; i < 3; i++ {
		cx, cy := popupCascadeOffset(x0, y0, i)
		assert.Equal(t, x0+i*2, cx)
		assert.Equal(t, y0+i, cy)
	}
}

func TestPopupStack_ActiveEntry(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.pushPopup(makeNotif("Bash", "@0"))

	entry := app.activeEntry()
	require.NotNil(t, entry)
	assert.Equal(t, 0, entry.scrollY)

	entry.scrollY = 5
	assert.Equal(t, 5, app.activeEntry().scrollY)
}
