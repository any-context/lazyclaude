#!/bin/zsh
# claude-popup.sh - popup wrapper for claude windows
# Runs attach-session in a loop; when prefix+O sets the flag, shows switcher inline

WINDOW="${1:-}"
TMUX_BIN=$(command -v tmux)
SCRIPT_DIR="${0:A:h}"
FLAG_FILE="/tmp/claude-popup-switch"

while true; do
  env -u TMUX $TMUX_BIN attach-session -t "claude:=$WINDOW"

  # attach-session exited — check if switcher was requested
  if [ -f "$FLAG_FILE" ]; then
    rm -f "$FLAG_FILE"
    NEW_WINDOW=$(CLAUDE_SWITCH_MODE=select "$SCRIPT_DIR/claude-switch.sh")
    [ -n "$NEW_WINDOW" ] && WINDOW="$NEW_WINDOW" && continue
  fi
  break
done
