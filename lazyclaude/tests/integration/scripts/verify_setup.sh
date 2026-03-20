#!/bin/bash
# Verify `lazyclaude setup` registers hooks and starts MCP server.
#
# PASS: all checks pass
# FAIL: any check fails

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/test_lib.sh"

init_test "Setup Command Test" "${1:-lazyclaude}" "${@:2}"

# Backup existing settings
[ -f "$HOME/.claude/settings.json" ] && cp "$HOME/.claude/settings.json" "$HOME/.claude/settings.json.bak"

# Remove existing hooks to test fresh install
rm -f "$HOME/.claude/settings.json"

# Need a tmux session for setup to work
tmux -L "$TEST_SOCKET" new-session -d -s test -x "$TEST_WIDTH" -y "$TEST_HEIGHT"

frame "before setup (clean state)"

# Run setup
LAZYCLAUDE_TMUX_SOCKET="$TEST_SOCKET" "$BINARY" setup 2>&1

# Show setup output in a frame
send_keys "echo '--- setup complete ---'" Enter
sleep 0.5
frame "after setup"

# 1. MCP server port file should exist (server starts async, wait)
for i in $(seq 1 30); do [ -f /tmp/lazyclaude-mcp.port ] && break; sleep 0.1; done
R=0; [ -f /tmp/lazyclaude-mcp.port ] || R=1
check "MCP port file exists" $R

if [ $R -eq 0 ]; then
    PORT=$(cat /tmp/lazyclaude-mcp.port)
    send_keys "echo 'MCP port: $PORT'" Enter
    sleep 0.3
    frame "MCP port file"
fi

# 2. Claude settings.json should have hooks
R=0; [ -f "$HOME/.claude/settings.json" ] || R=1
check "settings.json exists" $R

if [ $R -eq 0 ]; then
    # Display settings.json content in the pane for visual verification
    send_keys "cat ~/.claude/settings.json | head -30" Enter
    sleep 0.5
    frame "settings.json content"

    R=0; grep -q "/notify" "$HOME/.claude/settings.json" || R=1
    check "settings.json contains /notify hook" $R

    R=0; grep -q "PreToolUse" "$HOME/.claude/settings.json" || R=1
    check "settings.json contains PreToolUse" $R

    R=0; grep -q "Notification" "$HOME/.claude/settings.json" || R=1
    check "settings.json contains Notification" $R
fi

# 3. Running setup again should be idempotent (no error)
R=0; LAZYCLAUDE_TMUX_SOCKET="$TEST_SOCKET" "$BINARY" setup 2>&1 || R=1
check "setup idempotent (no error on re-run)" $R

send_keys "echo '--- idempotent re-run complete ---'" Enter
sleep 0.3
frame "after idempotent re-run"

# Restore backup
[ -f "$HOME/.claude/settings.json.bak" ] && mv "$HOME/.claude/settings.json.bak" "$HOME/.claude/settings.json"

finish_test
