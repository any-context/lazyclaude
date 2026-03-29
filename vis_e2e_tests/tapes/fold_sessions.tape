Output outputs/fold_sessions/fold_sessions.gif
Output outputs/fold_sessions/fold_sessions.txt

Set Shell "bash"
Set Width 1800
Set Height 1200
Set FontSize 16
Set WaitTimeout 30s

# Ctrl+\ で lazyclaude popup 起動
Ctrl+\
Wait+Screen /No sessions/

# n でセッション作成 → プロジェクトが展開状態で表示される
Type "n"
Wait+Screen /app/
Sleep 5s

# h でプロジェクトを折りたたみ (カーソルがプロジェクト行にある時のみ動作)
# 新規作成直後はカーソルがセッション行にいるので、まず k でプロジェクト行へ
Type "k"
Sleep 2s
Type "h"
Sleep 3s

# l でプロジェクトを展開 (セッション行が再表示)
Type "l"
Sleep 3s

# q で終了
Type "q" Sleep 1s
