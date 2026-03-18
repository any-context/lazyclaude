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
// Async to avoid blocking gocui event loop (~5ms subprocess per key).
func (a *App) forwardKey(ch rune) {
	target := a.resolveForwardTarget()
	if target == "" {
		return
	}
	fwd := a.inputForwarder
	key := RuneToTmuxKey(ch)
	go fwd.ForwardKey(target, key)
	a.triggerRefreshAfterInput()
}

func (a *App) forwardSpecialKey(tmuxKey string) {
	target := a.resolveForwardTarget()
	if target == "" {
		return
	}
	fwd := a.inputForwarder
	go fwd.ForwardKey(target, tmuxKey)
	a.triggerRefreshAfterInput()
}

// triggerRefreshAfterInput marks preview as stale after sending a key.
// Rate-limited to 50ms to prevent capture-per-keystroke during fast typing.
// Only called in insert mode (resolveForwardTarget blocks normal mode).
func (a *App) triggerRefreshAfterInput() {
	a.fullScreenScrollY = 0
	a.previewMu.Lock()
	if !a.previewBusy && time.Since(a.previewTime) > 50*time.Millisecond {
		a.previewTime = time.Time{}
	}
	a.previewMu.Unlock()
}


// setInputMode switches between insert and normal mode.
func (a *App) setInputMode(mode InputMode) {
	if a.inputMode == mode {
		return
	}
	a.inputMode = mode
}

func (a *App) enterFullScreen(sessionID string) {
	a.fullScreen = true
	a.fullScreenTarget = sessionID
	a.inputMode = ModeInsert
	a.fullScreenScrollY = 0
	// Force immediate re-capture with new dimensions
	a.previewMu.Lock()
	a.previewCache = ""
	a.previewTime = time.Time{} // stale → triggers capture on next layout
	a.previewMu.Unlock()
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
	a.inputMode = ModeInsert
	a.fullScreen = false
	a.fullScreenTarget = ""
	a.previewCache = ""
}
