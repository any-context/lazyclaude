package gui

import "github.com/any-context/lazyclaude/internal/gui/keymap"

// DialogKind identifies which input dialog is currently active.
type DialogKind int

const (
	DialogNone             DialogKind = iota // no dialog
	DialogRename                           // rename-input
	DialogWorktree                         // worktree-branch + worktree-prompt (new)
	DialogWorktreeChooser                  // worktree-chooser (select existing)
	DialogWorktreeResume                   // worktree-resume-prompt (prompt only for existing)
	DialogKeybindHelp                      // keybind-help overlay (Telescope style)
	DialogSearch                           // inline "/" search on active panel
	DialogConnect                          // connect-input for remote host
)

// DialogState groups all input dialog state into a single struct,
// keeping the App struct focused on core TUI concerns.
type DialogState struct {
	Kind           DialogKind     // current dialog (DialogNone = no dialog)
	RenameID       string         // session ID being renamed (empty = no rename)
	ActiveField    string         // which worktree dialog field has focus
	WorktreeItems  []WorktreeInfo // items in worktree chooser
	WorktreeCursor int            // selected index in chooser (len(items) = "New")
	SelectedPath   string         // path of chosen existing worktree

	// Keybind help state
	HelpItems    []keymap.ActionDef // filtered list of actions
	HelpAllItems []keymap.ActionDef // unfiltered source
	HelpCursor   int                // selected index in filtered list
	HelpFilter   string             // current fzf query
	HelpScrollY  int                // doc preview scroll offset

	// Search state (inline "/" filter on active panel)
	SearchQuery     string // current search query (live, updated on each keystroke)
	SearchPanel     string // panel name when search started ("sessions", "plugins", "logs")
	SearchPreCursor int    // cursor position before search (restore on Esc)

	// Active filter (persisted after Enter confirms a search)
	ActiveFilter      string // confirmed filter query (empty = no filter)
	ActiveFilterPanel string // panel the filter applies to
}

// HasActiveDialog returns true if any input dialog is open.
func (a *App) HasActiveDialog() bool {
	return a.dialog.Kind != DialogNone
}

// ActiveDialogKind returns the current dialog type.
func (a *App) ActiveDialogKind() DialogKind {
	return a.dialog.Kind
}

// dialogFocusView returns the gocui view name that should have focus
// for the current dialog. Returns "" if no dialog is active.
// Used by layoutMain to restore focus after popup dismiss.
func (a *App) dialogFocusView() string {
	switch a.dialog.Kind {
	case DialogRename:
		return "rename-input"
	case DialogWorktree:
		if a.dialog.ActiveField != "" {
			return a.dialog.ActiveField
		}
		return "worktree-branch"
	case DialogWorktreeChooser:
		return "worktree-chooser"
	case DialogWorktreeResume:
		return "worktree-resume-prompt"
	case DialogKeybindHelp:
		return "keybind-help-input"
	case DialogSearch:
		return "search-input"
	case DialogConnect:
		return "connect-input"
	default:
		return ""
	}
}
