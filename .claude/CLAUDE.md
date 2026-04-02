# lazyclaude

## Testing

### Unit tests (run on host)

```bash
go test ./internal/... -count=1
go test -cover ./internal/...
```

### VHS visual E2E (Docker required)

```bash
make test-vhs TAPE=smoke

# Check specific frame
awk '/\[Frame 5\]/,/\[Frame 6\]/{if(/\[Frame 6\]/)exit; print}' vis_e2e_tests/outputs/smoke/smoke.log
```

- Tapes contain only human interactions. Test setup goes in `entrypoint.sh`
- Output: `outputs/{name}/` with `.gif` + `.txt` + `.log`
- Launch lazyclaude via tmux plugin (`Ctrl+\`), not the binary directly. The Dockerfile's bash wrapper runs `lazyclaude setup` + `lazyclaude.tmux` automatically, so just press `Ctrl+\` in the tape to open the popup
- When working in worktrees, use `.claude/worktree/`. Check for container name conflicts before running (`docker compose ps`)
- After tests, run `open vis_e2e_tests/outputs/<tape>/` to inspect GIF results in Finder

### E2E manual debugging (Docker shell)

When VHS tapes are insufficient for reproduction/verification, ask the user to debug in a container shell:

```bash
docker compose -p lazyclaude-e2e-$(git rev-parse --short HEAD) \
  -f vis_e2e_tests/docker-compose.ssh.yml build

docker compose -p lazyclaude-e2e-$(git rev-parse --short HEAD) \
  -f vis_e2e_tests/docker-compose.ssh.yml run --rm vhs bash
```

- Include commit hash in `-p` to avoid conflicts with other runs
- Use only when automated E2E tests hit their limits; ask the user to run it

### Claude Code auth (Docker)

```bash
claude setup-token
echo "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-..." > vis_e2e_tests/.env
```

## gocui notes

### ErrUnknownView comparison

`==` and `errors.Is` do not match. Use string comparison:

```go
func isUnknownView(err error) bool {
    return err != nil && strings.Contains(err.Error(), "unknown view")
}
```

### Editor and keybinding dispatch order

```
1. View-specific bindings (popupViewName etc.)
2. Editor.Edit() — only for views with Editable=true
3. Global bindings — but rune keys (ch!=0) skip global bindings on Editable views
```

### Frame=false view coordinates

With `Frame=false`, the content area is still `(x0+1, y0+1)` to `(x1-1, y1-1)`.
The frame is not drawn, but the y0/y1 rows are not used for content.
When placing a frameless bar, note that y0+1 is where text starts.

```
InnerWidth  = Width  - 2  (always)
InnerHeight = Height - 2  (always)
```

### Ctrl+[ and Esc

Same byte (0x1B). Indistinguishable in gocui/tcell.
lazyclaude uses **Ctrl+\\** for normal mode toggle.

### Paste handling

- Bracketed paste is aggregated at the pollEvent level into a single `eventPasteContent` sent to gEvents
- Structurally prevents gEvents channel overflow (capacity 20)
- ESC[200~ fallback detection: workaround for tmux display-popup where tcell cannot send EventPaste
- inputEditor has no paste state machine. It just calls `forwardPaste` via `OnPasteContent` callback

### third_party/gocui, third_party/tcell

- `third_party/gocui`: fork of jesseduffield/gocui. Adds paste aggregation, rawEvents pipeline, etc.
- `third_party/tcell`: fork of gdamore/tcell/v2. Minimal build files only. Patches documented in `LAZYCLAUDE_PATCHES.md`
- Referenced via `replace` directives in `go.mod`

## tmux architecture

### Two tmux servers

1. **User's tmux** (default socket) -- displays lazyclaude TUI via `display-popup`
2. **lazyclaude tmux** (`-L lazyclaude` socket) -- manages Claude Code session windows

### Key input flow

```
Outside popup: key -> user's tmux root table -> execute if matched
Inside popup:  key -> delivered directly to popup process (user's tmux root table bypassed)
During attach: key -> lazyclaude tmux root table -> execute if matched
```

### display-popup behavior (tmux 3.4+)

- Used only for TUI launch (`lazyclaude-launch.sh` -> `display-popup`)
- Notification popups are rendered as gocui overlays (display-popup notification mode removed in #18)
- Calling `display-popup` from inside a popup **modifies** the existing popup (not nested)
- Border style can be toggled dynamically with `-b rounded` / `-B`
- Changes disappear when the process inside the popup exits

### `tmux source` does not reset keybindings

It only overwrites or adds. Full reset requires restarting the tmux server.

### MCP server

- Starts in-process at TUI launch (`tryStartInProcessServer`). Existing daemons are stopped via `StopDaemon` first
- `lazyclaude setup` starts a daemon, but TUI launch switches to in-process
- Server log: `/tmp/lazyclaude/server.log` (prefix: `lazyclaude-srv:`)
- Duplicate prevention: `server.IsAlive()` checks port file + TCP dial
- Using `slog.Default()` inside the gocui TUI process corrupts terminal rendering. Return errors via `fmt.Errorf` and display in the GUI layer
- Broker is created in root.go and injected via `WithBroker` option. Survives server restarts (GUI subscriptions remain valid)

### Hook injection

- Hooks are injected at session startup via `claude --settings <file>`. `~/.claude/settings.json` is never modified
- 5 hook types: PreToolUse, Notification, Stop, SessionStart, UserPromptSubmit
- `WriteHooksSettingsFile()` writes to runtime dir. Uses `SetEscapeHTML(false)` to preserve JS operators (`=>`)
- Server discovery always uses lock file scanning (`findAliveLockJS`). No env vars (restart-resilient)

### Activity state (5-stage)

- `ActivityState` enum: Unknown, Running, NeedsInput, Idle, Error, Dead
- Hook events -> broker -> GUI's `windowActivity` map -> sidebar icon update
- On startup: `ActivityUnknown` (gray `?`). Transitions to correct state on first hook event
- On popup dismiss: NeedsInput -> Running immediately

### Performance

- Diagnose performance issues with git bisect on binaries (more reliable than code analysis)
- Checkpoints recorded in `.claude/checkpoints.log`

### SSH command generation

- Remote commands are written as plain bash scripts and base64-encoded
- No nested quoting. Do not use `shell.Quote` inside SSH command strings
- `scripts/lazyclaude-launch.sh` is the entry point for tmux plugin (`Ctrl+\`) only (via display-popup)
- Standalone execution launches the Go binary (`bin/lazyclaude`) directly
