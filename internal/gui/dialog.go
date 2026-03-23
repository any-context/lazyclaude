package gui

// DialogKind identifies which input dialog is currently active.
type DialogKind int

const (
	DialogNone             DialogKind = iota // no dialog
	DialogRename                           // rename-input
	DialogWorktree                         // worktree-branch + worktree-prompt (new)
	DialogWorktreeChooser                  // worktree-chooser (select existing)
	DialogWorktreeResume                   // worktree-resume-prompt (prompt only for existing)
)

// HasActiveDialog returns true if any input dialog is open.
func (a *App) HasActiveDialog() bool {
	return a.activeDialog != DialogNone
}

// ActiveDialogKind returns the current dialog type.
func (a *App) ActiveDialogKind() DialogKind {
	return a.activeDialog
}

// dialogFocusView returns the gocui view name that should have focus
// for the current dialog. Returns "" if no dialog is active.
// Used by layoutMain to restore focus after popup dismiss.
func (a *App) dialogFocusView() string {
	switch a.activeDialog {
	case DialogRename:
		return "rename-input"
	case DialogWorktree:
		if a.worktreeActiveField != "" {
			return a.worktreeActiveField
		}
		return "worktree-branch"
	case DialogWorktreeChooser:
		return "worktree-chooser"
	case DialogWorktreeResume:
		return "worktree-resume-prompt"
	default:
		return ""
	}
}
