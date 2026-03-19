package gui

import "time"

// resolveForwardTarget returns the tmux target for key forwarding.
// Returns empty string if forwarding should be skipped.
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
		// TmuxWindow not yet synced (between Create and first GC Sync).
		// Construct name-based target from session ID as fallback.
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

// forwardKey sends a rune key to the Claude Code pane.
// Keys are queued and sent serially in order by a background goroutine
// to avoid blocking gocui while preserving keystroke order (critical for IME input).
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

// triggerRefreshAfterInput marks preview as stale after sending a key.
func (a *App) triggerRefreshAfterInput() {
	a.fullScreenScrollY = 0
	a.previewMu.Lock()
	if !a.previewBusy && time.Since(a.previewTime) > 50*time.Millisecond {
		a.previewTime = time.Time{}
	}
	a.previewMu.Unlock()
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

