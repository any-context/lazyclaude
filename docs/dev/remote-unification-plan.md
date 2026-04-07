# Plan: リモート機能の統一修正

## Context

daemon アーキテクチャの目的は、リモートセッションをローカルと同じように操作すること。
daemon がリモートで Manager/tmux/hooks を全てローカル操作するため、本来は特別な分岐不要。

しかし現在のコードには:
- `HostAwareCreator` interface + 全 WithHost メソッド (gui_adapter.go)
- `compositeInputForwarder` + `SessionContextSetter` (input_forwarder.go)
- 未接続の SSE ストリーム (RemoteProvider.StartSSE 未呼出し)
- 不要な API (scrollback, history-size)
- fullscreen でリモートキー入力が動作しない

## 方針

1. **fullscreen (Enter) はリモートなら SSH attach** — capture+sendkeys の HTTP ラウンドトリップ不要
2. **HostAwareCreator 廃止** — host ルーティングを adapter 内部に閉じ込め、GUI 層に漏洩させない
3. **SSE 接続** — Activity + 通知を TUI に反映
4. **不要コード削除** — scrollback/history-size API、compositeInputForwarder、SessionContextSetter

## 修正内容

### 1. fullscreen のリモート対応 (Enter = SSH attach)

**ファイル**: `internal/gui/app_actions.go`, `internal/gui/state.go`

EnterFullScreen でセッションの Host を確認:
- Host == "" → 既存の fullscreen (ローカル tmux capture + key forward)
- Host != "" → AttachSession を呼ぶ (SSH -t attach、`a` と同じ)

```go
func (a *App) EnterFullScreen() {
    sess := a.currentSession()
    if sess == nil { return }
    if sess.Host != "" {
        // リモート: SSH attach (a キーと同じ)
        a.AttachSession()
        return
    }
    a.enterFullScreen(sess.ID)
}
```

### 2. HostAwareCreator 廃止

**ファイル**: `internal/gui/app.go`, `internal/gui/app_actions.go`, `internal/gui/keybindings.go`, `cmd/lazyclaude/gui_adapter.go`

GUI 層から host 判定を除去。adapter が内部で `currentHostFn()` を使ってルーティング:

- `gui.HostAwareCreator` interface 削除
- `app_actions.go` の `if hac, ok := a.sessions.(HostAwareCreator)` 分岐 3箇所削除
- `app_actions.go` の `currentSessionHost()` メソッド削除
- `keybindings.go` の host キャプチャ削除
- `gui_adapter.go` の全 WithHost メソッド削除

代わりに `guiCompositeAdapter` に `currentHostFn func() string` を設定:
```go
type guiCompositeAdapter struct {
    currentHostFn func() string  // GUI のカーソル位置から host を返す
}

func (a *guiCompositeAdapter) Create(path string) error {
    host := a.currentHostFn()
    return a.createInternal(path, host)
}
```

`root.go` で currentHostFn を app.CurrentSessionHost() に設定（app 側に残す）。

### 3. SSE 接続 (Activity + 通知)

**ファイル**: `cmd/lazyclaude/root.go` (connectRemoteHost 内), `internal/daemon/remote_provider.go`

connectRemoteHost 成功後に `remoteProvider.StartSSE()` を呼ぶ。
SSE イベントを TUI の activity システムに接続:

- `EventActivity` → `app.WindowActivityMap` に反映
- `EventToolInfo` → notification broker に publish

RemoteProvider の SSE goroutine が受信したイベントを、コールバック経由で TUI に送る。

### 4. 不要コード削除

**削除ファイル**:
- `cmd/lazyclaude/input_forwarder.go` — compositeInputForwarder 全体 (102行)

**削除するコード**:
- `internal/gui/input.go` の `SessionContextSetter` interface
- `internal/gui/state.go` の SessionContextSetter 呼び出し (enterFullScreen, exitFullScreen)
- `internal/daemon/api.go` の `SendKeysRequest.Literal` フィールド
- `internal/daemon/remote_provider.go` の `SendKeysLiteral`, `PasteToPane`
- `internal/daemon/client.go` の `SendKeysLiteral`
- `internal/daemon/http_client.go` の `SendKeysLiteral`
- `internal/daemon/server.go` の scrollback/history-size ハンドラ (tmux scrollback は attach 中に使う)
  → **注意**: サイドバーのスクロールモードで使われている可能性。削除前に確認。

### 5. 1/2/3 キーと SendChoice のリモート対応

**ファイル**: `internal/gui/app_actions.go`, `cmd/lazyclaude/gui_adapter.go`

SendKeyToPane は現在ローカル tmux forwarder を使う。リモートセッションの場合は daemon API 経由にする。

guiCompositeAdapter に SendKeys(sessionID, key) メソッドを追加。CompositeProvider が providerForSession でルーティング:
- ローカル: tmux send-keys
- リモート: daemon API POST /session/{id}/send-keys

### 6. Plugin/MCP のリモート対応 (将来)

現時点ではスコープ外。リモートセッションで Plugin/MCP パネルを開いた場合、ローカルの設定を表示する（現状維持）。

## 修正しないもの

- daemon server の既存ハンドラ (動作する)
- CompositeProvider のルーティングロジック (動作する)
- TCP tunnel (daemon API 接続に必要)
- CWD 検出 (/proc ベース、動作する)

## Worker 構成

全てを1つの Worker に任せる。変更が密結合しているため並列化は不適切。

## 検証

1. `go build ./...` パス
2. `go vet ./...` パス
3. `go test -race ./internal/... ./cmd/lazyclaude/...` パス
4. ローカル: n/d/R/a/Enter/1/2/3/w/W/P/g が正常動作 (リグレッションなし)
5. リモート (AERO):
   - `c` → AERO 接続
   - `n` → セッション作成、サイドバー表示
   - `d` → セッション削除
   - Enter → SSH attach (文字入力可能)
   - `a` → SSH attach
   - Activity 状態がサイドバーに反映
   - 通知ポップアップが表示
