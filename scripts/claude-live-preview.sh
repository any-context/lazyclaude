#!/usr/bin/env bash
# claude-live-preview.sh — live preview of a tmux pane for fzf
# Usage: claude-live-preview.sh <tmux-target>
#
# Strategy: poll capture-pane at ~50ms, output only when content changes.
# capture-pane -e gives clean ANSI (no raw cursor-movement codes).
# fzf replaces the preview on each output — change-only emission avoids flash.

TARGET="$1"

prev=""
while true; do
  curr=$(tmux capture-pane -t "$TARGET" -p -e -S -50 2>/dev/null)
  if [ "$curr" != "$prev" ]; then
    printf '%s\n' "$curr"
    prev="$curr"
  fi
  sleep 0.05
done
