package tests_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// e2eBinary returns the lazyclaude binary path.
func e2eBinary(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat("/usr/local/bin/lazyclaude"); err == nil {
		return "/usr/local/bin/lazyclaude"
	}
	return ensureBinary(t)
}

// cleanLazyClaudeState removes stale port/state files for E2E test isolation.
// NOTE: Never kill the production "lazyclaude" tmux server here — tests must
// use their own tmux socket (via newTmuxHelper) to avoid destroying live sessions.
func cleanLazyClaudeState(t *testing.T) {
	t.Helper()
	os.Remove(filepath.Join(os.TempDir(), "lazyclaude-mcp.port"))
	// Remove stale state file so lazyclaude starts fresh
	os.Remove(filepath.Join(os.Getenv("HOME"), ".local", "share", "lazyclaude", "state.json"))
	time.Sleep(500 * time.Millisecond)
}

// TestE2E_LazyClaudeTUI_StartsAndShowsSessions verifies basic TUI startup.
func TestE2E_LazyClaudeTUI_StartsAndShowsSessions(t *testing.T) {
	bin := e2eBinary(t)
	h := newTmuxHelper(t)
	h.startSession("tui-test", 80, 24)

	h.sendKeys("tui-test", fmt.Sprintf("%s; sleep 999", bin), "Enter")

	found := h.waitForText("tui-test", "no sessions", 5*time.Second)
	if !found {
		t.Logf("capture:\n%s", h.capturePane("tui-test"))
	}
	require.True(t, found, "TUI should show Sessions panel")

	content := h.capturePane("tui-test")
	assert.Contains(t, content, "n: new")
	assert.Contains(t, content, "q: quit")
}

// enqueueNotification writes a queued notification file for the TUI to pick up.
func enqueueNotification(t *testing.T, toolName, window string) {
	t.Helper()
	runtimeDir := os.TempDir()
	notif := map[string]any{
		"tool_name": toolName,
		"input":     fmt.Sprintf(`{"arg":"%s"}`, toolName),
		"window":    window,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	data, err := json.Marshal(notif)
	require.NoError(t, err)
	name := fmt.Sprintf("lazyclaude-q-%020d.json", time.Now().UnixNano())
	path := filepath.Join(runtimeDir, name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	t.Cleanup(func() { os.Remove(path) })
}

// TestE2E_ToolPopup_AutoDisplay verifies that a notification file
// triggers a popup overlay in the TUI.
func TestE2E_ToolPopup_AutoDisplay(t *testing.T) {
	bin := e2eBinary(t)
	h := newTmuxHelper(t)
	h.startSession("popup-e2e", 80, 24)

	h.sendKeys("popup-e2e", fmt.Sprintf("%s; sleep 999", bin), "Enter")

	found := h.waitForText("popup-e2e", "no sessions", 5*time.Second)
	require.True(t, found, "TUI should start")

	enqueueNotification(t, "Write", "@0")

	found = h.waitForText("popup-e2e", "Write", 3*time.Second)
	assert.True(t, found, "tool popup should appear with tool name")

	if found {
		content := h.capturePane("popup-e2e")
		assert.Contains(t, content, "y/a/n")
		assert.Contains(t, content, "hide")
	}
}

// TestE2E_PopupStack_CascadeDisplay verifies multiple popups are stacked visually.
func TestE2E_PopupStack_CascadeDisplay(t *testing.T) {
	bin := e2eBinary(t)
	h := newTmuxHelper(t)
	h.startSession("stack-e2e", 120, 40)

	h.sendKeys("stack-e2e", fmt.Sprintf("%s; sleep 999", bin), "Enter")

	found := h.waitForText("stack-e2e", "no sessions", 5*time.Second)
	require.True(t, found, "TUI should start")

	// Enqueue 3 notifications with spacing to ensure distinct file timestamps
	for i := 1; i <= 3; i++ {
		enqueueNotification(t, fmt.Sprintf("Tool%d", i), "@0")
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for all 3 popups to be stacked
	found = h.waitForText("stack-e2e", "[3/3]", 5*time.Second)
	if !found {
		content := h.capturePane("stack-e2e")
		t.Logf("waiting for [3/3], got:\n%s", content)
	}
	require.True(t, found, "stack indicator should show 3 popups")

	content := h.capturePane("stack-e2e")
	assert.Contains(t, content, "Tool1", "Tool1 title should be visible in cascade")
	assert.Contains(t, content, "Tool2", "Tool2 title should be visible in cascade")
	assert.Contains(t, content, "Tool3", "Tool3 title should be visible in cascade")
	assert.Contains(t, content, "hide", "Esc:hide should be visible")
}

// TestE2E_PopupStack_ArrowKeySwitchFocus verifies arrow keys cycle focus.
func TestE2E_PopupStack_ArrowKeySwitchFocus(t *testing.T) {
	bin := e2eBinary(t)
	h := newTmuxHelper(t)
	h.startSession("focus-e2e", 120, 40)

	h.sendKeys("focus-e2e", fmt.Sprintf("%s; sleep 999", bin), "Enter")

	found := h.waitForText("focus-e2e", "no sessions", 5*time.Second)
	require.True(t, found)

	for i := 1; i <= 3; i++ {
		enqueueNotification(t, fmt.Sprintf("Tool%d", i), "@0")
		time.Sleep(50 * time.Millisecond)
	}

	found = h.waitForText("focus-e2e", "[3/3]", 5*time.Second)
	require.True(t, found, "3 popups should be stacked")

	// Arrow Up: focus moves from Tool3 to Tool2
	h.sendKeys("focus-e2e", "Up")
	time.Sleep(300 * time.Millisecond)
	content := h.capturePane("focus-e2e")
	assert.Contains(t, content, "[2/3]", "focus should move to popup 2")

	// Arrow Up: focus moves to Tool1
	h.sendKeys("focus-e2e", "Up")
	time.Sleep(300 * time.Millisecond)
	content = h.capturePane("focus-e2e")
	assert.Contains(t, content, "[1/3]", "focus should move to popup 1")
}

// TestE2E_PopupStack_SuspendAndReopen verifies Esc suspend and p reopen.
func TestE2E_PopupStack_SuspendAndReopen(t *testing.T) {
	bin := e2eBinary(t)
	h := newTmuxHelper(t)
	h.startSession("suspend-e2e", 120, 40)

	h.sendKeys("suspend-e2e", fmt.Sprintf("%s; sleep 999", bin), "Enter")

	found := h.waitForText("suspend-e2e", "no sessions", 5*time.Second)
	require.True(t, found)

	enqueueNotification(t, "TestTool", "@0")

	found = h.waitForText("suspend-e2e", "TestTool", 3*time.Second)
	require.True(t, found, "popup should appear")

	// Esc to suspend
	h.sendKeys("suspend-e2e", "Escape")
	time.Sleep(300 * time.Millisecond)
	content := h.capturePane("suspend-e2e")
	assert.NotContains(t, content, "TestTool", "popup should be hidden after Esc")

	// p to reopen
	h.sendKeys("suspend-e2e", "p")
	time.Sleep(300 * time.Millisecond)
	content = h.capturePane("suspend-e2e")
	assert.Contains(t, content, "TestTool", "popup should reappear after p")
}

// TestE2E_PopupStack_DismissAll verifies y dismisses focused popup only,
// and Y (uppercase) dismisses all popups at once.
func TestE2E_PopupStack_DismissAll(t *testing.T) {
	bin := e2eBinary(t)
	h := newTmuxHelper(t)
	h.startSession("dismiss-e2e", 120, 40)

	h.sendKeys("dismiss-e2e", fmt.Sprintf("%s; sleep 999", bin), "Enter")

	found := h.waitForText("dismiss-e2e", "no sessions", 5*time.Second)
	require.True(t, found)

	// Enqueue 2 notifications with enough spacing for file ordering
	enqueueNotification(t, "DismissTool1", "@0")
	time.Sleep(50 * time.Millisecond)
	enqueueNotification(t, "DismissTool2", "@0")

	// Wait until BOTH popups are visible (stack indicator [2/2])
	found = h.waitForText("dismiss-e2e", "[2/2]", 3*time.Second)
	if !found {
		content := h.capturePane("dismiss-e2e")
		t.Logf("waiting for [2/2], got:\n%s", content)
	}
	require.True(t, found, "both popups should be stacked")

	// y to dismiss focused only (DismissTool2), DismissTool1 should remain
	h.sendKeys("dismiss-e2e", "y")
	time.Sleep(300 * time.Millisecond)
	content := h.capturePane("dismiss-e2e")
	assert.Contains(t, content, "DismissTool1", "first popup should remain after single y")
	assert.NotContains(t, content, "DismissTool2", "second popup should be gone after y")

	// Enqueue 2 more to test Y (accept all)
	enqueueNotification(t, "DismissTool3", "@0")
	time.Sleep(50 * time.Millisecond)
	enqueueNotification(t, "DismissTool4", "@0")
	found = h.waitForText("dismiss-e2e", "DismissTool4", 3*time.Second)
	require.True(t, found, "new popups should appear")

	// Y (uppercase) to dismiss all at once
	h.sendKeys("dismiss-e2e", "Y")
	time.Sleep(300 * time.Millisecond)
	content = h.capturePane("dismiss-e2e")
	assert.NotContains(t, content, "DismissTool", "all popups gone after Y")
	assert.NotContains(t, content, "y/a/n", "action bar should be gone")
}

// TestE2E_FullScreenMode_EnterAndExit verifies Enter/Ctrl+D toggle.
func TestE2E_FullScreenMode_EnterAndExit(t *testing.T) {
	cleanLazyClaudeState(t)
	bin := e2eBinary(t)
	h := newTmuxHelper(t)
	h.startSession("fs-test", 80, 24)

	h.sendKeys("fs-test", fmt.Sprintf("%s; sleep 999", bin), "Enter")

	found := h.waitForText("fs-test", "no sessions", 5*time.Second)
	require.True(t, found, "TUI should start")

	// Create a session and wait for it to appear in the session list
	h.sendKeys("fs-test", "n")
	found = h.waitForText("fs-test", "Session created", 5*time.Second)
	if !found {
		// Fallback: wait for the session entry with ">" cursor
		found = h.waitForText("fs-test", ">", 3*time.Second)
	}
	require.True(t, found, "session should be created")

	// Extra wait for tmux window to be synced
	time.Sleep(1 * time.Second)

	// Enter full-screen
	h.sendKeys("fs-test", "Enter")

	// Should show INSERT status bar
	found = h.waitForText("fs-test", "INSERT", 5*time.Second)
	if !found {
		t.Logf("expected INSERT, got:\n%s", h.capturePane("fs-test"))
	}
	assert.True(t, found, "full-screen should show INSERT mode")

	// Ctrl+D to exit
	h.sendKeys("fs-test", "C-d")

	// Should be back to split panel
	found = h.waitForText("fs-test", "n: new", 3*time.Second)
	assert.True(t, found, "should return to split panel after Ctrl+D")
}

// TestE2E_NormalMode_SwitchAndExit verifies Ctrl+\ and q.
func TestE2E_NormalMode_SwitchAndExit(t *testing.T) {
	cleanLazyClaudeState(t)
	bin := e2eBinary(t)
	h := newTmuxHelper(t)
	h.startSession("mode-test", 80, 24)

	h.sendKeys("mode-test", fmt.Sprintf("%s; sleep 999", bin), "Enter")

	found := h.waitForText("mode-test", "no sessions", 5*time.Second)
	require.True(t, found)

	// Create session and wait for it
	h.sendKeys("mode-test", "n")
	found = h.waitForText("mode-test", "Session created", 5*time.Second)
	if !found {
		found = h.waitForText("mode-test", ">", 3*time.Second)
	}
	require.True(t, found, "session should be created")
	time.Sleep(1 * time.Second)

	// Enter full-screen
	h.sendKeys("mode-test", "Enter")

	found = h.waitForText("mode-test", "INSERT", 5*time.Second)
	if !found {
		t.Logf("expected INSERT, got:\n%s", h.capturePane("mode-test"))
	}
	require.True(t, found, "should show INSERT after Enter")

	// Ctrl+\ to normal mode
	// tmux send-keys interprets C-\ as Ctrl+backslash
	_, err := h.run("send-keys", "-t", "mode-test", "C-\\")
	require.NoError(t, err)

	found = h.waitForText("mode-test", "NORMAL", 3*time.Second)
	content := h.capturePane("mode-test")
	t.Logf("after Ctrl+\\:\n%s", content)
	require.True(t, found, "should switch to NORMAL mode")

	// q to exit full-screen
	h.sendKeys("mode-test", "q")

	found = h.waitForText("mode-test", "n: new", 3*time.Second)
	assert.True(t, found, "q should exit full-screen back to split panel")
}
