# 可視化 E2E テストカタログ

各 tape は人間の TUI 操作のみを記録する。
テスト都合（環境変数、モックデータ、サービス起動）は `entrypoint.sh` が処理する。

出力: `.txt`（ターミナルテキスト）+ `.log`（VHS ログ）。gif は使わない。

## テープ一覧

### ssh_launch
SSH セッション作成（スクリプトファイル方式）。
- 前提: TUI が SSH pane 環境変数付きで起動済み
- 操作: `n` を押す
- 期待: リモートセッションがリストに表示、pane が生存

### popup_stack
カスケードポップアップ通知の表示・操作。
- 前提: TUI 起動済み、3件のモック通知がキューに入っている
- 操作: カスケード待機 → 上矢印 → Esc（サスペンド）→ `p`（再表示）→ `y`（個別却下）→ `Y`（一括承認）
- 期待: ポップアップ数が変化、サスペンド/再表示が動作、却下でポップアップが消える

### option_count
2択 vs 3択ダイアログの検出。
- 前提: TUI 起動済み、max-option が異なる通知がキューに入っている
- 操作: 各ポップアップを待機、`y` で却下
- 期待: 2択は y/n、3択は y/a/n を表示

### diff_popup
差分ビューアポップアップ。
- 前提: テストファイル作成済み
- 操作: `lazyclaude diff` 実行 → スクロール（j/k）→ 承認（y）
- 期待: 差分表示、スクロール動作、承認で閉じる

### key_order
IME キー順序保持。
- 前提: TUI 起動済み、Claude Code セッションがフルスクリーン
- 操作: 日本語入力（あいうえお）
- 期待: 文字が入力順に表示
- 必須: Claude Code トークン

### remote_popup
リモート Claude Code フル E2E（SSH トンネル + MCP + ポップアップ）。
- 前提: SSH リモート利用可能、Claude Code トークンあり
- 操作: リモートに SSH → lazyclaude 起動 → ポップアップ観察
- 期待: リモートの Claude Code のツールリクエストからポップアップが表示
- 必須: SSH リモート、Claude Code トークン

### worktree
Worktree 入力ダイアログの操作フロー。
- 前提: SSH リモートで TUI 起動済み
- 操作: `w` → ブランチ名入力 → Tab → プロンプト入力 → Ctrl+D で確定
- 期待: ダイアログ表示、Tab 切り替え、worktree 作成完了メッセージ
- 必須: Claude Code トークン

### 2option_detect
リアル Claude Code の 2択 permission ダイアログ検出。
- 前提: TUI 起動済み、Claude Code セッションあり
- 操作: Claude にコマンド実行を依頼 → permission ダイアログ待機
- 期待: ポップアップが正しいオプション数で表示
- 必須: Claude Code トークン
