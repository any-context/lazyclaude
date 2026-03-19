package gui

import "time"

// AppState represents the current UI state of the application.
// Popup visibility is orthogonal to AppState (overlay, not a state).
type AppState int

const (
	StateMain       AppState = iota // session list + preview
	StateFullInsert                 // full-screen, keys forwarded to Claude Code
	StateFullNormal                 // full-screen, vim-like navigation
)

// IsFullScreen returns true if the state is any full-screen mode.
func (s AppState) IsFullScreen() bool {
	return s == StateFullInsert || s == StateFullNormal
}

// transition changes App state with entry/exit side effects.
func (a *App) transition(to AppState) {
	from := a.state

	if from == to {
		return
	}

	// Exit actions
	switch from {
	case StateFullInsert, StateFullNormal:
		if !to.IsFullScreen() {
			a.fullScreenTarget = ""
			a.previewCache = ""
		}
	}

	// Enter actions
	switch to {
	case StateFullInsert:
		if !from.IsFullScreen() {
			a.fullScreenScrollY = 0
			a.previewMu.Lock()
			a.previewCache = ""
			a.previewTime = time.Time{}
			a.previewMu.Unlock()
		}
	}

	a.state = to
}
