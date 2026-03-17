# lazyclaude

## Docker 仮想環境

全テスト・動作確認は Docker 内で行う。ホスト環境 (IDE, tmux, ~/.claude) に一切影響しない。

### ビルド

```bash
cd lazyclaude/
docker build -f Dockerfile.test -t lazyclaude-test .
```

### Claude Code 認証 (サブスクリプション)

Docker 内で Claude Code を使うには `.env` が必要:

```bash
# 1. トークン取得 (ホストで1回だけ実行、ブラウザ認証)
claude setup-token

# 2. .env に保存
echo "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-..." > .env

# 3. 認証確認
docker run --rm --env-file .env lazyclaude-test claude auth status
```

`.env` は `.gitignore` に登録済み。コミットされない。

Claude Code のオンボーディング (テーマ選択・信頼ダイアログ) は
lazyclaude が初回セッション作成時に自動スキップする。手動設定不要。

### デフォルト: 全テスト実行

```bash
docker run --rm lazyclaude-test
```

### UI の確認 (対話用)

```bash
docker run --rm lazyclaude-test bash -c '
  tmux -f /dev/null new-session -d -s ui -x 80 -y 20 "lazyclaude; sleep 999"
  sleep 2
  tmux capture-pane -p -t ui
  tmux kill-server 2>/dev/null
'
```

### Claude Code を使う操作 (--env-file 必須)

```bash
# bash で入る
docker run --rm -it --env-file .env lazyclaude-test bash

# Docker 内で lazyclaude 起動
lazyclaude
```

### クライアントレンダリングのテスト

`capture-pane` はサーバー内部バッファを返すため、クライアント側の表示問題
(UTF-8, locale, gocui Suspend 遷移) を検出できない。

クライアントレンダリングを非インタラクティブでテストするには `script` コマンドで
PTY を割り当てる:

```bash
docker run --rm --env-file .env lazyclaude-test bash -c '
  # セッション作成
  tmux -u -f /dev/null new-session -d -s ui -x 80 -y 20 "lazyclaude; sleep 999"
  sleep 3
  tmux send-keys -t ui n
  sleep 5

  # script コマンドでクライアントレンダリングをキャプチャ
  script -qc "tmux -u -L lazyclaude attach-session -t lazyclaude" /tmp/render.log &
  PID=$!
  sleep 3
  kill $PID 2>/dev/null

  # レンダリング結果を確認 (ANSI エスケープ含む)
  cat /tmp/render.log | strings | grep -o "╭\|╰\|│" | head -5
  # ╭ が表示されれば UTF-8 レンダリング正常
'
```

capture-pane テストが PASS でも「表示が正しい」とは限らない。
表示に関わる修正は必ずユーザーの仮想環境で確認すること。
```

### 任意のコマンド実行

```bash
# 特定テストだけ
docker run --rm lazyclaude-test go test -v ./internal/server/ -run TestServer_WebSocket

# カバレッジ
docker run --rm lazyclaude-test go test -cover ./internal/...
```

## gocui の注意点

### ErrUnknownView の比較

jesseduffield/gocui は `go-errors` の `Wrap` を使うため、`==` や `errors.Is` では一致しない。
文字列比較を使う:

```go
func isUnknownView(err error) bool {
    return err != nil && strings.Contains(err.Error(), "unknown view")
}
```

## Production Isolation

Docker 内で全て完結するため、ホスト環境への影響はゼロ。

| リソース | Docker 内 | ホスト |
|---------|----------|-------|
| tmux | Docker 内の tmux | 影響なし |
| ~/.claude/ide/ | Docker の /root/.claude/ide/ | 影響なし |
| /tmp/ | Docker の /tmp/ | 影響なし |
| state.json | Docker の /root/.local/share/ | 影響なし |
| ネットワーク | Docker 内 loopback | `-p` 指定時のみ expose |

## Development Plan

`docs/dev/go-migration-plan.md` (親ディレクトリ) を参照。