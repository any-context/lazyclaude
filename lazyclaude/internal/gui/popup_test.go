package gui

import (
	"testing"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/notify"
	"github.com/stretchr/testify/assert"
)

func TestApp_HasPopup(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	assert.False(t, app.hasPopup())

	app.showToolPopup(&notify.ToolNotification{
		ToolName: "Bash",
		Window:   "lc-test",
	})
	assert.True(t, app.hasPopup())
}

func TestApp_DismissPopup_RemovesFocusedOnly(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	app.showToolPopup(&notify.ToolNotification{
		ToolName:  "Bash",
		Window:    "lc-1",
		Timestamp: time.Now(),
	})
	app.showToolPopup(&notify.ToolNotification{
		ToolName:  "Write",
		Window:    "lc-2",
		Timestamp: time.Now(),
	})

	// Dismiss focused (Write), Bash should remain
	app.dismissPopup(ChoiceAccept)
	assert.True(t, app.hasPopup())
	assert.Equal(t, 1, app.popupCount())
	assert.Equal(t, "Bash", app.activePopup().ToolName)

	// Dismiss remaining (Bash)
	app.dismissPopup(ChoiceReject)
	assert.False(t, app.hasPopup())
	assert.Equal(t, 0, app.popupCount())
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
	n := &notify.ToolNotification{
		ToolName: "Edit",
		Input:    `{"file_path":"/tmp/test.go"}`,
		CWD:      "/home/user",
		Window:   "lc-abc",
	}
	app.showToolPopup(n)

	active := app.activePopup()
	assert.Equal(t, "Edit", active.ToolName)
	assert.Equal(t, "lc-abc", active.Window)
}
