Output outputs/ssh_worktree_pm/ssh_worktree_pm.gif
Output outputs/ssh_worktree_pm/ssh_worktree_pm.txt

Set Shell "bash"
Set Width 1600
Set Height 1200
Set FontSize 16
Set WaitTimeout 90s

# SSH into remote-host, cd to project directory
Sleep 1s
Type "ssh -t remote 'cd /root/app/test-app && exec bash'" Enter
Sleep 3s

# Launch lazyclaude (Ctrl+\) — detects SSH pane, pendingHost="remote"
Ctrl+\
Wait+Screen /No sessions/
Sleep 3s

# P: create PM session first (under project root)
Type "P"
Wait+Screen /\[PM\]/
Sleep 3s

# w: create worktree (under same project root)
Type "w"
Wait+Screen /Branch/
Type "feat-test"
Tab
Type "test worktree"
Enter

# Verify: both [PM] and [W] under same project "remote: test-app"
Wait+Screen /\[W\]/
Sleep 3s

# Verify tree structure:
#   remote: test-app
#     [PM] pm ?
#     [W] feat-test ?

# Enter fullscreen on [W] feat-test — verify independent session
Down
Down
Enter
Wait+Screen /Claude Code/
Sleep 3s

# Exit fullscreen
Ctrl+\
Wait+Screen /Sessions/
Sleep 2s

# Enter fullscreen on [PM] pm — verify it shows different content
Up
Enter
Wait+Screen /Claude Code/
Sleep 3s

# Exit fullscreen
Ctrl+\
Sleep 2s

# Quit
Type "q"
Sleep 1s
