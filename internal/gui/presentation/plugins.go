package presentation

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// FormatInstalledLine renders a line for an installed plugin.
// Layout: icon(3) + " " + name + "  " + version + "  " + scope
func FormatInstalledLine(id, version, scope string, enabled bool, maxWidth int) string {
	var icon string
	if enabled {
		icon = FgGreen + "[E]" + Reset
	} else {
		icon = FgYellow + "[D]" + Reset
	}

	rightStr := Dim + version + Reset + "  " + Dim + scope + Reset
	rightWidth := ansi.StringWidth(version) + 2 + ansi.StringWidth(scope)

	nameWidth := maxWidth - 4 - rightWidth - 2
	if nameWidth < 8 {
		nameWidth = 8
	}

	displayName := id
	if ansi.StringWidth(displayName) > nameWidth {
		displayName = ansi.Truncate(displayName, nameWidth-1, "") + "~"
	}
	pad := nameWidth - ansi.StringWidth(displayName)
	if pad < 0 {
		pad = 0
	}

	return fmt.Sprintf("%s %s%s  %s", icon, displayName, strings.Repeat(" ", pad), rightStr)
}

// FormatAvailableLine renders a line for a marketplace plugin.
func FormatAvailableLine(name, description string, installCount int, installed bool, maxWidth int) string {
	var prefix string
	if installed {
		prefix = FgGreen + "[I]" + Reset + " "
	} else {
		prefix = "    "
	}

	countStr := fmt.Sprintf("%d", installCount)
	right := Dim + countStr + Reset
	rightWidth := ansi.StringWidth(countStr)

	nameDescWidth := maxWidth - 4 - rightWidth - 2
	if nameDescWidth < 8 {
		nameDescWidth = 8
	}

	nameStr := Bold + name + Reset
	nameVisWidth := ansi.StringWidth(name)

	descWidth := nameDescWidth - nameVisWidth - 2
	descStr := ""
	if description != "" && descWidth > 3 {
		if ansi.StringWidth(description) > descWidth {
			descStr = Dim + ansi.Truncate(description, descWidth-1, "") + "~" + Reset
		} else {
			descStr = Dim + description + Reset
		}
	}

	visibleWidth := nameVisWidth
	if descStr != "" {
		visibleWidth += 2 + min(ansi.StringWidth(description), descWidth)
	}
	pad := nameDescWidth - visibleWidth
	if pad < 0 {
		pad = 0
	}

	if descStr != "" {
		return fmt.Sprintf("%s%s  %s%s  %s", prefix, nameStr, descStr, strings.Repeat(" ", pad), right)
	}
	return fmt.Sprintf("%s%s%s  %s", prefix, nameStr, strings.Repeat(" ", pad), right)
}

// FormatPluginPreview renders detailed plugin information for the preview pane.
func FormatPluginPreview(id, version, scope, installedAt string, enabled bool) string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  %s%s%s\n", Bold, id, Reset))
	sb.WriteString("\n")

	status := FgGreen + "Enabled" + Reset
	if !enabled {
		status = FgYellow + "Disabled" + Reset
	}

	sb.WriteString(fmt.Sprintf("  %sStatus:%s   %s\n", Dim, Reset, status))
	sb.WriteString(fmt.Sprintf("  %sVersion:%s  %s\n", Dim, Reset, version))
	sb.WriteString(fmt.Sprintf("  %sScope:%s    %s\n", Dim, Reset, scope))

	if installedAt != "" {
		date := installedAt
		if idx := strings.IndexByte(date, 'T'); idx > 0 {
			date = date[:idx]
		}
		sb.WriteString(fmt.Sprintf("  %sInstalled:%s %s\n", Dim, Reset, date))
	}

	return sb.String()
}

// FormatAvailablePreview renders detailed info for a marketplace plugin.
func FormatAvailablePreview(pluginID, name, description, marketplaceName string, installCount int) string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  %s%s%s\n", Bold, name, Reset))
	sb.WriteString("\n")

	if description != "" {
		sb.WriteString(fmt.Sprintf("  %s\n\n", description))
	}

	sb.WriteString(fmt.Sprintf("  %sMarketplace:%s %s\n", Dim, Reset, marketplaceName))
	sb.WriteString(fmt.Sprintf("  %sInstalls:%s    %d\n", Dim, Reset, installCount))
	sb.WriteString(fmt.Sprintf("  %sPlugin ID:%s   %s\n", Dim, Reset, pluginID))

	return sb.String()
}
