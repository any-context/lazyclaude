package keymap

import "github.com/jesseduffield/gocui"

// AppState represents the current UI state of the application.
// Used by the GUI layer to distinguish main vs full-screen mode.
// Key matching uses Scope (not AppState).
type AppState int

const (
	StateMain       AppState = iota // session list + preview
	StateFullScreen                 // full-screen, all keys forwarded to Claude Code
)

// IsFullScreen returns true if the state is full-screen mode.
func (s AppState) IsFullScreen() bool {
	return s == StateFullScreen
}

// Scope identifies which handler owns an action.
type Scope string

const (
	ScopeGlobal     Scope = "global"
	ScopeSession    Scope = "session"
	ScopePlugins    Scope = "plugins"
	ScopeLog        Scope = "log"
	ScopePopup      Scope = "popup"
	ScopeFullScreen Scope = "fullscreen"
	ScopeScroll     Scope = "scroll"
)

// KeyAction identifies a logical action in the keymap.
type KeyAction string

// --- Global actions ---
const (
	ActionQuit             KeyAction = "quit"
	ActionQuitCtrlC        KeyAction = "quit_ctrl_c"
	ActionQuitCtrlBackslash KeyAction = "quit_ctrl_backslash"
	ActionFocusNextPanel   KeyAction = "focus_next_panel"
	ActionFocusPrevPanel   KeyAction = "focus_prev_panel"
	ActionUnsuspendPopups  KeyAction = "unsuspend_popups"
	ActionPanelNextTab     KeyAction = "panel_next_tab"
	ActionPanelPrevTab     KeyAction = "panel_prev_tab"
)

// --- Session panel actions ---
const (
	ActionCursorUp             KeyAction = "cursor_up"
	ActionCursorDown           KeyAction = "cursor_down"
	ActionCollapseProject      KeyAction = "collapse_project"
	ActionExpandProject        KeyAction = "expand_project"
	ActionNewSession           KeyAction = "new_session"
	ActionNewSessionCWD        KeyAction = "new_session_cwd"
	ActionDeleteSession        KeyAction = "delete_session"
	ActionAttachSession        KeyAction = "attach_session"
	ActionLaunchLazygit        KeyAction = "launch_lazygit"
	ActionEnterFull   KeyAction = "enter_fullscreen"
	ActionEnterFullR  KeyAction = "enter_fullscreen_r"
	ActionStartRename KeyAction = "start_rename"
	ActionStartWorktree        KeyAction = "start_worktree"
	ActionSelectWorktree       KeyAction = "select_worktree"
	ActionPurgeOrphans         KeyAction = "purge_orphans"
	ActionStartPMSession       KeyAction = "start_pm_session"
	ActionSendKey1             KeyAction = "send_key_1"
	ActionSendKey2             KeyAction = "send_key_2"
	ActionSendKey3             KeyAction = "send_key_3"
	ActionConnectRemote        KeyAction = "connect_remote"
	ActionDismissError         KeyAction = "dismiss_error"
	ActionCopyError            KeyAction = "copy_error"
)

// --- Plugin panel actions ---
const (
	ActionPluginCursorUp     KeyAction = "plugin_cursor_up"
	ActionPluginCursorDown   KeyAction = "plugin_cursor_down"
	ActionPluginInstall      KeyAction = "plugin_install"
	ActionPluginUninstall    KeyAction = "plugin_uninstall"
	ActionPluginToggleEnabled KeyAction = "plugin_toggle_enabled"
	ActionPluginUpdate       KeyAction = "plugin_update"
	ActionPluginRefresh      KeyAction = "plugin_refresh"
)

// --- MCP panel actions ---
const (
	ActionMCPCursorDown   KeyAction = "mcp_cursor_down"
	ActionMCPCursorUp     KeyAction = "mcp_cursor_up"
	ActionMCPToggleDenied KeyAction = "mcp_toggle_denied"
	ActionMCPRefresh      KeyAction = "mcp_refresh"
)

// --- Log panel actions ---
const (
	ActionLogsCursorDown    KeyAction = "logs_cursor_down"
	ActionLogsCursorUp      KeyAction = "logs_cursor_up"
	ActionLogsCursorToEnd   KeyAction = "logs_cursor_to_end"
	ActionLogsCursorToTop   KeyAction = "logs_cursor_to_top"
	ActionLogsToggleSelect  KeyAction = "logs_toggle_select"
	ActionLogsCopySelection KeyAction = "logs_copy_selection"
)

// --- Popup actions ---
const (
	ActionPopupAccept    KeyAction = "popup_accept"
	ActionPopupAllow     KeyAction = "popup_allow"
	ActionPopupReject    KeyAction = "popup_reject"
	ActionPopupSuspend   KeyAction = "popup_suspend"
	ActionPopupAcceptAll KeyAction = "popup_accept_all"
	ActionPopupFocusNext KeyAction = "popup_focus_next"
	ActionPopupFocusPrev KeyAction = "popup_focus_prev"
	ActionPopupScrollDown KeyAction = "popup_scroll_down"
	ActionPopupScrollUp  KeyAction = "popup_scroll_up"
)

// --- Help actions ---
const (
	ActionShowKeybindHelp KeyAction = "show_keybind_help"
)

// --- Search actions ---
const (
	ActionStartSearch KeyAction = "start_search"
)

// --- FullScreen actions ---
const (
	ActionExitFull          KeyAction = "exit_fullscreen"
	ActionForwardEnter      KeyAction = "forward_enter"
	ActionForwardEsc        KeyAction = "forward_esc"
	ActionForwardDown       KeyAction = "forward_down"
	ActionForwardUp         KeyAction = "forward_up"
	ActionScrollEnter       KeyAction = "scroll_enter"
)

// --- Scroll mode actions ---
const (
	ActionScrollUp           KeyAction = "scroll_up"
	ActionScrollDown         KeyAction = "scroll_down"
	ActionScrollHalfUp       KeyAction = "scroll_half_up"
	ActionScrollHalfDown     KeyAction = "scroll_half_down"
	ActionScrollToTop        KeyAction = "scroll_to_top"
	ActionScrollToBottom     KeyAction = "scroll_to_bottom"
	ActionScrollToggleSelect KeyAction = "scroll_toggle_select"
	ActionScrollCopy         KeyAction = "scroll_copy"
	ActionScrollExit         KeyAction = "scroll_exit"
)

// TabAll means the action is active on all tabs within a panel.
const TabAll = -1

// --- Plugin panel tab indices ---
const (
	PluginTabMCP         = 0
	PluginTabPlugins     = 1
	PluginTabMarketplace = 2
)

// PluginTabLabels returns the ordered label list for plugin panel tabs.
// This is the single source of truth — TabCount and TabLabels derive from it.
func PluginTabLabels() []string {
	return []string{"MCP", "Plugins", "Marketplace"}
}

// ActionDef defines a logical action with its key bindings, scope, and hint.
type ActionDef struct {
	Action      KeyAction
	Bindings    []KeyBinding
	Scope       Scope
	Tab         int    // tab index this action is active on; TabAll (-1) = all tabs
	HintLabel   string // empty = do not show in options bar (e.g. navigation keys)
	HintKey     string // override key display in hint (e.g. "h/l", "1/2/3"); empty = auto from first binding
	Description string // one-line description shown in keybind help
	DocSection  string // section anchor in keybinds.md (e.g. "new_session"); empty = no detailed doc
}

// KeyBinding maps a physical key to an action.
type KeyBinding struct {
	Key  gocui.Key
	Rune rune
	Mod  gocui.Modifier
}

// Matches returns true if the given key event matches this binding.
func (kb KeyBinding) Matches(key gocui.Key, ch rune, mod gocui.Modifier) bool {
	if mod != kb.Mod {
		return false
	}
	if kb.Rune != 0 {
		return ch == kb.Rune
	}
	return key == kb.Key
}

// HintKey returns a human-readable key label for this binding.
func (kb KeyBinding) HintKey() string {
	if kb.Rune != 0 {
		return string(kb.Rune)
	}
	switch kb.Key {
	case gocui.KeyEnter:
		return "Enter"
	case gocui.KeyEsc:
		return "Esc"
	case gocui.KeyTab:
		return "Tab"
	case gocui.KeyBacktab:
		return "S-Tab"
	case gocui.KeyCtrlY:
		return "C-y"
	case gocui.KeyCtrlA:
		return "C-a"
	case gocui.KeyCtrlN:
		return "C-n"
	case gocui.KeyCtrlD:
		return "C-d"
	case gocui.KeyCtrlV:
		return "C-v"
	case gocui.KeyCtrlU:
		return "C-u"
	case gocui.KeyCtrlBackslash:
		return "C-\\"
	case gocui.KeyArrowUp:
		return "Up"
	case gocui.KeyArrowDown:
		return "Down"
	default:
		return "?"
	}
}
