package gui_test

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/core/model"
	"github.com/KEMSHlM/lazyclaude/internal/gui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorktreeDialog_ShowSetsDialogKind(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running"},
		},
	}
	app.SetSessions(mock)

	assert.Equal(t, gui.DialogNone, app.ActiveDialogForTest())

	app.ShowWorktreeDialogForTest()

	// Force layout to run
	app.TestLayout(app.Gui())
	assert.Equal(t, gui.DialogWorktree, app.ActiveDialogForTest())
}

func TestWorktreeDialog_CloseClearsDialogKind(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running"},
		},
	}
	app.SetSessions(mock)

	app.ShowWorktreeDialogForTest()
	app.TestLayout(app.Gui())
	assert.Equal(t, gui.DialogWorktree, app.ActiveDialogForTest())

	app.CloseWorktreeDialogForTest()
	app.TestLayout(app.Gui())
	assert.Equal(t, gui.DialogNone, app.ActiveDialogForTest())
}

func TestWorktreeDialog_BlocksPopupFocusSteal(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running"},
		},
	}
	app.SetSessions(mock)

	// Open dialog
	app.ShowWorktreeDialogForTest()
	app.TestLayout(app.Gui())
	assert.Equal(t, gui.DialogWorktree, app.ActiveDialogForTest())

	// Simulate notification popup arriving
	app.ShowToolPopupForTest(&model.ToolNotification{ToolName: "Write", Window: "@0"})
	app.TestLayout(app.Gui())

	// Dialog should still be active (popup should not steal focus)
	assert.Equal(t, gui.DialogWorktree, app.ActiveDialogForTest())
	assert.True(t, app.HasPopupForTest(), "popup should be present in background")
}

func TestWorktreeDialog_StartWorktreeInput_NoDoubleOpen(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running"},
		},
	}
	app.SetSessions(mock)

	// Open dialog first
	app.ShowWorktreeDialogForTest()
	app.TestLayout(app.Gui())
	assert.Equal(t, gui.DialogWorktree, app.ActiveDialogForTest())

	// StartWorktreeInput should be no-op when dialog is already open
	app.StartWorktreeInput()
	assert.Equal(t, gui.DialogWorktree, app.ActiveDialogForTest())
}
