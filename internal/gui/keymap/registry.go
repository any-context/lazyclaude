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
		Action:    ActionQuit,
		Bindings:  []KeyBinding{{Rune: 'q'}},
		Scope:     ScopeGlobal,
		HintLabel: "quit",
	})
	r.Register(ActionDef{
		Action:   ActionQuitCtrlC,
		Bindings: []KeyBinding{{Key: gocui.KeyCtrlC}},
		Scope:    ScopeGlobal,
	})
	r.Register(ActionDef{
		Action:   ActionQuitCtrlBackslash,
		Bindings: []KeyBinding{{Key: gocui.KeyCtrlBackslash}},
		Scope:    ScopeGlobal,
	})
	r.Register(ActionDef{
		Action:    ActionFocusNextPanel,
		Bindings:  []KeyBinding{{Key: gocui.KeyTab}},
		Scope:     ScopeGlobal,
		HintLabel: "panel",
	})
	r.Register(ActionDef{
		Action:   ActionFocusPrevPanel,
		Bindings: []KeyBinding{{Key: gocui.KeyBacktab}},
		Scope:    ScopeGlobal,
	})
	r.Register(ActionDef{
		Action:    ActionUnsuspendPopups,
		Bindings:  []KeyBinding{{Rune: 'p'}},
		Scope:     ScopeGlobal,
		HintLabel: "notif",
	})
	r.Register(ActionDef{
		Action:    ActionPanelNextTab,
		Bindings:  []KeyBinding{{Rune: ']'}},
		Scope:     ScopeGlobal,
		HintLabel: "tab",
		HintKey:   "[/]",
	})
	r.Register(ActionDef{
		Action:   ActionPanelPrevTab,
		Bindings: []KeyBinding{{Rune: '['}},
		Scope:    ScopeGlobal,
	})

	// --- Session panel ---
	r.Register(ActionDef{
		Action:   ActionCursorDown,
		Bindings: []KeyBinding{{Rune: 'j'}, {Key: gocui.KeyArrowDown}},
		Scope:    ScopeSession,
	})
	r.Register(ActionDef{
		Action:   ActionCursorUp,
		Bindings: []KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		Scope:    ScopeSession,
	})
	r.Register(ActionDef{
		Action:   ActionCollapseProject,
		Bindings: []KeyBinding{{Rune: 'h'}, {Key: gocui.KeyArrowLeft}},
		Scope:    ScopeSession,
	})
	r.Register(ActionDef{
		Action:   ActionExpandProject,
		Bindings: []KeyBinding{{Rune: 'l'}, {Key: gocui.KeyArrowRight}},
		Scope:    ScopeSession,
	})
	r.Register(ActionDef{
		Action:    ActionNewSession,
		Bindings:  []KeyBinding{{Rune: 'n'}},
		Scope:     ScopeSession,
		HintLabel: "new",
	})
	r.Register(ActionDef{
		Action:    ActionNewSessionCWD,
		Bindings:  []KeyBinding{{Rune: 'N'}},
		Scope:     ScopeSession,
		HintLabel: "new[cwd]",
	})
	r.Register(ActionDef{
		Action:    ActionDeleteSession,
		Bindings:  []KeyBinding{{Rune: 'd'}},
		Scope:     ScopeSession,
		HintLabel: "del",
	})
	r.Register(ActionDef{
		Action:    ActionAttachSession,
		Bindings:  []KeyBinding{{Rune: 'a'}},
		Scope:     ScopeSession,
		HintLabel: "attach",
	})
	r.Register(ActionDef{
		Action:    ActionLaunchLazygit,
		Bindings:  []KeyBinding{{Rune: 'g'}},
		Scope:     ScopeSession,
		HintLabel: "lazygit",
	})
	r.Register(ActionDef{
		Action:    ActionEnterFull,
		Bindings:  []KeyBinding{{Key: gocui.KeyEnter}},
		Scope:     ScopeSession,
		HintLabel: "full",
		HintKey:   "enter",
	})
	r.Register(ActionDef{
		Action:   ActionEnterFullR,
		Bindings: []KeyBinding{{Rune: 'r'}},
		Scope:    ScopeSession,
	})
	r.Register(ActionDef{
		Action:    ActionStartRename,
		Bindings:  []KeyBinding{{Rune: 'R'}},
		Scope:     ScopeSession,
		HintLabel: "rename",
	})
	r.Register(ActionDef{
		Action:    ActionStartWorktree,
		Bindings:  []KeyBinding{{Rune: 'w'}},
		Scope:     ScopeSession,
		HintLabel: "worktree",
	})
	r.Register(ActionDef{
		Action:    ActionSelectWorktree,
		Bindings:  []KeyBinding{{Rune: 'W'}},
		Scope:     ScopeSession,
		HintLabel: "select",
	})
	r.Register(ActionDef{
		Action:    ActionStartPMSession,
		Bindings:  []KeyBinding{{Rune: 'P'}},
		Scope:     ScopeSession,
		HintLabel: "pm",
	})
	r.Register(ActionDef{
		Action:    ActionSendKey1,
		Bindings:  []KeyBinding{{Rune: '1'}},
		Scope:     ScopeSession,
		HintLabel: "send",
		HintKey:   "1/2/3",
	})
	r.Register(ActionDef{
		Action:   ActionSendKey2,
		Bindings: []KeyBinding{{Rune: '2'}},
		Scope:    ScopeSession,
	})
	r.Register(ActionDef{
		Action:   ActionSendKey3,
		Bindings: []KeyBinding{{Rune: '3'}},
		Scope:    ScopeSession,
	})
	r.Register(ActionDef{
		Action:   ActionPurgeOrphans,
		Bindings: []KeyBinding{{Rune: 'D'}},
		Scope:    ScopeSession,
	})

	// --- Plugins panel ---
	// Tab layout: 0 = MCP, 1 = Plugins, 2 = Marketplace
	// MCP tab (0): cursor, toggle denied, refresh
	r.Register(ActionDef{
		Action:   ActionMCPCursorDown,
		Bindings: []KeyBinding{{Rune: 'j'}, {Key: gocui.KeyArrowDown}},
		Scope:    ScopePlugins,
		Tab:      0,
	})
	r.Register(ActionDef{
		Action:   ActionMCPCursorUp,
		Bindings: []KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		Scope:    ScopePlugins,
		Tab:      0,
	})
	r.Register(ActionDef{
		Action:    ActionMCPToggleDenied,
		Bindings:  []KeyBinding{{Rune: 'e'}},
		Scope:     ScopePlugins,
		Tab:       0,
		HintLabel: "toggle",
	})
	r.Register(ActionDef{
		Action:    ActionMCPRefresh,
		Bindings:  []KeyBinding{{Rune: 'r'}},
		Scope:     ScopePlugins,
		Tab:       0,
		HintLabel: "refresh",
	})
	// Plugins tab (1): cursor, toggle, uninstall, update, refresh
	r.Register(ActionDef{
		Action:   ActionPluginCursorDown,
		Bindings: []KeyBinding{{Rune: 'j'}, {Key: gocui.KeyArrowDown}},
		Scope:    ScopePlugins,
		Tab:      1,
	})
	r.Register(ActionDef{
		Action:   ActionPluginCursorUp,
		Bindings: []KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		Scope:    ScopePlugins,
		Tab:      1,
	})
	r.Register(ActionDef{
		Action:    ActionPluginToggleEnabled,
		Bindings:  []KeyBinding{{Rune: 'e'}},
		Scope:     ScopePlugins,
		Tab:       1,
		HintLabel: "toggle",
	})
	r.Register(ActionDef{
		Action:    ActionPluginUninstall,
		Bindings:  []KeyBinding{{Rune: 'd'}},
		Scope:     ScopePlugins,
		Tab:       1,
		HintLabel: "uninstall",
	})
	r.Register(ActionDef{
		Action:    ActionPluginUpdate,
		Bindings:  []KeyBinding{{Rune: 'u'}},
		Scope:     ScopePlugins,
		Tab:       1,
		HintLabel: "update",
	})
	r.Register(ActionDef{
		Action:    ActionPluginRefresh,
		Bindings:  []KeyBinding{{Rune: 'r'}},
		Scope:     ScopePlugins,
		Tab:       1,
		HintLabel: "refresh",
	})
	// Marketplace tab (2): cursor, install, refresh
	r.Register(ActionDef{
		Action:   ActionPluginCursorDown,
		Bindings: []KeyBinding{{Rune: 'j'}, {Key: gocui.KeyArrowDown}},
		Scope:    ScopePlugins,
		Tab:      2,
	})
	r.Register(ActionDef{
		Action:   ActionPluginCursorUp,
		Bindings: []KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		Scope:    ScopePlugins,
		Tab:      2,
	})
	r.Register(ActionDef{
		Action:    ActionPluginInstall,
		Bindings:  []KeyBinding{{Rune: 'i'}},
		Scope:     ScopePlugins,
		Tab:       2,
		HintLabel: "install",
	})
	r.Register(ActionDef{
		Action:    ActionPluginRefresh,
		Bindings:  []KeyBinding{{Rune: 'r'}},
		Scope:     ScopePlugins,
		Tab:       2,
		HintLabel: "refresh",
	})

	// --- Logs panel ---
	r.Register(ActionDef{
		Action:   ActionLogsCursorDown,
		Bindings: []KeyBinding{{Rune: 'j'}, {Key: gocui.KeyArrowDown}},
		Scope:    ScopeLog,
	})
	r.Register(ActionDef{
		Action:   ActionLogsCursorUp,
		Bindings: []KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		Scope:    ScopeLog,
	})
	r.Register(ActionDef{
		Action:    ActionLogsCursorToEnd,
		Bindings:  []KeyBinding{{Rune: 'G'}},
		Scope:     ScopeLog,
		HintLabel: "end",
	})
	r.Register(ActionDef{
		Action:   ActionLogsCursorToTop,
		Bindings: []KeyBinding{{Rune: 'g'}},
		Scope:    ScopeLog,
	})
	r.Register(ActionDef{
		Action:    ActionLogsToggleSelect,
		Bindings:  []KeyBinding{{Rune: 'v'}},
		Scope:     ScopeLog,
		HintLabel: "select",
	})
	r.Register(ActionDef{
		Action:    ActionLogsCopySelection,
		Bindings:  []KeyBinding{{Rune: 'y'}},
		Scope:     ScopeLog,
		HintLabel: "copy",
	})

	// --- Popup ---
	r.Register(ActionDef{
		Action:    ActionPopupAccept,
		Bindings:  []KeyBinding{{Key: gocui.KeyCtrlY}, {Rune: '1'}},
		Scope:     ScopePopup,
		HintLabel: "yes",
	})
	r.Register(ActionDef{
		Action:    ActionPopupAllow,
		Bindings:  []KeyBinding{{Key: gocui.KeyCtrlA}, {Rune: '2'}},
		Scope:     ScopePopup,
		HintLabel: "allow",
	})
	r.Register(ActionDef{
		Action:    ActionPopupReject,
		Bindings:  []KeyBinding{{Key: gocui.KeyCtrlN}, {Rune: '3'}},
		Scope:     ScopePopup,
		HintLabel: "no",
	})
	r.Register(ActionDef{
		Action:    ActionPopupAcceptAll,
		Bindings:  []KeyBinding{{Rune: 'Y'}},
		Scope:     ScopePopup,
		HintLabel: "all",
	})
	r.Register(ActionDef{
		Action:    ActionPopupSuspend,
		Bindings:  []KeyBinding{{Key: gocui.KeyEsc}},
		Scope:     ScopePopup,
		HintLabel: "hide",
	})
	r.Register(ActionDef{
		Action:   ActionPopupFocusNext,
		Bindings: []KeyBinding{{Key: gocui.KeyArrowDown}},
		Scope:    ScopePopup,
	})
	r.Register(ActionDef{
		Action:   ActionPopupFocusPrev,
		Bindings: []KeyBinding{{Key: gocui.KeyArrowUp}},
		Scope:    ScopePopup,
	})
	r.Register(ActionDef{
		Action:    ActionPopupScrollDown,
		Bindings:  []KeyBinding{{Rune: 'j'}},
		Scope:     ScopePopup,
		HintLabel: "scroll",
		HintKey:   "j/k",
	})
	r.Register(ActionDef{
		Action:   ActionPopupScrollUp,
		Bindings: []KeyBinding{{Rune: 'k'}},
		Scope:    ScopePopup,
	})

	// --- FullScreen ---
	r.Register(ActionDef{
		Action:    ActionExitFull,
		Bindings:  []KeyBinding{{Key: gocui.KeyCtrlBackslash}, {Key: gocui.KeyCtrlD}},
		Scope:     ScopeFullScreen,
		HintLabel: "exit",
		HintKey:   "C-\\",
	})
	r.Register(ActionDef{
		Action:   ActionForwardEnter,
		Bindings: []KeyBinding{{Key: gocui.KeyEnter}},
		Scope:    ScopeFullScreen,
	})
	r.Register(ActionDef{
		Action:   ActionForwardEsc,
		Bindings: []KeyBinding{{Key: gocui.KeyEsc}},
		Scope:    ScopeFullScreen,
	})
	r.Register(ActionDef{
		Action:   ActionForwardDown,
		Bindings: []KeyBinding{{Key: gocui.KeyArrowDown}},
		Scope:    ScopeFullScreen,
	})
	r.Register(ActionDef{
		Action:   ActionForwardUp,
		Bindings: []KeyBinding{{Key: gocui.KeyArrowUp}},
		Scope:    ScopeFullScreen,
	})

	return r
}
