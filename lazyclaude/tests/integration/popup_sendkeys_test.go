package integration_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestE2E_ToolPopup_SendKeys verifies that `lazyclaude tool --send-keys`
// sends the user's choice key to the target tmux pane.
//
// Setup: a `cat` process in a pane captures stdin.
// The tool popup runs in another pane, user presses 'y',
// and "1" should arrive at the cat pane.
func TestE2E_ToolPopup_SendKeys(t *testing.T) {
	bin := e2eBinary(t)
	h := newTmuxHelper(t)

	// Start a "lazyclaude" session with cat as a key listener
	h.startSession("lazyclaude", 80, 24)
	h.sendKeys("lazyclaude", "cat", "Enter")
	time.Sleep(200 * time.Millisecond)

	// Get the window ID for the cat pane
	winID, err := h.run("display-message", "-t", "lazyclaude", "-p", "#{window_id}")
	require.NoError(t, err)
	t.Logf("cat window ID: %s", winID)

	// Run tool popup in a separate window with --send-keys
	_, err = h.run("new-window", "-t", "lazyclaude", "-n", "popup")
	require.NoError(t, err)

	toolCmd := fmt.Sprintf(
		"TOOL_NAME=Bash TOOL_INPUT='{\"command\":\"ls\"}' %s tool --window %s --send-keys",
		bin, winID)
	h.sendKeys("lazyclaude:popup", toolCmd, "Enter")

	// Wait for the tool popup to render
	found := h.waitForText("lazyclaude:popup", "Bash", 5*time.Second)
	require.True(t, found, "tool popup should render with tool name")

	// Press 'y' in the popup
	h.sendKeys("lazyclaude:popup", "y")

	// Verify "1" arrived at the cat pane (choice Accept -> key "1")
	require.Eventually(t, func() bool {
		return strings.Contains(h.capturePane("lazyclaude:"+winID), "1")
	}, 3*time.Second, 200*time.Millisecond, "cat pane should have received key '1'")
}

// TestE2E_DiffPopup_SendKeys verifies `lazyclaude diff --send-keys`.
func TestE2E_DiffPopup_SendKeys(t *testing.T) {
	bin := e2eBinary(t)
	h := newTmuxHelper(t)

	h.startSession("lazyclaude", 80, 24)
	h.sendKeys("lazyclaude", "cat", "Enter")
	time.Sleep(200 * time.Millisecond)

	winID, err := h.run("display-message", "-t", "lazyclaude", "-p", "#{window_id}")
	require.NoError(t, err)

	_, err = h.run("new-window", "-t", "lazyclaude", "-n", "popup")
	require.NoError(t, err)

	oldFile := testdataPath(t, "old.go")
	newFile := testdataPath(t, "new.go")
	diffCmd := fmt.Sprintf("%s diff --window %s --send-keys --old %s --new %s",
		bin, winID, oldFile, newFile)
	h.sendKeys("lazyclaude:popup", diffCmd, "Enter")

	found := h.waitForText("lazyclaude:popup", "hello", 5*time.Second)
	require.True(t, found, "diff popup should render")

	// Press 'n' (reject -> key "3")
	h.sendKeys("lazyclaude:popup", "n")

	// Verify "3" arrived at the cat pane (choice Reject -> key "3")
	require.Eventually(t, func() bool {
		return strings.Contains(h.capturePane("lazyclaude:"+winID), "3")
	}, 3*time.Second, 200*time.Millisecond, "cat pane should have received key '3'")
}
