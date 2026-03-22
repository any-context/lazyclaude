Output outputs/worktree/worktree.gif
Output outputs/worktree/worktree.txt

Set Shell "bash"
Set Width 1800
Set Height 1200
Set FontSize 16
Set WaitTimeout 30s

# SSH 先にログイン (worktree 作成先として使用)
Type "ssh remote" Enter
Wait+Screen /root@/

# Ctrl+\ で lazyclaude popup 起動
Ctrl+\
Wait+Screen /No sessions/

# w で worktree ダイアログを開く
Type "w"
Wait+Screen /Branch/

# ブランチ名入力
Type "fix-popup"
Sleep 500ms

# Tab でプロンプト欄に切り替え
Tab
Sleep 500ms

# プロンプト入力
Type "Fix the notification popup bug"
Sleep 500ms

# Ctrl+D で確定 → worktree 作成
# Note: Ctrl+D also sends EOF to tmux display-popup, closing the popup.
# The SSH session also receives EOF and logs out. This is expected behavior.
Ctrl+D
Sleep 3s

# Re-login to SSH and re-open popup to verify the [W] session
Type "ssh remote" Enter
Wait+Screen /root@/

Ctrl+\
Wait+Screen /\[W\]/
Sleep 2s

# q で終了
Type "q" Sleep 1s
