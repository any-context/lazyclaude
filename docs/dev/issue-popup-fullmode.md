# Issue: tmux display-popup Fails for Full Mode Popups

## Summary

lazyclaude's full mode (Enter to attach) suspends gocui and runs `tmux attach-session`.
When Claude Code needs permission approval in this mode, the MCP server attempts to show
a `tmux display-popup` overlay. This approach has fundamental, unsolvable problems.

## Current Architecture

```
Preview mode (gocui running):
  MCP server writes notification file
  -> gocui ticker polls file every 500ms
  -> gocui overlay popup (tool-popup view)
  -> user presses y/a/n
  -> SendChoice via tmux send-keys
  WORKS CORRECTLY

Full mode (gocui suspended):
  MCP server writes notification file
  -> server calls triggerToolPopup()
  -> tmux display-popup runs "lazyclaude tool-popup"
  -> spawns a NEW gocui instance inside the popup
  -> user presses y/a/n
  -> choice written to file, then tmux send-keys
  FAILS
```

## Failure Modes

### 1. gocui inside display-popup does not receive keyboard input

The gocui instance spawned inside `tmux display-popup` does not reliably receive
keyboard events. y/a/n keys are either intercepted by tmux or lost in the PTY chain.

### 2. Esc is intercepted by tmux

Pressing Esc inside a `display-popup` is intercepted by tmux itself, which
closes the popup rather than forwarding the key to the child process (gocui).

### 3. display-popup requires attached clients

`tmux display-popup` requires a client attached to the session. Race conditions
during suspend/resume cause the call to fail silently.

### 4. validateShellSafe blocks internal commands

`DisplayPopup` runs the command through `validateShellSafe()`, which rejects
shell metacharacters. Required adding `Internal: true` bypass flag -- a security smell.

### 5. tmux send-keys cannot target display-popup children

`tmux send-keys` sends to panes, not popup children. No tmux API exists to send
keys into a `display-popup` subprocess. Automated testing is impossible.

## Testing Methodology Problem

`tmux send-keys` is an internal tmux command that writes to the pane's PTY input
buffer. It does NOT reproduce real keyboard input:

- Bypasses terminal emulator key processing
- Does not trigger the same event flow as physical key presses
- Keys sent to a pane are not forwarded to display-popup children
- gocui key handlers may not fire because the event dispatch path differs

Integration tests using `tmux send-keys` can PASS while the actual user
experience is BROKEN.

## Design Decision

**Eliminate `tmux display-popup` entirely. Use gocui for both modes.**

- Full mode renders Claude Code output into a full-screen gocui view using
  the same `capture-pane` + ANSI rendering pipeline as preview mode
- Tool/diff popups overlay on top of this gocui view -- identical in both modes
- No `tmux attach-session`, no `display-popup`, no suspend/resume
- All keyboard input handled by gocui, forwarded to Claude Code pane via
  `tmux send-keys` when in full mode with no popup showing

See: `docs/dev/popup-redesign-plan.md`

## Code to Remove

| File | What |
|------|------|
| `cmd/lazyclaude/tool_popup.go` | display-popup subcommand |
| `internal/server/server.go` | `triggerToolPopup()` |
| `internal/core/tmux/types.go` | `PopupOpts.Internal` |
| `internal/gui/app.go` | `suspended atomic.Bool`, `attachSelected()` |
