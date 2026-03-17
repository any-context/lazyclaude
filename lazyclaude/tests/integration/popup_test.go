package integration_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTmux_DiffPopup_ShowsContent(t *testing.T) {
	bin := ensureBinary(t)
	h := newTmuxHelper(t)
	h.startSession("diff-test", 80, 20)

	oldFile := testdataPath(t, "old.go")
	newFile := testdataPath(t, "new.go")

	// Launch diff as pane command (gocui needs PTY)
	h.sendKeys("diff-test",
		fmt.Sprintf("%s diff --window lc-test --old %s --new %s", bin, oldFile, newFile),
		"Enter")

	// gocui should render diff content
	found := h.waitForText("diff-test", "hello", 5000000000)
	assert.True(t, found, "expected diff content")

	// Action bar should be visible
	content := h.capturePane("diff-test")
	assert.Contains(t, content, "y:")

	// Press y to exit
	h.sendKeys("diff-test", "y")
}

func TestTmux_ToolPopup_ShowsContent(t *testing.T) {
	bin := ensureBinary(t)
	h := newTmuxHelper(t)
	h.startSession("tool-test", 80, 15)

	// Set env vars for tool popup
	h.sendKeys("tool-test",
		fmt.Sprintf("TOOL_NAME=Bash TOOL_INPUT='{\"command\":\"ls\"}' TOOL_CWD=/tmp %s tool --window lc-tool", bin),
		"Enter")

	found := h.waitForText("tool-test", "Bash", 5000000000)
	assert.True(t, found, "expected tool name in popup")

	content := h.capturePane("tool-test")
	assert.Contains(t, content, "y:")

	h.sendKeys("tool-test", "n")
}

func TestTmux_ServerCommand_StartsAndStops(t *testing.T) {
	bin := ensureBinary(t)
	h := newTmuxHelper(t)
	h.startSession("server-test", 120, 40)

	h.sendKeys("server-test",
		fmt.Sprintf("%s server --port 0 &", bin),
		"Enter")

	found := h.waitForText("server-test", "MCP server", 5000000000)
	require.True(t, found, "expected MCP server started message")

	h.sendKeys("server-test", "kill %1", "Enter")
}

func TestTmux_HelpCommand_InPane(t *testing.T) {
	bin := ensureBinary(t)
	h := newTmuxHelper(t)
	h.startSession("help-test", 120, 40)

	h.sendKeys("help-test",
		fmt.Sprintf("%s --help", bin),
		"Enter")

	found := h.waitForText("help-test", "terminal UI", 5000000000)
	assert.True(t, found, "expected help text in pane output")
}
