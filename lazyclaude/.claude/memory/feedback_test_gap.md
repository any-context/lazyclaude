---
name: test-gap-capture-pane-vs-client
description: capture-pane テストはクライアントレンダリングを検証できない。インタラクティブテストとの乖離に注意
type: feedback
---

capture-pane はサーバー内部バッファを返すため、クライアント側の問題を検出できない:
- UTF-8 表示 (tmux -u フラグ)
- タブ文字の置換
- gocui Suspend → tmux attach の遷移アーティファクト
- locale 依存の表示

**Why:** タブ→アンダースコア問題、UTF-8 表示問題がすべて capture-pane テストをパスしたが、
ユーザーのインタラクティブ Docker 環境で発覚した。

**How to apply:**
- 表示に関わる修正は必ずユーザーに仮想環境での確認を依頼する
- capture-pane テストが PASS でも「表示が正しい」とは断言しない
- tmux コマンドのフラグ変更 (-u, -c, -x/-y 等) は生成コマンドのテストで検証する