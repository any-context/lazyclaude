# Optimistic UI + Error Display: Development Plan

Based on the investigation in [optimistic-ui-issue.md](./optimistic-ui-issue.md).

## Phase 1: Async Conversion (Pessimistic UI -> Optimistic UI)

### 1.1 Rename goroutine化

**File:** `internal/gui/keybindings.go:95-108`

Current: `a.sessions.Rename()` called synchronously in rename-input Enter handler.

Change:
- Capture `renameID` and `newName` before goroutine
- Call `a.closeRenameInput(g)` before goroutine (don't keep dialog open during async)
- Wrap `a.sessions.Rename()` in `go func()`
- Use `a.gui.Update()` + `showError` / `setStatus` for result

```go
// Before
if err := a.sessions.Rename(a.dialog.RenameID, newName); err != nil {
    a.showError(g, fmt.Sprintf("Error: %v", err))
} else {
    a.setStatus(g, "Renamed to "+newName)
}
a.closeRenameInput(g)

// After
renameID := a.dialog.RenameID
a.closeRenameInput(g)
go func() {
    err := a.sessions.Rename(renameID, newName)
    a.gui.Update(func(g *gocui.Gui) error {
        if err != nil {
            a.showError(g, fmt.Sprintf("Error: %v", err))
        } else {
            a.setStatus(g, "Renamed to "+newName)
        }
        return nil
    })
}()
```

### 1.2 layout Sessions() キャッシュ + 非同期更新

**Files:** `internal/gui/layout.go:218`, `internal/gui/layout.go:335`

Current: `a.sessions.Sessions()` called every layout cycle (100ms ticker).
On remote, this queries all providers including HTTP calls to remote daemons.

Change:
- Add `cachedSessionItems []SessionItem` field to `App`
- Add `sessionItemsDirty bool` field (set true by Create/Delete/Rename callbacks
  and by periodic ticker)
- In layout, use `cachedSessionItems` directly (never call Sessions() in layout)
- Refresh in a background goroutine triggered by dirty flag:

```go
// In layout:
items := a.cachedSessionItems // always cached, never block

// Refresh trigger (in ticker goroutine or gui.Update):
if a.sessionItemsDirty {
    a.sessionItemsDirty = false
    go func() {
        items := a.sessions.Sessions()
        a.gui.Update(func(g *gocui.Gui) error {
            a.cachedSessionItems = items
            return nil
        })
    }()
}
```

Dirty flag set by:
- `createSession` success callback
- `DeleteSession` success callback
- `Rename` success callback
- `PurgeOrphans` success callback
- Periodic ticker (every 100ms, same as current layout cycle)
- Broker events (activity state changes)

### 1.3 HistorySize goroutine化

**Files:** `internal/gui/app_actions.go:843` (ScrollModeToTop),
`internal/gui/app_actions.go:905` (ScrollModeEnter)

Current: `a.sessions.HistorySize()` called synchronously. This runs
`tmux display-message` which is fast locally but slow over remote.

Change: Query HistorySize in existing `captureScrollbackAsync` goroutine.

```go
// ScrollModeEnter / ScrollModeToTop:
// Remove synchronous HistorySize call
// Instead, query in captureScrollbackAsync and update via gui.Update

func (a *App) captureScrollbackAsync() {
    target := a.fullscreen.Target()
    if target == "" { return }
    gen := a.scroll.Generation()
    startLine, endLine := a.scroll.CaptureRange()
    viewW := a.scrollViewWidth()
    queryHistSize := a.scroll.NeedsHistorySize() // new flag

    go func() {
        if queryHistSize {
            if histSize, err := a.sessions.HistorySize(target); err == nil && histSize > 0 {
                // Will be applied in gui.Update below
                a.gui.Update(func(g *gocui.Gui) error {
                    if a.scroll.Generation() == gen {
                        a.scroll.SetMaxOffset(histSize)
                    }
                    return nil
                })
            }
        }
        result, err := a.sessions.CaptureScrollback(target, viewW, startLine, endLine)
        a.gui.Update(func(g *gocui.Gui) error {
            if err != nil || a.scroll.Generation() != gen { return nil }
            a.scroll.SetLines(splitLines(result.Content))
            return nil
        })
    }()
}
```

## Phase 2: Error Display Unification

### 2.1 Pattern A: setStatus -> showError (9 cases)

All error messages currently using `setStatus()` should use `showError()` instead.

| # | File:Line | Current | Change |
|---|-----------|---------|--------|
| 1 | app_actions.go:315 | `a.setStatus(g, fmt.Sprintf("Suspend error: %v", err))` | `a.showError(g, ...)` |
| 2 | app_actions.go:326 | `a.setStatus(g, fmt.Sprintf("lazygit error: %v", launchErr))` | `a.showError(g, ...)` |
| 3 | app_actions.go:343 | `a.setStatus(g, fmt.Sprintf("Suspend error: %v", err))` | `a.showError(g, ...)` |
| 4 | app_actions.go:354 | `a.setStatus(g, fmt.Sprintf("Attach error: %v", attachErr))` | `a.showError(g, ...)` |
| 5 | app_actions.go:413 | `a.setStatus(g, "Error: could not open worktree dialog")` | `a.showError(g, ...)` |
| 6 | app_actions.go:438 | `a.setStatus(g, "Error: could not open worktree chooser")` | `a.showError(g, ...)` |
| 7 | app_actions.go:451 | `a.setStatus(g, "Error: could not open connect dialog")` | `a.showError(g, ...)` |
| 8 | keybindings.go:247 | `a.setStatus(g, "Error: could not open worktree dialog")` | `a.showError(g, ...)` |
| 9 | keybindings.go:253 | `a.setStatus(g, "Error: could not open prompt dialog")` | `a.showError(g, ...)` |

### 2.2 Pattern B: Plugin/MCP errMsg -> showError (3 write sites + 2 render sites)

**Write sites (change to showError):**

| # | File:Line | Current | Change |
|---|-----------|---------|--------|
| 1 | app_actions.go:635 | `a.pluginState.errMsg = "only project-scoped..."` | `a.showError(g, "...")` (needs gui.Update since this is sync) |
| 2 | app_actions.go:689 | `a.pluginState.errMsg = err.Error()` | `a.showError(g, fmt.Sprintf("Plugin error: %v", err))` |
| 3 | app_actions.go:758 | `a.mcpState.errMsg = err.Error()` | `a.showError(g, fmt.Sprintf("MCP error: %v", err))` |

**Render sites (remove errMsg display):**

| # | File:Line | Change |
|---|-----------|--------|
| 4 | render_plugins.go:39-41 | Remove errMsg yellow text block |
| 5 | render_mcp.go:24-27 | Remove errMsg yellow text block |

**Cleanup (remove errMsg fields):**

| # | File:Line | Change |
|---|-----------|--------|
| 6 | plugin_state.go:47 | Remove `errMsg string` field |
| 7 | mcp_state.go:28 | Remove `errMsg string` field |
| 8 | app_actions.go:683 | Remove `a.pluginState.errMsg = ""` clear |
| 9 | app_actions.go:752 | Remove `a.mcpState.errMsg = ""` clear |

### 2.3 Pattern C #7: composite_adapter Sessions() error

**File:** `cmd/lazyclaude/composite_adapter.go:309`

Current:
```go
func (a *guiCompositeAdapter) Sessions() []gui.SessionItem {
    sessions, err := a.cp.Sessions()
    if err != nil {
        fmt.Fprintf(os.Stderr, "warning: composite sessions: %v\n", err)
        return nil
    }
    ...
}
```

Change: Return error to caller. This requires modifying the `SessionProvider`
interface to return `([]SessionItem, error)` for `Sessions()`, or alternatively
use a callback/channel to report errors to the GUI.

**Option A (interface change):** `Sessions() ([]SessionItem, error)` -- affects
all implementations and callers. Clean but high blast radius.

**Option B (error callback):** Add `OnError func(string)` to adapter, wired
to `app.ShowError` via gui.Update. No interface change.

**Recommended: Option B** -- minimal change, consistent with how other async
errors are reported.

```go
type guiCompositeAdapter struct {
    // ... existing fields
    onError func(msg string) // wired to app.ShowError via gui.Update
}

func (a *guiCompositeAdapter) Sessions() []gui.SessionItem {
    sessions, err := a.cp.Sessions()
    if err != nil {
        if a.onError != nil {
            a.onError(fmt.Sprintf("Session list error: %v", err))
        }
        return nil
    }
    ...
}

// In root.go, after creating app:
compositeAdapter.onError = func(msg string) {
    app.Gui().Update(func(g *gocui.Gui) error {
        app.ShowError(g, msg)
        return nil
    })
}
```

## Phase 3: Verification

1. `go build ./...`
2. `go vet ./...`
3. `go test ./internal/... -count=1 -race`
4. `/go-review` with all findings addressed
5. Manual TUI testing: trigger each error path, confirm main view display

## Worker Allocation

### Parallelizable work units

| Unit | Scope | Dependencies | Estimated Size |
|------|-------|-------------|----------------|
| W1: Async Rename + setStatus->showError | Phase 1.1 + Phase 2.1 | None | Small (2 files, ~20 line changes) |
| W2: Plugin/MCP errMsg removal | Phase 2.2 | None | Small (6 files, ~15 line changes) |
| W3: Sessions cache + composite error | Phase 1.2 + Phase 2.3 | None | Medium (3 files, ~40 line changes, new App fields) |
| W4: HistorySize async | Phase 1.3 | None | Small (1 file, ~20 line changes) |

**W1 and W2** can run in parallel (no file overlap).
**W3** is independent but larger; can run in parallel with W1/W2.
**W4** is independent; can run in parallel with all others.

All four units can run concurrently if four workers are available.
Minimum serial order: any single worker can handle all in sequence
(W1 -> W2 -> W3 -> W4) with no dependency issues.
