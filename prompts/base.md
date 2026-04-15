## Communication Reference

### List active sessions

```bash
lazyclaude sessions
```

Use `lazyclaude sessions -v` to see additional details including tmux window IDs.

### Spawn a new Worker session

```bash
lazyclaude msg create --from %s --name <worker-name> --type worker --prompt "<initial task>"
```

Optional flags: `--profile <name>` selects a launch profile (empty uses the effective default).
`--options "--flag1 --flag2"` appends extra flags to the claude invocation (space-separated).

### Resume a terminated Worker session

```bash
lazyclaude sessions resume <session-id> --name <worker-name> [--prompt "<new task>"]
```

Respawns a dead session with the same ID. The `--name` flag specifies the worktree directory name (required when the session has been garbage-collected from state.json). Claude Code conversation history is preserved via `--resume`.

### Fallback: tmux send-keys

If lazyclaude CLI is not available, bypass the API and paste
the message directly into the tmux pane:

```bash
tmux -L lazyclaude send-keys -l -t <window-id> -- "<message text>"
tmux -L lazyclaude send-keys -t <window-id> Enter
```

Use `lazyclaude sessions -v` to find the recipient's `window` field (e.g. `@4`).
