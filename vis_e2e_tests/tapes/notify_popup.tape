Output outputs/notify_popup/notify_popup.gif
Output outputs/notify_popup/notify_popup.txt

Set Shell "bash"
Set Width 1800
Set Height 1200
Set FontSize 16
Set WaitTimeout 60s

# lazyclaude 起動
Type "lazyclaude" Enter
Wait+Screen /No sessions/

# n でセッション作成
Type "n"
Wait+Screen /app/

# フルスクリーン
Enter
Wait+Screen /Sonnet/

# Claude にファイル作成を依頼 (Write ツールは許可が必要)
Type "create a file /tmp/hello.txt with content 'hello world'" Enter

# すぐに lazyclaude を閉じる
Sleep 3s
Ctrl+\ Sleep 1s
Type "q" Sleep 1s

# popup が閉じた状態で通知 popup が来るか待機 (Write or Bash)
Wait+Screen /Do you want/

# y で承認 (通知 popup が出ていれば)
Type "y" Sleep 3s
