package gui

import (
	"fmt"

	"github.com/any-context/lazyclaude/internal/gui/presentation"
	"github.com/jesseduffield/gocui"
)

// renderMCPList renders the MCP servers list view.
func (a *App) renderMCPList(v *gocui.View, maxWidth int, focused bool) {
	if a.mcpState.remoteDisabled {
		renderRemoteDisabledPlaceholder(v, "MCP editing on remote hosts is not supported in this build.\n\nSwitch cursor to a local session to manage MCP servers.")
		return
	}
	if a.mcpServers == nil {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  No MCP provider"+presentation.Reset)
		return
	}

	if a.mcpState.loading {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  Loading..."+presentation.Reset)
		return
	}

	servers := a.filteredMCPServers()
	if len(servers) == 0 {
		fmt.Fprintln(v, "")
		if a.dialog.Kind == DialogSearch && a.dialog.SearchQuery != "" {
			fmt.Fprintln(v, presentation.Dim+"  No matches"+presentation.Reset)
		} else {
			fmt.Fprintln(v, presentation.Dim+"  No MCP servers configured"+presentation.Reset)
		}
		return
	}

	for _, s := range servers {
		line := presentation.FormatMCPLine(s.Name, s.Type, s.Scope, s.Denied, maxWidth)
		fmt.Fprintln(v, line)
	}

	if focused {
		scrollToCursor(v, a.mcpState.cursor)
	} else {
		v.SetCursor(0, a.mcpState.cursor)
	}
}

// renderMCPPreview renders the right panel when MCP tab is active.
func (a *App) renderMCPPreview(v *gocui.View) {
	if a.mcpState.remoteDisabled {
		v.Title = " Preview "
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  Remote session — MCP editing not supported"+presentation.Reset)
		return
	}
	if a.mcpServers == nil || a.mcpState.loading {
		v.Title = " Preview "
		return
	}

	servers := a.filteredMCPServers()
	if a.mcpState.cursor < len(servers) {
		s := servers[a.mcpState.cursor]
		v.Title = fmt.Sprintf(" %s ", s.Name)
		fmt.Fprint(v, presentation.FormatMCPPreview(
			s.Name, s.Type, s.Scope, s.Denied,
			s.Command, s.Args, s.URL,
		))
		return
	}

	v.Title = " Preview "
	fmt.Fprintln(v, "")
	fmt.Fprintln(v, presentation.Dim+"  Select a server to view details"+presentation.Reset)
}
