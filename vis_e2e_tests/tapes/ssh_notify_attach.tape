Output outputs/ssh_notify_attach/ssh_notify_attach.gif
Output outputs/ssh_notify_attach/ssh_notify_attach.txt

Set Shell "bash"
Set Width 1200
Set Height 800
Set FontSize 16
Set WaitTimeout 60s

# SSH 先にログイン
Type "ssh remote" Enter Sleep 3s

# Ctrl+\ で lazyclaude popup 起動
Ctrl+\ Sleep 5s

# n でリモートセッション作成
Type "n" Sleep 10s

# Enter で attach (gocui Suspend → tmux attach)
Enter Sleep 15s

# Claude にファイル作成を依頼
Type "create a file /tmp/hello.txt with content 'hello world'" Enter

# 通知が来るまで待機
Sleep 30s

# y で承認 (lazyclaude の display-popup or Claude Code 自身のUI)
Type "y" Sleep 3s

# detach して lazyclaude に戻る
Ctrl+\ Sleep 3s

# q で終了
Type "q" Sleep 1s
