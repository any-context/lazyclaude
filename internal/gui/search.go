package gui

import (
	"fmt"
	"strings"

	"github.com/any-context/lazyclaude/internal/gui/presentation"
	"github.com/jesseduffield/gocui"
)

const searchInputView = "search-input"
const filterIndicatorView = "filter-indicator"

// layoutSearchInput creates or updates the inline search input at the bottom
// of the active panel. Called from layoutMain when DialogSearch is active.
func (a *App) layoutSearchInput(g *gocui.Gui, panelRect Rect) error {
	// Search input sits at the bottom of the panel, 1 row high.
	x0 := panelRect.X0 + 1
	x1 := panelRect.X1 - 1
	y0 := panelRect.Y1 - 2
	y1 := panelRect.Y1
	if y0 <= panelRect.Y0+1 {
		// Panel too small for search input.
		return nil
	}

	v, err := g.SetView(searchInputView, x0, y0, x1, y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v.Frame = false
	v.Editable = true
	v.Editor = &searchInputEditor{app: a}

	// Render the search bar content: "/" prefix + query.
	v.Clear()
	query := a.dialog.SearchQuery
	fmt.Fprintf(v, "%s/%s %s%s",
		presentation.FgCyan, presentation.Reset,
		query,
		presentation.Dim+"_"+presentation.Reset)

	g.SetViewOnTop(searchInputView)
	return nil
}

// layoutFilterIndicator creates a read-only indicator at the bottom of the
// panel showing the active filter (e.g. "/query"). Displayed after Enter
// confirms a search so the user knows a filter is still applied.
func (a *App) layoutFilterIndicator(g *gocui.Gui, panelRect Rect) error {
	x0 := panelRect.X0 + 1
	x1 := panelRect.X1 - 1
	y0 := panelRect.Y1 - 2
	y1 := panelRect.Y1
	if y0 <= panelRect.Y0+1 {
		return nil
	}

	v, err := g.SetView(filterIndicatorView, x0, y0, x1, y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v.Frame = false
	v.Editable = false

	v.Clear()
	fmt.Fprintf(v, "%s/%s %s%s%s",
		presentation.FgCyan, presentation.Reset,
		presentation.Dim, a.dialog.ActiveFilter, presentation.Reset)

	g.SetViewOnTop(filterIndicatorView)
	return nil
}

// closeSearch removes the search input view and restores state.
// If cancel is true, restores the pre-search cursor position and clears filter.
// If cancel is false (Enter), persists the filter so items stay filtered.
func (a *App) closeSearch(g *gocui.Gui, cancel bool) {
	resyncSessions := false
	if cancel {
		// Restore cursor positions from before search.
		switch a.dialog.SearchPanel {
		case "sessions":
			a.cursor = a.dialog.SearchPreCursor
			// Defer the plugin/MCP re-sync until AFTER the search
			// query and active filter are cleared below. If we
			// called syncPluginProject right here, currentNode()
			// would still see the filtered tree and the restored
			// pre-search cursor index may lie outside the filtered
			// range, producing a nil node and leaving the panel
			// state unchanged.
			resyncSessions = true
		case "plugins":
			a.pluginState.SetCursor(a.dialog.SearchPreCursor)
		case "logs":
			a.logs.cursorY = a.dialog.SearchPreCursor
		}
		// Clear any active filter for this panel.
		a.dialog.ActiveFilter = ""
		a.dialog.ActiveFilterPanel = ""
	} else {
		// Enter: persist the filter so items stay filtered after dialog closes.
		if a.dialog.SearchQuery != "" {
			a.dialog.ActiveFilter = a.dialog.SearchQuery
			a.dialog.ActiveFilterPanel = a.dialog.SearchPanel
		} else {
			// Empty query = clear filter.
			a.dialog.ActiveFilter = ""
			a.dialog.ActiveFilterPanel = ""
		}
	}

	a.dialog.Kind = DialogNone
	a.dialog.SearchQuery = ""
	a.dialog.SearchPanel = ""
	a.dialog.SearchPreCursor = 0

	// Now that the filter state is fully cleared, re-sync the
	// plugin/MCP panels to whatever the restored cursor resolves to.
	// See comment above — this must happen after the query/filter
	// reset so currentNode() reads the unfiltered tree.
	if resyncSessions {
		a.syncPluginProject()
	}

	g.DeleteView(searchInputView)
	g.Cursor = false

	// Restore focus to the panel.
	panelName := a.panelManager.ActivePanel().Name()
	if _, err := g.SetCurrentView(panelName); err != nil && !isUnknownView(err) {
		_ = err
	}
}

// clearActiveFilter clears the persisted filter for the given panel.
func (a *App) clearActiveFilter(panel string) {
	if a.dialog.ActiveFilterPanel == panel {
		a.dialog.ActiveFilter = ""
		a.dialog.ActiveFilterPanel = ""
	}
}

// searchInputEditor handles text input in the search filter field.
// On each keystroke it re-filters the active panel's content.
type searchInputEditor struct {
	app *App
}

func (e *searchInputEditor) Edit(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
	switch {
	case key == gocui.KeyBackspace || key == gocui.KeyBackspace2:
		if e.app.dialog.SearchQuery == "" {
			return true
		}
		q := e.app.dialog.SearchQuery
		// Remove last rune.
		runes := []rune(q)
		e.app.dialog.SearchQuery = string(runes[:len(runes)-1])
	case key == gocui.KeySpace:
		e.app.dialog.SearchQuery += " "
	case ch != 0 && mod == gocui.ModNone:
		e.app.dialog.SearchQuery += string(ch)
	default:
		return false
	}

	e.app.applySearchFilter()
	return true
}

// applySearchFilter applies the current search query to the active panel.
func (a *App) applySearchFilter() {
	switch a.dialog.SearchPanel {
	case "sessions":
		// Filtering is applied during renderTree in layoutMain.
		// Reset cursor to 0 when query changes.
		filtered := a.filteredTreeNodes()
		if len(filtered) > 0 {
			a.cursor = 0
		}
		// Re-sync the plugin/MCP panels so their remoteDisabled flags
		// (and any cached project context) match the filtered cursor.
		// Without this, searching from a remote selection into a local
		// match would leave the plugin/MCP panels showing the stale
		// "remote disabled" placeholder even though the new cursor is
		// local. The write-guard handles this at the key-press layer,
		// but the render layer also needs to refresh for the UI to be
		// coherent.
		a.syncPluginProject()
	case "plugins":
		a.pluginState.SetCursor(0)
	case "logs":
		a.logs.cursorY = 0
	}
}

// effectiveQuery returns the active filter query for the given panel.
// During a live search (DialogSearch), returns the live query.
// After Enter confirmation, returns the persisted ActiveFilter.
// Returns "" if no filter is active for this panel.
func (a *App) effectiveQuery(panel string) string {
	if a.dialog.Kind == DialogSearch && a.dialog.SearchPanel == panel {
		return a.dialog.SearchQuery
	}
	if a.dialog.ActiveFilterPanel == panel {
		return a.dialog.ActiveFilter
	}
	return ""
}

// filteredTreeNodes returns tree nodes filtered by the search query.
// If no search is active, returns all nodes.
func (a *App) filteredTreeNodes() []TreeNode {
	nodes := a.cachedNodes
	q := a.effectiveQuery("sessions")
	if q == "" {
		return nodes
	}
	return filterTreeNodes(nodes, q)
}

// filterTreeNodes filters tree nodes by case-insensitive substring match
// on project name or session name. Uses a two-pass approach:
//  1. Identify which projects have matching children or match themselves.
//  2. Include matching project headers and their matching sessions.
//
// This ensures session nodes are never shown without their parent project header.
func filterTreeNodes(nodes []TreeNode, query string) []TreeNode {
	q := strings.ToLower(query)

	// Pass 1: collect project IDs that should be included, and track
	// whether the project itself matched (vs only its children matching).
	type projectMatch struct {
		included    bool // project should appear in results
		nameMatched bool // project name itself matched the query
	}
	projects := make(map[string]*projectMatch)
	for _, node := range nodes {
		switch node.Kind {
		case ProjectNode:
			if node.Project != nil && strings.Contains(strings.ToLower(node.Project.Name), q) {
				projects[node.ProjectID] = &projectMatch{included: true, nameMatched: true}
			}
		case SessionNode:
			if node.Session != nil && strings.Contains(strings.ToLower(node.Session.Name), q) {
				pm := projects[node.ProjectID]
				if pm == nil {
					pm = &projectMatch{}
					projects[node.ProjectID] = pm
				}
				pm.included = true
			}
		}
	}

	// Pass 2: build result with project headers and matching sessions.
	var result []TreeNode
	for _, node := range nodes {
		pm := projects[node.ProjectID]
		if pm == nil || !pm.included {
			continue
		}
		switch node.Kind {
		case ProjectNode:
			result = append(result, node)
		case SessionNode:
			// If the project name matched, include all its sessions.
			// Otherwise, only include sessions whose name matches.
			if pm.nameMatched {
				result = append(result, node)
			} else if node.Session != nil && strings.Contains(strings.ToLower(node.Session.Name), q) {
				result = append(result, node)
			}
		}
	}
	return result
}

// filterLogLines filters log lines by case-insensitive substring match.
func filterLogLines(lines []string, query string) []string {
	if query == "" {
		return lines
	}
	q := strings.ToLower(query)
	var result []string
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), q) {
			result = append(result, line)
		}
	}
	return result
}

// filteredLogLines returns log lines filtered by search query.
func (a *App) filteredLogLines() []string {
	lines := a.readLogLines()
	q := a.effectiveQuery("logs")
	if q == "" {
		return lines
	}
	return filterLogLines(lines, q)
}

// filteredInstalledPlugins returns installed plugins filtered by search query.
func (a *App) filteredInstalledPlugins() []PluginItem {
	if a.plugins == nil {
		return nil
	}
	installed := a.plugins.Installed()
	q := a.effectiveQuery("plugins")
	if q == "" {
		return installed
	}
	return filterPluginItems(installed, q)
}

// filterPluginItems filters installed plugins by case-insensitive substring match on ID.
func filterPluginItems(items []PluginItem, query string) []PluginItem {
	q := strings.ToLower(query)
	var result []PluginItem
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.ID), q) {
			result = append(result, item)
		}
	}
	return result
}

// filteredAvailablePlugins returns marketplace plugins filtered by search query.
func (a *App) filteredAvailablePlugins() []AvailablePluginItem {
	if a.plugins == nil {
		return nil
	}
	available := a.plugins.Available()
	q := a.effectiveQuery("plugins")
	if q == "" {
		return available
	}
	return filterAvailablePlugins(available, q)
}

// filterAvailablePlugins filters marketplace plugins by case-insensitive
// substring match on Name or Description.
func filterAvailablePlugins(items []AvailablePluginItem, query string) []AvailablePluginItem {
	q := strings.ToLower(query)
	var result []AvailablePluginItem
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Name), q) ||
			strings.Contains(strings.ToLower(item.Description), q) {
			result = append(result, item)
		}
	}
	return result
}

// filteredMCPServers returns MCP servers filtered by search query.
func (a *App) filteredMCPServers() []MCPItem {
	if a.mcpServers == nil {
		return nil
	}
	servers := a.mcpServers.Servers()
	q := a.effectiveQuery("plugins")
	if q == "" {
		return servers
	}
	return filterMCPItems(servers, q)
}

// filterMCPItems filters MCP items by case-insensitive substring match on Name.
func filterMCPItems(items []MCPItem, query string) []MCPItem {
	q := strings.ToLower(query)
	var result []MCPItem
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Name), q) {
			result = append(result, item)
		}
	}
	return result
}
