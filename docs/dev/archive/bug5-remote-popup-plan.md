# Plan: Bug 5 — Remote permission popup routing fix + duplicate diagnostic

## Context

リモートセッションで claude code が permission prompt を出すと:

1. Local lazyclaude に popup が **2 つ** 表示される (原因未確定)
2. どの popup で Accept/Reject しても **remote claude 側で action が実行されない** (原因特定済)

本 plan は **action 不達を本修正 (Phase B) として daemon-arch に merge**、**2 popup の原因を調査用 logging (Phase A) で特定** する 2 段階構成。

## Codex 議論の要約

Codex 相談の結論 (議論ログ参照):

### 2 popup の仮説 (3 つ)
1. **共有 runtime dir**: remote daemon が `notify.Enqueue` で書いたファイルが NFS/rsync で local の `paths.RuntimeDir` から見える → 経路 B (disk polling) と経路 C (SSE) の両発火
2. **SSE 再送**: `NotificationEvent.ID` 単調増加あるが `Last-Event-ID` 処理なし、client 側 dedup なし → reconnect / retry で 2 回受信
3. **dual-hook registration**: remote shell が local の port file を発見、hook が両 daemon に POST → 経路 A (broker) と経路 C (SSE) 両発火

### Action 不達の修正方針 (codex が賛同)
Bug 4 の SessionID hop pattern を ToolNotification にも適用:
- `server_sse.go:case evt.Notification` に `SessionID: s.sessionIDForWindow(n.Window)` 追加
- `RemoteProvider` に `WithSSEToolInfo(cb)` option 追加
- `root.go` で callback closure 実装、local store lookup → `ToolNotification.Window` を local mirror の TmuxWindow に rewrite
- `remapRemoteWindow` は **backwards-compat fallback として残す**
- SessionID 欠落時 guard、古い daemon 互換性維持

### 順序判断
Codex:
> Action hop fix は先に入れて安全。scope が SSE serialization と provider に限定、2 popup の状況を悪化させない (キーの送信先を変えるだけ)。duplicate 調査は並行で進められる。

## 既存 fix との regression risk

本 plan が触る経路と、既存 fix への影響評価:

| 既存 fix | 触る file | 触る関数 | Risk |
|---------|---------|--------|------|
| Bug 1 (attach, TmuxTarget helper) | `internal/session/store.go`, `internal/session/manager.go`, `cmd/lazyclaude/local_provider.go` | **触らない** | None |
| Bug 3 Phase 2 (remote scrollback) | `internal/daemon/server.go` (capture handlers), `http_client.go`, `composite_provider.go (providerForCapture)` | **触らない** | None |
| Bug 4 (activity state, 4 commits) | `internal/daemon/remote_provider.go:handleSSEEvent:EventActivity`, `cmd/lazyclaude/root.go:activityFwd`, `sessionIDForWindow`, `addToCache` | **`handleSSEEvent` の別 case (EventToolInfo) を追加、EventActivity case は無変更**。`sessionIDForWindow` は参照のみ、変更なし | Very Low |
| Bug 2 Phase 2 (MCP SSH) | `internal/mcp/*`, `internal/gui/app_actions.go:syncPluginProject` | **触らない** | None |

### 具体的な注意点
- `handleSSEEvent` の EventActivity case は **完全に保持**、EventToolInfo case のみ変更
- `RemoteProvider` への callback option 追加は既存の `onSSEActivity` と並列、互いに干渉しない
- `SSEActivityCallback` signature は変更しない (Bug 4 の test が regression しない)
- `server_sse.go:brokerEventToNotification` の EventActivity case は無変更、EventToolInfo case のみ 1 行追加 (`SessionID:` field)
- `remapRemoteWindow` は削除せず残す (古い daemon との互換性 + fallback 用)

## Phase B (Production): Action routing fix via SessionID hop

### Step B1: SSE wire format 拡張

ファイル: `internal/daemon/server_sse.go:82-96` (`case evt.Notification`)

```go
case evt.Notification != nil:
    n := evt.Notification
    return &NotificationEvent{
        ID:        s.nextEventID(),
        Type:      EventToolInfo,
        Time:      n.Timestamp,
        SessionID: s.sessionIDForWindow(n.Window), // NEW
        ToolNotification: &model.ToolNotification{
            ToolName:  n.ToolName,
            Input:     n.Input,
            CWD:       n.CWD,
            Window:    n.Window,  // 既存 wire format と互換、old client が読めるまま
            Timestamp: n.Timestamp,
            MaxOption: n.MaxOption,
        },
    }
```

**互換性**: 古い local (SessionID を読まない) は既存の `ToolNotification.Window` (remote tmux ID) を使う → popup は表示されるが action は不達 (現状と同じ挙動)。新しい local は SessionID を使って rewrite → action 動作。既存の wire format は touched せず、`NotificationEvent.SessionID` 追加のみ。

### Step B2: `RemoteProvider.WithSSEToolInfo` option 追加

ファイル: `internal/daemon/remote_provider.go`

Bug 4 の `SSEActivityCallback` と同じ pattern で新設:

```go
// SSEToolInfoCallback is invoked when an SSE EventToolInfo arrives.
// The callback may mutate the notification (e.g., rewrite Window to the
// local mirror's tmux ID) before it is buffered into rp.notifications.
// The sessionID comes from NotificationEvent.SessionID emitted by the
// daemon SSE handler.
type SSEToolInfoCallback func(n *model.ToolNotification, sessionID string)

// WithSSEToolInfo sets the callback invoked on EventToolInfo.
func WithSSEToolInfo(cb SSEToolInfoCallback) RemoteProviderOption {
    return func(rp *RemoteProvider) { rp.onSSEToolInfo = cb }
}
```

`RemoteProvider` struct に `onSSEToolInfo SSEToolInfoCallback` field 追加。

### Step B3: `handleSSEEvent:EventToolInfo` で callback 呼び出し

ファイル: `internal/daemon/remote_provider.go:189-196`

```go
case EventToolInfo:
    if ev.ToolNotification != nil {
        // Apply optional rewrite hook (e.g., rewrite Window to the
        // local mirror's tmux ID using ev.SessionID). Modifies the
        // ToolNotification in place before buffering.
        if rp.onSSEToolInfo != nil {
            rp.onSSEToolInfo(ev.ToolNotification, ev.SessionID)
        }
        rp.notifications = append(rp.notifications, ev.ToolNotification)
    }
```

**注**: EventActivity case は完全に保持 (Bug 4 fix 無変更)。

### Step B4: `root.go` で callback 実装

ファイル: `cmd/lazyclaude/root.go` の `connectRemoteHost` 内

既存の `activityFwd` と並べて `toolInfoFwd` を追加:

```go
activityFwd := func(ev model.Event, sessionID string) {
    notifyBroker.Publish(resolveActivityWindow(mgr.Store(), ev, sessionID))
}
toolInfoFwd := func(n *model.ToolNotification, sessionID string) {
    // Rewrite Window from remote tmux window ID (e.g. "@22") to the
    // local mirror's tmux window ID (e.g. "@42") so that SendChoice
    // reaches the correct pane when the user answers the popup.
    //
    // Uses the same SessionID hop pattern as resolveActivityWindow
    // (Bug 4). When sessionID is empty (old daemon without Phase B
    // wire format) or the local session is not found, the original
    // Window is left untouched so we degrade to current behavior.
    if sessionID == "" || n == nil {
        return
    }
    localSess := mgr.Store().FindByID(sessionID)
    if localSess == nil || localSess.TmuxWindow == "" {
        return
    }
    n.Window = localSess.TmuxWindow
}

remoteProvider := daemon.NewRemoteProvider(host, remoteConn,
    daemon.WithPostCreate(hook),
    daemon.WithSSEActivity(activityFwd),
    daemon.WithSSEToolInfo(toolInfoFwd), // NEW
)
```

**対称性**: `resolveActivityWindow` (Bug 4) と同じ pattern。違いは ActivityNotification の Window 置換ではなく ToolNotification の Window 置換。defensive copy は不要 (SSE から来た notification は既に新しいインスタンス)。

### Step B5: 互換性 shim (`remapRemoteWindow` 保持、dead code として)

`CompositeProvider.PendingNotifications` の `remapRemoteWindow` は **削除せず残す**。ただし fallback 動作の評価 (codex LOW 指摘反映):

**重要な実態**: `dispatchToolNotification` は現時点で常に `resolveNotifyWindow` が返す tmux window ID (`"@3"` 等) を `ToolNotification.Window` に格納する (`internal/server/server.go:455-503`)。つまり:
- 古い remote daemon (SessionID 送らない) と接続しても、`Window` は `"@N"` 形式であり `lc-` prefix では来ない
- `remapRemoteWindow` は **old daemon 時も no-op のまま**、fallback として機能しない
- **Mixed-version 運用 (local = 新、remote daemon = 旧) では Bug 5 の action routing は動作しない**

**運用方針**: Phase B は **local と remote daemon の両方を同時に update する前提**。Release notes / 運用ドキュメントに明記する。

**remapRemoteWindow を残す理由**: 理論上の backwards-compat や、将来的に何らかの経路で `lc-xxxx` 形式の window が来た際の defensive no-op としての存在意義のみ。実質 dead code だが、削除は別 cleanup plan で (本 plan の scope 外、touch しない)。

**代替 fallback の検討**: Phase B 自体が old daemon 互換を保証できないので、追加の fallback 実装 (例: `"@N"` を session store 上の TmuxWindow と突き合わせる client-side lookup) は本 plan では入れない。必要なら将来 Bug 5.1 で追加。

### Step B6: Tests

ファイル: `internal/daemon/remote_provider_test.go` 追記
- `TestRemoteProvider_SSEToolInfo_Callback_Rewrites`: mock `SSEToolInfoCallback` を inject、`handleSSEEvent(NotificationEvent{Type: EventToolInfo, SessionID: "xxx", ToolNotification: ...})` で callback 呼出しを assert、rewrite 後の Window が rp.notifications に入ることを確認
- `TestRemoteProvider_SSEToolInfo_EmptySessionID_NoRewrite`: sessionID 空で callback は呼ばれるが no-op、既存 Window のまま
- `TestRemoteProvider_SSEToolInfo_NoCallback_Passthrough`: callback 未設定で regression なし (既存の rp.notifications append 動作)

ファイル: `cmd/lazyclaude/root_test.go` 追記 (Bug 4 の resolveActivityWindow test pattern を流用)
- `TestToolInfoFwd_RemapsRemoteWindow`: local store に session 追加、`ToolNotification{Window: "@22"}` を渡して `@42` に rewrite されることを assert
- `TestToolInfoFwd_EmptySessionID_NoRewrite`
- `TestToolInfoFwd_SessionNotFound_NoRewrite`
- `TestToolInfoFwd_NilNotification_NoCrash`
- `TestToolInfoFwd_EmptyTmuxWindow_NoRewrite` (store に session あるが TmuxWindow 未同期)

ファイル: `internal/daemon/server_sse_test.go` 追記
- `TestBrokerEventToNotification_ToolInfo_SetsSessionID`: mock store に session を入れて `brokerEventToNotification` に ToolNotification を渡し、返り値の `SessionID` が session UUID になることを assert

### Step B7: Verification

1. `go build ./...` clean
2. `go vet ./...` clean
3. `go test -race -count=1 ./internal/daemon/... ./internal/gui/... ./cmd/lazyclaude/...` 全 PASS
4. `/go-review` → CRITICAL/HIGH ゼロ
5. `/codex --enable-review-gate` → APPROVED
6. 手動検証 (要ユーザー、Phase A ログと同時):
   - [ ] Remote session で permission prompt 発生 → popup 表示 (1 つでも 2 つでも可)
   - [ ] popup で Accept → remote claude が permission granted で先へ進む
   - [ ] popup で Reject → remote claude が permission denied で拒否する
   - [ ] Local session で permission popup (regression check) → 従来通り動作
   - [ ] Bug 1/3/4/2-Phase2 の regression 無し (attach / scrollback / activity / MCP)

## Phase A (Diagnostic-only): 2 popup の原因特定 logging

**重要**: Phase A の logging は diag branch 専用、merge しない。Phase B merge 後、remote + local 両方で log を収集し 2 popup の原因を特定 → 別 plan で dedup fix を実装。

### 追加する log 箇所

1. **Remote daemon `dispatchToolNotification`** (`internal/server/server.go:488-524`)
   ```go
   s.log.Printf("dispatchToolNotification: pid=%d window=%q toolName=%q hasSubscribers=%v branch=%s",
       req.PID, window, toolName, hasSubscribers, branch)
   ```
   `branch` は `"broker"` or `"disk"`

2. **Remote daemon `brokerEventToNotification` (EventToolInfo branch)** (`internal/daemon/server_sse.go:82-96`)
   ```go
   s.log.Printf("brokerEventToNotification: emit tool_info eventID=%s window=%q sessionID=%q",
       id, n.Window, sessionIDForWindow(n.Window))
   ```

3. **Local `handleSSEEvent` EventToolInfo case** (`internal/daemon/remote_provider.go:189-196`)
   ```go
   debugLog("handleSSEEvent: EventToolInfo host=%q eventID=%q sessionID=%q window=%q tool=%q rewritten=%v",
       rp.host, ev.ID, ev.SessionID, ev.ToolNotification.Window, ev.ToolNotification.ToolName, rewritten)
   ```
   `rewritten` は callback が Window を変更したか

4. **Local GUI polling drain** (`internal/gui/app.go:333-353`)
   ```go
   if len(pending) > 0 {
       debugLog("polling drain: %d pending notifications", len(pending))
       for _, n := range pending {
           debugLog("  poll pending: window=%q tool=%q source=poll", n.Window, n.ToolName)
       }
   }
   ```

5. **Local GUI brokerCh subscriber** (`internal/gui/app.go:266-279`)
   ```go
   if ev.Notification != nil {
       debugLog("brokerCh: window=%q tool=%q source=broker", ev.Notification.Window, ev.Notification.ToolName)
       ...
   }
   ```

### 運用

- Logging は `debuglog.Log` / `s.log.Printf` 経由、既存 debug infrastructure を流用
- Phase A 用の branch 名: `diag-bug5-popup-duplicate` (別 worktree)
- Phase A の logging 追加 commit は Phase B の production fix commit とは **別 commit**
- Phase B 完了後、ユーザーが Phase A 版 binary で再現 → PM が両 daemon の log を SSH で取得 → 原因確定
- 原因特定後、diag branch 破棄、dedup fix は別 plan

**重要**: Phase A の log は本 plan での merge 対象外。Phase B のみ merge。

## Scope まとめ

### In scope
- Phase B: SSE SessionID 拡張 + RemoteProvider callback + root.go closure で ToolNotification.Window rewrite
- Phase B: 関連 unit test (remote_provider_test, root_test, server_sse_test)
- Phase A: 5 箇所の diag logging (別 commit、別 branch、merge 対象外)

### Out of scope
- 2 popup の dedup fix (Phase A 診断後、別 plan)
- `remapRemoteWindow` の削除 (backwards-compat 保持、別 cleanup plan)
- Bug 1 / Bug 3 / Bug 4 / Bug 2 Phase 2 への変更
- SSE `Last-Event-ID` 対応 / event dedup by ID (別 cleanup plan)

## Files Changed

### Phase B (merge 対象)
| ファイル | 変更 |
|---------|------|
| `internal/daemon/server_sse.go` | EventToolInfo case に `SessionID: s.sessionIDForWindow(n.Window)` 追加 |
| `internal/daemon/remote_provider.go` | `SSEToolInfoCallback` type + `WithSSEToolInfo` option + `onSSEToolInfo` field + `handleSSEEvent EventToolInfo` case に callback 呼出し |
| `cmd/lazyclaude/root.go` | `toolInfoFwd` closure 追加、`WithSSEToolInfo(toolInfoFwd)` を NewRemoteProvider に渡す |
| `internal/daemon/remote_provider_test.go` | `SSEToolInfo` callback test 追加 |
| `cmd/lazyclaude/root_test.go` | `toolInfoFwd` helper test (resolveActivityWindow pattern 流用) |
| `internal/daemon/server_sse_test.go` | `brokerEventToNotification` の ToolInfo SessionID test |

### Phase A (diag 専用、非 merge)
| ファイル | 変更 |
|---------|------|
| `internal/server/server.go` | `dispatchToolNotification` に 1 行 Printf |
| `internal/daemon/server_sse.go` | `brokerEventToNotification` に 1 行 Printf |
| `internal/daemon/remote_provider.go` | `handleSSEEvent EventToolInfo` case に debugLog |
| `internal/gui/app.go` | polling loop + brokerCh subscriber に debugLog |

## Risk Assessment

- **Very Low**: Phase B は Bug 4 の既存 pattern を ToolNotification に展開するだけ。scope が SSE wire format + provider callback + root.go closure に限定
- **Low**: `SSEToolInfoCallback` 追加は `SSEActivityCallback` と並列、既存の activity 経路は touched ない
- **Low**: `SessionID` field 追加は wire format の互換性あり (既存 client は無視)
- **Very Low**: `remapRemoteWindow` は削除せず backwards-compat fallback として残すので、古い daemon と接続しても既存挙動
- **Medium** (operational): 2 popup の dedup は Phase A の診断後、別 plan で実装。Phase B 単独では popup 数は変わらない

## Dependencies

- Bug 4 (`f673a30`) の `sessionIDForWindow` helper を前提 (merge 済)
- `daemon-arch` HEAD 以降で実装
- **Mixed-version 非対応**: 本 fix は local TUI と remote daemon の **両方が Phase B 以降** であることを前提とする。片方が旧 version の場合、action routing は動作しない (popup は表示されるが accept/reject が remote に届かない)。運用上、local と remote daemon を同時に install し直す必要あり。Release notes に明記

## Open Questions (解決済)

1. ~~Phase A を Phase B と同時に merge すべきか~~ → **解決**: Phase A は diag-only、別 branch、非 merge
2. ~~`remapRemoteWindow` を削除すべきか~~ → **解決**: 残す (backwards-compat + fallback)
3. ~~Action fix を先に入れて 2 popup 調査を後回しにする順序は安全か~~ → **解決**: codex が "action fix は先に入れて安全、scope が限定、duplicate 状況を悪化させない" と判断済

## Verification sequence

1. Worker が Phase B を実装 → 単体 test + /go-review + /codex --enable-review-gate → review_request
2. PM 側 codex:review → APPROVED → merge to daemon-arch
3. Phase A の diag-only commit を別 branch で worker or PM が実装 → install (merge せず)
4. ユーザーが remote session で permission prompt 再現 → PM が両 daemon の log 収集 → 2 popup の原因確定
5. 原因確定後、別 plan で dedup fix を実装
