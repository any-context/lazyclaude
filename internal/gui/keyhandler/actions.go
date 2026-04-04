package keyhandler

import "github.com/any-context/lazyclaude/internal/core/choice"

// ---------------------------------------------------------------------------
// Domain-specific action interfaces.
// Each handler depends only on the narrow interface it needs.
// ---------------------------------------------------------------------------

// SessionActions provides session list and tree operations.
type SessionActions interface {
	MoveCursorDown()
	MoveCursorUp()
	CreateSession()
	CreateSessionAtCWD()
	DeleteSession()
	AttachSession()
	LaunchLazygit()
	EnterFullScreen()
	StartRename()
	StartWorktreeInput()
	SelectWorktree()
	PurgeOrphans()
	StartPMSession()
	SendKeyToPane(key string)
	ToggleProjectExpanded()
	CollapseProject()
	ExpandProject()
	CursorIsProject() bool
	StartSearch()
	ConnectRemote()
}

// PopupActions provides popup management.
type PopupActions interface {
	HasPopup() bool
	DismissPopup(c choice.Choice)
	DismissAllPopups(c choice.Choice)
	SuspendPopups()
	PopupFocusNext()
	PopupFocusPrev()
	PopupScrollDown()
	PopupScrollUp()
}

// FullScreenActions provides fullscreen mode operations.
type FullScreenActions interface {
	IsFullScreen() bool
	ExitFullScreen()
	ForwardSpecialKey(tmuxKey string)
}

// LogsActions provides logs panel operations.
type LogsActions interface {
	LogsCursorDown()
	LogsCursorUp()
	LogsCursorToEnd()
	LogsCursorToTop()
	LogsToggleSelect()
	LogsCopySelection()
	StartSearch()
}

// PluginsPanelActions provides plugin/MCP panel operations.
type PluginsPanelActions interface {
	ActivePanelTabIndex() int
	PluginSetTab(tab int)
	PluginCursorDown()
	PluginCursorUp()
	PluginInstall()
	PluginUninstall()
	PluginToggleEnabled()
	PluginUpdate()
	PluginRefresh()
	MCPCursorDown()
	MCPCursorUp()
	MCPToggleDenied()
	MCPRefresh()
	StartSearch()
}

// ScrollActions provides scroll mode operations in fullscreen.
type ScrollActions interface {
	IsScrollMode() bool
	ScrollModeEnter()
	ScrollModeExit()
	ScrollModeUp()
	ScrollModeDown()
	ScrollModeHalfUp()
	ScrollModeHalfDown()
	ScrollModeToTop()
	ScrollModeToBottom()
	ScrollModeToggleSelect()
	ScrollModeCopy()
}

// GlobalActions provides application-level key handler operations.
type GlobalActions interface {
	Mode() int
	Quit()
	UnsuspendPopups()
	PanelNextTab()
	PanelPrevTab()
	ShowKeybindHelp()
}

// ---------------------------------------------------------------------------
// Composite interface for the dispatch boundary.
// ---------------------------------------------------------------------------

// AppActions is the composite of all action interfaces.
// Implemented by *App; used by the Dispatcher to route keys through
// the full handler chain.
type AppActions interface {
	SessionActions
	PopupActions
	FullScreenActions
	ScrollActions
	LogsActions
	PluginsPanelActions
	GlobalActions
}
