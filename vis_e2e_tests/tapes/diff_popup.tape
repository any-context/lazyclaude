Output outputs/diff_popup/diff_popup.gif
Output outputs/diff_popup/diff_popup.txt

Set Shell "bash"
Set Width 1800
Set Height 1200
Set FontSize 16
Set WaitTimeout 30s

# lazyclaude 起動
Type "lazyclaude" Enter
Wait+Screen /No sessions/

# セッション作成
Type "n"
Wait+Screen /app/

# フルスクリーン
Enter
Wait+Screen /Sonnet/

# Claude に編集依頼
Type "edit /tmp/test.go and change hello to goodbye" Enter

# diff popup が表示される → y で承認
Wait+Screen /Do you want/
Type "y" Sleep 3s

# フルスクリーンから戻る → 終了
Ctrl+\ Sleep 1s
Type "q" Sleep 1s
