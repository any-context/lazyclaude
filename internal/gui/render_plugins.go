package gui

import (
	"fmt"
	"strings"

	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/jesseduffield/gocui"
)

// renderPluginPanel renders the plugins list view with tab header.
func (a *App) renderPluginPanel(v *gocui.View, maxWidth int) {
	// Use gocui native Tabs API for consistent tab rendering.
	// TitlePrefix renders before tabs: "╭─ Plugins ─ Installed - Marketplace ──╮"
	v.Tabs = []string{"MCP", "Plugins", "Marketplace"}
	v.TabIndex = a.pluginState.tabIdx
	v.SelFgColor = gocui.ColorWhite

	focused := a.panelManager.ActivePanel().Name() == "plugins"

	switch a.pluginState.tabIdx {
	case 0: // MCP
		a.renderMCPList(v, maxWidth, focused)
		return
	case 1: // Plugins
		// fall through to plugin rendering below
	case 2: // Marketplace
		a.renderAvailableList(v, maxWidth, focused)
		return
	}

	// Tab 1: Plugins (installed)
	if a.pluginState.loading {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  Loading..."+presentation.Reset)
		return
	}

	if a.pluginState.errMsg != "" {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.FgYellow+"  "+a.pluginState.errMsg+presentation.Reset)
		return
	}

	if a.plugins == nil {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  No plugin provider"+presentation.Reset)
		return
	}

	a.renderInstalledList(v, maxWidth, focused)
}

func (a *App) renderInstalledList(v *gocui.View, maxWidth int, focused bool) {
	installed := a.plugins.Installed()
	if len(installed) == 0 {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  No plugins installed"+presentation.Reset)
		return
	}

	for _, p := range installed {
		line := presentation.FormatInstalledLine(p.ID, p.Version, p.Enabled, maxWidth)
		fmt.Fprintln(v, line)
	}

	if focused {
		scrollToCursor(v, a.pluginState.installedCursor)
	} else {
		v.SetCursor(0, a.pluginState.installedCursor)
	}
}

func (a *App) renderAvailableList(v *gocui.View, maxWidth int, focused bool) {
	available := a.plugins.Available()
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
func (a *App) renderPluginPreview(v *gocui.View) {
	switch a.pluginState.tabIdx {
	case 0: // MCP
		a.renderMCPPreview(v)
		return
	case 1: // Plugins
		if a.plugins == nil || a.pluginState.loading {
			v.Title = " Preview "
			return
		}
		installed := a.plugins.Installed()
		if a.pluginState.installedCursor < len(installed) {
			p := installed[a.pluginState.installedCursor]
			v.Title = fmt.Sprintf(" %s ", pluginDisplayName(p.ID))
			fmt.Fprint(v, presentation.FormatPluginPreview(p.ID, p.Version, p.Scope, p.InstalledAt, p.Enabled))
			return
		}
	case 2: // Marketplace
		if a.plugins == nil || a.pluginState.loading {
			v.Title = " Preview "
			return
		}
		available := a.plugins.Available()
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
