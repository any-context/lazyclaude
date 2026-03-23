package keyhandler

import "github.com/KEMSHlM/lazyclaude/internal/core/choice"

// AppActions defines actions that key handlers can invoke.
// Handlers depend only on this interface, never on the concrete App type.
type AppActions interface {
	// State queries (used by popup/fullscreen/global handlers)
	HasPopup() bool
	IsFullScreen() bool
	Mode() int // 0=Main, 1=Diff, 2=Tool

	// Session cursor
	MoveCursorDown()
	MoveCursorUp()

	// Session operations
	CreateSession()
	DeleteSession()
	AttachSession()
	LaunchLazygit()
	EnterFullScreen()
	StartRename()
	StartWorktreeInput()
	SelectWorktree()
	PurgeOrphans()

	// Popup
	DismissPopup(c choice.Choice)
	DismissAllPopups(c choice.Choice)
	SuspendPopups()
	UnsuspendPopups()
	PopupFocusNext()
	PopupFocusPrev()
	PopupScrollDown()
	PopupScrollUp()

	// FullScreen
	ExitFullScreen()
	ForwardSpecialKey(tmuxKey string)

	// Send key to the selected session's pane (works without fullscreen)
	SendKeyToPane(key string)

	// Logs panel
	LogsCursorDown()
	LogsCursorUp()
	LogsCursorToEnd()
	LogsCursorToTop()
	LogsToggleSelect()
	LogsCopySelection()

	// Application
	Quit()
}
