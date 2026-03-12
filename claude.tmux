#!/usr/bin/env bash
# tmux-claude: Claude AI session manager for tmux
# Configurable options (set in tmux.conf / plugins.conf):
#   @claude-launch-key        key to launch claude (default: a)
#   @claude-resume-key        key to launch claude --resume (default: A)
#   @claude-switch-key        key to open session switcher (default: O)
#   @claude-suppress-keys     space-separated keys to disable inside claude popup (default: "w")

CURRENT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
SCRIPTS_DIR="$CURRENT_DIR/scripts"

launch_key=$(tmux show-option -gv @claude-launch-key 2>/dev/null)
resume_key=$(tmux show-option -gv @claude-resume-key 2>/dev/null)
switch_key=$(tmux show-option -gv @claude-switch-key 2>/dev/null)
suppress_keys=$(tmux show-option -gv @claude-suppress-keys 2>/dev/null)

launch_key="${launch_key:-a}"
resume_key="${resume_key:-A}"
switch_key="${switch_key:-O}"
suppress_keys="${suppress_keys:-w}"

tmux bind-key "$launch_key" run-shell "${SCRIPTS_DIR}/claude-launch.sh \"#{pane_current_command}\" \"#{pane_pid}\" \"#{pane_current_path}\" \"#{pane_path}\" \"#{session_name}\" \"#{pane_tty}\""
tmux bind-key "$resume_key" run-shell "${SCRIPTS_DIR}/claude-launch.sh \"#{pane_current_command}\" \"#{pane_pid}\" \"#{pane_current_path}\" \"#{pane_path}\" \"#{session_name}\" \"#{pane_tty}\" \"--resume\""
tmux bind-key "$switch_key" if -F '#{==:#{session_name},claude}' \
  "detach-client" \
  "display-popup -w80% -h70% -E '${SCRIPTS_DIR}/claude-switch.sh'"

# Suppress specified keys inside claude popup, preserving original binding elsewhere
for key in $suppress_keys; do
  original=$(tmux list-keys -T prefix 2>/dev/null | awk -v k="$key" '$4 == k {$1=$2=$3=$4=""; sub(/^[[:space:]]+/,""); print}')
  if [ -n "$original" ]; then
    tmux bind-key "$key" if -F '#{==:#{session_name},claude}' '' "$original"
  else
    tmux bind-key "$key" if -F '#{==:#{session_name},claude}' '' ''
  fi
done
