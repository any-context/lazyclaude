# Plan: リモート機能の統一修正

## コンセプト

**ローカルの実装がそのままリモートで動作する。通信の部分はしょうがないので、リモート用を作る。**

通信の部分 = ローカル `tmux -L lazyclaude` の window 内で `ssh -t host tmux -L lazyclaude attach-session -t target` を実行する。これだけ。

全てのセッションはローカルの `tmux -L lazyclaude` 上の window として存在する。リモートかローカルかの区別はない。TUI のコード（preview, fullscreen, sendkeys, scrollback, paste）は一切変更不要。

## 仕組み

```
リモートセッション作成:
1. daemon API でリモートに Claude Code セッション作成 → lc-xxxx (リモート tmux)
2. ローカルの tmux -L lazyclaude に mirror window 作成:
   ssh -t host tmux -L lazyclaude attach-session -t lazyclaude:lc-xxxx
3. ローカル window がリモートセッションをミラー

結果:
- capture-pane → ローカル window を読む → リモートの内容が見える
- send-keys → ローカル window に送る → SSH 経由でリモートに届く
- attach → ローカル window に attach → リモートを直接操作
- fullscreen → ローカル window を gocui で描画 → 既存コードそのまま
```

## daemon API に残すもの

| エンドポイント | 用途 |
|--------------|------|
| POST /session/create | リモートでセッション作成 |
| DELETE /session/{id} | 削除 |
| POST /session/{id}/rename | リネーム |
| GET /sessions | セッション一覧（mirror window 作成に必要） |
| POST /worktree/create | worktree |
| POST /worktree/resume | worktree 再開 |
| GET /worktrees | worktree 一覧 |
| POST /msg/send, /msg/create, GET /msg/sessions | メッセージング |
| GET /health | ヘルスチェック |
| POST /shutdown | daemon 停止 |
| GET /notifications (SSE) | Activity 状態 + ツール通知 |
| GET /cwd | リモートパス取得 |

## 不要になるもの（削除）

| 削除対象 | 理由 |
|---------|------|
| GET /session/{id}/preview | ローカル mirror window の capture-pane で済む |
| GET /session/{id}/scrollback | 同上 |
| GET /session/{id}/history-size | 同上 |
| POST /session/{id}/send-keys | ローカル mirror window の send-keys で済む |
| POST /session/{id}/send-choice | 同上 |
| GET /session/{id}/attach | ローカル mirror window に attach するだけ |
| compositeInputForwarder (input_forwarder.go) | 不要 |
| SessionContextSetter interface | 不要 |
| HostAwareCreator interface | 不要 |
| SSHClient (tmux.Client SSH実装) | 不要。mirror window があるので |
| CapturePreviewContent の daemon API fallback | 不要 |
| RemoteProvider の tmux 直接操作コード | 不要 |
| http_client.go の preview/sendkeys メソッド | 不要 |

## 修正内容

### 1. mirror window の作成

リモートセッション作成後、ローカル `tmux -L lazyclaude` に window を追加:

```go
// リモートセッション lc-xxxx が作成された後:
cmd := fmt.Sprintf("ssh -t %s tmux -L lazyclaude attach-session -t lazyclaude:lc-%s", host, id[:8])
tmux.NewWindow(ctx, NewWindowOpts{
    Session: "lazyclaude",
    Name:    "lc-" + id[:8],  // ローカルと同じ命名規則
    Command: cmd,
})
```

### 2. c キーで接続時の既存セッション mirror 化

daemon API GET /sessions で既存セッション一覧取得 → 各セッションに対して mirror window 作成。

### 3. HostAwareCreator 廃止

mirror window があるのでローカルと同じ。host 分岐不要。
gui_adapter.go の WithHost メソッド群を削除。

### 4. SSE 接続 (Activity + 通知)

connectRemoteHost 成功後に remoteProvider.StartSSE() を呼ぶ。
SSE イベントを TUI の activity/通知システムに反映。
これだけはローカルと異なる（リモートの hooks はローカル broker に直接届かないため）。

### 5. 不要コード大量削除

daemon API の preview/sendkeys 関連エンドポイント、http_client のメソッド、RemoteProvider の tmux 操作コード、input_forwarder.go 全体。

## Worker 構成

1つの Worker。

## 検証

1. go build, go vet, go test -race パス
2. ローカル: 全機能が正常動作（リグレッションなし）
3. リモート (AERO):
   - c → AERO 接続 → 既存セッションがサイドバーに表示
   - n → セッション作成 → プレビュー表示
   - Enter → gocui fullscreen で文字入力可能
   - Esc → 元の画面に戻る
   - a → attach
   - d → 削除
   - 1/2/3 → キー送信
   - Activity 状態がサイドバーに反映
