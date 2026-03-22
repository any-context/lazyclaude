Output outputs/ssh_notify/ssh_notify.gif
Output outputs/ssh_notify/ssh_notify.txt

Set Shell "bash"
Set Width 1800
Set Height 1200
Set FontSize 16
Set WaitTimeout 60s

# SSH 先にログイン
Type "ssh remote" Enter
Wait+Screen /root@/

# Ctrl+\ で lazyclaude popup 起動
Ctrl+\
Wait+Screen /No sessions/

# n でリモートセッション作成
Type "n"
Wait+Screen /remote:/

# --- モード選択 (どちらか一方をコメントアウト) ---

# bypass モード: Enter でフルスクリーン (TUI 内プレビュー、キーは send-keys 転送)
Enter
Wait+Screen /Sonnet/

# attach モード: a で tmux attach (gocui Suspend → tmux session に直接接続)
# Type "a"
# Wait+Screen /Sonnet/

# --- ここまで ---

# Claude にファイル作成を依頼 (Write ツール → permission_prompt → 通知)
Type "create a file /tmp/hello.txt with content 'hello world'" Enter

# ツール許可ポップアップが来るまで待機 (Write or Bash)
Wait+Screen /Do you want/

# y で承認
Type "y" Sleep 3s

# 終了
Ctrl+\ Sleep 1s
Type "q" Sleep 1s
