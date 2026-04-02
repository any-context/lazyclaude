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

### Fallback: tmux send-keys

If lazyclaude CLI is not available, bypass the API and paste
the message directly into the tmux pane:

```bash
tmux -L lazyclaude send-keys -l -t <window-id> -- "<message text>"
tmux -L lazyclaude send-keys -t <window-id> Enter
```

Use `lazyclaude sessions -v` to find the recipient's `window` field (e.g. `@4`).
