#!/bin/bash
# Verify tmux display-popup VISUAL E2E.
#
# Proves the real lazyclaude tool gocui popup renders inside display-popup by:
# 1. Running display-popup with `script` to record the PTY output
# 2. Replaying the recording into a tmux pane
# 3. Using capture-pane to extract readable text
# 4. Verifying tool name, command, and action bar are visible
#
# This script MUST run inside a tmux session.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/test_lib.sh"

BINARY="${1:-lazyclaude}"
SOCKET="${LAZYCLAUDE_TMUX_SOCKET:-}"

if [ -z "$SOCKET" ]; then
    echo "FAIL: LAZYCLAUDE_TMUX_SOCKET not set" >&2
    exit 1
fi

# Override test socket to use the provided one
TEST_SOCKET="$SOCKET"

_TEST_NAME="tmux display-popup VISUAL E2E"
PASS=0
FAIL=0
FRAME_NUM=0
_PREV_FRAME_FILE=$(mktemp /tmp/frame-prev-XXXX)
_CURR_FRAME_FILE=$(mktemp /tmp/frame-curr-XXXX)

TMPDIR=$(mktemp -d /tmp/lazyclaude-popup-e2e-XXXX)
cleanup_popup() {
    tmux -L "$SOCKET" kill-window -t lazyclaude:replay 2>/dev/null || true
    rm -rf "$TMPDIR"
    rm -f "$_PREV_FRAME_FILE" "$_CURR_FRAME_FILE" 2>/dev/null || true
}
trap cleanup_popup EXIT

CURRENT_SESSION=$(tmux -L "$SOCKET" display-message -p '#{session_name}')
tmux -L "$SOCKET" rename-session -t "$CURRENT_SESSION" lazyclaude 2>/dev/null || true

SCRIPT_LOG="$TMPDIR/popup.log"

# --- 1. Run lazyclaude tool inside display-popup with script recording ---
POPUP_CMD="LAZYCLAUDE_TMUX_SOCKET=$SOCKET TOOL_NAME=Bash TOOL_INPUT='{\"command\":\"for i in \$(seq 1 10); do echo line_\$i; done && ls /tmp\"}' TOOL_CWD=/tmp timeout 3 script -q -c '$BINARY tool --window @0' $SCRIPT_LOG"

tmux -L "$SOCKET" display-popup -w 80 -h 24 -E "$POPUP_CMD" 2>/dev/null || true

FRAME_NUM=$((FRAME_NUM + 1))
_draw_frame "display-popup executed" "script log: $SCRIPT_LOG"

R=0; [ -f "$SCRIPT_LOG" ] || R=1
check "script log recorded" $R

if [ ! -f "$SCRIPT_LOG" ]; then
    finish_test
    exit 1
fi

# --- 2. Replay into tmux pane for visual capture ---
tmux -L "$SOCKET" new-window -t lazyclaude -n replay
tmux -L "$SOCKET" send-keys -t lazyclaude:replay "cat '$SCRIPT_LOG'" Enter
sleep 1

# -S - captures full scrollback history (title bar may be above visible area)
POPUP_CONTENT=$(tmux -L "$SOCKET" capture-pane -t lazyclaude:replay -p -S -)

FRAME_NUM=$((FRAME_NUM + 1))
_draw_frame "display-popup content (replayed)" "$POPUP_CONTENT"

# --- 3. Verify popup rendered correctly ---
R=0; echo "$POPUP_CONTENT" | grep -qE "Bash|Command:" || R=1
check "popup shows tool context (Bash or Command:)" $R

R=0; echo "$POPUP_CONTENT" | grep -q "Command:" || R=1
check "popup shows 'Command:'" $R

R=0; echo "$POPUP_CONTENT" | grep -q "seq" || R=1
check "popup shows command content" $R

R=0; echo "$POPUP_CONTENT" | grep -qE "yes.*no" || R=1
check "popup shows action bar (yes/no)" $R

R=0; echo "$POPUP_CONTENT" | grep -q "Esc" || R=1
check "popup shows Esc: cancel" $R

# Check border rendering (gocui box drawing)
R=0; echo "$POPUP_CONTENT" | grep -qE "┌|─|└|│" || R=1
check "popup shows gocui border" $R

finish_test
