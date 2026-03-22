#!/usr/bin/env bash
# lazyclaude TPM plugin entry point.
# 1. Runs `lazyclaude setup` (MCP server + Claude Code hooks)
# 2. Registers tmux keybinding that calls the launcher script
#
# Configurable options (set in tmux.conf / plugins.conf):
#   @claude-launch-key    key to launch lazyclaude TUI (default: C-\)
#   @claude-suppress-keys space-separated keys to disable inside lazyclaude session

CURRENT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
LAUNCHER="${CURRENT_DIR}/scripts/lazyclaude-launch.sh"
BINARY="${CURRENT_DIR}/bin/lazyclaude"

if [ ! -x "$BINARY" ]; then
    echo "lazyclaude: binary not found at $BINARY (run 'make build')" >&2
    exit 1
fi

# Detect user's tmux socket.
# If inside lazyclaude tmux, fall back to default socket path.
HOST_SOCKET=$(tmux display-message -p '#{socket_path}' 2>/dev/null || echo "")
if echo "$HOST_SOCKET" | grep -q "lazyclaude"; then
    HOST_SOCKET="/tmp/tmux-$(id -u)/default"
fi

# Run Go setup (MCP server + Claude Code hooks)
LAZYCLAUDE_HOST_TMUX="$HOST_SOCKET" "$BINARY" setup

# Read tmux options
launch_key=$(tmux show-option -gqv @claude-launch-key 2>/dev/null)
suppress_keys=$(tmux show-option -gqv @claude-suppress-keys 2>/dev/null)
launch_key="${launch_key:-C-\\}"

# Keybinding does ONE thing: call the launcher with pane info as arguments.
# run-shell expands #{} formats at keypress time using the active pane's context.
tmux bind-key -T root "$launch_key" run-shell \
    "$LAUNCHER '#{pane_current_command}' '#{pane_pid}' '#{pane_tty}' '#{pane_path}' '#{pane_current_path}'"

# Register detach binding on lazyclaude tmux server (same key).
tmux -L lazyclaude bind-key -T root "$launch_key" detach-client 2>/dev/null

# Suppress specified keys on the lazyclaude tmux server only.
for key in $suppress_keys; do
    tmux -L lazyclaude unbind-key -T prefix "$key" 2>/dev/null
done
