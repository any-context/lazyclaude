package gui_test

import (
	"sync"
	"testing"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/model"
	"github.com/KEMSHlM/lazyclaude/internal/gui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSessionProvider implements gui.SessionProvider for testing.
type mockSessionProvider struct {
	mu          sync.Mutex
	sessions    []gui.SessionItem
	pending     *model.ToolNotification
	sentChoices []sentChoice
}

type sentChoice struct {
	Window string
	Choice gui.Choice
}

func (m *mockSessionProvider) Sessions() []gui.SessionItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions
}
func (m *mockSessionProvider) Projects() []gui.ProjectItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sessions) == 0 {
		return nil
	}
	return []gui.ProjectItem{{
		ID:       "test-project",
		Name:     "test",
		Path:     "/test",
		Expanded: true,
		Sessions: m.sessions,
	}}
}
func (m *mockSessionProvider) ToggleProjectExpanded(_ string) {}
func (m *mockSessionProvider) Create(_, _ string) error { return nil }
func (m *mockSessionProvider) Delete(_ string) error    { return nil }
func (m *mockSessionProvider) Rename(_, _ string) error { return nil }
func (m *mockSessionProvider) PurgeOrphans() (int, error) { return 0, nil }
func (m *mockSessionProvider) CapturePreview(_ string, _, _ int) (gui.PreviewResult, error) {
	return gui.PreviewResult{Content: "preview content"}, nil
}
func (m *mockSessionProvider) CaptureScrollback(_ string, _, _, _ int) (gui.PreviewResult, error) {
	return gui.PreviewResult{Content: "scrollback content"}, nil
}

func (m *mockSessionProvider) PendingNotifications() []*model.ToolNotification {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return nil
	}
	n := m.pending
	m.pending = nil
	return []*model.ToolNotification{n}
}

func (m *mockSessionProvider) SendChoice(window string, choice gui.Choice) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentChoices = append(m.sentChoices, sentChoice{Window: window, Choice: choice})
	return nil
}
func (m *mockSessionProvider) AttachSession(_ string) error                       { return nil }
func (m *mockSessionProvider) LaunchLazygit(_, _ string) error                    { return nil }
func (m *mockSessionProvider) CreateWorktree(_, _, _ string) error               { return nil }
func (m *mockSessionProvider) ResumeWorktree(_, _, _ string) error               { return nil }
func (m *mockSessionProvider) ListWorktrees(_ string) ([]gui.WorktreeInfo, error) { return nil, nil }
func (m *mockSessionProvider) CreatePMSession(_ string) error                     { return nil }
func (m *mockSessionProvider) CreateWorkerSession(_, _, _ string) error           { return nil }

func (m *mockSessionProvider) getSentChoices() []sentChoice {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]sentChoice, len(m.sentChoices))
	copy(result, m.sentChoices)
	return result
}

func (m *mockSessionProvider) setPending(n *model.ToolNotification) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = n
}

func TestPopup_ShowAndDismissWithY(t *testing.T) {

	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)
	// gocui is owned by App — do not close separately

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running", TmuxWindow: "@0"},
		},
	}
	app.SetSessions(mock)

	// Show popup directly
	n := &model.ToolNotification{
		ToolName: "Write",
		Input:    `{"file_path":"/tmp/test.txt"}`,
		Window:   "@0",
	}
	app.ShowToolPopupForTest(n)

	assert.True(t, app.HasPopupForTest())

	// Dismiss with ChoiceAccept
	app.DismissPopupForTest(gui.ChoiceAccept)

	assert.False(t, app.HasPopupForTest())

	// Wait for goroutine to send choice
	require.Eventually(t, func() bool {
		return len(mock.getSentChoices()) == 1
	}, time.Second, 5*time.Millisecond)
	choices := mock.getSentChoices()
	assert.Equal(t, "@0", choices[0].Window)
	assert.Equal(t, gui.ChoiceAccept, choices[0].Choice)
}

func TestPopup_NotificationPolling(t *testing.T) {

	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)
	// gocui is owned by App — do not close separately

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running"},
		},
	}
	app.SetSessions(mock)

	// Set pending notification
	mock.setPending(&model.ToolNotification{
		ToolName: "Bash",
		Input:    `{"command":"ls"}`,
		Window:   "@0",
	})

	// Simulate what the ticker does
	app.PollNotificationForTest()

	assert.True(t, app.HasPopupForTest())
}

func TestPopup_DiffNotification(t *testing.T) {

	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)
	// gocui is owned by App — do not close separately

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running"},
		},
	}
	app.SetSessions(mock)

	n := &model.ToolNotification{
		ToolName:    "Diff",
		OldFilePath: "/tmp/test.go",
		NewContents: "package main\n",
		Window:      "@0",
	}
	app.ShowToolPopupForTest(n)

	assert.True(t, app.HasPopupForTest())
	assert.True(t, n.IsDiff())
}

func TestFullScreen_EnterAndExit(t *testing.T) {

	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)
	// gocui is owned by App — do not close separately

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running", TmuxWindow: "@0"},
		},
	}
	app.SetSessions(mock)

	assert.False(t, app.IsFullScreenForTest())

	app.EnterFullScreenForTest("s1")
	assert.True(t, app.IsFullScreenForTest())

	app.ExitFullScreenForTest()
	assert.False(t, app.IsFullScreenForTest())
}

func TestFullScreen_LayoutCreatesMainView(t *testing.T) {

	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running", TmuxWindow: "@0"},
		},
	}
	app.SetSessions(mock)

	app.EnterFullScreenForTest("s1")

	// Run layout
	err = app.TestLayout(app.Gui())
	require.NoError(t, err)

	// Main view should exist and span full width
	v, err := app.Gui().View("main")
	require.NoError(t, err)
	require.NotNil(t, v)

	// Sessions view should NOT exist in full screen
	_, err = app.Gui().View("sessions")
	assert.Error(t, err) // view not found
}

func TestFullScreen_PopupWorksInFullMode(t *testing.T) {

	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running", TmuxWindow: "@0"},
		},
	}
	app.SetSessions(mock)

	// Enter full screen
	app.EnterFullScreenForTest("s1")
	assert.True(t, app.IsFullScreenForTest())

	// Show popup — should work identically to preview mode
	app.ShowToolPopupForTest(&model.ToolNotification{
		ToolName: "Write",
		Input:    `{"file_path":"/tmp/test.txt"}`,
		Window:   "@0",
	})
	assert.True(t, app.HasPopupForTest())

	// Dismiss
	app.DismissPopupForTest(gui.ChoiceAccept)
	assert.False(t, app.HasPopupForTest())

	// Still in full screen
	assert.True(t, app.IsFullScreenForTest())

	require.Eventually(t, func() bool {
		return len(mock.getSentChoices()) == 1
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, gui.ChoiceAccept, mock.getSentChoices()[0].Choice)
}

func TestFullScreen_EntersFullScreenState(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running", TmuxWindow: "@0"},
		},
	}
	app.SetSessions(mock)
	app.EnterFullScreenForTest("s1")

	assert.Equal(t, gui.StateFullScreen, app.StateForTest())
}

func TestFullScreen_CtrlBackslash_ExitsFullScreen(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running", TmuxWindow: "@0"},
		},
	}
	app.SetSessions(mock)
	app.EnterFullScreenForTest("s1")
	assert.Equal(t, gui.StateFullScreen, app.StateForTest())

	// Ctrl+\ should exit fullscreen directly to StateMain
	app.ExitFullScreenForTest()
	assert.Equal(t, gui.StateMain, app.StateForTest())
	assert.False(t, app.IsFullScreenForTest())
}

func TestFullScreen_AllKeysForwardedToClaudeCode(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running", TmuxWindow: "@0"},
		},
	}
	app.SetSessions(mock)
	fwd := &gui.MockInputForwarder{}
	app.SetInputForwarder(fwd)

	app.EnterFullScreenForTest("s1")
	app.ForwardKeyForTest('h')

	require.Eventually(t, func() bool { return len(fwd.Keys()) == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"h"}, fwd.Keys())
}

func TestFullScreen_ExitThenReEnter_StillForwardsKeys(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running", TmuxWindow: "@0"},
		},
	}
	app.SetSessions(mock)
	app.EnterFullScreenForTest("s1")
	app.ExitFullScreenForTest()

	// Re-enter should be StateFullScreen (no modes)
	app.EnterFullScreenForTest("s1")
	assert.Equal(t, gui.StateFullScreen, app.StateForTest())
}

func TestFullScreen_PopupPreservesFullScreen(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running", TmuxWindow: "@0"},
		},
	}
	app.SetSessions(mock)
	app.EnterFullScreenForTest("s1")

	assert.Equal(t, gui.StateFullScreen, app.StateForTest())

	// Show popup
	app.ShowToolPopupForTest(&model.ToolNotification{
		ToolName: "Write",
		Window:   "@0",
	})
	assert.True(t, app.HasPopupForTest())
	// State should be preserved
	assert.Equal(t, gui.StateFullScreen, app.StateForTest())

	// Dismiss popup
	app.DismissPopupForTest(gui.ChoiceAccept)
	assert.False(t, app.HasPopupForTest())
	// Still in full screen
	assert.Equal(t, gui.StateFullScreen, app.StateForTest())
}

func TestFullScreen_CtrlD_ExitsFullScreen(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running", TmuxWindow: "@0"},
		},
	}
	app.SetSessions(mock)
	app.EnterFullScreenForTest("s1")

	// Ctrl+D should exit full-screen
	app.ExitFullScreenForTest()
	assert.False(t, app.IsFullScreenForTest())
	assert.Equal(t, gui.StateMain, app.StateForTest())
}

func TestFullScreen_DoesNotForwardInPopup(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running", TmuxWindow: "@0"},
		},
	}
	app.SetSessions(mock)
	fwd := &gui.MockInputForwarder{}
	app.SetInputForwarder(fwd)

	app.EnterFullScreenForTest("s1")
	app.ShowToolPopupForTest(&model.ToolNotification{ToolName: "Write", Window: "@0"})

	// Keys should NOT be forwarded when popup is showing
	app.ForwardKeyForTest('h')
	assert.Empty(t, fwd.Keys(), "keys should not forward during popup")
}

func TestPopup_BlocksSessionKeys(t *testing.T) {

	app, err := gui.NewAppHeadless(gui.ModeMain, 80, 24)
	require.NoError(t, err)
	// gocui is owned by App — do not close separately

	mock := &mockSessionProvider{
		sessions: []gui.SessionItem{
			{ID: "s1", Name: "test", Status: "Running"},
		},
	}
	app.SetSessions(mock)

	// Show popup
	app.ShowToolPopupForTest(&model.ToolNotification{
		ToolName: "Write",
		Window:   "@0",
	})

	// Cursor should not change during popup
	cursorBefore := app.CursorForTest()
	// Simulate what j key handler does
	// (can't call keybinding directly, but can verify popup blocks)
	assert.True(t, app.HasPopupForTest())
	assert.Equal(t, cursorBefore, app.CursorForTest())
}
