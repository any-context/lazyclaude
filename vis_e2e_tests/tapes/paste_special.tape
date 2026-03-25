Output outputs/paste_special/paste_special.gif
Output outputs/paste_special/paste_special.txt

Set Shell "bash"
Set Width 1800
Set Height 1200
Set FontSize 16
Set WaitTimeout 60s

# lazyclaude 起動
Type "lazyclaude" Enter
Wait+Screen /No sessions/

# セッション作成
Type "n"
Wait+Screen /app/

# Enter でフルスクリーン
Enter
Wait+Screen /Sonnet/

# entrypoint が 15 秒後に ESC[200~ + 10行日本語テキスト + ESC[201~ を送信
# Claude Code が [Pasted text #1 +10 lines] を表示するはず
Wait+Screen /Pasted text/

Sleep 3s
Ctrl+\ Sleep 1s
Type "q" Sleep 1s
