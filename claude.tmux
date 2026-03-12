#!/usr/bin/env bash
# tmux-claude: Claude AI session manager for tmux
# Bindings:
#   prefix+a  launch claude at current path (SSH-aware)
#   prefix+A  launch claude --resume
#   prefix+O  session switcher (inline in popup, no nesting)

CURRENT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
SCRIPTS_DIR="$CURRENT_DIR/scripts"

tmux bind-key a run-shell "${SCRIPTS_DIR}/claude-launch.sh \"#{pane_current_command}\" \"#{pane_pid}\" \"#{pane_current_path}\" \"#{pane_path}\" \"#{session_name}\" \"#{pane_tty}\""
tmux bind-key A run-shell "${SCRIPTS_DIR}/claude-launch.sh \"#{pane_current_command}\" \"#{pane_pid}\" \"#{pane_current_path}\" \"#{pane_path}\" \"#{session_name}\" \"#{pane_tty}\" \"--resume\""

# From claude popup: set flag + detach (switcher runs inline in same popup)
# From other session: open switcher popup
tmux bind-key O if -F '#{==:#{session_name},claude}' \
  "run-shell 'touch /tmp/claude-popup-switch && tmux detach-client'" \
  "display-popup -w80% -h70% -E '${SCRIPTS_DIR}/claude-switch.sh'"
