package gui_test

import (
	"testing"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/model"
	"github.com/KEMSHlM/lazyclaude/internal/gui"
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
	assert.Equal(t, "a", gui.RuneToLiteral('a'))
	assert.Equal(t, "Z", gui.RuneToLiteral('Z'))
	assert.Equal(t, "1", gui.RuneToLiteral('1'))
	assert.Equal(t, " ", gui.RuneToLiteral(' '))
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

func TestFullScreen_RuneKeysSentAsLiteral(t *testing.T) {
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

	// Rune characters (including tmux metacharacters) must use literal mode
	for _, ch := range []rune{';', '&', '|', '$', '(', ')', 'あ', 'A'} {
		app.ForwardKeyForTest(ch)
	}

	expected := []string{";", "&", "|", "$", "(", ")", "あ", "A"}
	assert.Equal(t, expected, fwd.Literals(), "rune chars must be sent via ForwardLiteral")
}

func TestFullScreen_SpecialKeysSentAsKeyName(t *testing.T) {
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

	// Special keys must NOT be sent as literal
	app.ForwardSpecialKeyForTest("Enter")
	app.ForwardSpecialKeyForTest("Space")

	assert.Equal(t, []string{"Enter", "Space"}, fwd.Keys())
	assert.Empty(t, fwd.Literals(), "special keys must NOT use ForwardLiteral")
}

// --- Paste detection tests ---
// These test the inputEditor state machine by calling EditForTest directly.

// setupPasteTestApp creates a headless App with sessions, forwarder, and editor
// ready for paste detection testing.
func setupPasteTestApp(t *testing.T) (*gui.App, *gui.MockInputForwarder) {
	t.Helper()
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
	app.InitEditorForTest()

	return app, fwd
}

// sendRunes calls EditForTest for each rune in the string.
func sendRunes(app *gui.App, s string) {
	for _, ch := range s {
		app.EditForTest(0, ch, 0)
	}
}

func TestPaste_BracketedPasteFlow(t *testing.T) {
	app, fwd := setupPasteTestApp(t)

	// Send ESC to start escape sequence detection
	app.EditForTest(gui.KeyEscForTest, 0, 0)
	// Send "[200~" to complete the paste start marker
	sendRunes(app, "[200~")
	// Send paste content
	sendRunes(app, "hello")
	// Send ESC to start end marker detection
	app.EditForTest(gui.KeyEscForTest, 0, 0)
	// Send "[201~" to complete the paste end marker
	sendRunes(app, "[201~")

	// forwardPaste runs in a goroutine, so wait for it
	require.Eventually(t, func() bool { return len(fwd.Pastes()) == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"hello"}, fwd.Pastes(), "paste content should be forwarded via ForwardPaste")
}

func TestPaste_IncompleteEscFlushesAsNormalInput(t *testing.T) {
	app, fwd := setupPasteTestApp(t)

	// Send ESC followed by a non-matching char (not '[')
	app.EditForTest(gui.KeyEscForTest, 0, 0)
	sendRunes(app, "x")

	// Drain the queue
	app.DrainQueueForTest()

	// Esc should be forwarded as "Escape" key and 'x' as literal
	keys := fwd.Keys()
	assert.NotEmpty(t, keys, "incomplete escape should flush as normal input")
	assert.Empty(t, fwd.Pastes(), "no paste should be detected")
}

func TestPaste_NestedEscInsidePasteTreatedAsContent(t *testing.T) {
	app, fwd := setupPasteTestApp(t)

	// Start paste
	app.EditForTest(gui.KeyEscForTest, 0, 0)
	sendRunes(app, "[200~")

	// Send content with an Esc that doesn't form end marker
	sendRunes(app, "before")
	app.EditForTest(gui.KeyEscForTest, 0, 0)
	sendRunes(app, "X") // Not "[201~", so Esc+X become paste content

	sendRunes(app, "after")

	// End paste
	app.EditForTest(gui.KeyEscForTest, 0, 0)
	sendRunes(app, "[201~")

	// forwardPaste runs in a goroutine, so wait for it
	require.Eventually(t, func() bool { return len(fwd.Pastes()) == 1 }, time.Second, 5*time.Millisecond)

	pastes := fwd.Pastes()
	require.Len(t, pastes, 1, "should have exactly one paste")
	assert.Contains(t, pastes[0], "before", "paste should contain content before nested Esc")
	assert.Contains(t, pastes[0], "after", "paste should contain content after nested Esc")
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
	app.ShowToolPopupForTest(&model.ToolNotification{
		ToolName: "Write",
		Window:   "@0",
	})

	app.ForwardKeyForTest('h')
	assert.Empty(t, fwd.Keys(), "keys should not be forwarded when popup is showing")
}
