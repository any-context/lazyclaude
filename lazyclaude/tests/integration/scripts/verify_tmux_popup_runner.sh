#!/bin/bash
# Runner for verify_tmux_popup.sh — uses `script` to allocate a PTY,
# then starts tmux and runs the test inside it.
# display-popup requires an attached tmux client with a real terminal.

set -euo pipefail

BINARY="${1:-lazyclaude}"
SOCKET="popup-e2e-runner"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
RESULT_FILE="/tmp/popup-e2e-result-$$"

cleanup() {
    tmux -L "$SOCKET" kill-server 2>/dev/null || true
    rm -f "$RESULT_FILE"
}
trap cleanup EXIT

cleanup

# Use `script` to create a PTY, attach tmux, run test, exit
script -q -c "
    tmux -L $SOCKET new-session -d -s runner -x 120 -y 40
    tmux -L $SOCKET send-keys -t runner \
        'LAZYCLAUDE_TMUX_SOCKET=$SOCKET bash $SCRIPT_DIR/verify_tmux_popup.sh $BINARY; echo RESULT=\$? > $RESULT_FILE; tmux -L $SOCKET kill-server' Enter
    tmux -L $SOCKET attach -t runner
" /dev/null 2>&1 || true

# Read result
if [ -f "$RESULT_FILE" ]; then
    EXIT_CODE=$(grep -oP 'RESULT=\K\d+' "$RESULT_FILE" 2>/dev/null || echo "1")
    exit "$EXIT_CODE"
else
    echo "FAIL: test did not produce result file" >&2
    exit 1
fi
