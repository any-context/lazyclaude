package gui

import (
	"fmt"
	"strings"

	"github.com/any-context/lazyclaude/internal/gui/keymap"
	"github.com/any-context/lazyclaude/internal/gui/presentation"
	"github.com/jesseduffield/gocui"
)

// renderPluginPanel renders the plugins list view with tab header.
//
// NOTE: Do not early-return on pluginState.remoteDisabled at the top of
// this function. The MCP tab dispatches to renderMCPList which carries
// its own mcpState.remoteDisabled guard; a blanket early return would
// make that guard dead code and paint plugin-flavoured text over the
// MCP tab. The remote-disabled placeholder is rendered per-tab below.
func (a *App) renderPluginPanel(v *gocui.View, maxWidth int) {
	// Use gocui native Tabs API for consistent tab rendering.
	v.Tabs = keymap.PluginTabLabels()
	v.TabIndex = a.pluginState.tabIdx
	v.SelFgColor = gocui.ColorWhite

	focused := a.panelManager.ActivePanel().Name() == "plugins"

	switch a.pluginState.tabIdx {
	case keymap.PluginTabMCP:
		// MCP tab has its own mcpState.remoteDisabled guard inside
		// renderMCPList; do not short-circuit here.
		a.renderMCPList(v, maxWidth, focused)
		return
	case keymap.PluginTabPlugins:
		// fall through to plugin rendering below
	case keymap.PluginTabMarketplace:
		if a.pluginState.remoteDisabled {
			renderRemoteDisabledPlaceholder(v, "Plugin editing on remote hosts is not supported in this build.\n\nSwitch cursor to a local session to manage plugins.")
			return
		}
		a.renderAvailableList(v, maxWidth, focused)
		return
	}

	// Tab 1: Plugins (installed)
	if a.pluginState.remoteDisabled {
		renderRemoteDisabledPlaceholder(v, "Plugin editing on remote hosts is not supported in this build.\n\nSwitch cursor to a local session to manage plugins.")
		return
	}
	if a.pluginState.loading {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  Loading..."+presentation.Reset)
		return
	}

	if a.plugins == nil {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  No plugin provider"+presentation.Reset)
		return
	}

	a.renderInstalledList(v, maxWidth, focused)
}

// renderRemoteDisabledPlaceholder renders a dim multiline placeholder for
// the plugin/MCP panels when the cursor is on a remote session. The caller
// supplies the message so plugin and MCP panels share a single helper.
//
// Intentionally a free function (no *App receiver): the placeholder has no
// state dependency on the App. Keeping it free-standing also makes it
// trivially callable from both render_plugins.go and render_mcp.go without
// a circular dispatch. Do not add an (a *App) receiver unless new state is
// actually required.
//
// The helper resets BOTH the view cursor and the view origin to (0, 0).
// Origin reset is the critical bit — without it, a previously-scrolled
// plugin/MCP list keeps the old y-offset and the placeholder ends up
// rendered off-screen or partly clipped. Cursor reset is a follow-up so
// the invisible selection marker does not stick to a stale row. The
// helper never invokes scrollToCursor because the placeholder has no
// "items" to navigate.
func renderRemoteDisabledPlaceholder(v *gocui.View, msg string) {
	fmt.Fprintln(v, "")
	for _, line := range strings.Split(msg, "\n") {
		fmt.Fprintln(v, "  "+presentation.Dim+line+presentation.Reset)
	}
	v.SetOrigin(0, 0)
	v.SetCursor(0, 0)
}

func (a *App) renderInstalledList(v *gocui.View, maxWidth int, focused bool) {
	installed := a.filteredInstalledPlugins()
	if len(installed) == 0 {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  No plugins installed"+presentation.Reset)
		return
	}

	for _, p := range installed {
		line := presentation.FormatInstalledLine(p.ID, p.Version, p.Scope, p.Enabled, maxWidth)
		fmt.Fprintln(v, line)
	}

	if focused {
		scrollToCursor(v, a.pluginState.installedCursor)
	} else {
		v.SetCursor(0, a.pluginState.installedCursor)
	}
}

func (a *App) renderAvailableList(v *gocui.View, maxWidth int, focused bool) {
	available := a.filteredAvailablePlugins()
	if len(available) == 0 {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  No plugins available"+presentation.Reset)
		return
	}

	installedSet := a.buildInstalledSet()
	for _, p := range available {
		_, isInstalled := installedSet[p.PluginID]
		line := presentation.FormatAvailableLine(p.Name, p.Description, p.InstallCount, isInstalled, maxWidth)
		fmt.Fprintln(v, line)
	}

	if focused {
		scrollToCursor(v, a.pluginState.marketCursor)
	} else {
		v.SetCursor(0, a.pluginState.marketCursor)
	}
}

// renderPluginPreview renders the right panel when plugins panel is focused.
//
// NOTE: mirrors renderPluginPanel — do not early-return on remoteDisabled
// at the top. The MCP tab dispatches to renderMCPPreview which has its
// own mcpState.remoteDisabled guard.
func (a *App) renderPluginPreview(v *gocui.View) {
	switch a.pluginState.tabIdx {
	case keymap.PluginTabMCP:
		a.renderMCPPreview(v)
		return
	case keymap.PluginTabPlugins:
		if a.pluginState.remoteDisabled {
			v.Title = " Preview "
			fmt.Fprintln(v, "")
			fmt.Fprintln(v, presentation.Dim+"  Remote session — plugin editing not supported"+presentation.Reset)
			return
		}
		if a.plugins == nil || a.pluginState.loading {
			v.Title = " Preview "
			return
		}
		installed := a.filteredInstalledPlugins()
		if a.pluginState.installedCursor < len(installed) {
			p := installed[a.pluginState.installedCursor]
			v.Title = fmt.Sprintf(" %s ", pluginDisplayName(p.ID))
			fmt.Fprint(v, presentation.FormatPluginPreview(p.ID, p.Version, p.Scope, p.InstalledAt, p.Enabled))
			return
		}
	case keymap.PluginTabMarketplace:
		if a.pluginState.remoteDisabled {
			v.Title = " Preview "
			fmt.Fprintln(v, "")
			fmt.Fprintln(v, presentation.Dim+"  Remote session — plugin editing not supported"+presentation.Reset)
			return
		}
		if a.plugins == nil || a.pluginState.loading {
			v.Title = " Preview "
			return
		}
		available := a.filteredAvailablePlugins()
		if a.pluginState.marketCursor < len(available) {
			p := available[a.pluginState.marketCursor]
			v.Title = fmt.Sprintf(" %s ", p.Name)
			fmt.Fprint(v, presentation.FormatAvailablePreview(p.PluginID, p.Name, p.Description, p.MarketplaceName, p.InstallCount))
			return
		}
	}

	v.Title = " Preview "
	fmt.Fprintln(v, "")
	fmt.Fprintln(v, presentation.Dim+"  Select an item to view details"+presentation.Reset)
}

func pluginDisplayName(id string) string {
	if idx := strings.IndexByte(id, '@'); idx > 0 {
		return id[:idx]
	}
	return id
}

func (a *App) buildInstalledSet() map[string]struct{} {
	installed := a.plugins.Installed()
	set := make(map[string]struct{}, len(installed))
	for _, p := range installed {
		set[p.ID] = struct{}{}
	}
	return set
}
