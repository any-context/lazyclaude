package gui

// DialogKind identifies which input dialog is currently active.
type DialogKind int

const (
	DialogNone     DialogKind = iota // no dialog
	DialogRename                     // rename-input
	DialogWorktree                   // worktree-branch + worktree-prompt
)

// HasActiveDialog returns true if any input dialog is open.
func (a *App) HasActiveDialog() bool {
	return a.activeDialog != DialogNone
}

// ActiveDialogKind returns the current dialog type.
func (a *App) ActiveDialogKind() DialogKind {
	return a.activeDialog
}
