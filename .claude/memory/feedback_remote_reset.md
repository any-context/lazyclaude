---
name: remote_reset_checklist
description: リモートセッションのリセット時に確認すべき全箇所
type: feedback
---

リモートセッションをリセットするときは以下を全て確認・削除する:

1. リモートの daemon プロセス: `pkill -f 'lazyclaude daemon'`
2. リモートの tmux サーバー: `tmux -L lazyclaude kill-server`
3. リモートの state.json: `rm ~/.local/share/lazyclaude/state.json`
4. リモートの daemon.json: `rm -rf /tmp/lazyclaude-$USER/`
5. リモートの tmux ソケット: `rm -f /tmp/tmux-*/lazyclaude`
6. リモートの一時ファイル: `rm -rf /tmp/lazyclaude/`
7. ローカルの rm- mirror windows: `tmux -L lazyclaude` から rm- プレフィックスの window を全て kill
8. ローカルの state.json からリモートセッション (host != "") を削除

**Why:** daemon はリモートの state.json からセッションをロードする。これを消さないと古いセッションが GET /sessions で返され、ローカルに dead mirror window が作成されて一瞬表示されて消える。

**How to apply:** ユーザーが「リモートをリセットして」と言ったら、上記8項目を全て実行する。1つでも漏らさない。
