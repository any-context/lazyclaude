Output outputs/ssh_launch/ssh_launch.gif
Output outputs/ssh_launch/ssh_launch.txt

Set Shell "bash"
Set Width 1800
Set Height 1200
Set FontSize 16
Set WaitTimeout 30s

# SSH 先にログイン
Type "ssh remote" Enter
Wait+Screen /root@/

# Ctrl+\ で lazyclaude popup 起動
Ctrl+\
Wait+Screen /No sessions/

# n でリモートセッション作成
Type "n"
Wait+Screen /remote:/

# q で終了
Type "q" Sleep 1s
