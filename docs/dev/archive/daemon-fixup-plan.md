# Daemon Fixup Plan: End-to-End Integration Fixes

## 背景

daemon-arch ブランチは Phase 0-5 で構築されたが、統合テストが行われておらず実機で動作しない。
本計画は end-to-end で動作するために必要な修正をまとめる。

## 現在の問題一覧

### Bug 1: deploy コマンド不要 — 削除

`lazyclaude deploy` は不要。ユーザーが自分でリモートにインストールする前提。

対象ファイル:
- `cmd/lazyclaude/deploy_cmd.go` — 削除
- `internal/daemon/deploy.go` — 削除
- `internal/daemon/deploy_test.go` — 削除
- `cmd/lazyclaude/root.go` — deploy サブコマンド登録削除
- エラーメッセージ変更: `"lazyclaude deploy"` → `"lazyclaude is not installed on {host}"`

### Bug 2: daemon.json パス不一致

daemon_cmd.go の `writeDaemonInfo` は `config.DefaultPaths().RuntimeDir` = `/tmp` に書く。
lifecycle.go の `DiscoverRemoteDaemon` は `/tmp/lazyclaude-$(whoami)/daemon.json` を読む。
パスが一致しない。

修正: 統一パスを `daemon` パッケージ内に定義:
```go
func DaemonInfoDir() string {
    return fmt.Sprintf("/tmp/lazyclaude-%s", os.Getenv("USER"))
}
```
daemon_cmd.go と lifecycle.go の両方でこの関数を使用。

### Bug 3: deploy.go 内のユーティリティ関数の移動

`deploy.go` を削除すると、そこに定義されている以下の関数が失われる:
- `posixQuote()` — tunnel.go, lifecycle.go 等で使用されているか確認
- `splitHostPort()` — 同上
- `SSHExecutor` interface — lifecycle.go, connection_impl.go で使用
- `ExecSSHExecutor` — root.go で使用

これらが deploy.go にのみ定義されているなら、適切なファイルに移動する必要がある。

### Bug 4: daemon 起動方式の問題

lifecycle.go:33 `lm.ssh.Run(ctx, host, "lazyclaude daemon --port 0")`
- `ssh.Run` は `exec.Command(...).Output()` を使う — daemon はフォアグラウンドで動き続けるため Output() は永遠に返らない
- daemon をバックグラウンドで起動し、stdout から port/token を読む必要がある
- もしくは daemon が daemon.json を書いてから DiscoverRemoteDaemon で読む方式に変更

修正案:
```
ssh host "nohup lazyclaude daemon --port 0 > /dev/null 2>&1 & sleep 1 && cat /tmp/lazyclaude-$(whoami)/daemon.json"
```
もしくは ssh.Run を ssh.Start (non-blocking) に変更。

### Bug 5: posixQuote によるチルダ展開阻害

deploy.go で発見済み: `posixQuote("~/.local/bin/lazyclaude")` → `'~/.local/bin/lazyclaude'`
シングルクォート内ではチルダが展開されない。
daemon パス等でも同様の問題が起きる可能性。

修正: リモートパスでチルダを含む場合は展開後のパスを使うか、クォートしない。

### Bug 6: crossBuild の CWD 依存

deploy.go の `crossBuild` は CWD で `go build ./cmd/lazyclaude` を実行。
lazyclaude のソースディレクトリにいなければ失敗。
→ deploy 自体を削除するので解消。

### Bug 7: daemon stdout 出力の設計

daemon_cmd.go:74 で `fmt.Fprintf(os.Stdout, "port=%d\n", actualPort)` を出力。
しかし parseDaemonOutput は JSON (`{"port":12345,"token":"abc"}`) を期待。
フォーマット不一致。

修正: daemon は JSON を stdout に出力するか、daemon.json ファイル経由のみにする。

## 修正計画

### Worker A: deploy 削除 + ユーティリティ移動

1. `deploy.go` 内の共有ユーティリティ (`SSHExecutor`, `ExecSSHExecutor`, `posixQuote`, `splitHostPort`) を適切なファイルに移動:
   - `SSHExecutor` / `ExecSSHExecutor` → `internal/daemon/ssh.go` (新規)
   - `posixQuote` / `splitHostPort` → 同上
2. `deploy.go`, `deploy_test.go`, `deploy_cmd.go` を削除
3. root.go から deploy サブコマンド登録を削除
4. エラーメッセージから "lazyclaude deploy" の言及を全て削除

### Worker B: daemon 起動フロー修正

1. daemon.json パス統一: `DaemonInfoDir()` 関数を定義し、daemon_cmd.go と lifecycle.go で共有
2. daemon stdout を JSON に統一 (parseDaemonOutput と一致)
3. daemon バックグラウンド起動:
   - `StartRemoteDaemon` を非ブロッキングに変更
   - SSH でバックグラウンド起動 → daemon.json 読み取り
   - もしくは `ssh.Start()` メソッドを SSHExecutor に追加
4. daemon_cmd.go: daemon.json にトークンを含めて書き出し (stdout ではなくファイル経由)
5. DiscoverRemoteDaemon: 統一パスから読み取り

### 並列実行

Worker A と Worker B は独立。同時に実行可能。
ただし deploy.go のユーティリティ移動 (Worker A) が完了しないと Worker B のコードがコンパイルできない可能性がある。
→ Worker A を先行させ、完了後に Worker B を開始。

### 品質チェック

各 Worker:
1. `go build ./...` パス
2. `go vet ./...` パス
3. `go test ./internal/... -count=1 -race` パス
4. `/go-review` 実行して全 findings 対応

統合テスト (PM が手動確認):
1. リモートに lazyclaude を手動インストール済みの状態で:
   - `ssh host lazyclaude daemon --port 0` が起動し daemon.json が書かれる
   - ローカル TUI 起動 → SSH 検出 → daemon 自動起動 → tunnel + socket → 接続
   - リモートで n (セッション作成) が動作する
2. リモートに lazyclaude がない状態で:
   - ローカル TUI 起動 → SSH 検出 → daemon 起動失敗 → エラー表示
   - n/w/W/P で明確なエラーメッセージ
