package gui

// enterFullScreen enters fullscreen mode for the given session.
func (a *App) enterFullScreen(sessionID string) {
	a.fullscreen.Enter(sessionID)
	// Rebuild cache in case it hasn't been populated yet (e.g. called before layout).
	a.refreshTreeNodes()
	// Ensure cursor points at the session node (for resolveForwardTarget).
	for i, node := range a.treeNodes() {
		if node.Kind == SessionNode && node.Session != nil && node.Session.ID == sessionID {
			a.cursor = i
			break
		}
	}
}

// exitFullScreen exits fullscreen mode.
func (a *App) exitFullScreen() {
	a.fullscreen.Exit()
}

// resolveSessionTarget returns the tmux target for the selected session.
func (a *App) resolveSessionTarget() string {
	sess := a.currentSession()
	if sess == nil {
		return ""
	}
	t := sess.TmuxWindow
	if t == "" {
		id := sess.ID
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

// resolveForwardTarget returns the tmux target for key forwarding.
// Returns empty string if not in fullscreen mode or popup is active.
func (a *App) resolveForwardTarget() string {
	if !a.fullscreen.IsActive() || a.fullscreen.forwarder == nil || a.hasPopup() {
		return ""
	}
	return a.resolveSessionTarget()
}

func (a *App) forwardKey(ch rune) {
	target := a.resolveForwardTarget()
	if target == "" {
		return
	}
	a.fullscreen.EnqueueLiteral(target, RuneToLiteral(ch))
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

// forwardPaste sends text as a bracketed paste to the Claude Code pane.
// Executes synchronously to serialize tmux load-buffer/paste-buffer calls.
// Callers (watchdog drainPaste, event loop flushPaste) already run outside
// the hot gocui event loop, so blocking here is acceptable.
func (a *App) forwardPaste(text string) {
	target := a.resolveForwardTarget()
	if target == "" {
		return
	}
	if a.fullscreen.forwarder == nil {
		return
	}
	_ = a.fullscreen.forwarder.ForwardPaste(target, text)
	a.fullscreen.TriggerRefresh()
}

