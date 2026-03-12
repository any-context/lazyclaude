#!/bin/zsh
# claude-switch.sh - claude session manager with preview and send keys

TMUX_BIN=$(command -v tmux)

if ! $TMUX_BIN has-session -t "claude" 2>/dev/null; then
  $TMUX_BIN display-message "No claude session running"
  exit 0
fi

CURRENT_WINDOW=$($TMUX_BIN display-message -p -t claude '#{window_name}' 2>/dev/null)

SELECTED=$($TMUX_BIN list-windows -t claude -F "#{window_name}	#{pane_current_command}	#{pane_current_path}	#{pane_path}" | \
  while IFS=$'\t' read name cmd dirpath oscpath; do
    if [ "$cmd" = "ssh" ]; then
      remote_host=$(echo "$oscpath" | sed 's|file://\([^/]*\).*|\1|')
      remote_path=$(echo "$oscpath" | sed 's|file://[^/]*||')
      if [ -n "$remote_path" ]; then
        label="[$remote_host] $remote_path"
      else
        label="[remote] $name"
      fi
    else
      label="[local] $dirpath"
    fi
    [ "$name" = "$CURRENT_WINDOW" ] && marker="*" || marker=" "
    printf "claude:=%s\t%s %s\n" "$name" "$marker" "$label"
  done | \
  fzf \
    --delimiter='\t' \
    --with-nth=2 \
    --border rounded \
    --padding 1,2 \
    --header $'  Claude Sessions\n  Enter: open  1/2/3: send  ctrl-x: kill\n' \
    --header-first \
    --preview "$TMUX_BIN capture-pane -t {1} -p -e -S - 2>/dev/null | tail -50" \
    --preview-window 'right:60%:wrap:border-left' \
    --bind "1:execute-silent($TMUX_BIN send-keys -t {1} '1' Enter)" \
    --bind "2:execute-silent($TMUX_BIN send-keys -t {1} '2' Enter)" \
    --bind "3:execute-silent($TMUX_BIN send-keys -t {1} '3' Enter)" \
    --bind "ctrl-x:execute-silent($TMUX_BIN kill-window -t {1})+abort")

[ -z "$SELECTED" ] && exit 0

WINDOW=$(echo "$SELECTED" | cut -f1 | sed 's/claude:=//')
[ -z "$WINDOW" ] && exit 0

if [ "${CLAUDE_SWITCH_MODE:-}" = "select" ]; then
  echo "$WINDOW"
else
  exec env -u TMUX $TMUX_BIN attach-session -t "claude:=$WINDOW"
fi
