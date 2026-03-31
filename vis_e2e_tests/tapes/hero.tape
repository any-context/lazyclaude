Output outputs/hero/hero.gif
Output outputs/hero/hero.txt

Set Shell "bash"
Set Width 1600
Set Height 1200
Set FontSize 16
Set WaitTimeout 90s

# Launch lazyclaude via tmux plugin (Ctrl+\)
Sleep 1s
Ctrl+\
Wait+Screen /No sessions/
Sleep 1s

# Create a new Claude Code session
Type "n"
Wait+Screen /⟳/
Sleep 1s

# Navigate to the session row and enter fullscreen
Down
Enter
Wait+Screen /Sonnet/
Sleep 1s

# Dismiss welcome screen and type a prompt
Escape
Wait+Screen /❯/
Sleep 1s
Type "create a file /tmp/hello.txt with content 'Hello from lazyclaude!'" Enter
Sleep 1s

# Exit fullscreen
Ctrl+\

# Permission popup appears as a gocui overlay in session list view
Wait+Screen /C-y/
Sleep 2s

# Approve the permission
Ctrl+y
Wait+Screen /✓/
Sleep 1s

# Exit lazyclaude
Type "q"
Sleep 1s
