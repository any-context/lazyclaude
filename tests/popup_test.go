package tests_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTmux_DiffPopup_ShowsContent(t *testing.T) {
	bin := e2eBinary(t)
	h := newTmuxHelper(t)
	h.startSession("diff-test", 80, 20)

	oldFile := testdataPath(t, "old.go")
	newFile := testdataPath(t, "new.go")

	// No real Claude pane → maxOption defaults to 3 → action bar: y: yes  a: allow always  n: no
	h.sendKeys("diff-test",
		fmt.Sprintf("%s diff --window lc-test --old %s --new %s", bin, oldFile, newFile),
		"Enter")

	found := h.waitForText("diff-test", "hello", 5*time.Second)
	assert.True(t, found, "expected diff content")

	content := h.capturePane("diff-test")
	assert.Contains(t, content, "y: yes", "action bar should show y: yes")
	assert.Contains(t, content, "a: allow always", "3-option default: should show allow always")
	assert.Contains(t, content, "n: no", "action bar should show n: no")

	h.sendKeys("diff-test", "y")
}

func TestTmux_ToolPopup_ShowsContent(t *testing.T) {
	bin := e2eBinary(t)
	h := newTmuxHelper(t)
	h.startSession("tool-test", 80, 20)

	// No real Claude pane → capture fails → maxOption defaults to 3 → 3-option action bar
	h.sendKeys("tool-test",
		fmt.Sprintf("TOOL_NAME=Bash TOOL_INPUT='{\"command\":\"ls\"}' TOOL_CWD=/tmp %s tool --window lc-tool", bin),
		"Enter")

	// Wait for gocui to render (not just the command line)
	found := h.waitForText("tool-test", "Esc: cancel", 10*time.Second)
	if !found {
		t.Logf("capture:\n%s", h.capturePane("tool-test"))
	}
	require.True(t, found, "expected gocui popup to render")

	content := h.capturePane("tool-test")
	assert.Contains(t, content, "y: yes", "action bar should show y: yes")
	assert.Contains(t, content, "a: allow always", "3-option default: should show allow always")
	assert.Contains(t, content, "n: no", "action bar should show n: no")
	assert.Contains(t, content, "Esc: cancel", "action bar should show Esc: cancel")

	h.sendKeys("tool-test", "n")
}

func TestTmux_ToolPopup_2Option(t *testing.T) {
	bin := e2eBinary(t)
	h := newTmuxHelper(t)

	// Create a "lazyclaude" session with a pane that simulates a 2-option dialog
	h.startSession("lazyclaude", 80, 24)
	// Write a fake 2-option dialog to the pane
	h.sendKeys("lazyclaude",
		"echo ' Do you want to proceed?'; echo ' ❯ 1. Yes'; echo '   2. No'", "Enter")
	time.Sleep(300 * time.Millisecond)

	// Get window ID
	winID, err := h.run("display-message", "-t", "lazyclaude", "-p", "#{window_id}")
	require.NoError(t, err)

	// Start tool popup targeting that window
	_, err = h.run("new-window", "-t", "lazyclaude", "-n", "popup")
	require.NoError(t, err)

	toolCmd := fmt.Sprintf(
		"TOOL_NAME=Bash TOOL_INPUT='{\"command\":\"ls\"}' %s tool --window %s",
		bin, strings.TrimSpace(winID))
	h.sendKeys("lazyclaude:popup", toolCmd, "Enter")

	found := h.waitForText("lazyclaude:popup", "Esc: cancel", 10*time.Second)
	if !found {
		t.Logf("capture:\n%s", h.capturePane("lazyclaude:popup"))
	}
	require.True(t, found, "expected gocui popup to render")

	content := h.capturePane("lazyclaude:popup")
	// 2-option: must show y/n, must NOT show "allow always"
	assert.Contains(t, content, "y: yes", "2-option: should show y: yes")
	assert.Contains(t, content, "n: no", "2-option: should show n: no")
	assert.NotContains(t, content, "a: allow always", "2-option: must NOT show allow always")
	assert.Contains(t, content, "Esc: cancel", "2-option: should show Esc: cancel")

	h.sendKeys("lazyclaude:popup", "n")
}

func TestTmux_ServerCommand_StartsAndStops(t *testing.T) {
	bin := e2eBinary(t)
	h := newTmuxHelper(t)
	h.startSession("server-test", 120, 40)

	h.sendKeys("server-test",
		fmt.Sprintf("%s server --port 0 &", bin),
		"Enter")

	found := h.waitForText("server-test", "MCP server", 5*time.Second)
	require.True(t, found, "expected MCP server started message")

	h.sendKeys("server-test", "kill %1", "Enter")
}

func TestTmux_HelpCommand_InPane(t *testing.T) {
	bin := e2eBinary(t)
	h := newTmuxHelper(t)
	h.startSession("help-test", 120, 40)

	h.sendKeys("help-test",
		fmt.Sprintf("%s --help", bin),
		"Enter")

	found := h.waitForText("help-test", "terminal UI", 5*time.Second)
	assert.True(t, found, "expected help text in pane output")
}
