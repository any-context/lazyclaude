Output outputs/mcp_toggle/mcp_toggle.gif
Output outputs/mcp_toggle/mcp_toggle.txt

Set Shell "bash"
Set Width 1800
Set Height 1200
Set FontSize 16
Set WaitTimeout 60s

# ============================
# MCP Tab E2E Test
# ============================
# entrypoint.sh が ~/.claude.json に MCP サーバーを事前設定済み
# テスト対象: MCP タブ表示、カーソル移動、on/off トグル

# 1. 起動: Sessions / MCP-Plugins-Marketplace / Logs の3パネル表示
Ctrl+\
Wait+Screen /No sessions/
Sleep 2s

# 2. Tab → Plugins パネルにフォーカス (MCP タブがデフォルト)
#    MCP サーバー一覧が表示される (github, memory, vercel)
Tab
Sleep 3s

# 3. j で2つ目のサーバーに移動
Type "j"
Sleep 2s

# 4. e で deny (off) → [D] に変わる
Type "e"
Sleep 3s

# 5. e で enable (on) → [E] に戻る
Type "e"
Sleep 3s

# 6. ] で Plugins タブに切替
Type "]"
Sleep 2s

# 7. ] で Marketplace タブに切替
Type "]"
Sleep 2s

# 8. [ で Plugins に戻る
Type "["
Sleep 2s

# 9. [ で MCP タブに戻る
Type "["
Sleep 2s

# 10. Tab → Logs → Sessions に戻る
Tab
Sleep 1s
Tab
Sleep 2s

# 11. 終了
Type "q" Sleep 1s
