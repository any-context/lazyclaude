Output outputs/worktree_pm/worktree_pm.gif
Output outputs/worktree_pm/worktree_pm.txt

Set Shell "bash"
Set Width 1600
Set Height 1200
Set FontSize 16
Set WaitTimeout 30s

# Launch lazyclaude via tmux plugin (Ctrl+\)
Sleep 1s
Ctrl+\
Wait+Screen /No sessions/
Sleep 1s

# Test w: open worktree creation dialog
Type "w"
Sleep 2s

# Close worktree dialog
Escape
Sleep 1s

# Test W: open worktree selector (should show empty or list)
Type "W"
Sleep 2s

# Close with Escape
Escape
Sleep 1s

# Test P: create PM session
Type "P"
Sleep 3s

# Quit
Type "q"
Sleep 1s
