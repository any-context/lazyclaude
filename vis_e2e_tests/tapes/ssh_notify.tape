Output outputs/ssh_notify/ssh_notify.gif
Output outputs/ssh_notify/ssh_notify.txt

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

# フルスクリーン (Claude Code が起動するのを待つ)
Type "r" Sleep 15s

# Claude にファイル作成を依頼 (Write ツール → permission_prompt → 通知)
Type "create a file /tmp/hello.txt with content 'hello world'" Enter

# 通知 popup が来るまで待機
Sleep 30s

# y で承認
Type "y" Sleep 3s

# 終了
Ctrl+\ Sleep 1s
Type "q" Sleep 1s
