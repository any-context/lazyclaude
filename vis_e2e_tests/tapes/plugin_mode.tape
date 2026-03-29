Output outputs/plugin_mode/plugin_mode.gif
Output outputs/plugin_mode/plugin_mode.txt

Set Shell "bash"
Set Width 1800
Set Height 1200
Set FontSize 16
Set WaitTimeout 60s

# ============================
# Plugin Management E2E Test
# ============================
# entrypoint.sh が事前に agent-sdk-dev と plugin-dev をインストール済み
# 公式マーケットプレイスが追加済み

# 1. 起動: Sessions / Plugins / Logs の3パネル表示
Ctrl+\
Wait+Screen /No sessions/
Sleep 2s

# 2. Tab → Plugins にフォーカス (Installed タブ)
#    右パネルにプラグイン詳細が表示される
#    agent-sdk-dev, plugin-dev が [E] で表示される
Tab
Sleep 3s

# 3. j で2つ目のプラグインに移動
Type "j"
Sleep 2s

# 4. e で disable → [D] に変わる
Type "e"
Sleep 3s

# 5. e で enable → [E] に戻る
Type "e"
Sleep 3s

# 6. d でアンインストール
Type "d"
Sleep 3s

# 7. ] で Marketplace タブに切替
#    マーケットプレイスのプラグイン一覧が表示される
Type "]"
Sleep 3s

# 8. j でナビゲーション
Type "j" Sleep 500ms
Type "j" Sleep 500ms

# 9. [ で Installed タブに戻る
Type "["
Sleep 2s

# 10. Tab → Logs → Sessions に戻り、右パネルがセッションプレビューに復帰
Tab
Sleep 1s
Tab
Sleep 2s

# 11. 終了
Type "q" Sleep 1s
