package gui

import (
	"fmt"

	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/jesseduffield/gocui"
)

// renderMCPList renders the MCP servers list view.
func (a *App) renderMCPList(v *gocui.View, maxWidth int, focused bool) {
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

	if a.mcpState.errMsg != "" {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.FgYellow+"  "+a.mcpState.errMsg+presentation.Reset)
		return
	}

	servers := a.mcpServers.Servers()
	if len(servers) == 0 {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  No MCP servers configured"+presentation.Reset)
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
	if a.mcpServers == nil || a.mcpState.loading {
		v.Title = " Preview "
		return
	}

	servers := a.mcpServers.Servers()
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
