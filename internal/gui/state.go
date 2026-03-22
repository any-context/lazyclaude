package gui

// enterFullScreen enters fullscreen mode for the given session.
func (a *App) enterFullScreen(sessionID string) {
	a.fullscreen.Enter(sessionID)
	if a.sessions != nil {
		for i, item := range a.sessions.Sessions() {
			if item.ID == sessionID {
				a.cursor = i
				break
			}
		}
	}
}

// exitFullScreen exits fullscreen mode.
func (a *App) exitFullScreen() {
	a.fullscreen.Exit()
}

// resolveForwardTarget returns the tmux target for key forwarding.
func (a *App) resolveForwardTarget() string {
	if !a.fullscreen.IsActive() || a.fullscreen.forwarder == nil || a.hasPopup() || a.sessions == nil {
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
	a.fullscreen.EnqueueKey(target, RuneToTmuxKey(ch))
	a.fullscreen.TriggerRefresh()
}

func (a *App) forwardSpecialKey(tmuxKey string) {
	target := a.resolveForwardTarget()
	if target == "" {
		return
	}
	a.fullscreen.EnqueueKey(target, tmuxKey)
	a.fullscreen.TriggerRefresh()
}
