Output outputs/popup_stack/popup_stack.gif
Output outputs/popup_stack/popup_stack.txt

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

# フルスクリーン
Enter
Wait+Screen /Sonnet/

# Claude に複数ツールを使うタスクを依頼
Type "create 3 files: /tmp/a.txt with 'aaa', /tmp/b.txt with 'bbb', /tmp/c.txt with 'ccc'" Enter

# popup が表示される → y で承認 (3回)
Wait+Screen /Do you want/
Type "y"
Wait+Screen /Do you want/
Type "y"
Wait+Screen /Do you want/
Type "y" Sleep 3s

# フルスクリーンから戻る → 終了
Ctrl+\ Sleep 1s
Type "q" Sleep 1s
