# Plan: Remote fullscreen copy mode の scrollback を remote daemon 経由で取得 (Bug 3 Phase 2)

## Context

Phase 1 (`docs/dev/copy-mode-remote-plan.md`) の診断で仮説 A が確定:

- 再現実機の debug log で
  - `[remote] HistorySize: host="AERO" target="@440" histSize=0`
  - `[local]  HistorySize: host=""     target="@414" histSize=43100`
- Remote session の local mirror window は tmux pane scrollback buffer が空 (0 行)
- `CapturePaneANSIRange` が返す bytes は **現在画面に見える領域** のみ (約 8KB)
- 結果: fullscreen Ctrl+V で copy mode に入ると表示領域が固定され、過去の出力にさかのぼれない

## 設計上の根本原因

Mirror window の仕組み:
- Local tmux の window 内で `ssh -t host 'tmux attach'` プロセスが走る
- SSH セッションは remote tmux の **現在画面を描画しているだけ** で、remote tmux の scrollback buffer は local tmux からは見えない
- Remote tmux pane の実際の scrollback は remote 側にしか存在しない
- `capture-pane` を local mirror に対して実行すると、local tmux が SSH セッション上に描画した current viewport + local tmux が独自に蓄積した scrollback (ほぼ空) しか取れない

ローカルはこの問題を持たない:
- Local claude は lazyclaude tmux server の pane 内で **直接** 動作
- 出力は local tmux pane の scrollback buffer にそのまま蓄積 (実測 43100 行)
- `capture-pane -S -N` でフル履歴にアクセスできる

## 修正方針 (Approach 1)

Scrollback / history_size の取得を **remote daemon API 経由** に切り替える。Remote daemon の lazyclaude server は remote tmux server (`-L lazyclaude`) に直接アクセスできるので、そこで `capture-pane` / `show-message` を実行し HTTP で結果を返す。

`CapturePreview` は現状動いている (mirror window から現在画面を取得) ので **触らない**。scrollback 系 2 メソッドのみ routing を変更する。

### Host 分岐の最小化

- 新規 host 分岐は `CompositeProvider.CaptureScrollback/HistorySize` の 1 箇所に集約
- 「session.Host が非空なら remote provider、空なら local provider」という既存の routing pattern (CreateWorktree 等) と同じ方針
- `providerForSession(id)` は attach/delete 等で引き続き local を返す (Mirror 経由 runtime ops は保持)
- 新しい helper `providerForCapture(id)` を内部に作り、session を lookup して Host 見て分岐、**capture 系メソッドのみ** この helper 経由

## 実装ステップ

### Step 0: APIVersion の bump

ファイル: `internal/daemon/api.go:13`

新 endpoint を追加するため API version を 1 → 2 に bump:
```go
const APIVersion = 2
```

理由 (codex review 指摘): 現状 connection handshake は `health.APIVersion != APIVersion` で拒否する (`internal/daemon/connection_impl.go:198-205`)。bump しないと、古い remote daemon (未 update) に接続した時に新 endpoint が存在せず HTTP 404 で silent fail する。bump することで handshake 段階で「remote binary を upgrade してください」エラーを明示的に返せる。

**影響**: 既存の remote daemon は再 build + re-install が必要になる。本 bug fix を merge した後、remote でも同 commit を install し直す運用を documentation に追記。

### Step 1: daemon API endpoints の追加

ファイル: `internal/daemon/api.go`

既に `ScrollbackRequest` / `ScrollbackResponse` / `HistorySizeResponse` は定義済 (L85-102)。`HistorySize` 用のリクエスト型だけ追加 (URL パス + id でも可だが既存 pattern に合わせる):

既存の `ScrollbackRequest` で `ID` と `Width/StartLine/EndLine` を持つので流用。HistorySize はパス `{id}` のみで request body 不要。

ファイル: `internal/daemon/server.go`

ルーティング追加 (L94 付近の `mux.HandleFunc(...)` リストに):
```go
mux.HandleFunc("POST /session/{id}/scrollback", s.withAuth(s.handleScrollback))
mux.HandleFunc("GET /session/{id}/history-size", s.withAuth(s.handleHistorySize))
```

ハンドラー実装 (新規追記):
```go
// handleScrollback captures scrollback for a session.
// Uses the local (remote daemon's) tmux server via capture-pane -S -E range.
func (s *Server) handleScrollback(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if id == "" {
        http.Error(w, "missing session id", http.StatusBadRequest)
        return
    }
    var req ScrollbackRequest
    if err := readJSON(w, r, &req); err != nil {
        return
    }
    sess := s.mgr.Store().FindByID(id)
    if sess == nil {
        http.Error(w, "session not found", http.StatusNotFound)
        return
    }
    target := sess.TmuxTarget()
    content, err := s.tmux.CapturePaneANSIRange(r.Context(), target, req.StartLine, req.EndLine)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadGateway)
        return
    }
    writeJSON(w, http.StatusOK, ScrollbackResponse{Content: content})
}

// handleHistorySize returns the pane's scrollback history size.
func (s *Server) handleHistorySize(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if id == "" {
        http.Error(w, "missing session id", http.StatusBadRequest)
        return
    }
    sess := s.mgr.Store().FindByID(id)
    if sess == nil {
        http.Error(w, "session not found", http.StatusNotFound)
        return
    }
    target := sess.TmuxTarget()
    out, err := s.tmux.ShowMessage(r.Context(), target, "#{history_size}")
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadGateway)
        return
    }
    n, _ := strconv.Atoi(strings.TrimSpace(out))
    writeJSON(w, http.StatusOK, HistorySizeResponse{Lines: n})
}
```

必要 import: `strconv`, `strings` (既存の import に追加)。

**注**: `Server.mgr.Store().FindByID(id)` と `sess.TmuxTarget()` は Bug 1 で merge 済の Session.TmuxTarget() を流用する。Remote daemon の `s.tmux` は remote tmux server client なので、同じ helper が remote でも valid target を返す。

### Step 2: ClientAPI に capture methods を追加

ファイル: `internal/daemon/client.go`

`ClientAPI` interface に追加:
```go
// --- Capture ---

// CaptureScrollback retrieves a range of scrollback lines for a session.
// Used by the fullscreen copy mode for remote sessions where the local
// mirror window's tmux buffer is empty.
CaptureScrollback(ctx context.Context, req ScrollbackRequest) (*ScrollbackResponse, error)

// HistorySize returns the number of scrollback lines for a session.
HistorySize(ctx context.Context, id string) (int, error)
```

**重要 (codex review 反映)**: `ClientAPI` を拡張すると全 implementer に stub を追加しないとビルドが通らない。実装確認済の影響範囲:
- `internal/daemon/http_client.go:HTTPClient` (本命 implementation、Step 3)
- `internal/daemon/connection_impl_test.go:mockClientAPI` (L11-52 の範囲)

#### 2-a: mockClientAPI の stub 追加
ファイル: `internal/daemon/connection_impl_test.go`

```go
func (m *mockClientAPI) CaptureScrollback(_ context.Context, _ ScrollbackRequest) (*ScrollbackResponse, error) {
    return &ScrollbackResponse{}, nil
}

func (m *mockClientAPI) HistorySize(_ context.Context, _ string) (int, error) {
    return 0, nil
}
```

これ以外の mock/stub があれば同様に追加する。Worker は Step 2 実装前に以下を grep:
```bash
grep -rn 'ClientAPI\b' --include='*.go' .
grep -rn 'implements ClientAPI\|_ ClientAPI =' --include='*.go' .
```

### Step 3: HTTPClient 実装

ファイル: `internal/daemon/http_client.go`

**重要 (codex review 反映)**: 既存 HTTPClient の helper は `postJSON` / `getJSON` / `delete` で、URL 構築は `sessionPath(id, suffix)` で `url.PathEscape` 込み。plan 初版の `c.post` / `c.get` / 文字列連結は誤り。正しい pattern に合わせる:

```go
func (c *HTTPClient) CaptureScrollback(ctx context.Context, req ScrollbackRequest) (*ScrollbackResponse, error) {
    var resp ScrollbackResponse
    if err := c.postJSON(ctx, sessionPath(req.ID, "/scrollback"), req, &resp); err != nil {
        return nil, fmt.Errorf("capture scrollback: %w", err)
    }
    return &resp, nil
}

func (c *HTTPClient) HistorySize(ctx context.Context, id string) (int, error) {
    var resp HistorySizeResponse
    if err := c.getJSON(ctx, sessionPath(id, "/history-size"), &resp); err != nil {
        return 0, fmt.Errorf("history size: %w", err)
    }
    return resp.Lines, nil
}
```

既存の `DeleteSession` / `RenameSession` が `sessionPath` + `postJSON`/`delete` helper を使っているのと同じ style (`http_client.go:52-58`)。

### Step 4: RemoteProvider の stub を実装に差し替え

ファイル: `internal/daemon/remote_provider.go:325-334`

```go
// CaptureScrollback retrieves scrollback via the remote daemon API. This is
// the fullscreen copy-mode path for remote sessions; the mirror window's
// local tmux buffer does not contain the remote tmux's historical scrollback,
// so we ask the remote daemon to run capture-pane on its own tmux server.
func (rp *RemoteProvider) CaptureScrollback(id string, width, startLine, endLine int) (*ScrollbackResponse, error) {
    client, err := rp.conn.Client()
    if err != nil {
        return nil, fmt.Errorf("capture scrollback: %w", err)
    }
    return client.CaptureScrollback(context.Background(), ScrollbackRequest{
        ID:        id,
        Width:     width,
        StartLine: startLine,
        EndLine:   endLine,
    })
}

// HistorySize returns the remote tmux pane's scrollback length via the
// daemon API. Same rationale as CaptureScrollback.
func (rp *RemoteProvider) HistorySize(id string) (int, error) {
    client, err := rp.conn.Client()
    if err != nil {
        return 0, fmt.Errorf("history size: %w", err)
    }
    return client.HistorySize(context.Background(), id)
}
```

**注**:
- `CapturePreview` は error stub のまま保持 (mirror 経由で動いているから触らない)
- 既存の client 取得 pattern (`rp.conn.Client()`) に従う
- `fmt.Errorf` で context wrap

### Step 5: CompositeProvider の routing を変更

ファイル: `internal/daemon/composite_provider.go:218-234`

```go
// CaptureScrollback captures scrollback. Remote sessions go through the
// remote daemon API because the local mirror window's tmux buffer does
// not contain the remote tmux's historical scrollback. Local sessions
// still use the local provider.
func (c *CompositeProvider) CaptureScrollback(id string, width, startLine, endLine int) (*ScrollbackResponse, error) {
    p := c.providerForCapture(id)
    if p == nil {
        return nil, fmt.Errorf("no provider found for session %q", id)
    }
    return p.CaptureScrollback(id, width, startLine, endLine)
}

// HistorySize returns scrollback size. Remote sessions go through the
// remote daemon API for the same reason as CaptureScrollback.
func (c *CompositeProvider) HistorySize(id string) (int, error) {
    p := c.providerForCapture(id)
    if p == nil {
        return 0, fmt.Errorf("no provider found for session %q", id)
    }
    return p.HistorySize(id)
}
```

新規 helper の具体は Step 6.5 で定義 (`LocalSessionHost` を使う版)。

**Host 分岐の局所化**: このファイル/関数に限定。providerForSession は変更せず、capture 専用の routing を追加する形。`if host == ""` 分岐は 1 箇所 (providerForCapture 内部) のみ。

### Step 6: SessionProvider interface に Host lookup helper を追加

codex review で判明した制約:
- `providerForCapture` が session の host を知る必要がある
- `SessionProvider` interface に新 method を追加する場合、**全 implementer に stub を追加する必要** がある
- 影響範囲 (実測済):
  - `cmd/lazyclaude/local_provider.go:localDaemonProvider` (本命 implementation)
  - `internal/daemon/remote_provider.go:RemoteProvider` (compile-time check `var _ SessionProvider = (*RemoteProvider)(nil)` at L17)
  - `cmd/lazyclaude/routing_integration_test.go:fakeSessionProvider` (統合テスト内)
  - `internal/daemon/composite_provider_test.go:stubProvider` (他にも stub 系があれば要確認)

**Import cycle 予備確認 (PM 側で実施済)**:
- `internal/daemon/server.go` は既に `github.com/any-context/lazyclaude/internal/session` を import している (L25)
- `internal/daemon/composite_provider.go` は `internal/session` を import していない (`internal/core/model` のみ)
- `internal/session/*.go` は `internal/daemon` を import していない (事前 grep 済)
- 従って `composite_provider.go` で `internal/session` を import して `*session.Session` を型に使うのは cyclic にならない。安全

**ただし API 最小化のため**: `Session(id) *session.Session` ではなく `LocalSessionHost(id string) (host string, found bool)` にする。これで:
- `composite_provider.go` は `internal/session` を import しなくて済む
- interface の surface area が小さい
- 他の implementer も 1 行 stub で済む

ファイル: `internal/daemon/composite_provider.go` の `SessionLister` interface (既存) に追加:

```go
type SessionLister interface {
    HasSession(sessionID string) bool
    Host() string
    Sessions() ([]SessionInfo, error)

    // LocalSessionHost returns the host of a session by ID, if known to
    // this provider. Used by CompositeProvider.providerForCapture to
    // dispatch capture ops by host without leaking session.Session type
    // into the daemon package's interface surface.
    // Returns ("", false) when the session is not found by this provider.
    LocalSessionHost(id string) (host string, found bool)
}
```

**重要**: 本当は `SessionLister` よりも capture routing 専用の helper として分離する方が綺麗だが、既存の interface 分割 pattern に従うと `SessionLister` が一番近い (既に `HasSession` も持っている)。または新規 `SessionResolver` interface を切り出して capture routing だけで使う案もあり、worker が実装時に判断可能。

#### 6-a: localDaemonProvider の実装
ファイル: `cmd/lazyclaude/local_provider.go`
```go
func (p *localDaemonProvider) LocalSessionHost(id string) (string, bool) {
    sess := p.mgr.Store().FindByID(id)
    if sess == nil {
        return "", false
    }
    return sess.Host, true
}
```

#### 6-b: RemoteProvider の実装
ファイル: `internal/daemon/remote_provider.go`
```go
// LocalSessionHost returns the host for a session if it is in this remote
// provider's cache. Used by capture routing to dispatch remote-originated
// lookups to the correct provider.
func (rp *RemoteProvider) LocalSessionHost(id string) (string, bool) {
    rp.mu.Lock()
    defer rp.mu.Unlock()
    for _, s := range rp.sessions {
        if s.ID == id {
            return rp.host, true
        }
    }
    return "", false
}
```

#### 6-c: fakeSessionProvider (統合テスト) の stub
ファイル: `cmd/lazyclaude/routing_integration_test.go`
```go
func (f *fakeSessionProvider) LocalSessionHost(_ string) (string, bool) {
    return "", false
}
```

#### 6-d: その他の stub
- `internal/daemon/composite_provider_test.go` の `stubProvider` と近隣のフェイクにも 1 行 stub 追加
- Worker は実装時に `grep -rn 'SessionProvider' --include='*.go' .` で全 implementer を確認

### Step 6.5: providerForCapture の実装

前 Step で決めた `LocalSessionHost` を使う:

```go
func (c *CompositeProvider) providerForCapture(sessionID string) SessionProvider {
    // Try local first: most sessions are local, and even remote mirror
    // sessions are registered in the local store (but with Host != "").
    if host, ok := c.local.LocalSessionHost(sessionID); ok {
        if host == "" {
            return c.local
        }
        c.mu.RLock()
        defer c.mu.RUnlock()
        if rp, ok := c.remotes[host]; ok {
            return rp
        }
        return nil
    }
    // Fallback: not in local store (shouldn't happen normally). Try the
    // existing providerForSession path which searches remote caches.
    return c.providerForSession(sessionID)
}
```

注: `composite_provider.go` の `CaptureScrollback` / `HistorySize` を Step 5 でこの helper に switch する。

### Step 7: Unit / integration tests

#### 7-a: daemon server handlers
ファイル: `internal/daemon/server_capture_test.go` (新規)

- TestServer_HandleScrollback_Success: mock tmux で capture-pane を stub、POST /session/{id}/scrollback に valid リクエスト、期待する content を返す
- TestServer_HandleScrollback_SessionNotFound: 存在しない id → 404
- TestServer_HandleHistorySize_Success: mock tmux で show-message を stub、GET /session/{id}/history-size → 期待する数値
- TestServer_HandleHistorySize_SessionNotFound: 404

#### 7-b: HTTPClient methods
ファイル: `internal/daemon/http_client_capture_test.go` (新規)

- httptest.Server で mock サーバー立てて CaptureScrollback / HistorySize のリクエスト/レスポンスを検証

#### 7-c: RemoteProvider methods
ファイル: `internal/daemon/remote_provider_test.go` (既存に追記)

- TestRemoteProvider_CaptureScrollback: mock ClientAPI で stub、RemoteProvider.CaptureScrollback が client にリクエスト転送することを assert
- TestRemoteProvider_HistorySize: 同様

#### 7-d: CompositeProvider routing
ファイル: `internal/daemon/composite_provider_test.go` (既存に追記)

- TestCompositeProvider_CaptureScrollback_LocalSession: session.Host="" → localProvider が呼ばれる
- TestCompositeProvider_CaptureScrollback_RemoteSession: session.Host="AERO" → remoteProvider が呼ばれる
- TestCompositeProvider_HistorySize_LocalSession: 同様 local
- TestCompositeProvider_HistorySize_RemoteSession: 同様 remote
- TestCompositeProvider_CaptureScrollback_RemoteSessionNoProvider: session.Host="GHOST" but no registered remote → error
- TestCompositeProvider_CapturePreview_StillUsesLocal: session.Host="AERO" でも preview は local 経由を確認 (regression guard)

### Step 8: 検証

1. `go build ./...` clean
2. `go vet ./...` clean
3. `go test -race ./internal/daemon/... ./cmd/lazyclaude/... ./internal/gui/...` 全 PASS
4. `/go-review` → CRITICAL/HIGH ゼロ
5. `/codex --enable-review-gate` → APPROVED
6. **手動検証** (要ユーザー):
   - [ ] Local session で fullscreen Ctrl+V → 従来通り scrollback 表示 (regression なし)
   - [ ] Remote session で fullscreen Ctrl+V → **remote tmux のフル scrollback** にアクセスできる
   - [ ] Remote session の preview (fullscreen でない通常表示) が regression なく動く
   - [ ] Remote session の attach / delete / send-keys が regression なく動く
   - [ ] Remote connection が切れた状態で Ctrl+V → "no provider" 的なエラー

## Out of Scope

- `CapturePreview` の routing 変更 (現状動いているので触らない)
- `ToolNotification` (permission popup) の remote 対応 (Bug 5 相当、別 plan)
- Bug 4 (activity state) — 並行中
- Diagnostic logging (Phase 1) の revert — 本 plan merge 時に必ず revert する (diag-copy-mode-remote branch を破棄)
- **Remote daemon の rolling update 戦略**: 本 plan で `APIVersion` を 1 → 2 に bump するため、merge 後は **全 remote host で lazyclaude binary を再 install する必要** がある。古い daemon に接続すると handshake で弾かれて接続不可。段階的 rollout / 自動 binary push 機構は本 plan では扱わず、運用手順として documentation + release notes に追記する

## Files Changed

| ファイル | 変更 |
|---------|------|
| `internal/daemon/api.go` | `APIVersion` を 1 → 2 に bump (Step 0)、既存の ScrollbackRequest/ScrollbackResponse/HistorySizeResponse を流用 |
| `internal/daemon/connection_impl_test.go` | `mockClientAPI` に CaptureScrollback / HistorySize stub 追加 (Step 2-a) |
| `internal/daemon/server.go` | `/session/{id}/scrollback`, `/session/{id}/history-size` のルート追加、handleScrollback / handleHistorySize 実装 |
| `internal/daemon/client.go` | ClientAPI に CaptureScrollback / HistorySize 追加 |
| `internal/daemon/http_client.go` | HTTPClient.CaptureScrollback / HistorySize 実装 (`postJSON` / `getJSON` + `sessionPath`) |
| `internal/daemon/remote_provider.go` | CaptureScrollback / HistorySize を error stub から HTTP client 経由実装に差し替え (CapturePreview は stub のまま)。`RemoteProvider.LocalSessionHost(id)` stub 追加 |
| `internal/daemon/composite_provider.go` | `SessionLister` interface に `LocalSessionHost(id) (string, bool)` 追加、`providerForCapture` helper 追加、`CaptureScrollback` / `HistorySize` の routing をそれ経由に変更 |
| `cmd/lazyclaude/local_provider.go` | `localDaemonProvider.LocalSessionHost(id)` 実装 |
| `cmd/lazyclaude/routing_integration_test.go` | `fakeSessionProvider.LocalSessionHost(id)` stub 追加 |
| `internal/daemon/composite_provider_test.go` | 既存 `stubProvider` 等に `LocalSessionHost` stub 追加、providerForCapture routing test 追加 |
| `internal/daemon/server_capture_test.go` (新規) | handler unit test |
| `internal/daemon/http_client_capture_test.go` (新規) | client unit test |
| `internal/daemon/remote_provider_test.go` | CaptureScrollback / HistorySize test 追加 |

## Risk Assessment

- **Low**: capture ops は既に interface 化されていて、routing 変更と実装追加のみ
- **Low**: `CapturePreview` を触らないので現在動いている mirror 経由の preview は regression なし
- **Low**: `SessionLister` に `LocalSessionHost` を追加する interface 拡張は最小 (`(string, bool)` 返し)、既存の `HasSession` と同じ型面。session package を interface 境界に露出しないので import cycle の risk なし (事前確認済)
- **Medium**: `ClientAPI` / `SessionLister` interface 拡張で既存の stub / mock 実装が全て更新必要。worker は Step 2-a / Step 6-a〜6-d の stub 追加を漏れなく実施すること
- **Low**: Daemon API endpoint の security/auth は既存の `s.withAuth` を使うので追加リスクなし
- **High (operational, not code)**: **APIVersion 1 → 2 bump** により、merge 後 **全 remote host の daemon を再 install しないと接続できなくなる**。ユーザーは全 SSH 接続先で `make install PREFIX=$HOME/.local` 相当のコマンドを実行する必要。release notes / Out of Scope 参照

## Open Questions

1. Daemon server handler で `capture-pane` の `-ep` flag を付けるか: 既存の `CapturePaneANSIRange` (`internal/core/tmux/exec.go`) は `-e` ANSI 付きを返す実装のはずなので、worker が実装時に確認
2. `ScrollbackResponse` に `CursorX/CursorY` field があるが scrollback では使わないので空のまま。将来 preview も routing する時のために placeholder として残す
3. `LocalSessionHost` を `SessionLister` に置くか、新規 `SessionResolver` interface を切るか: plan は `SessionLister` に追加する方針だが、worker が実装時に分離の方がきれいと判断すれば option として許容 (interface 位置のみの変更、logic 影響なし)

## Dependencies

- Bug 1 (attach) の `Session.TmuxTarget()` helper は merge 済 (c56b85c base 以降)。本 plan は TmuxTarget に依存
- Bug 3 Phase 1 の diagnostic logging (diag-copy-mode-remote branch) は本 plan merge 前に revert / branch 破棄すること
