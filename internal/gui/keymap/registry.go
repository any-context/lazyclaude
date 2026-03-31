package keymap

import "github.com/jesseduffield/gocui"

// Registry is the single source of truth for all key bindings.
// Created once at startup and injected into all handlers via DI.
type Registry struct {
	defs []ActionDef
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds an action definition to the registry.
func (r *Registry) Register(def ActionDef) {
	r.defs = append(r.defs, def)
}

// Match finds an action matching the key event in the given scope.
func (r *Registry) Match(ch rune, key gocui.Key, mod gocui.Modifier, scope Scope) (ActionDef, bool) {
	for _, def := range r.defs {
		if def.Scope != scope {
			continue
		}
		for _, b := range def.Bindings {
			if b.Matches(key, ch, mod) {
				return def, true
			}
		}
	}
	return ActionDef{}, false
}

// MatchTab finds an action matching the key event in the given scope and tab.
// Actions with Tab == TabAll match any tab.
func (r *Registry) MatchTab(ch rune, key gocui.Key, mod gocui.Modifier, scope Scope, tab int) (ActionDef, bool) {
	for _, def := range r.defs {
		if def.Scope != scope {
			continue
		}
		if def.Tab != TabAll && def.Tab != tab {
			continue
		}
		for _, b := range def.Bindings {
			if b.Matches(key, ch, mod) {
				return def, true
			}
		}
	}
	return ActionDef{}, false
}

// HintsForScope returns all actions with non-empty HintLabel for the given scope,
// in registration order. Used to generate the options bar.
func (r *Registry) HintsForScope(scope Scope) []ActionDef {
	var result []ActionDef
	for _, def := range r.defs {
		if def.Scope == scope && def.HintLabel != "" {
			result = append(result, def)
		}
	}
	return result
}

// HintsForScopeTab returns hints filtered by scope and tab index.
// Actions with Tab == TabAll are included for any tab.
func (r *Registry) HintsForScopeTab(scope Scope, tab int) []ActionDef {
	var result []ActionDef
	for _, def := range r.defs {
		if def.Scope != scope || def.HintLabel == "" {
			continue
		}
		if def.Tab != TabAll && def.Tab != tab {
			continue
		}
		result = append(result, def)
	}
	return result
}

// BindingsForScopeTab returns all actions for a given scope and tab,
// including those with empty HintLabel. Actions with Tab == TabAll match any tab.
func (r *Registry) BindingsForScopeTab(scope Scope, tab int) []ActionDef {
	var result []ActionDef
	for _, def := range r.defs {
		if def.Scope != scope {
			continue
		}
		if def.Tab != TabAll && def.Tab != tab {
			continue
		}
		result = append(result, def)
	}
	return result
}

// BindingsForScope returns all actions registered under the given scope.
func (r *Registry) BindingsForScope(scope Scope) []ActionDef {
	var result []ActionDef
	for _, def := range r.defs {
		if def.Scope == scope {
			result = append(result, def)
		}
	}
	return result
}

// Runes returns all unique rune keys registered across all scopes.
// Used by the GUI layer to register gocui keybindings without manual lists.
func (r *Registry) Runes() []rune {
	seen := make(map[rune]bool)
	var result []rune
	for _, def := range r.defs {
		for _, b := range def.Bindings {
			if b.Rune != 0 && !seen[b.Rune] {
				seen[b.Rune] = true
				result = append(result, b.Rune)
			}
		}
	}
	return result
}

// SpecialKeys returns all unique special (non-rune) keys registered across all scopes.
func (r *Registry) SpecialKeys() []gocui.Key {
	seen := make(map[gocui.Key]bool)
	var result []gocui.Key
	for _, def := range r.defs {
		for _, b := range def.Bindings {
			if b.Rune == 0 && !seen[b.Key] {
				seen[b.Key] = true
				result = append(result, b.Key)
			}
		}
	}
	return result
}

// RunesForScope returns all unique rune keys registered under the given scope.
func (r *Registry) RunesForScope(scope Scope) []rune {
	seen := make(map[rune]bool)
	var result []rune
	for _, def := range r.defs {
		if def.Scope != scope {
			continue
		}
		for _, b := range def.Bindings {
			if b.Rune != 0 && !seen[b.Rune] {
				seen[b.Rune] = true
				result = append(result, b.Rune)
			}
		}
	}
	return result
}

// SpecialKeysForScope returns all unique special keys registered under the given scope.
func (r *Registry) SpecialKeysForScope(scope Scope) []gocui.Key {
	seen := make(map[gocui.Key]bool)
	var result []gocui.Key
	for _, def := range r.defs {
		if def.Scope != scope {
			continue
		}
		for _, b := range def.Bindings {
			if b.Rune == 0 && !seen[b.Key] {
				seen[b.Key] = true
				result = append(result, b.Key)
			}
		}
	}
	return result
}

// AllActions returns all registered actions in registration order.
func (r *Registry) AllActions() []ActionDef {
	result := make([]ActionDef, len(r.defs))
	copy(result, r.defs)
	return result
}

// HintKeyLabel returns the display key for this action's hint.
// If HintKey is set, it is used; otherwise auto-generated from the first binding.
func (def ActionDef) HintKeyLabel() string {
	if def.HintKey != "" {
		return def.HintKey
	}
	if len(def.Bindings) > 0 {
		return def.Bindings[0].HintKey()
	}
	return "?"
}

// Default returns the default lazyclaude key registry with all actions.
func Default() *Registry {
	r := NewRegistry()

	// --- Global ---
	r.Register(ActionDef{
		Action:      ActionQuit,
		Bindings:    []KeyBinding{{Rune: 'q'}},
		Scope:       ScopeGlobal,
		HintLabel:   "quit",
		Description: "Quit lazyclaude",
		DocSection:  "quit",
	})
	r.Register(ActionDef{
		Action:      ActionQuitCtrlC,
		Bindings:    []KeyBinding{{Key: gocui.KeyCtrlC}},
		Scope:       ScopeGlobal,
		Description: "Quit lazyclaude",
		DocSection:  "quit",
	})
	r.Register(ActionDef{
		Action:      ActionQuitCtrlBackslash,
		Bindings:    []KeyBinding{{Key: gocui.KeyCtrlBackslash}},
		Scope:       ScopeGlobal,
		Description: "Quit lazyclaude",
		DocSection:  "quit",
	})
	r.Register(ActionDef{
		Action:      ActionFocusNextPanel,
		Bindings:    []KeyBinding{{Key: gocui.KeyTab}},
		Scope:       ScopeGlobal,
		HintLabel:   "panel",
		Description: "Focus next panel",
		DocSection:  "focus_panel",
	})
	r.Register(ActionDef{
		Action:      ActionFocusPrevPanel,
		Bindings:    []KeyBinding{{Key: gocui.KeyBacktab}},
		Scope:       ScopeGlobal,
		Description: "Focus previous panel",
		DocSection:  "focus_panel",
	})
	r.Register(ActionDef{
		Action:      ActionUnsuspendPopups,
		Bindings:    []KeyBinding{{Rune: 'p'}},
		Scope:       ScopeGlobal,
		HintLabel:   "notif",
		Description: "Show suspended notifications",
		DocSection:  "unsuspend_popups",
	})
	r.Register(ActionDef{
		Action:      ActionPanelNextTab,
		Bindings:    []KeyBinding{{Rune: ']'}},
		Scope:       ScopeGlobal,
		HintLabel:   "tab",
		HintKey:     "[/]",
		Description: "Switch to next tab",
		DocSection:  "panel_tab",
	})
	r.Register(ActionDef{
		Action:      ActionPanelPrevTab,
		Bindings:    []KeyBinding{{Rune: '['}},
		Scope:       ScopeGlobal,
		Description: "Switch to previous tab",
		DocSection:  "panel_tab",
	})

	r.Register(ActionDef{
		Action:      ActionShowKeybindHelp,
		Bindings:    []KeyBinding{{Rune: '?'}},
		Scope:       ScopeGlobal,
		HintLabel:   "help",
		Description: "Show keybind help",
		DocSection:  "show_keybind_help",
	})

	// --- Session panel ---
	r.Register(ActionDef{
		Action:      ActionCursorDown,
		Bindings:    []KeyBinding{{Rune: 'j'}, {Key: gocui.KeyArrowDown}},
		Scope:       ScopeSession,
		Description: "Move cursor down",
		DocSection:  "cursor_move",
	})
	r.Register(ActionDef{
		Action:      ActionCursorUp,
		Bindings:    []KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		Scope:       ScopeSession,
		Description: "Move cursor up",
		DocSection:  "cursor_move",
	})
	r.Register(ActionDef{
		Action:      ActionCollapseProject,
		Bindings:    []KeyBinding{{Rune: 'h'}, {Key: gocui.KeyArrowLeft}},
		Scope:       ScopeSession,
		Description: "Collapse project group",
		DocSection:  "collapse_expand",
	})
	r.Register(ActionDef{
		Action:      ActionExpandProject,
		Bindings:    []KeyBinding{{Rune: 'l'}, {Key: gocui.KeyArrowRight}},
		Scope:       ScopeSession,
		Description: "Expand project group",
		DocSection:  "collapse_expand",
	})
	r.Register(ActionDef{
		Action:      ActionNewSession,
		Bindings:    []KeyBinding{{Rune: 'n'}},
		Scope:       ScopeSession,
		HintLabel:   "new",
		Description: "Create new Claude Code session",
		DocSection:  "new_session",
	})
	r.Register(ActionDef{
		Action:      ActionNewSessionCWD,
		Bindings:    []KeyBinding{{Rune: 'N'}},
		Scope:       ScopeSession,
		HintLabel:   "new[cwd]",
		Description: "Create new session in current directory",
		DocSection:  "new_session",
	})
	r.Register(ActionDef{
		Action:      ActionDeleteSession,
		Bindings:    []KeyBinding{{Rune: 'd'}},
		Scope:       ScopeSession,
		HintLabel:   "del",
		Description: "Delete selected session",
		DocSection:  "delete_session",
	})
	r.Register(ActionDef{
		Action:      ActionAttachSession,
		Bindings:    []KeyBinding{{Rune: 'a'}},
		Scope:       ScopeSession,
		HintLabel:   "attach",
		Description: "Attach to selected session",
		DocSection:  "attach_session",
	})
	r.Register(ActionDef{
		Action:      ActionLaunchLazygit,
		Bindings:    []KeyBinding{{Rune: 'g'}},
		Scope:       ScopeSession,
		HintLabel:   "lazygit",
		Description: "Launch lazygit for selected project",
		DocSection:  "launch_lazygit",
	})
	r.Register(ActionDef{
		Action:      ActionEnterFull,
		Bindings:    []KeyBinding{{Key: gocui.KeyEnter}},
		Scope:       ScopeSession,
		HintLabel:   "full",
		HintKey:     "enter",
		Description: "Enter full-screen mode",
		DocSection:  "enter_fullscreen",
	})
	r.Register(ActionDef{
		Action:      ActionEnterFullR,
		Bindings:    []KeyBinding{{Rune: 'r'}},
		Scope:       ScopeSession,
		Description: "Enter full-screen mode",
		DocSection:  "enter_fullscreen",
	})
	r.Register(ActionDef{
		Action:      ActionStartRename,
		Bindings:    []KeyBinding{{Rune: 'R'}},
		Scope:       ScopeSession,
		HintLabel:   "rename",
		Description: "Rename selected session",
		DocSection:  "start_rename",
	})
	r.Register(ActionDef{
		Action:      ActionStartWorktree,
		Bindings:    []KeyBinding{{Rune: 'w'}},
		Scope:       ScopeSession,
		HintLabel:   "worktree",
		Description: "Create new worktree session",
		DocSection:  "worktree",
	})
	r.Register(ActionDef{
		Action:      ActionSelectWorktree,
		Bindings:    []KeyBinding{{Rune: 'W'}},
		Scope:       ScopeSession,
		HintLabel:   "select",
		Description: "Select existing worktree",
		DocSection:  "worktree",
	})
	r.Register(ActionDef{
		Action:      ActionStartPMSession,
		Bindings:    []KeyBinding{{Rune: 'P'}},
		Scope:       ScopeSession,
		HintLabel:   "pm",
		Description: "Start PM orchestration session",
		DocSection:  "pm_session",
	})
	r.Register(ActionDef{
		Action:      ActionSendKey1,
		Bindings:    []KeyBinding{{Rune: '1'}},
		Scope:       ScopeSession,
		HintLabel:   "send",
		HintKey:     "1/2/3",
		Description: "Send accept to session",
		DocSection:  "send_key",
	})
	r.Register(ActionDef{
		Action:      ActionSendKey2,
		Bindings:    []KeyBinding{{Rune: '2'}},
		Scope:       ScopeSession,
		Description: "Send allow to session",
		DocSection:  "send_key",
	})
	r.Register(ActionDef{
		Action:      ActionSendKey3,
		Bindings:    []KeyBinding{{Rune: '3'}},
		Scope:       ScopeSession,
		Description: "Send reject to session",
		DocSection:  "send_key",
	})
	r.Register(ActionDef{
		Action:      ActionPurgeOrphans,
		Bindings:    []KeyBinding{{Rune: 'D'}},
		Scope:       ScopeSession,
		Description: "Purge orphaned sessions",
		DocSection:  "purge_orphans",
	})

	// --- Plugins panel ---
	// MCP tab: cursor, toggle denied, refresh
	r.Register(ActionDef{
		Action:      ActionMCPCursorDown,
		Bindings:    []KeyBinding{{Rune: 'j'}, {Key: gocui.KeyArrowDown}},
		Scope:       ScopePlugins,
		Tab:         PluginTabMCP,
		Description: "Move cursor down",
		DocSection:  "cursor_move",
	})
	r.Register(ActionDef{
		Action:      ActionMCPCursorUp,
		Bindings:    []KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		Scope:       ScopePlugins,
		Tab:         PluginTabMCP,
		Description: "Move cursor up",
		DocSection:  "cursor_move",
	})
	r.Register(ActionDef{
		Action:      ActionMCPToggleDenied,
		Bindings:    []KeyBinding{{Rune: 'e'}},
		Scope:       ScopePlugins,
		Tab:         PluginTabMCP,
		HintLabel:   "toggle",
		Description: "Toggle MCP server enabled/disabled",
		DocSection:  "mcp_toggle",
	})
	r.Register(ActionDef{
		Action:      ActionMCPRefresh,
		Bindings:    []KeyBinding{{Rune: 'r'}},
		Scope:       ScopePlugins,
		Tab:         PluginTabMCP,
		HintLabel:   "refresh",
		Description: "Refresh MCP server list",
		DocSection:  "mcp_refresh",
	})
	// Plugins tab: cursor, toggle, uninstall, update, refresh
	r.Register(ActionDef{
		Action:      ActionPluginCursorDown,
		Bindings:    []KeyBinding{{Rune: 'j'}, {Key: gocui.KeyArrowDown}},
		Scope:       ScopePlugins,
		Tab:         PluginTabPlugins,
		Description: "Move cursor down",
		DocSection:  "cursor_move",
	})
	r.Register(ActionDef{
		Action:      ActionPluginCursorUp,
		Bindings:    []KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		Scope:       ScopePlugins,
		Tab:         PluginTabPlugins,
		Description: "Move cursor up",
		DocSection:  "cursor_move",
	})
	r.Register(ActionDef{
		Action:      ActionPluginToggleEnabled,
		Bindings:    []KeyBinding{{Rune: 'e'}},
		Scope:       ScopePlugins,
		Tab:         PluginTabPlugins,
		HintLabel:   "toggle",
		Description: "Toggle plugin enabled/disabled",
		DocSection:  "plugin_toggle",
	})
	r.Register(ActionDef{
		Action:      ActionPluginUninstall,
		Bindings:    []KeyBinding{{Rune: 'd'}},
		Scope:       ScopePlugins,
		Tab:         PluginTabPlugins,
		HintLabel:   "uninstall",
		Description: "Uninstall selected plugin",
		DocSection:  "plugin_uninstall",
	})
	r.Register(ActionDef{
		Action:      ActionPluginUpdate,
		Bindings:    []KeyBinding{{Rune: 'u'}},
		Scope:       ScopePlugins,
		Tab:         PluginTabPlugins,
		HintLabel:   "update",
		Description: "Update selected plugin",
		DocSection:  "plugin_update",
	})
	r.Register(ActionDef{
		Action:      ActionPluginRefresh,
		Bindings:    []KeyBinding{{Rune: 'r'}},
		Scope:       ScopePlugins,
		Tab:         PluginTabPlugins,
		HintLabel:   "refresh",
		Description: "Refresh plugin list",
		DocSection:  "plugin_refresh",
	})
	// Marketplace tab: cursor, install, refresh
	r.Register(ActionDef{
		Action:      ActionPluginCursorDown,
		Bindings:    []KeyBinding{{Rune: 'j'}, {Key: gocui.KeyArrowDown}},
		Scope:       ScopePlugins,
		Tab:         PluginTabMarketplace,
		Description: "Move cursor down",
		DocSection:  "cursor_move",
	})
	r.Register(ActionDef{
		Action:      ActionPluginCursorUp,
		Bindings:    []KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		Scope:       ScopePlugins,
		Tab:         PluginTabMarketplace,
		Description: "Move cursor up",
		DocSection:  "cursor_move",
	})
	r.Register(ActionDef{
		Action:      ActionPluginInstall,
		Bindings:    []KeyBinding{{Rune: 'i'}},
		Scope:       ScopePlugins,
		Tab:         PluginTabMarketplace,
		HintLabel:   "install",
		Description: "Install selected plugin",
		DocSection:  "plugin_install",
	})
	r.Register(ActionDef{
		Action:      ActionPluginRefresh,
		Bindings:    []KeyBinding{{Rune: 'r'}},
		Scope:       ScopePlugins,
		Tab:         PluginTabMarketplace,
		HintLabel:   "refresh",
		Description: "Refresh marketplace",
		DocSection:  "plugin_refresh",
	})

	// --- Logs panel ---
	r.Register(ActionDef{
		Action:      ActionLogsCursorDown,
		Bindings:    []KeyBinding{{Rune: 'j'}, {Key: gocui.KeyArrowDown}},
		Scope:       ScopeLog,
		Description: "Move cursor down",
		DocSection:  "cursor_move",
	})
	r.Register(ActionDef{
		Action:      ActionLogsCursorUp,
		Bindings:    []KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		Scope:       ScopeLog,
		Description: "Move cursor up",
		DocSection:  "cursor_move",
	})
	r.Register(ActionDef{
		Action:      ActionLogsCursorToEnd,
		Bindings:    []KeyBinding{{Rune: 'G'}},
		Scope:       ScopeLog,
		HintLabel:   "end",
		Description: "Jump to end of log",
		DocSection:  "logs_jump",
	})
	r.Register(ActionDef{
		Action:      ActionLogsCursorToTop,
		Bindings:    []KeyBinding{{Rune: 'g'}},
		Scope:       ScopeLog,
		Description: "Jump to top of log",
		DocSection:  "logs_jump",
	})
	r.Register(ActionDef{
		Action:      ActionLogsToggleSelect,
		Bindings:    []KeyBinding{{Rune: 'v'}},
		Scope:       ScopeLog,
		HintLabel:   "select",
		Description: "Toggle line selection",
		DocSection:  "logs_select",
	})
	r.Register(ActionDef{
		Action:      ActionLogsCopySelection,
		Bindings:    []KeyBinding{{Rune: 'y'}},
		Scope:       ScopeLog,
		HintLabel:   "copy",
		Description: "Copy selected lines to clipboard",
		DocSection:  "logs_copy",
	})

	// --- Search (per-panel) ---
	r.Register(ActionDef{
		Action:      ActionStartSearch,
		Bindings:    []KeyBinding{{Rune: '/'}},
		Scope:       ScopeSession,
		HintLabel:   "search",
		Description: "Filter sessions by name",
		DocSection:  "search",
	})
	r.Register(ActionDef{
		Action:      ActionStartSearch,
		Bindings:    []KeyBinding{{Rune: '/'}},
		Scope:       ScopePlugins,
		Tab:         TabAll,
		HintLabel:   "search",
		Description: "Filter items by name",
		DocSection:  "search",
	})
	r.Register(ActionDef{
		Action:      ActionStartSearch,
		Bindings:    []KeyBinding{{Rune: '/'}},
		Scope:       ScopeLog,
		HintLabel:   "search",
		Description: "Filter log lines",
		DocSection:  "search",
	})

	// --- Popup ---
	r.Register(ActionDef{
		Action:      ActionPopupAccept,
		Bindings:    []KeyBinding{{Key: gocui.KeyCtrlY}, {Rune: '1'}},
		Scope:       ScopePopup,
		HintLabel:   "yes",
		Description: "Accept tool execution",
		DocSection:  "popup_accept",
	})
	r.Register(ActionDef{
		Action:      ActionPopupAllow,
		Bindings:    []KeyBinding{{Key: gocui.KeyCtrlA}, {Rune: '2'}},
		Scope:       ScopePopup,
		HintLabel:   "allow",
		Description: "Allow tool for this session",
		DocSection:  "popup_allow",
	})
	r.Register(ActionDef{
		Action:      ActionPopupReject,
		Bindings:    []KeyBinding{{Key: gocui.KeyCtrlN}, {Rune: '3'}},
		Scope:       ScopePopup,
		HintLabel:   "no",
		Description: "Reject tool execution",
		DocSection:  "popup_reject",
	})
	r.Register(ActionDef{
		Action:      ActionPopupAcceptAll,
		Bindings:    []KeyBinding{{Rune: 'Y'}},
		Scope:       ScopePopup,
		HintLabel:   "all",
		Description: "Accept all pending tools",
		DocSection:  "popup_accept_all",
	})
	r.Register(ActionDef{
		Action:      ActionPopupSuspend,
		Bindings:    []KeyBinding{{Key: gocui.KeyEsc}},
		Scope:       ScopePopup,
		HintLabel:   "hide",
		Description: "Suspend notification popup",
		DocSection:  "popup_suspend",
	})
	r.Register(ActionDef{
		Action:      ActionPopupFocusNext,
		Bindings:    []KeyBinding{{Key: gocui.KeyArrowDown}},
		Scope:       ScopePopup,
		Description: "Focus next notification",
		DocSection:  "popup_navigate",
	})
	r.Register(ActionDef{
		Action:      ActionPopupFocusPrev,
		Bindings:    []KeyBinding{{Key: gocui.KeyArrowUp}},
		Scope:       ScopePopup,
		Description: "Focus previous notification",
		DocSection:  "popup_navigate",
	})
	r.Register(ActionDef{
		Action:      ActionPopupScrollDown,
		Bindings:    []KeyBinding{{Rune: 'j'}},
		Scope:       ScopePopup,
		HintLabel:   "scroll",
		HintKey:     "j/k",
		Description: "Scroll notification down",
		DocSection:  "popup_scroll",
	})
	r.Register(ActionDef{
		Action:      ActionPopupScrollUp,
		Bindings:    []KeyBinding{{Rune: 'k'}},
		Scope:       ScopePopup,
		Description: "Scroll notification up",
		DocSection:  "popup_scroll",
	})

	// --- FullScreen ---
	r.Register(ActionDef{
		Action:      ActionExitFull,
		Bindings:    []KeyBinding{{Key: gocui.KeyCtrlBackslash}, {Key: gocui.KeyCtrlD}},
		Scope:       ScopeFullScreen,
		HintLabel:   "exit",
		HintKey:     "C-\\",
		Description: "Exit full-screen mode",
		DocSection:  "exit_fullscreen",
	})
	r.Register(ActionDef{
		Action:      ActionForwardEnter,
		Bindings:    []KeyBinding{{Key: gocui.KeyEnter}},
		Scope:       ScopeFullScreen,
		Description: "Forward Enter to Claude Code",
		DocSection:  "fullscreen_forward",
	})
	r.Register(ActionDef{
		Action:      ActionForwardEsc,
		Bindings:    []KeyBinding{{Key: gocui.KeyEsc}},
		Scope:       ScopeFullScreen,
		Description: "Forward Esc to Claude Code",
		DocSection:  "fullscreen_forward",
	})
	r.Register(ActionDef{
		Action:      ActionForwardDown,
		Bindings:    []KeyBinding{{Key: gocui.KeyArrowDown}},
		Scope:       ScopeFullScreen,
		Description: "Forward Down to Claude Code",
		DocSection:  "fullscreen_forward",
	})
	r.Register(ActionDef{
		Action:      ActionForwardUp,
		Bindings:    []KeyBinding{{Key: gocui.KeyArrowUp}},
		Scope:       ScopeFullScreen,
		Description: "Forward Up to Claude Code",
		DocSection:  "fullscreen_forward",
	})

	// --- FullScreen: scroll mode entry ---
	r.Register(ActionDef{
		Action:      ActionScrollEnter,
		Bindings:    []KeyBinding{{Key: gocui.KeyCtrlV}},
		Scope:       ScopeFullScreen,
		HintLabel:   "scroll",
		HintKey:     "C-v",
		Description: "Enter scroll mode for scrollback browsing",
		DocSection:  "scroll_mode",
	})

	// --- Scroll mode ---
	r.Register(ActionDef{
		Action:      ActionScrollUp,
		Bindings:    []KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		Scope:       ScopeScroll,
		Description: "Scroll up one line",
	})
	r.Register(ActionDef{
		Action:      ActionScrollDown,
		Bindings:    []KeyBinding{{Rune: 'j'}, {Key: gocui.KeyArrowDown}},
		Scope:       ScopeScroll,
		Description: "Scroll down one line",
	})
	r.Register(ActionDef{
		Action:   ActionScrollHalfUp,
		// gocui may send Ctrl+U as rune 'u' when Editable=false.
		Bindings: []KeyBinding{{Key: gocui.KeyCtrlU}, {Rune: 'u'}},
		Scope:    ScopeScroll,
		HintLabel:   "half up",
		HintKey:     "C-u",
		Description: "Scroll up half page",
	})
	r.Register(ActionDef{
		Action:   ActionScrollHalfDown,
		// gocui sends Ctrl+D as rune 'd' when Editable=false, so bind both.
		Bindings: []KeyBinding{{Key: gocui.KeyCtrlD}, {Rune: 'd', Mod: gocui.ModNone}},
		Scope:    ScopeScroll,
		HintLabel:   "half down",
		HintKey:     "C-d",
		Description: "Scroll down half page",
	})
	r.Register(ActionDef{
		Action:      ActionScrollToTop,
		Bindings:    []KeyBinding{{Rune: 'g'}},
		Scope:       ScopeScroll,
		HintLabel:   "top",
		HintKey:     "g",
		Description: "Scroll to top of scrollback",
	})
	r.Register(ActionDef{
		Action:      ActionScrollToBottom,
		Bindings:    []KeyBinding{{Rune: 'G'}},
		Scope:       ScopeScroll,
		HintLabel:   "bottom",
		HintKey:     "G",
		Description: "Scroll to bottom (exit scroll)",
	})
	r.Register(ActionDef{
		Action:      ActionScrollToggleSelect,
		Bindings:    []KeyBinding{{Rune: 'v'}},
		Scope:       ScopeScroll,
		HintLabel:   "select",
		HintKey:     "v",
		Description: "Toggle visual line selection",
	})
	r.Register(ActionDef{
		Action:      ActionScrollCopy,
		Bindings:    []KeyBinding{{Rune: 'y'}},
		Scope:       ScopeScroll,
		HintLabel:   "copy",
		HintKey:     "y",
		Description: "Copy selected text to clipboard",
	})
	r.Register(ActionDef{
		Action:      ActionScrollExit,
		Bindings:    []KeyBinding{{Key: gocui.KeyEsc}, {Rune: 'q'}},
		Scope:       ScopeScroll,
		HintLabel:   "exit",
		HintKey:     "Esc",
		Description: "Exit scroll mode",
	})

	return r
}
