Output outputs/ssh_msg/ssh_msg.gif
Output outputs/ssh_msg/ssh_msg.txt

Set Shell "bash"
Set Width 1600
Set Height 1200
Set FontSize 16
Set WaitTimeout 90s

# SSH into remote-host, cd to project directory
Sleep 1s
Type "ssh -t remote 'cd /root/app/test-app && exec bash'" Enter
Sleep 3s

# Launch lazyclaude (Ctrl+\)
Ctrl+\
Wait+Screen /No sessions/
Sleep 3s

# P: create PM session
Type "P"
Wait+Screen /\[PM\]/
Sleep 3s

# w: create worktree (worker target)
Type "w"
Wait+Screen /Branch/
Type "msg-test"
Tab
Type "message test worker"
Enter
Wait+Screen /\[W\]/
Sleep 5s

# Quit popup and exit SSH to return to local shell
Type "q"
Sleep 1s
Type "exit" Enter
Sleep 1s

# Now on local VHS container shell. Send message from PM to worker.
Type "bash-real /app/vis_e2e_tests/scripts/send_test_msg.sh" Enter
Wait+Screen /Message sent/
Sleep 2s

# SSH back and re-open lazyclaude to verify message delivery
Type "ssh -t remote 'cd /root/app/test-app && exec bash'" Enter
Sleep 2s
Ctrl+\
Wait+Screen /\[PM\]/
Sleep 3s

# Select worker session and enter fullscreen to see the delivered message
Down
Down
Enter
Wait+Screen /hello from PM/
Sleep 5s

# Exit fullscreen
Ctrl+\
Sleep 2s

# Quit
Type "q"
Sleep 1s
