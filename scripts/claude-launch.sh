#!/bin/bash
# claude-launch.sh - tmux `prefix + a` handler
# Args: pane_current_command pane_pid pane_current_path pane_path session_name pane_tty [flags]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

PANE_CMD="$1"
PANE_PID="$2"
LOCAL_PATH="$3"
OSC_PATH="$4"    # set via OSC 7 (only useful if remote shell emits it)
SESSION_NAME="$5"
PANE_TTY="${6##/dev/}"  # strip /dev/ prefix, e.g. ttys007
FLAGS="${7:-}"          # e.g. --resume

# Resolve local claude binary
CLAUDE_BIN=$(command -v claude 2>/dev/null)
CLAUDE_BIN="${CLAUDE_BIN:-$HOME/.local/bin/claude}"

_get_ssh_host() {
  # Try children of pane PID first (SSH is typically a child of the shell)
  local ssh_pid
  ssh_pid=$(pgrep -P "$PANE_PID" -x ssh 2>/dev/null | head -1)
  if [ -n "$ssh_pid" ]; then
    ps -p "$ssh_pid" -o args= 2>/dev/null | awk '{print $NF}'
    return
  fi
  # Fallback: scan all processes on this TTY for ssh
  if [ -n "$PANE_TTY" ]; then
    ps -t "$PANE_TTY" -o args= 2>/dev/null | grep '^ssh ' | awk '{print $NF}' | head -1
    return
  fi
  # Last resort: pane PID itself
  ps -p "$PANE_PID" -o args= 2>/dev/null | awk '{print $NF}'
}

_get_remote_dir() {
  [ -z "$OSC_PATH" ] && return
  # OSC 7 format: file://hostname/path
  local osc_host
  osc_host=$(echo "$OSC_PATH" | sed 's|file://\([^/]*\).*|\1|')
  local local_host
  local_host=$(hostname)
  # Only use OSC 7 path if hostname differs from local (i.e., it came from the remote shell)
  if [ "$osc_host" != "$local_host" ] && [ "$osc_host" != "${local_host%%.*}" ]; then
    echo "$OSC_PATH" | sed 's|file://[^/]*||'
  fi
}

if [ "$PANE_CMD" = "ssh" ] && [ "$SESSION_NAME" != "claude" ]; then
  # --- Remote (SSH) case ---
  SSH_HOST=$(_get_ssh_host)
  REMOTE_DIR=$(_get_remote_dir)

  if [ -z "$SSH_HOST" ]; then
    tmux display-message "claude-launch: could not detect SSH host"
    exit 1
  fi

  WINDOW="claude-$(echo "${SSH_HOST}${REMOTE_DIR}" | md5sum | cut -c1-8)"

  # Use login shell on remote so PATH (~/.profile, ~/.zprofile) is loaded
  if [ -n "$REMOTE_DIR" ]; then
    REMOTE_CMD="ssh -t '$SSH_HOST' 'bash -lic \"cd \\\"$REMOTE_DIR\\\" && claude $FLAGS\"'"
  else
    REMOTE_CMD="ssh -t '$SSH_HOST' 'bash -lic \"claude $FLAGS\"'"
  fi

  if ! tmux has-session -t "claude" 2>/dev/null; then
    tmux new-session -d -s "claude" -n "$WINDOW" "$REMOTE_CMD"
  fi

  tmux set-option -t "claude" automatic-rename off 2>/dev/null

  tmux list-windows -t "claude" -F "#{window_name}" | grep -qF "$WINDOW" || \
    tmux new-window -t "claude" -n "$WINDOW" "$REMOTE_CMD"

  tmux display-popup -w80% -h80% -E "$SCRIPT_DIR/claude-popup.sh $WINDOW"

else
  # --- Local case (original behavior) ---
  WINDOW="claude-$(echo "$LOCAL_PATH" | md5sum | cut -c1-8)"

  if [ "$SESSION_NAME" = "claude" ]; then
    tmux detach-client
  else
    CLAUDE_CMD="zsh -lic 'cd \"$LOCAL_PATH\" && claude${FLAGS:+ $FLAGS}'"

    if ! tmux has-session -t "claude" 2>/dev/null; then
      tmux new-session -d -s "claude" -n "$WINDOW" "$CLAUDE_CMD"
    fi

    tmux set-option -t "claude" automatic-rename off 2>/dev/null

    tmux list-windows -t "claude" -F "#{window_name}" | grep -qF "$WINDOW" || \
      tmux new-window -t "claude" -n "$WINDOW" "$CLAUDE_CMD"

    tmux display-popup -w80% -h80% -E "$SCRIPT_DIR/claude-popup.sh $WINDOW"
  fi
fi
