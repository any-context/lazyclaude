Output outputs/paste_special/paste_special.gif
Output outputs/paste_special/paste_special.txt

Set Shell "bash"
Set Width 1800
Set Height 1200
Set FontSize 16
Set WaitTimeout 90s

# bash wrapper auto-attaches to tmux session "main".
# Start lazyclaude directly so that paste goes through PTY -> tcell EventPaste.
Sleep 2s
Type "lazyclaude --debug" Enter
Wait+Screen /No sessions/

# Create a session
Type "n"
Sleep 5s

# Move cursor to session row, then enter fullscreen.
Type "j"
Sleep 1s
Enter
Sleep 3s

# entrypoint.sh sends paste via "tmux paste-buffer -p" at ~20s.
# Wait for the paste to arrive and be processed.
Sleep 15s

# Verify TUI is still responsive after paste:
# Type a character — if TUI is frozen, this will hang VHS.
# If TUI is alive, the char gets forwarded to Claude Code pane.
Type "h"
Sleep 2s
Type "e"
Sleep 2s

# The fullscreen status bar should still be visible (not frozen).
Wait+Screen /exit/

Sleep 1s
