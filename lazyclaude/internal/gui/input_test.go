package gui_test

import (
	"testing"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/gui"
	"github.com/KEMSHlM/lazyclaude/internal/notify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockInputForwarder_RecordsKeys(t *testing.T) {
	t.Parallel()
	f := &gui.MockInputForwarder{}

	require.NoError(t, f.ForwardKey("@0", "h"))
	require.NoError(t, f.ForwardKey("@0", "e"))
	require.NoError(t, f.ForwardKey("@0", "l"))

	assert.Equal(t, []string{"h", "e", "l"}, f.Keys())
}

func TestMockInputForwarder_RecordsTarget(t *testing.T) {
	t.Parallel()
	f := &gui.MockInputForwarder{}

	f.ForwardKey("@1", "x")
	assert.Equal(t, "@1", f.LastTarget())
}

func TestKeyMapping_PrintableRune(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "a", gui.RuneToTmuxKey('a'))
	assert.Equal(t, "Z", gui.RuneToTmuxKey('Z'))
	assert.Equal(t, "1", gui.RuneToTmuxKey('1'))
	assert.Equal(t, " ", gui.RuneToTmuxKey(' '))
}

func TestFullScreen_ForwardsKeys(t *testing.T) {
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

func TestFullScreen_ForwardsSpecialKey(t *testing.T) {
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

	app.ForwardSpecialKeyForTest("Enter")
	require.Eventually(t, func() bool { return len(fwd.Keys()) == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"Enter"}, fwd.Keys())
}

func TestFullScreen_ExistingKeysForwardInFullMode(t *testing.T) {
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

	// j in full mode should forward, not move cursor
	cursorBefore := app.CursorForTest()
	app.ForwardKeyForTest('j')
	assert.Equal(t, cursorBefore, app.CursorForTest(), "cursor should not change in full mode")
	require.Eventually(t, func() bool { return len(fwd.Keys()) == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"j"}, fwd.Keys())
}

func TestFullScreen_KeyOrderPreserved(t *testing.T) {
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

	// Simulate rapid IME-like input: あいうえお mapped to keys a,i,u,e,o
	keys := []rune{'a', 'i', 'u', 'e', 'o'}
	for _, ch := range keys {
		app.ForwardKeyForTest(ch)
	}

	expected := []string{"a", "i", "u", "e", "o"}
	assert.Equal(t, expected, fwd.Keys(), "keys must arrive in order (IME input)")
}

func TestFullScreen_PopupBlocksForwarding(t *testing.T) {
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

	// Show popup — forwarding should be blocked
	app.ShowToolPopupForTest(&notify.ToolNotification{
		ToolName: "Write",
		Window:   "@0",
	})

	app.ForwardKeyForTest('h')
	assert.Empty(t, fwd.Keys(), "keys should not be forwarded when popup is showing")
}
