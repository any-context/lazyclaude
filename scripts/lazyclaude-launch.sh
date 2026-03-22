#!/bin/bash
# Common launcher for lazyclaude.
# Used by both lazyclaude.tmux (display-popup) and standalone invocation.
#
# tmux plugin: called with args (pane_cmd pane_pid pane_tty pane_path pane_cwd)
# standalone:  called with no args, queries tmux directly

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY="${SCRIPT_DIR}/../bin/lazyclaude"

if [ ! -x "$BINARY" ]; then
    echo "lazyclaude: binary not found at $BINARY" >&2
    echo "Run 'make build' in $(dirname "$SCRIPT_DIR")" >&2
    exit 1
fi

# Capture pane info.
# tmux plugin: args provided by run-shell (#{} expanded at keypress time)
# standalone:  no args, query tmux directly
if [ $# -ge 4 ]; then
    PANE_CMD="$1"
    PANE_PID="$2"
    PANE_TTY="$3"
    PANE_PATH="$4"
    PANE_CWD="${5:-.}"
elif [ -n "$TMUX" ]; then
    PANE_CMD=$(tmux display-message -p '#{pane_current_command}')
    PANE_PID=$(tmux display-message -p '#{pane_pid}')
    PANE_TTY=$(tmux display-message -p '#{pane_tty}')
    PANE_PATH=$(tmux display-message -p '#{pane_path}')
    PANE_CWD=$(tmux display-message -p '#{pane_current_path}')
fi

export LAZYCLAUDE_PANE_CMD="${PANE_CMD:-}"
export LAZYCLAUDE_PANE_PID="${PANE_PID:-}"
export LAZYCLAUDE_PANE_TTY="${PANE_TTY:-}"
export LAZYCLAUDE_PANE_PATH="${PANE_PATH:-}"

# If already inside a popup (LAZYCLAUDE_POPUP_MODE set), just exec the binary.
if [ -n "${LAZYCLAUDE_POPUP_MODE:-}" ]; then
    exec "$BINARY" "$@"
fi

# Otherwise, open a display-popup with the binary.
LAZYCLAUDE_HOST_TMUX="$TMUX"
exec tmux display-popup -b rounded -w 80% -h 80% -d "${PANE_CWD:-.}" \
    -E "LAZYCLAUDE_HOST_TMUX='$LAZYCLAUDE_HOST_TMUX' \
        LAZYCLAUDE_PANE_CMD='$PANE_CMD' \
        LAZYCLAUDE_PANE_PID='$PANE_PID' \
        LAZYCLAUDE_PANE_TTY='$PANE_TTY' \
        LAZYCLAUDE_PANE_PATH='$PANE_PATH' \
        LAZYCLAUDE_POPUP_MODE=tmux \
        env -u TMUX $BINARY"
