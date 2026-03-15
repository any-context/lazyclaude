Restart the MCP server, preserving the current port so Claude Code reconnects automatically.

Run these two bash commands in order:

```bash
PREV_PORT="$(cat /tmp/tmux-claude-mcp.port 2>/dev/null)"; pkill -f mcp-server.js 2>/dev/null; sleep 0.5; find ~/.claude/ide -name '*.lock' -delete 2>/dev/null; TMUX_CLAUDE_PORT="${PREV_PORT:-0}" node /Users/kenshin/.local/share/tmux/plugins/tmux-claude/scripts/mcp-server.js &
```

Then verify only one process is running and the port is preserved:

```bash
sleep 1 && echo "PID: $(cat /tmp/tmux-claude-mcp.pid)" && echo "port: $(cat /tmp/tmux-claude-mcp.port)" && echo "procs: $(pgrep -c -f mcp-server.js)"
```

Claude Code will reconnect automatically within a few seconds since the port is preserved.