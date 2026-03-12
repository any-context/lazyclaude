#!/usr/bin/env bash
# tmux-claude: Claude AI session manager for tmux
# Configurable options (set in tmux.conf / plugins.conf):
#   @claude-launch-key        key to launch claude (default: a)
#   @claude-resume-key        key to launch claude --resume (default: A)
#   @claude-switch-key        key to open session switcher (default: O)
#   @claude-suppress-w        disable choose-tree inside claude popup (default: on)

CURRENT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
SCRIPTS_DIR="$CURRENT_DIR/scripts"

launch_key=$(tmux show-option -gv @claude-launch-key 2>/dev/null)
resume_key=$(tmux show-option -gv @claude-resume-key 2>/dev/null)
switch_key=$(tmux show-option -gv @claude-switch-key 2>/dev/null)
suppress_w=$(tmux show-option -gv @claude-suppress-w 2>/dev/null)

launch_key="${launch_key:-a}"
resume_key="${resume_key:-A}"
switch_key="${switch_key:-O}"
suppress_w="${suppress_w:-on}"

tmux bind-key "$launch_key" run-shell "${SCRIPTS_DIR}/claude-launch.sh \"#{pane_current_command}\" \"#{pane_pid}\" \"#{pane_current_path}\" \"#{pane_path}\" \"#{session_name}\" \"#{pane_tty}\""
tmux bind-key "$resume_key" run-shell "${SCRIPTS_DIR}/claude-launch.sh \"#{pane_current_command}\" \"#{pane_pid}\" \"#{pane_current_path}\" \"#{pane_path}\" \"#{session_name}\" \"#{pane_tty}\" \"--resume\""
tmux bind-key "$switch_key" if -F '#{==:#{session_name},claude}' \
  "run-shell 'touch /tmp/claude-popup-switch && tmux detach-client'" \
  "display-popup -w80% -h70% -E '${SCRIPTS_DIR}/claude-switch.sh'"

if [ "$suppress_w" = "on" ]; then
  tmux bind-key w if -F '#{==:#{session_name},claude}' '' 'choose-tree -Zw'
fi
