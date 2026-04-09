Output outputs/ssh_notify/ssh_notify.gif
Output outputs/ssh_notify/ssh_notify.txt

Set Shell "bash"
Set Width 1600
Set Height 1200
Set FontSize 16
Set WaitTimeout 120s

# SSH into remote-host, cd to project directory
Sleep 1s
Type "ssh -t remote 'cd /root/app/test-app && exec bash'" Enter
Sleep 3s

# Launch lazyclaude (Ctrl+\)
Ctrl+\
Wait+Screen /No sessions/
Sleep 3s

# n: create plain session on remote
Type "n"
Wait+Screen /remote: test-app/
Sleep 5s

# Enter fullscreen on the session
Down
Enter
Wait+Screen /Claude Code/
Sleep 3s

# Ask Claude to create a file (triggers Write tool -> permission popup)
Escape
Wait+Screen /❯/
Type "create a file called /tmp/test-notify.txt with the text hello" Enter

# Wait for the permission popup overlay (C-y to approve)
Wait+Screen /C-y/
Sleep 3s

# Approve the permission
Ctrl+y
Sleep 5s

# Quit
Type "q"
Sleep 1s
