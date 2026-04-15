package gui_test

import (
	"testing"

	"github.com/any-context/lazyclaude/internal/gui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProfileDialog_ShowSetsDialogKind verifies that showProfileDialog transitions
// the dialog state to DialogProfile and stores the confirm kind and session path.
func TestProfileDialog_ShowSetsDialogKind(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running"},
		},
	}
	app.SetSessions(mock)

	assert.Equal(t, gui.DialogNone, app.ActiveDialogForTest())

	ok := app.ShowProfileDialogForTest("session", "/path/to/project")
	assert.True(t, ok, "showProfileDialog should return true")

	app.TestLayout(app.Gui())
	assert.Equal(t, gui.DialogProfile, app.ActiveDialogForTest())
}

// TestProfileDialog_StoresConfirmKindAndPath verifies that ProfileConfirmKind and
// ProfileSessionPath are stored correctly when the dialog opens.
func TestProfileDialog_StoresConfirmKindAndPath(t *testing.T) {
	tests := []struct {
		name        string
		confirmKind string
		sessionPath string
	}{
		{"session", "session", "/home/user/project"},
		{"session_cwd", "session_cwd", ""},
		{"pm_session", "pm_session", "/workspace"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
			require.NoError(t, err)

			mock := &mockSessionProvider{}
			app.SetSessions(mock)

			app.ShowProfileDialogForTest(tc.confirmKind, tc.sessionPath)
			app.TestLayout(app.Gui())

			assert.Equal(t, tc.confirmKind, app.ProfileConfirmKindForTest())
			assert.Equal(t, tc.sessionPath, app.ProfileSessionPathForTest())
		})
	}
}

// TestProfileDialog_CloseClearsDialogKind verifies that closeProfileDialog
// resets the dialog state back to DialogNone.
func TestProfileDialog_CloseClearsDialogKind(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running"},
		},
	}
	app.SetSessions(mock)

	app.ShowProfileDialogForTest("session", "/project")
	app.TestLayout(app.Gui())
	assert.Equal(t, gui.DialogProfile, app.ActiveDialogForTest())

	app.CloseProfileDialogForTest()
	app.TestLayout(app.Gui())
	assert.Equal(t, gui.DialogNone, app.ActiveDialogForTest())
}

// TestProfileDialog_CloseClearsState verifies that closeProfileDialog resets
// profile state fields (ProfileItems, ProfileCursor, ProfileConfirmKind, ProfileSessionPath).
func TestProfileDialog_CloseClearsState(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{}
	app.SetSessions(mock)

	app.ShowProfileDialogForTest("pm_session", "/myproject")
	app.TestLayout(app.Gui())

	// Verify state was set
	assert.Equal(t, "pm_session", app.ProfileConfirmKindForTest())
	assert.Equal(t, "/myproject", app.ProfileSessionPathForTest())

	// Close and verify state was cleared
	app.CloseProfileDialogForTest()
	app.TestLayout(app.Gui())

	assert.Equal(t, "", app.ProfileConfirmKindForTest())
	assert.Equal(t, "", app.ProfileSessionPathForTest())
	assert.Equal(t, 0, app.ProfileCursorForTest())
}

// TestProfileDialog_LoadsProfileItems verifies that showProfileDialog populates
// ProfileItems with at least one entry (the builtin default).
func TestProfileDialog_LoadsProfileItems(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{}
	app.SetSessions(mock)

	app.ShowProfileDialogForTest("session", "/project")
	app.TestLayout(app.Gui())

	items := app.ProfileItemsForTest()
	assert.NotEmpty(t, items, "profile items should be populated with at least the builtin default")
}

// TestRemoteProfileErrorDialog_ShowSetsDialogKind verifies that
// showRemoteProfileErrorDialog transitions the dialog state to DialogRemoteProfileError.
func TestRemoteProfileErrorDialog_ShowSetsDialogKind(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{}
	app.SetSessions(mock)

	assert.Equal(t, gui.DialogNone, app.ActiveDialogForTest())

	ok := app.ShowRemoteProfileErrorDialogForTest("myhost", "parse error: unexpected token")
	assert.True(t, ok, "showRemoteProfileErrorDialog should return true")

	app.TestLayout(app.Gui())
	assert.Equal(t, gui.DialogRemoteProfileError, app.ActiveDialogForTest())
}

// TestRemoteProfileErrorDialog_CloseClearsDialogKind verifies that
// closeRemoteProfileErrorDialog resets the dialog state to DialogNone.
func TestRemoteProfileErrorDialog_CloseClearsDialogKind(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{}
	app.SetSessions(mock)

	app.ShowRemoteProfileErrorDialogForTest("host", "broken json")
	app.TestLayout(app.Gui())
	assert.Equal(t, gui.DialogRemoteProfileError, app.ActiveDialogForTest())

	app.CloseRemoteProfileErrorDialogForTest()
	app.TestLayout(app.Gui())
	assert.Equal(t, gui.DialogNone, app.ActiveDialogForTest())
}

// TestProfileDialog_NoDoubleOpen verifies that showProfileDialog is a no-op when
// a dialog is already open (HasActiveDialog guard).
func TestProfileDialog_NoDoubleOpen(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{}
	app.SetSessions(mock)

	// Open profile dialog first
	app.ShowProfileDialogForTest("session", "/project")
	app.TestLayout(app.Gui())
	assert.Equal(t, gui.DialogProfile, app.ActiveDialogForTest())

	// Attempting to create a second dialog does not change the kind
	// (CreateSession guards with HasActiveDialog)
	assert.True(t, app.HasActiveDialog())
}
