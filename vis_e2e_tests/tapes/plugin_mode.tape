Output outputs/plugin_mode/plugin_mode.gif
Output outputs/plugin_mode/plugin_mode.txt

Set Shell "bash"
Set Width 1800
Set Height 1200
Set FontSize 16
Set WaitTimeout 60s

# 1. 起動時レイアウト: 3パネル同時表示を確認
Ctrl+\
Wait+Screen /No sessions/
Sleep 2s

# 2. Tab: Sessions -> Plugins にフォーカス移動
Tab
Sleep 2s

# 3. ] タブ切替: Installed -> Marketplace
Type "]"
Sleep 2s

# 4. [ タブ切替: Marketplace -> Installed
Type "["
Sleep 2s

# 5. j/k ナビゲーション (Plugins パネル内)
Type "j" Sleep 500ms
Type "j" Sleep 500ms
Type "k" Sleep 500ms

# 6. Tab: Plugins -> Logs
Tab
Sleep 2s

# 7. Tab: Logs -> Sessions に復帰 (右パネルがセッションプレビューに戻る)
Tab
Sleep 2s

# 8. 終了
Type "q" Sleep 1s
