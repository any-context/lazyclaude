package gui

import "time"

// InputMode controls key handling in full-screen mode (vim-like).
type InputMode int

const (
	ModeInsert InputMode = iota // all keys forwarded to Claude Code
	ModeNormal                  // lazyclaude handles keys (scroll, quit, popup)
)

// resolveForwardTarget returns the tmux target for key forwarding.
// Returns empty string if forwarding should be skipped.
func (a *App) resolveForwardTarget() string {
	if !a.fullScreen || a.inputMode != ModeInsert || a.inputForwarder == nil || a.hasPopup() || a.sessions == nil {
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
func (a *App) forwardKey(ch rune) {
	target := a.resolveForwardTarget()
	if target == "" {
		return
	}
	a.inputForwarder.ForwardKey(target, RuneToTmuxKey(ch))
	a.triggerRefreshAfterInput()
}

func (a *App) forwardSpecialKey(tmuxKey string) {
	target := a.resolveForwardTarget()
	if target == "" {
		return
	}
	a.inputForwarder.ForwardKey(target, tmuxKey)
	a.triggerRefreshAfterInput()
}

// triggerRefreshAfterInput marks preview as stale after sending a key.
// When control mode is connected, %output events handle this.
// Without control mode, this ensures immediate display updates.
func (a *App) triggerRefreshAfterInput() {
	a.previewMu.Lock()
	if !a.previewBusy {
		a.previewTime = time.Time{}
	}
	a.previewMu.Unlock()
}

// scrollDown moves the full-screen scroll offset down by one line.
func (a *App) scrollDown() {
	a.fullScreenScrollY++
}

// scrollUp moves the full-screen scroll offset up by one line (min 0).
func (a *App) scrollUp() {
	if a.fullScreenScrollY > 0 {
		a.fullScreenScrollY--
	}
}

func (a *App) enterFullScreen(sessionID string) {
	a.fullScreen = true
	a.fullScreenTarget = sessionID
	a.inputMode = ModeInsert
	a.fullScreenScrollY = 0
	a.previewCache = ""
	// Set cursor to the target session once at entry (not in layout)
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
	a.fullScreen = false
	a.fullScreenTarget = ""
	a.inputMode = ModeInsert
	a.previewCache = ""
}
