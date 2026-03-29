package presentation

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// FormatMCPLine renders a line for an MCP server in the list view.
func FormatMCPLine(name, serverType, scope string, denied bool, maxWidth int) string {
	var icon string
	if denied {
		icon = FgYellow + "[D]" + Reset
	} else {
		icon = FgGreen + "[E]" + Reset
	}

	right := Dim + scope + Reset
	rightWidth := ansi.StringWidth(scope)

	typeStr := Dim + serverType + Reset
	typeWidth := ansi.StringWidth(serverType)

	nameWidth := maxWidth - 4 - typeWidth - 2 - rightWidth - 2
	if nameWidth < 8 {
		nameWidth = 8
	}

	displayName := name
	if ansi.StringWidth(displayName) > nameWidth {
		displayName = ansi.Truncate(displayName, nameWidth-1, "") + "~"
	}
	pad := nameWidth - ansi.StringWidth(displayName)
	if pad < 0 {
		pad = 0
	}

	return fmt.Sprintf("%s %s%s  %s  %s", icon, displayName, strings.Repeat(" ", pad), typeStr, right)
}

// FormatMCPPreview renders detailed MCP server information for the preview pane.
func FormatMCPPreview(name, serverType, scope string, denied bool, command string, args []string, url string) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "\n")
	fmt.Fprintf(&sb, "  %s%s%s\n", Bold, name, Reset)
	fmt.Fprintf(&sb, "\n")

	status := FgGreen + "Enabled" + Reset
	if denied {
		status = FgYellow + "Disabled" + Reset
	}

	fmt.Fprintf(&sb, "  %sStatus:%s    %s\n", Dim, Reset, status)
	fmt.Fprintf(&sb, "  %sType:%s      %s\n", Dim, Reset, serverType)
	fmt.Fprintf(&sb, "  %sScope:%s     %s\n", Dim, Reset, scope)

	if command != "" {
		cmdLine := command
		if len(args) > 0 {
			cmdLine += " " + strings.Join(args, " ")
		}
		fmt.Fprintf(&sb, "  %sCommand:%s   %s\n", Dim, Reset, cmdLine)
	}

	if url != "" {
		fmt.Fprintf(&sb, "  %sURL:%s       %s\n", Dim, Reset, url)
	}

	return sb.String()
}
