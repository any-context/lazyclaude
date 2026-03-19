#!/bin/bash
# Verify keystroke order is preserved in full-screen mode.
# Critical for IME input (Japanese, Chinese, etc.) where
# goroutine scheduling must not reorder keystrokes.
#
# PASS: characters appear in order in capture-pane
# FAIL: characters are reordered

set -euo pipefail

BINARY="${1:-lazyclaude}"
UI_SOCKET="keyorder-test"

cleanup() {
    tmux -L "$UI_SOCKET" kill-server 2>/dev/null || true
    tmux -L lazyclaude kill-server 2>/dev/null || true
    rm -f /tmp/lazyclaude-mcp.port
    rm -f "$HOME/.local/share/lazyclaude/state.json"
}
trap cleanup EXIT

cleanup
sleep 0.5

echo "Key order test: $BINARY" >&2

tmux -L "$UI_SOCKET" new-session -d -s test -x 60 -y 25 "$BINARY"
sleep 3

# Create session
tmux -L "$UI_SOCKET" send-keys -t test "n"
sleep 2

# Enter full-screen
tmux -L "$UI_SOCKET" send-keys -t test "Enter"
sleep 3

# Wait for Claude Code prompt
sleep 5

# Send Japanese characters rapidly (the actual bug scenario)
tmux -L "$UI_SOCKET" send-keys -t test "あ" "い" "う" "え" "お"
sleep 2

OUTPUT=$(tmux -L "$UI_SOCKET" capture-pane -p -t test 2>/dev/null)
echo "$OUTPUT" >&2
echo "" >&2

if echo "$OUTPUT" | grep -q "あいうえお"; then
    echo "  PASS: key order preserved (あいうえお found)" >&2
    exit 0
else
    echo "  FAIL: あいうえお not found in output" >&2
    exit 1
fi
