# lazyclaude

## テスト

### ユニットテスト (ホストで実行可能)

```bash
go test ./internal/... -count=1
go test -cover ./internal/...
```

### VHS 可視化 E2E (Docker 必須)

```bash
# テスト実行
make test-vhs TAPE=ssh_launch
make test-vhs TAPE=smoke

# フレーム確認
awk '/\[Frame 5\]/,/\[Frame 6\]/{if(/\[Frame 6\]/)exit; print}' vis_e2e_tests/outputs/ssh_launch/ssh_launch.log
```

- tape は人間の操作のみ。テスト都合は `entrypoint.sh`
- 出力: `outputs/{name}/` に `.gif` + `.txt` + `.log`
- lazyclaude の起動は `lazyclaude` コマンド直接入力ではなく、tmux plugin 経由 (`Ctrl+\`) で行う。Dockerfile の bash ラッパーが `lazyclaude setup` + `lazyclaude.tmux` を自動実行するため、tape 内では `Ctrl+\` を押すだけで popup が開く (SSH 不要。SSH は SSH テスト専用)
- worktree で作業する場合は `.claude/worktree/` 配下で行う。Docker コンテナ名・ネットワーク名が他の実行と競合しないか事前確認すること (`docker compose ps` で既存コンテナを確認)
- テスト完了後は `open vis_e2e_tests/outputs/<tape名>/` で Finder から gif 等の結果を確認する

### Claude Code 認証 (Docker)

```bash
claude setup-token
echo "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-..." > vis_e2e_tests/.env
```

## gocui の注意点

### ErrUnknownView の比較

`==` や `errors.Is` では一致しない。文字列比較を使う:

```go
func isUnknownView(err error) bool {
    return err != nil && strings.Contains(err.Error(), "unknown view")
}
```

### Editor と keybinding の dispatch 順序

```
1. View-specific bindings (popupViewName 等)
2. Editor.Edit() — Editable=true の view のみ
3. Global bindings — ただし Editable view では rune キー (ch!=0) のグローバルバインドはスキップ
```

### Frame=false ビューの座標系

`Frame=false` でもコンテンツ領域は `(x0+1, y0+1)` から `(x1-1, y1-1)` のまま。
フレームは描画されないが、y0 / y1 の行はコンテンツに使われない。
frameless バーを配置するときは y0+1 がテキスト開始位置になることに注意。

```
InnerWidth  = Width  - 2  (常に)
InnerHeight = Height - 2  (常に)
```

### Ctrl+[ と Esc

同じバイト (0x1B)。gocui/tcell で区別不可能。
lazyclaude は **Ctrl+\\** を normal mode 切替に使用。

## tmux アーキテクチャ

### 2つの tmux サーバー

1. **ユーザーの tmux** (デフォルトソケット) — `display-popup` で lazyclaude TUI を表示
2. **lazyclaude tmux** (`-L lazyclaude` ソケット) — Claude Code セッションを管理

### キー入力の流れ

```
popup 外: キー → ユーザーの tmux root table → マッチなら実行
popup 内: キー → popup プロセスに直接渡る (ユーザーの tmux root table はバイパス)
attach 中: キー → lazyclaude tmux の root table → マッチなら実行
```

### display-popup の動作 (tmux 3.4+)

- popup 内から `display-popup` を呼ぶと既存 popup を **変更** できる (ネストではない)
- `-b rounded` / `-B` で枠線を動的に切り替え可能
- popup 内のプロセスが終了すると変更も消える

### `tmux source` はキーバインドをリセットしない

上書きまたは追加のみ。完全リセットは tmux サーバーの再起動が必要。

### MCP サーバー

- `lazyclaude setup` で起動される常駐デーモン
- サーバーログ: `/tmp/lazyclaude-server.log`
- 重複起動防止: `server.IsAlive()` で port file + TCP dial チェック

### パフォーマンス

- パフォーマンス問題は git bisect でバイナリ比較して特定する (コード分析より確実)
- チェックポイントは `.claude/checkpoints.log` に記録

### SSH コマンド生成

- リモートコマンドは plain bash スクリプトとしてファイルに書き出し、base64 でエンコード
- ネストクォート禁止。`shell.Quote` を SSH コマンド文字列内で使わない
- `scripts/lazyclaude-launch.sh` が唯一のエントリポイント (tmux plugin + standalone)
