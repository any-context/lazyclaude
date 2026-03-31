# Implementation Plan: Fullscreen Scroll + Selection Copy

## Requirements

1. **Scrollback browsing**: fullscreen モードで過去の出力にスクロールバックできる
2. **Selection + Copy**: スクロール中にテキストを範囲選択してクリップボードにコピーできる
3. **Persistent**: popup を閉じて再度開いても tmux scrollback buffer は残っているのでスクロール可能

## Technical Foundation

- `capture-pane -ep -S <start> -E <end>` で scrollback buffer の任意範囲を取得可能（検証済み）
- ANSI エスケープコード保持、取得行数を viewHeight に限定すればパフォーマンス同等
- tmux copy-mode は使えない（capture-pane がスクロール状態を無視する）
- LogsState の行選択パターン (`v`/`y`) を再利用

## Architecture Changes

| Change | File | Description |
|--------|------|-------------|
| New | `internal/gui/scroll_state.go` | Pure state machine (no gocui dependency) |
| New | `internal/gui/scroll_state_test.go` | Unit tests |
| Modify | `internal/core/tmux/client.go` | Add `CapturePaneANSIRange` to Client interface |
| Modify | `internal/core/tmux/exec.go` | Implement `CapturePaneANSIRange` |
| Modify | `internal/core/tmux/mock.go` | Mock implementation |
| Modify | `internal/gui/keymap/types.go` | Add `ScopeScroll` and scroll actions |
| Modify | `internal/gui/keymap/registry.go` | Register scroll mode key bindings |
| Modify | `internal/gui/keyhandler/actions.go` | Add `ScrollActions` interface |
| Modify | `internal/gui/keyhandler/fullscreen.go` | Dispatch scroll keys when active |
| Modify | `internal/gui/app.go` | Add `scroll *ScrollState`, `CaptureScrollback` to SessionProvider |
| Modify | `internal/gui/app_actions.go` | Implement ScrollActions methods |
| Modify | `internal/gui/input.go` | Block forwarding in scroll mode |
| Modify | `internal/gui/layout.go` | Render scroll content + status indicator |
| Modify | `internal/gui/export_test.go` | Test helpers |
| Modify | `cmd/lazyclaude/root.go` | Implement `CaptureScrollback` on sessionAdapter |
| Modify | `internal/gui/app_integration_test.go` | Update mock |

## Phase 1: tmux Range Capture

**Step 1**: Add `CapturePaneANSIRange(ctx, target, start, end int) (string, error)` to Client interface.

**Step 2**: Implement in ExecClient: `capture-pane -t <target> -ep -S <start> -E <end>`.

**Step 3**: Add to MockClient with `RangeCaptures map[string]string`.

## Phase 2: Scroll State Machine

**Step 4**: Create `ScrollState` (`internal/gui/scroll_state.go`).

Pure struct, no gocui dependency. Fields:
- `active`, `scrollOffset` (0=live, positive=lines up)
- `cursorY` (within viewport), `selecting`, `selAnchor`
- `lines []string`, `viewHeight`, `maxOffset`
- `generation int` (async race detection)

Methods:
- `Enter(viewHeight int)` -- initial offset = viewHeight (one screen up)
- `Exit()` -- reset all
- `ScrollUp(n) / ScrollDown(n)` -- adjust offset with clamping
- `ToTop() / ToBottom()`
- `CursorUp() / CursorDown()` -- move cursor within viewport
- `ToggleSelect() / SelectionRange() / ClearSelection()`
- `SetLines(lines []string) / CopyText() string`
- `Generation() / BumpGeneration()`
- `CaptureRange(viewHeight int) (start, end int)` -- compute -S/-E from scrollOffset

**Step 5**: Table-driven unit tests for all state transitions.

## Phase 3: Key Bindings and Dispatch

**Step 6**: Add `ScopeScroll` and action constants to `keymap/types.go`:
- `ActionScrollUp/Down/HalfUp/HalfDown/ToTop/ToBottom`
- `ActionScrollToggleSelect/Copy/Exit`
- `ActionScrollEnter` in fullscreen scope

**Step 7**: Register `ScopeScroll` bindings in `keymap/registry.go`:
- k/Up -> ScrollUp, j/Down -> ScrollDown
- Ctrl+U -> ScrollHalfUp, Ctrl+D -> ScrollHalfDown
- g -> ScrollToTop, G -> ScrollToBottom
- v -> ScrollToggleSelect, y -> ScrollCopy
- Esc/q -> ScrollExit
- Fullscreen: Ctrl+V -> ActionScrollEnter

**Step 8**: Add `ScrollActions` interface to `keyhandler/actions.go`, embed in `AppActions`.

**Step 9**: Update `FullScreenHandler.HandleKey`:
- If `actions.IsScrollMode()`: match against `ScopeScroll`, dispatch, consume all keys
- Else: existing `ScopeFullScreen` matching + `ActionScrollEnter` case

## Phase 4: App Integration

**Step 10**: Wire in `app.go`:
- Add `scroll *ScrollState` field
- Add `CaptureScrollback(id, width, startLine, endLine int) (PreviewResult, error)` to `SessionProvider`

**Step 11**: Implement ScrollActions on App (`app_actions.go`):
- `ScrollModeEnter()`: get viewHeight, `scroll.Enter(viewHeight)`, trigger async capture
- Navigation methods: adjust state, bump generation, async capture
- `ScrollModeCopy()`: `scroll.CopyText()` -> `copyToClipboard()` -> exit
- `ScrollModeExit()`: `scroll.Exit()`, invalidate preview
- Async capture helper: compute range, launch goroutine, check generation on return

**Step 12**: Block key forwarding in `input.go`:
- In `inputEditor.Edit()`: `if e.app.scroll.IsActive() { return false }`

**Step 13**: Implement `CaptureScrollback` on sessionAdapter (`root.go`):
- Resolve target, call `CapturePaneANSIRange`, ANSI truncate, no resize/cursor

**Step 14**: Update mock SessionProvider in tests.

## Phase 5: Rendering

**Step 15**: In `layoutFullScreen` (`layout.go`):
- When `scroll.IsActive()`: `v.Clear()`, `v.Editable = false`
- Render `scroll.Lines()` with cursor/selection highlighting
- When scroll exits: `v.Editable = true`

**Step 16**: Status bar during scroll mode:
- `"SCROLL"` indicator + `"[-%d]"` offset position
- `"VISUAL"` when selecting
- Hint bar from `ScopeScroll` entries

## Phase 6: Tests and Verification

**Step 17**: Export test helpers in `export_test.go`.

**Step 18**: Integration tests: enter fullscreen -> Ctrl+U -> scroll -> select -> copy -> exit.

## Offset Calculation

```
scrollOffset = 0  -> live mode (existing capture-pane -ep)
scrollOffset = N  -> capture-pane -ep -S -(N+viewH) -E -(N+1)
```

Example: viewH=40, scrollOffset=10:
- `-S -50`, `-E -11` -> 40 lines, 50 lines from bottom to 11 lines from bottom

When capture returns fewer lines than viewH, top of buffer reached:
`maxOffset = scrollOffset + (viewH - returnedLines)`

## Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| Async capture race (rapid scrolling) | Medium | Generation counter: discard stale results |
| Editable toggle key dispatch | Medium | scroll active -> Editable=false, FullScreenHandler consumes all |
| SessionProvider interface change | Low | Additive, only 2 implementations |
| capture-pane offset off-by-one | Low | Mock-based unit test for computed -S/-E |

## Success Criteria

- [ ] Ctrl+V in fullscreen enters scroll mode
- [ ] j/k scrolls line by line
- [ ] g/G jumps to top/bottom of scrollback
- [ ] Ctrl+U/Ctrl+D scrolls half page
- [ ] v toggles visual line selection
- [ ] y copies selected text to clipboard and exits
- [ ] Esc/q exits scroll mode
- [ ] Status bar shows SCROLL indicator with position
- [ ] Popup notifications still work during scroll mode
- [ ] `go test -race ./internal/...` passes
- [ ] 80%+ coverage on new code
- [ ] VHS E2E or user visual verification
