# Plan: リモート機能の統一修正

**Status:** IMPLEMENTED (daemon-arch branch)

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
- capture-pane → ローカル mirror window を読む → リモートの内容が見える
- send-keys → ローカル mirror window に送る → SSH 経由でリモートに届く
- attach → ローカル mirror window に attach → リモートを直接操作
- fullscreen → ローカル mirror window を gocui で描画 → 既存コードそのまま
```

## 詳細設計

### 1. mirror window の命名と grouped tmux session

mirror window のプレフィックスは `rm-` を使用（`lc-` ではない）。
理由: GC の SyncWithTmux は `lc-` プレフィックスで window name と session ID をマッチングする。`lc-` を使うと GC がローカルセッションと混同して状態を破壊する。

**Implemented:** Grouped tmux sessions (tmux new-session -t lazyclaude -s {localWindowName})
- Each mirror gets its own grouped session (named after localWindowName)
- destroy-unattached=on so the session cleans up when SSH drops
- Independent window selection per mirror

```
ローカルセッション: lc-abcd1234  (lc- + session ID[:8])
リモート mirror:    rm-abcd1234  (rm- + remote session ID[:8])

Grouped session per mirror:
  tmux new-session -t lazyclaude -s {localWindowName} \; set-option destroy-unattached on \; select-window -t {remoteWindow}
```

SyncWithTmux の window name マッチング: `rm-` プレフィックスは既存の `lc-` マッチングロジックの対象外。GC は mirror window を無視する。mirror window のライフサイクルは guiCompositeAdapter が管理する。

session.WindowName() に影響しない。WindowName() は Session.ID から `lc-` プレフィックスで生成する。mirror window は別の命名規則。

### 2. mirror window の SSH コマンド（Shell injection 対策）

**Implemented in gui_adapter.go:createMirrorWindow**

host はユーザー入力。直接 `fmt.Sprintf` で展開しない。base64 エンコードパターンで安全に処理:

```go
remoteCmd := fmt.Sprintf(
    "tmux -L lazyclaude set-option -t lazyclaude window-size largest 2>/dev/null; "+
    "tmux -L lazyclaude new-session -t lazyclaude -s %s "+
    "\\; set-option destroy-unattached on "+
    "\\; select-window -t %s",
    daemon.PosixQuote(localWindowName),
    daemon.PosixQuote(remoteWindow),
)
encoded := base64.StdEncoding.EncodeToString([]byte(remoteCmd))
sshHost, port := daemon.SplitHostPort(host)
sshArgs := "ssh -t"
if port != "" {
    sshArgs += " -p " + port
}
sshArgs += " " + sshHost
command := fmt.Sprintf("exec %s eval \"$(echo %s | base64 -d)\"", sshArgs, encoded)
```

Key details:
- `PosixQuote()` for safe shell quoting of strings passed to tmux
- Base64 encoding prevents injection via host or window name
- `new-session -t lazyclaude -s {localWindowName}` creates grouped session
- `destroy-unattached on` cleans up when SSH drops

### 3. ローカル Store エントリ

mirror window を作成したら、ローカル Store にもセッションを追加する:

```go
sess := session.Session{
    ID:        remoteSession.ID,    // リモートと同じ ID
    Name:      remoteSession.Name,
    Path:      remoteSession.Path,
    Host:      host,                // "AERO" 等
    Status:    session.StatusRunning,
    TmuxWindow: "rm-" + remoteSession.ID[:8],  // mirror window の ID
}
store.Add(sess, remoteSession.Path)
```

TmuxWindow フィールドは mirror window の名前 (`rm-xxxx`)。
SyncWithTmux は `rm-` window を ListWindows で検出し、この Store エントリとマッチングする。
pane が alive → Running、dead → Dead。

### 4. セッション作成フロー

**Implemented in gui_adapter.go**

```
n 押下 (リモートプロジェクト上)
  → guiCompositeAdapter.Create(path)
  → resolveHost() で host を判定 (cachedHost or pendingHost)
  → createWithHost(path, host) 呼び出し
  → 現在のパスを placeholder セッションで表示（optimistic UI）
  → completeRemoteCreate() が bg goroutine で実行:
     a. ensureRemoteConnected(host) で daemon 接続（lazyConn で once-only）
     b. resolveRemotePath(localPath, host) で remote CWD を取得
     c. rp.CreateSession(remotePath) → daemon API POST /session/create (remote tmux)
     d. ensureMirrorForRemoteSession() で mirror window 作成:
        - createMirrorWindow(host, remoteWindow, localWindowName)
        - grouped tmux session + base64-encoded SSH attach command
     e. PostCreateHook が呼ばれて local store に session 追加
     f. placeholder を削除して本物の session に置き換え
  → サイドバーに表示、preview が動く
```

Key behavioral details:
- **Optimistic creation**: UI shows placeholder immediately, real session added in background
- **LazyConn pattern**: ensureRemoteConnected uses sync.Once per host for exactly one connection
- **Path resolution**: resolveRemotePath() calls daemon API GET /cwd to get remote CWD
- **PostCreateHook pattern**: RemoteProvider calls hook after remote session creation to set up mirror
- **Placeholder cleanup**: if any step fails, failPlaceholder() marks it dead; user sees error in status

### 5. セッション削除フロー（双方向）

**Implemented in gui_adapter.go:Delete and local_provider.go:Delete**

```
d 押下
  → session.Host を確認
  → Host != "":
    1. daemon API DELETE /session/{id} → リモート側を削除
    2. ローカル tmux KillWindow(rm-xxxx) → mirror window を削除
    3. ローカル Store から削除
  → Host == "":
    既存のローカル削除フロー（変更なし）
```

順序: daemon API 先 → ローカル後。daemon API が失敗してもローカルは削除（リモートは GC が掃除）。

**Implementation details:**
- `guiCompositeAdapter.Delete()` routes to local or remote via `session.Host`
- Remote: calls `rp.DeleteSession()` then `a.localMgr.Delete()` to remove mirror
- Local: calls `a.localMgr.Delete()` which handles tmux window cleanup

### 6. リネームフロー（双方向）

```
R 押下
  → Host != "":
    1. daemon API POST /session/{id}/rename
    2. ローカル Store の名前更新
  → Host == "":
    既存フロー
```

### 7. `c` キー接続時の既存セッション mirror 化

**Implemented in root.go:connectRemoteHost**

```
c → AERO 接続
  → connectRemoteHost(host) → daemon 起動、tunnel 確立
  → daemon API GET /sessions → 既存リモートセッション一覧
  → 各セッションに対して:
    1. ensureMirrorForRemoteSession() で mirror window 作成
    2. ローカル Store に session 追加 (Host=host, TmuxWindow=rm-xxxx)
  → SSE 開始 (activity/通知)
  → サイドバーにリモートセッション全て表示
```

**Session and Role propagation:**
- API response `SessionCreateResponse` includes `Role` field ("pm", "worker", or empty)
- Session persisted to local store with `session.Role` field
- PM/Worker sessions displayed with `[PM]` or `[W]` prefix in sidebar
- Role information preserved across daemon restarts (stored in state.json)

### 8. SSH 切断時のハンドリング

mirror window 内の SSH プロセスが終了した場合:
- tmux remain-on-exit=on でpane は dead 状態で残る
- SyncWithTmux が StatusDead を設定
- **GC は mirror window (rm-) を自動削除しない** → GC コードで `rm-` プレフィックスは grace period を長くするか、削除しない
- ユーザーが `d` で手動削除、もしくは再接続時に RespawnPane でSSH再接続

再接続ロジック:
- `c` で同じホストに再接続 → 既存の dead mirror window を RespawnPane で復活
- RespawnPane の command は元の SSH attach コマンド

### 9. SSE 接続 (Activity + 通知)

mirror window で preview/sendkeys は解決するが、Activity 状態と通知は SSE が必要:
- リモートの Claude Code hooks → リモート daemon broker → SSE → ローカル TUI
- connectRemoteHost 成功後に `remoteProvider.StartSSE()` を呼ぶ
- SSE EventActivity → ローカル TUI の windowActivity map に反映
  - キー: mirror window の名前 (rm-xxxx) にマッピング
- SSE EventToolInfo → 通知ポップアップ表示

### 10. fullscreen

**TUI コード変更なし。**
EnterFullScreen → resolveSessionTarget() → `lazyclaude:rm-xxxx` (mirror window)
capture-pane → mirror window のローカルコンテンツ取得
send-keys → mirror window にキー送信 → SSH 経由でリモートに到達

ただし `state.go` の SessionContextSetter 呼び出しは削除する（コンパイルエラー防止）:
- `enterFullScreen` から `if setter, ok := ...SetSessionContext` ブロックを削除
- `exitFullScreen` から同様に削除

### 11. window サイズ

mirror window のサイズは lazyclaude tmux セッションの `window-size largest` に従う。
CapturePreviewContent が resize-window を呼ぶ → mirror window がリサイズされる → SSH attach 先のリモート pane もリサイズされる（tmux attach は client サイズに追従）。
追加対応不要。

### 12. scrollback

mirror window の scrollback はローカル tmux の history-limit に制限される。
リモートの完全な scrollback は取得できない。これは制限として受け入れる。
（attach モードでは tmux scrollback がフルに使える）

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

## 削除するもの

| 削除対象 | 理由 |
|---------|------|
| GET /session/{id}/preview | mirror window の capture-pane で済む |
| GET /session/{id}/scrollback | 同上 |
| GET /session/{id}/history-size | 同上 |
| POST /session/{id}/send-keys | mirror window の send-keys で済む |
| POST /session/{id}/send-choice | 同上 |
| GET /session/{id}/attach | mirror window に attach するだけ |
| input_forwarder.go 全体 | mirror window で不要 |
| gui.SessionContextSetter interface | 不要 |
| gui.HostAwareCreator interface | 不要 |
| gui_adapter.go の全 WithHost メソッド | 不要 |
| app_actions.go の HostAwareCreator 型アサーション (3箇所) | 不要 |
| state.go の SessionContextSetter 呼び出し | 不要（コンパイルエラー防止で削除必須） |
| RemoteProvider の CapturePreview/SendKeys/SendChoice | mirror window で不要 |
| http_client.go の preview/sendkeys/scrollback メソッド | 不要 |
| daemon server.go の preview/sendkeys/scrollback ハンドラ | 不要 |
| api.go の SendKeysRequest.Literal フィールド | 不要 |
| capture_preview.go の daemon API fallback | 不要 |

## HostAwareCreator 廃止後の host ルーティング

gui_adapter.go 内部で解決。GUI 層に host を漏洩させない。

```go
type guiCompositeAdapter struct {
    currentHostFn func() string  // app.currentSessionHost() を設定
    pendingHost   string         // DetectSSHHost() の結果、c キー接続後に更新
}

func (a *guiCompositeAdapter) Create(path string) error {
    host := a.currentHostFn()
    if host == "" {
        host = a.readPendingHost()
    }
    if host == "" {
        return a.localCreate(path)
    }
    return a.remoteCreate(path, host)
}
```

app_actions.go:
```go
// HostAwareCreator 分岐なし。単純に a.sessions.Create(path) を呼ぶだけ。
func (a *App) createSession(localPath string) {
    go func() {
        err := a.sessions.Create(localPath)
        // ...
    }()
}
```

currentSessionHost() は app.go に残す（gui_adapter が参照する関数として）。
ただし app_actions.go から直接呼ばない。gui_adapter の currentHostFn 経由。

## 修正ファイル一覧

| ファイル | 変更内容 |
|---------|---------|
| cmd/lazyclaude/gui_adapter.go | remoteCreate (mirror window 作成)、WithHost メソッド削除、HostAwareCreator assertion 削除、host ルーティング内部化 |
| cmd/lazyclaude/root.go | connectRemoteHost に既存セッション mirror 化追加、SSE 開始 |
| cmd/lazyclaude/input_forwarder.go | **ファイル削除** |
| cmd/lazyclaude/local_provider.go | 変更なし |
| internal/gui/app.go | HostAwareCreator interface 削除 |
| internal/gui/app_actions.go | HostAwareCreator 分岐削除 (3箇所)、createSession 簡素化 |
| internal/gui/keybindings.go | host キャプチャ削除 |
| internal/gui/state.go | SessionContextSetter 呼び出し削除 |
| internal/gui/input.go | SessionContextSetter interface 削除 |
| internal/daemon/server.go | preview/sendkeys/scrollback/history-size/attach ハンドラ削除 |
| internal/daemon/api.go | Literal フィールド削除 |
| internal/daemon/client.go | preview/sendkeys 関連メソッド削除 |
| internal/daemon/http_client.go | 同上 |
| internal/daemon/remote_provider.go | CapturePreview/SendKeys/SendChoice 削除、SSE 接続は残す |
| internal/daemon/capture_preview.go | daemon API fallback 削除（ローカル用ヘルパーとして残す） |
| internal/session/store.go | SyncWithTmux で `rm-` プレフィックスを認識 |

## Worker 構成

1つの Worker。変更が密結合。

## 実装済みの変更

| Component | Change | File |
|-----------|--------|------|
| GUI Adapter | New composite adapter with lazy remote connection, optimistic UI | cmd/lazyclaude/gui_adapter.go |
| Mirror Windows | Grouped tmux sessions with base64-encoded SSH commands | gui_adapter.go:createMirrorWindow |
| Session Store | `Host` and `Role` fields propagated from daemon response | internal/session/store.go |
| Remote Provider | PostCreateHook pattern for mirror creation | internal/daemon/remote_provider.go |
| API Response | SessionCreateResponse includes Role field | internal/daemon/api.go |
| Local Provider | Routes through composite adapter for dual local/remote | cmd/lazyclaude/local_provider.go |
| Root.go | connectRemoteHost with existing session mirror discovery | cmd/lazyclaude/root.go |

## 検証

1. `go build ./...` パス
2. `go vet ./...` パス
3. `go test -race ./internal/... ./cmd/lazyclaude/...` パス
4. ローカル: n/d/R/a/Enter/Esc/1/2/3/w/W/P/g が正常動作（リグレッションなし）
5. リモート (AERO):
   - `c` → AERO 接続 → 既存セッションがサイドバーに表示
   - `n` → セッション作成 → プレビュー表示（右パネル）
   - Enter → gocui fullscreen → 文字入力可能 → Esc で戻る
   - `a` → attach → Ctrl+\ で戻る
   - `d` → リモートセッション削除（mirror + daemon 両方）
   - `R` → リネーム
   - 1/2/3 → キー送信
   - Activity 状態がサイドバーに反映
   - ツール通知ポップアップが表示
   - PM/Worker セッションが正しいロール表示で表示
6. SSH 切断テスト:
   - SSH 接続を切断 → mirror window が dead → サイドバーに反映
   - `c` で再接続 → RespawnPane で復活

## リモートリセットチェックリスト

リモートセッションをリセットする際は以下全て確認（see .claude/memory/feedback_remote_reset.md）:

1. リモート daemon プロセス停止
2. リモート tmux サーバー停止
3. リモート state.json 削除
4. リモート daemon.json 削除
5. リモート tmux ソケット削除
6. リモート一時ファイル削除
7. ローカルの rm- mirror windows 削除
8. ローカル state.json からリモートセッション削除
