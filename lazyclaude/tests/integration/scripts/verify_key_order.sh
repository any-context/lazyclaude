#!/bin/bash
# Verify keystroke order is preserved in full-screen mode.
# Critical for IME input (Japanese, Chinese, etc.) where
# goroutine scheduling must not reorder keystrokes.
#
# PASS: characters appear in order in capture-pane
# FAIL: characters are reordered

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/test_lib.sh"

init_test "Key Order Test" "${1:-lazyclaude}" "${@:2}"

start_session "$BINARY"
sleep 3
frame "TUI started"

# Create session
send_keys "n"
sleep 2
frame "session created"

# Enter full-screen
send_keys "Enter"
sleep 3
frame "full-screen entered"

# Wait for Claude Code prompt
sleep 5
frame "waiting for Claude Code prompt"

# Send Japanese characters rapidly (the actual bug scenario)
send_keys "あ" "い" "う" "え" "お"
sleep 2
frame "after sending あいうえお"

OUTPUT=$(capture)
if echo "$OUTPUT" | grep -q "あいうえお"; then
    check "key order preserved (あいうえお found)" 0
else
    check "key order preserved (あいうえお found)" 1
fi

finish_test
