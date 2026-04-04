# Optimistic UI + Error Display Issues

## Pessimistic UI Violations

Operations that block the GUI event loop. Remote operations go through
`ensureRemoteConnected` (daemon startup: 2-10s) before the actual operation.

| # | File:Line | Function | Blocking Operation | Est. Duration | Notes |
|---|-----------|----------|-------------------|---------------|-------|
| 1 | keybindings.go:98 | rename-input Enter handler | `a.sessions.Rename()` | 0.5-5s (remote) | Sync on GUI thread. No goroutine wrapper |
| 2 | layout.go:218 | layout() | `a.sessions.Sessions()` | 0.1-2s (remote) | Called every layout cycle. CompositeProvider iterates all remotes |
| 3 | layout.go:335 | layout() (fullscreen) | `a.sessions.Sessions()` | 0.1-2s (remote) | Same as #2, fullscreen path |
| 4 | app_actions.go:843 | ScrollModeToTop | `a.sessions.HistorySize()` | 0.1-1s | Sync tmux ShowMessage call |
| 5 | app_actions.go:905 | ScrollModeEnter (re-query) | `a.sessions.HistorySize()` | 0.1-1s | Same as #4 |

### Already fixed (in this branch, pending review)

| # | File:Line | Function | Blocking Operation | Fix |
|---|-----------|----------|-------------------|-----|
| A | app_actions.go:259 | createSession | `a.sessions.Create()` | Wrapped in goroutine |
| B | app_actions.go:276 | DeleteSession | `a.sessions.Delete()` | Wrapped in goroutine |
| C | app_actions.go:455 | PurgeOrphans | `a.sessions.PurgeOrphans()` | Wrapped in goroutine |

### Already async (no change needed)

| File:Line | Function | Notes |
|-----------|----------|-------|
| app_actions.go:392 | StartPMSession | goroutine |
| app_actions.go:422 | SelectWorktree | goroutine |
| keybindings.go:138 | CreateWorktree handler | goroutine |
| keybindings.go:275 | ResumeWorktree handler | goroutine |
| keybindings.go:338 | ConnectRemote handler | goroutine |
| layout.go:467 | CapturePreview | goroutine |
| app_actions.go:979 | captureScrollbackAsync | goroutine |
| popup.go:34 | dismissPopup SendChoice | goroutine |
| popup.go:52 | dismissAllPopups SendChoice | goroutine |

### Borderline (blocking by design)

| File:Line | Function | Notes |
|-----------|----------|-------|
| app_actions.go:311 | LaunchLazygit | Uses `g.Suspend()` -- must block (takes over terminal) |
| app_actions.go:339 | AttachSession | Uses `g.Suspend()` -- must block (takes over terminal) |

## Error Display Inconsistencies

### Pattern A: `setStatus()` for errors (logs panel only, not visible in main view)

| # | File:Line | Error Message | Should Be |
|---|-----------|---------------|-----------|
| 1 | app_actions.go:315 | `Suspend error: %v` | showError -- user needs to see this |
| 2 | app_actions.go:326 | `lazygit error: %v` | showError -- user needs to see this |
| 3 | app_actions.go:343 | `Suspend error: %v` | showError -- user needs to see this |
| 4 | app_actions.go:354 | `Attach error: %v` | showError -- user needs to see this |
| 5 | app_actions.go:413 | `Error: could not open worktree dialog` | showError |
| 6 | app_actions.go:438 | `Error: could not open worktree chooser` | showError |
| 7 | app_actions.go:451 | `Error: could not open connect dialog` | showError |
| 8 | keybindings.go:247 | `Error: could not open worktree dialog` | showError |
| 9 | keybindings.go:253 | `Error: could not open prompt dialog` | showError |

### Pattern B: Panel-scoped error state (yellow text in specific panel)

These display errors only within their panel context. Not visible from the main
sessions view.

| # | File:Line | Variable | Display Location |
|---|-----------|----------|-----------------|
| 1 | app_actions.go:635 | `pluginState.errMsg` | Plugins panel only |
| 2 | app_actions.go:689 | `pluginState.errMsg` | Plugins panel only |
| 3 | app_actions.go:758 | `mcpState.errMsg` | MCP tab only |

**Action required:** Migrate to showError for consistency. Remove panel-scoped
errMsg display. All errors should appear in both logs and main view.

### Pattern C: `fmt.Fprintf(os.Stderr)` (invisible in TUI)

These are startup/initialization warnings that occur before or outside the TUI.

| # | File:Line | Context | Should Change? |
|---|-----------|---------|---------------|
| 1 | root.go:71 | tmux cmd log open failure | No -- pre-TUI startup |
| 2 | root.go:83 | session load warning | No -- pre-TUI startup |
| 3 | root.go:153 | socket tunnel warning | No -- pre-TUI startup |
| 4 | root.go:282 | server token generation | No -- pre-TUI startup |
| 5 | root.go:305 | MCP server start | No -- pre-TUI startup |
| 6 | root.go:329 | ensureMCPServer | No -- pre-TUI startup |
| 7 | composite_adapter.go:309 | `Sessions()` error | **Yes** -- occurs during TUI operation, silently drops error |
| 8 | setup.go:42 | MCP server daemon | No -- CLI subcommand |
| 9 | daemon_cmd.go:50 | session load | No -- CLI subcommand |

### Pattern D: Suppressed errors (`_ = err`)

| # | File:Line | Operation | Risk |
|---|-----------|-----------|------|
| 1 | popup.go:35 | `SendChoice` | Low -- fire-and-forget is correct design |
| 2 | popup.go:54 | `SendChoice` (batch) | Low -- same as above |
| 3 | app_actions.go:530 | `ForwardKey` | Low -- key forwarding best-effort |

**Verdict:** These suppressions are intentional and appropriate.

## Proposed Error Handling Pattern

```go
// Pattern 1: Async remote operation (MUST for all SessionProvider calls that
// may go through ensureRemoteConnected)
func (a *App) SomeOperation() {
    if a.sessions == nil || a.HasActiveDialog() {
        return
    }
    // Capture values from GUI thread before goroutine
    val := a.someGUIState
    go func() {
        err := a.sessions.RemoteOp(val)
        a.gui.Update(func(g *gocui.Gui) error {
            if err != nil {
                a.showError(g, fmt.Sprintf("Error: %v", err))
            } else {
                a.setStatus(g, "Success message")
            }
            return nil
        })
    }()
}

// Pattern 2: Error display -- always use showError for user-facing errors
// showError writes to BOTH logs panel and main view
a.showError(g, fmt.Sprintf("Error: %v", err))

// Pattern 3: Status message -- use setStatus for success/info only
a.setStatus(g, "Operation completed")

// Pattern 4: Panel-scoped errors -- migrate to showError
// Remove pluginState.errMsg / mcpState.errMsg display, use showError instead
a.showError(g, fmt.Sprintf("Plugin error: %v", err))
```

## Priority

1. **High:** #1 (Rename sync) -- user-initiated operation, blocks GUI
2. **High:** #7 in Pattern C (composite_adapter Sessions error) -- silently drops errors during TUI
3. **High:** Pattern B (plugin/MCP errMsg) -- migrate to showError
4. **Medium:** #1-9 in Pattern A -- `setStatus` -> `showError` migration
5. **Low:** #2-3 (layout Sessions) -- frequent but usually fast locally; blocking only with remote
6. **Low:** #4-5 (HistorySize) -- only affects scroll mode, brief block
