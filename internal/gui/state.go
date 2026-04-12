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
			// Clear unread badge when entering fullscreen for this session.
			a.clearUnreadActivity(node.Session.TmuxWindow)
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

// handlePasteContent is the OnPasteContent callback registered with gocui.
// Called when a complete bracketed paste is received (either via native
// EventPaste or raw ESC[200~ fallback). Runs on the gocui event loop.
func (a *App) handlePasteContent(text string) {
	if text == "" {
		return
	}
	// In fullscreen mode, forward paste to the Claude Code pane.
	if a.fullscreen.IsActive() && !a.hasPopup() {
		// Run synchronously — the paste text has already been accumulated
		// outside the event loop (in pollEvent or filter goroutine), so
		// blocking briefly for tmux load-buffer/paste-buffer is fine.
		a.forwardPaste(text)
	}
	// TODO: future — support paste into input dialogs (rename, worktree, etc.)
}

