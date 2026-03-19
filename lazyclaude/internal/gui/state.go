package gui

import "time"

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

func (a *App) enterFullScreen(sessionID string) {
	a.fullScreenTarget = sessionID
	a.transition(StateFullInsert)
	if a.sessions != nil {
		for i, item := range a.sessions.Sessions() {
			if item.ID == sessionID {
				a.cursor = i
				break
			}
		}
	}
}

func (a *App) exitFullScreen() {
	a.transition(StateMain)
}

// resolveForwardTarget returns the tmux target for key forwarding.
func (a *App) resolveForwardTarget() string {
	if a.state != StateFullInsert || a.inputForwarder == nil || a.hasPopup() || a.sessions == nil {
		return ""
	}
	items := a.sessions.Sessions()
	if a.cursor < 0 || a.cursor >= len(items) {
		return ""
	}
	t := items[a.cursor].TmuxWindow
	if t == "" {
		id := items[a.cursor].ID
		if id == "" {
			return ""
		}
		windowName := "lc-" + id
		if len(id) > 8 {
			windowName = "lc-" + id[:8]
		}
		return "lazyclaude:" + windowName
	}
	return "lazyclaude:" + t
}

func (a *App) forwardKey(ch rune) {
	target := a.resolveForwardTarget()
	if target == "" {
		return
	}
	a.enqueueKey(target, RuneToTmuxKey(ch))
	a.triggerRefreshAfterInput()
}

func (a *App) forwardSpecialKey(tmuxKey string) {
	target := a.resolveForwardTarget()
	if target == "" {
		return
	}
	a.enqueueKey(target, tmuxKey)
	a.triggerRefreshAfterInput()
}

func (a *App) triggerRefreshAfterInput() {
	a.fullScreenScrollY = 0
	a.previewMu.Lock()
	if !a.previewBusy && time.Since(a.previewTime) > 50*time.Millisecond {
		a.previewTime = time.Time{}
	}
	a.previewMu.Unlock()
}
