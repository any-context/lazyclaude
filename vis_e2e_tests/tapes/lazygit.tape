Output outputs/lazygit/lazygit.gif
Output outputs/lazygit/lazygit.txt

Set Shell "bash"
Set Width 1800
Set Height 1200
Set FontSize 16
Set WaitTimeout 30s

# Ctrl+\ で lazyclaude popup 起動
Ctrl+\
Wait+Screen /No sessions/

# n でローカルセッション作成
Type "n"
Wait+Screen /app/
Sleep 3s

# g で lazygit 起動
Type "g"
Sleep 8s

# q で lazygit 終了 -> lazyclaude に復帰
Type "q"
Sleep 3s

# q で lazyclaude 終了
Type "q" Sleep 1s
