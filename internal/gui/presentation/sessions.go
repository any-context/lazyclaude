package presentation

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// FormatSessionLine renders a single session line for the list view.
// Accepts primitive fields to avoid a dependency on the session package.
// Format: "name          status_indicator flags"
//
// Note: worktree [W] icon is NOT added here — it is handled by renderSessionList
// in render.go, which is the actual live render path. This function is used
// only for tests and external formatting.
func FormatSessionLine(name, status, host string, flags []string, maxWidth int) string {
	icon := statusIndicator(status)
	flagStr := formatFlags(flags)

	// Build right-side indicators
	right := strings.TrimSpace(icon + " " + flagStr)

	// Prepend host if present
	displayName := name
	if host != "" {
		displayName = host + ":" + name
	}

	// Use visual width (ignoring ANSI escapes) for padding/truncation.
	rightWidth := ansi.StringWidth(right)
	nameWidth := maxWidth - rightWidth - 2 // 2 for spacing
	if nameWidth < 5 {
		nameWidth = 5
	}

	// Truncate name if needed (visual width)
	displayWidth := ansi.StringWidth(displayName)
	if displayWidth > nameWidth {
		displayName = ansi.Truncate(displayName, nameWidth-1, "") + "~"
		displayWidth = ansi.StringWidth(displayName)
	}

	if right == "" {
		return displayName
	}
	padding := nameWidth - displayWidth
	if padding < 1 {
		padding = 1
	}
	return displayName + strings.Repeat(" ", padding) + right
}

// FormatSessionLines renders all sessions for the list view.
// Each session is described by parallel slices of names, statuses, hosts, and flags.
func FormatSessionLines(names, statuses, hosts []string, flags [][]string, maxWidth int) []string {
	lines := make([]string, len(names))
	for i, name := range names {
		var f []string
		if i < len(flags) {
			f = flags[i]
		}
		var host string
		if i < len(hosts) {
			host = hosts[i]
		}
		var status string
		if i < len(statuses) {
			status = statuses[i]
		}
		lines[i] = FormatSessionLine(name, status, host, f, maxWidth)
	}
	return lines
}

func statusIndicator(status string) string {
	switch status {
	case "Running":
		return IconRunning
	case "Dead":
		return IconDead
	case "Orphan":
		return IconOrphan
	case "Detached":
		return IconDetached
	default:
		return IconUnknown
	}
}

func formatFlags(flags []string) string {
	var parts []string
	for _, f := range flags {
		switch f {
		case "--resume":
			parts = append(parts, "R")
		default:
			// skip unknown flags
		}
	}
	return strings.Join(parts, "")
}

// ServerStatusLine formats a server status line.
func ServerStatusLine(port int, connCount int, uptime string) string {
	return fmt.Sprintf("MCP: listening :%d  |  Connections: %d  |  %s", port, connCount, uptime)
}
