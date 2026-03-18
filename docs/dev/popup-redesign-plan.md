# Implementation Plan: Popup System Redesign (gocui-Only Architecture)

## Overview

Replace the dual-path popup system (gocui overlay for preview, tmux display-popup
for full mode) with a single gocui-based rendering pipeline. Full mode renders
Claude Code output into a full-screen gocui view using capture-pane. Tool/diff
popups work identically in both modes as gocui overlays.

## Requirements

- Full mode renders Claude Code pane content in a full-screen gocui view
- User keyboard input in full mode forwarded to Claude Code pane via send-keys
- Tool/diff popups overlay in full mode, identical to preview mode
- Remove: tmux display-popup, tool-popup subcommand, triggerToolPopup, suspend/resume
- Designated key combo exits full mode (Ctrl+D)
- Testable via gocui headless mode

## Phase 1: Full-Screen Rendering

### 1.1 Add fullScreen state to App
- Add `fullScreen bool`, `fullScreenTarget string` fields
- Add `EnterFullScreen(sessionID)` / `ExitFullScreen()` methods

### 1.2 Create layoutFullScreen
- Single borderless "main" view spanning entire terminal
- Status bar: "Ctrl+D: exit full mode"
- Reuse `CapturePreview` pipeline with full terminal dimensions
- Delete "sessions", "server", "options" views when entering full mode

### 1.3 Increase refresh rate in full mode
- Ticker: 100ms in full mode, 500ms in preview mode
- ~10fps effective update rate (capture-pane ~2ms)

## Phase 2: Input Forwarding

### 2.1 Create InputForwarder interface
```go
type InputForwarder interface {
    SendKey(ctx context.Context, target string, key string) error
}
```
- `TmuxInputForwarder` wraps `tmux.Client.SendKeys`
- `MockInputForwarder` records keys for test assertions

### 2.2 Wire to App
- `app.SetInputForwarder(forwarder)`
- `sessionAdapter` in root.go creates `TmuxInputForwarder`

## Phase 3: Full-Mode Keybindings

### 3.1 Extract keybindings to separate file
- Move `setupGlobalKeybindings()` to `keybindings.go`
- Split into preview mode and full-screen mode bindings

### 3.2 Full-mode key forwarding
- When full mode + no popup: forward all keys to Claude Code pane
- Key mapping: rune -> literal, Enter -> "Enter", Tab -> "Tab", etc.
- Reserve Ctrl+D to exit full mode
- Use `g.SetUnhandledKeyHandler()` if available, else register ~120 individual bindings

### 3.3 Popup priority
- When popup showing: y/a/n/Esc go to popup handler (existing)
- When no popup: all keys forwarded to Claude Code pane

## Phase 4: Wire Enter Key

### 4.1 Replace attachSelected
- Enter keybinding -> `EnterFullScreen(item.ID)` instead of `attachSelected(g)`
- Remove `attachSelected()`, `suspended atomic.Bool`
- Remove `!a.suspended.Load()` guard from ticker

## Phase 5: Remove Dead Code

- Delete `cmd/lazyclaude/tool_popup.go`
- Remove `triggerToolPopup()` from server.go
- Remove `PopupOpts.Internal` from types.go
- Remove `Internal` bypass from exec.go DisplayPopup

## Phase 6: Resize Optimization

- Cache last-resized dimensions in CapturePreview
- Skip ResizeWindow + 150ms sleep when dimensions unchanged
- Critical for 100ms refresh interval in full mode

## Key Design Details

### Exit Full Mode: Ctrl+D
- Esc conflicts with vim/Claude Code usage
- Ctrl+D = "detach" convention (tmux)
- Shown in full-mode status bar

### Input Forwarding Latency
- Each keystroke: `tmux send-keys` ~5ms subprocess
- Normal typing (~100ms gap): imperceptible
- Fast paste: batch keystrokes within 10ms window

### Refresh and Flicker
- Only redraw when capture-pane output changed (diff against cache)
- Skip render if content identical to previous frame
- ResizeWindow only on dimension change, not every frame

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| gocui has no catch-all key handler | HIGH | Register individual bindings for ~120 keys |
| Refresh rate causes flicker/CPU | MED | Content-diff before render, skip if unchanged |
| Input forwarding latency | LOW | Async send-keys, batch rapid keystrokes |
| No tmux status bar in full mode | LOW | lazyclaude status bar replaces it |

## Testing Strategy

### Unit (headless gocui)
- `EnterFullScreen`/`ExitFullScreen` toggle
- `layoutFullScreen` creates correct views
- Key-to-sendkeys mapping table
- `MockInputForwarder` records keys

### Integration
- Full-mode entry/exit with mock SessionProvider
- Popup overlay in full mode with mock notification
- Input forwarding: type "hello", verify mock received `["h","e","l","l","o"]`
- Popup interrupts forwarding: popup shown, y goes to popup handler

### E2E (Docker)
- Start lazyclaude, create session, Enter, verify full-screen via capture-pane
- Trigger notification, verify popup appears
- Press y, verify Claude Code receives "1"
- Ctrl+D, verify return to split-panel

## Success Criteria

- [ ] Full mode renders Claude Code in full-screen gocui view
- [ ] User can type into Claude Code prompt in full mode
- [ ] Tool/diff popup identical in both modes
- [ ] y/a/n sends correct key to Claude Code
- [ ] Ctrl+D exits full mode
- [ ] No tmux display-popup in codebase
- [ ] No gocui Suspend/Resume in codebase
- [ ] All tests pass, new tests for full-screen + input forwarding
