# Phase 7: tmux display-popup Integration

**Created**: 2026-03-20
**Status**: P7.2 Complete (Step 10 done), P7.3 next

---

## Overview

Replace the gocui overlay popup system (P5) with tmux `display-popup` based popups
that render OUTSIDE the main TUI. The MCP server becomes the popup orchestrator,
spawning `lazyclaude tool`/`lazyclaude diff` inside `tmux display-popup`. This enables
popups to intercept active Claude Code sessions in arbitrary tmux windows without
requiring the lazyclaude TUI to be focused.

## Architecture

### Current Flow (P5 -- gocui overlay)

```
Claude Code -> MCP server -> notify queue (file) -> GUI ticker (100ms) polls
                                                         |
                                              PopupController.Push() -> gocui overlay
                                                         |
                                              User choice -> SendChoice() -> tmux send-keys
```

### New Flow (P7 -- tmux display-popup)

```
Claude Code -> MCP server -> POST /notify -> server.handleNotify()
                                                    |
                                          tmux display-popup -E "lazyclaude tool ..."
                                                    |
                                          gocui inside popup PTY (separate process)
                                                    |
                                          User choice -> send-keys to Claude pane
```

### Key Differences from P5

1. **MCP server is the popup orchestrator** -- not the TUI. The server spawns
   `display-popup` directly when it receives a notification, matching `mcp-server.js`.
2. **No notification queue for popups** -- the server handles popup lifecycle
   synchronously (spawn popup, wait for close, send key). The file-based notify queue
   remains only as a fallback for TUI overlay mode.
3. **`lazyclaude tool`/`lazyclaude diff` already exist** and render gocui inside a
   terminal. They work unmodified inside `display-popup` because it provides a PTY.
4. **Input bypass handled by popup process** -- the popup process sends the choice key
   to Claude's pane on exit, not the TUI.

---

## The Input Bypass Problem

### Data Flow (step by step)

```
1. Claude Code runs tool -> PreToolUse hook -> POST /notify (type=tool_info)
2. Claude Code shows permission dialog -> Notification hook -> POST /notify
3. MCP server resolves PID -> tmux window (e.g., @3 in session "lazyclaude")
4. MCP server spawns: tmux display-popup -E "lazyclaude tool --window @3 --send-keys"
5. User sees gocui popup inside tmux display-popup
6. User presses 'y' -> gocui exits with ChoiceAccept
7. Popup process maps choice to key: ChoiceAccept -> "1"
8. Popup process captures Claude pane, detects maxOption, clamps key
9. Popup process calls: tmux send-keys -t "lazyclaude:@3" "1"
   (single key, no Enter -- Claude's dialog selects immediately)
10. display-popup closes, Claude Code proceeds
```

### Design Decisions

**A. Popup process sends the key (not the server)**

The popup process itself calls `tmux send-keys` on exit. Rationale:
- Knows exactly when the user chose (no race between popup close and server read).
- The old JS `installChoiceHandler` adds a 100ms delay on `proc.on('close')`.
  Direct send from the popup process eliminates that latency.
- If the popup crashes without choosing, no key is sent (safe default).

**B. maxOption detection**

Claude Code's permission dialog shows numbered options (`1. Yes`, `2. Allow always`,
`3. No`). Some dialogs have only 2 options. Before sending the key, the popup process:
1. Calls `tmux capture-pane -t lazyclaude:@3 -p` to get current pane content.
2. Scans for `^\s*(?:[>]\s+)?(\d+)[.)]` patterns to find the highest option number.
3. Clamps the choice if it exceeds maxOption (e.g., choice=3 on a 2-option dialog
   sends "2").

This matches `detectMaxOption()` in `mcp-server.js` (line 231).

**C. send-keys target format**

The `--window` flag receives a bare window ID (e.g., `@3`). The send-keys logic
prepends `lazyclaude:` to form the target `lazyclaude:@3`. This matches the existing
`sessionAdapter.SendChoice()` in `cmd/lazyclaude/root.go` (line 255).

**D. Race conditions**

1. **openDiff race**: Claude sends `openDiff` via WebSocket, then immediately fires the
   permission_prompt Notification hook. The diff popup must complete BEFORE the
   notification arrives. Solution: `pendingDiffChoices` map in server State (same as
   JS `pendingDiffChoices` Map). When notification arrives and a diff choice exists,
   skip the popup and send the key directly.
2. **Popup and HTTP handler**: `display-popup -E` blocks until the subprocess exits.
   The server must spawn the popup in a goroutine so the HTTP 200 is returned
   immediately (same as JS: `endSocket()` before async popup).

---

## Files Affected

| File | Change |
|------|--------|
| `cmd/lazyclaude/tool.go` | Add `--send-keys` flag for direct send-keys on exit |
| `cmd/lazyclaude/diff.go` | Add `--send-keys` flag for direct send-keys on exit |
| `cmd/lazyclaude/sendkeys.go` | New: shared send-keys-on-exit helper |
| `cmd/lazyclaude/setup.go` | Implement keybind registration, hooks, server ensure |
| `internal/server/popup.go` | New: popup orchestration (spawn, size, queue) |
| `internal/server/server.go` | Route notifications to popup orchestrator |
| `internal/server/handler.go` | Route openDiff to display-popup |
| `internal/server/state.go` | Add `pendingDiffChoices` map |
| `internal/core/tmux/types.go` | Extend `PopupOpts` with `Env` and `Target` |
| `internal/core/tmux/exec.go` | Extend `DisplayPopup` to pass env vars |
| `internal/core/config/hooks.go` | New: Claude Code hooks configuration |
| `internal/gui/choice/detect.go` | New: `DetectMaxOption` utility |
| `lazyclaude.tmux` | New: TPM plugin entry shell script |

---

## Implementation Steps

### Phase 7.1: `lazyclaude tool --send-keys` (direct input bypass)

Goal: The existing `lazyclaude tool` and `lazyclaude diff` subcommands gain the
ability to send the user's choice directly to Claude Code's tmux pane after exit.

#### Step 1: maxOption detection utility

**File**: `internal/gui/choice/detect.go` (new)

- Create `DetectMaxOption(paneContent string) int`.
  Scans capture-pane output for `^\s*(?:[>]\s+)?(\d+)[.)]` patterns.
  Returns highest option number found, or 3 as default.
- Port from: `mcp-server.js` `detectMaxOption()` (line 231-238).

#### Step 2: Shared send-keys-on-exit helper

**File**: `cmd/lazyclaude/sendkeys.go` (new)

- Create `sendChoiceToPane(window string, choice gui.Choice) error`.
  1. Create `tmux.NewExecClient()` -- uses default tmux server (not lazyclaude socket).
  2. Build target: if window has no `:`, prepend `lazyclaude:`.
  3. Map choice to key via `choiceToKey`.
  4. If choice is Cancel, skip send-keys.
  5. Call `CapturePaneContent(target)`, `DetectMaxOption()`, clamp key.
  6. Call `SendKeys(target, clampedKey)` -- single key, no Enter.

#### Step 3: Add `--send-keys` to `lazyclaude tool`

**File**: `cmd/lazyclaude/tool.go`

- Add `--send-keys` boolean flag. After gocui exits, if `sendKeys && window != ""`:
  call `sendChoiceToPane(window, choice)`.

#### Step 4: Add `--send-keys` to `lazyclaude diff`

**File**: `cmd/lazyclaude/diff.go` -- same as Step 3.

#### Step 5: Extend `PopupOpts` with Env and Target

**File**: `internal/core/tmux/types.go`

- Add `Env map[string]string` and `Target string` to `PopupOpts`.

#### Step 6: Extend `DisplayPopup` for env vars

**File**: `internal/core/tmux/exec.go`

- Prepend `KEY='VALUE'` to `opts.Cmd` for each env var.
- Support `opts.Target` via `-t` flag.

#### Tests for Phase 7.1

| File | Type | What |
|------|------|------|
| `internal/gui/choice/detect_test.go` | Unit | `DetectMaxOption`: 2-option, 3-option, no options |
| `tests/integration/popup_sendkeys_test.go` | E2E | tool popup + cat listener, press 'y', verify cat received "1" |

---

### Phase 7.2: Server-side popup spawning

Goal: MCP server spawns `tmux display-popup` when it receives a notification.

#### Step 7: Popup orchestrator

**File**: `internal/server/popup.go` (new)

- `PopupOrchestrator` with `SpawnToolPopup()`, `SpawnDiffPopup()`, `EstimateSize()`.
- Each spawn: find active client, get terminal size, estimate popup size,
  spawn `display-popup -E` in goroutine.
- Port `estimateToolPopupSize()` from `mcp-server.js` (line 241-298).

#### Step 8: pendingDiffChoices in State

**File**: `internal/server/state.go`

- Add `diffChoices map[string]string` (window -> choice key) with 15s TTL.
- `SetDiffChoice()`, `GetDiffChoice()` (consume on get).

#### Step 9: Wire popup orchestrator into handleNotify

**File**: `internal/server/server.go`

- permission_prompt branch:
  1. Check pendingDiffChoices first -- send key directly if exists.
  2. Classify tool: Edit/Write/MultiEdit -> diff popup, others -> tool popup.
  3. Call orchestrator (goroutine, return HTTP 200 immediately).

#### Step 10: Wire openDiff to display-popup

**File**: `internal/server/handler.go`

- `handleOpenDiff()`: write new contents to temp file, spawn diff popup,
  wait for close, store choice in pendingDiffChoices.

#### Tests for Phase 7.2

| File | Type | What |
|------|------|------|
| `internal/server/popup_test.go` | Unit | `EstimateSize`, pendingDiffChoices |
| `tests/integration/server_popup_test.go` | E2E | notify -> display-popup -> choice -> send-keys |

---

### Phase 7.3: Popup queue (sequential)

Goal: Multiple notifications for the same client are queued and shown sequentially.

#### Step 11: Popup queue tracker

- `activePopups map[string]*popupProc` and `queues map[string][]popupReq` in orchestrator.
- If active popup exists for client, enqueue instead of spawning.

#### Step 12: Queue drain loop

- On popup process exit: check queue, pop next, spawn after 200ms delay.

#### Tests for Phase 7.3

| File | Type | What |
|------|------|------|
| `tests/integration/popup_queue_test.go` | E2E | 3 rapid notifications, 3 sequential popups, 3 keys arrive |

---

### Phase 7.4: `lazyclaude setup` and lazyclaude.tmux

Goal: TPM plugin entry + tmux keybind registration + Claude hooks configuration.

#### Step 13: Claude Code hooks configuration

**File**: `internal/core/config/hooks.go` (new)

- `ReadClaudeSettings()`, `HasLazyClaudeHooks()`, `SetLazyClaudeHooks()`,
  `WriteClaudeSettings()`.
- Hook commands use `curl -s -X POST` to `/notify` with auth token.

#### Step 14: Implement `lazyclaude setup`

**File**: `cmd/lazyclaude/setup.go`

- Keybind registration via tmux options (`@claude-launch-key` etc).
- MCP server ensure (already implemented).
- Claude Code hooks write.

#### Step 15: Create `lazyclaude.tmux`

**File**: `lazyclaude/lazyclaude.tmux`

- Minimal TPM entry: find binary, run `lazyclaude setup`.

#### Tests for Phase 7.4

| File | Type | What |
|------|------|------|
| `internal/core/config/hooks_test.go` | Unit | Merge hooks into various settings states |
| `tests/integration/setup_test.go` | E2E | setup in tmux, verify keybindings + port file |

---

### Phase 7.5: TUI fallback mode

Goal: Overlay popups when tmux display-popup is unavailable.

#### Step 16: PopupMode config (Auto/Tmux/Overlay)
#### Step 17: Route notifications based on mode in `app.go`

---

## E2E Test Design

### Challenge

`capture-pane` cannot see `display-popup` content. Test via side-effect verification.

### Primary Strategy: verify choice key arrival at cat listener

```
1. Start tmux session with `cat` in a pane
2. Start MCP server
3. Register PID via ide_connected
4. POST /notify -> server spawns display-popup
5. send-keys 'y' to client (goes to popup)
6. Popup exits, sends "1" to cat pane
7. capture-pane on cat -> assert contains "1"
```

### Test Cases

| # | Name | Scope |
|---|------|-------|
| 1 | Basic popup spawn + dismiss | P7.2 |
| 2 | Diff popup + pendingDiffChoices | P7.2 |
| 3 | Popup queue (3 sequential) | P7.3 |
| 4 | SSH remote popup | P7.2 + SSH |
| 5 | lazyclaude setup | P7.4 |

---

## Phase Dependencies

```
P7.1 (--send-keys)  -----> P7.2 (server popup) -----> P7.3 (queue)

P7.4 (setup + TPM)  -----> (independent)

P7.5 (fallback)     -----> (independent)
```

Recommended order: **P7.1 and P7.4 in parallel -> P7.2 -> P7.3 -> P7.5**

---

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| tmux < 3.3 (no display-popup) | P7.2 unusable | Version check in setup, overlay fallback |
| openDiff + notify race | Double popup | pendingDiffChoices with 15s TTL |
| send-keys to stale window | Wrong target | Validate window exists before send-keys |
| Multiple tmux clients | Popup on wrong client | FindActiveClient() |
| gocui inside display-popup | Rendering failure | Subcommands are simpler than main TUI, ensure TERM |
| Concurrent notifications | Queue corruption | Mutex-protected queue per client |
