# Plan: Remote session activity state が常に Unknown の修正 (Bug 4)

## Context

リモートセッションの activity アイコンがサイドバーで常に `?` (ActivityUnknown) のまま。PreToolUse / Notification / Stop / SessionStart / UserPromptSubmit いずれの hook イベントも反映されない。ローカルは動く。

## Root Cause (確定済)

**Activity emission の key space と sidebar lookup の key が remote mirror session で不一致。**

### Event emission 側

#### Local (動く)
1. ローカルの `claude` が hook 発火 → 127.0.0.1:<port>/notify へ POST
2. `internal/server/server.go:455` `resolveNotifyWindow(pid)` → `FindWindowForPid` → **ローカル tmux window ID** `"@42"`
3. `server.go:416` `notifyBroker.Publish(Event{ActivityNotification: {Window: "@42", State: ...}})`
4. GUI subscriber (`internal/gui/app.go:308-317`) → `a.setWindowActivity("@42", ...)`
5. `windowActivity["@42"] = entry`

#### Remote (壊れる)
1. リモート `claude` が hook 発火 → リモート側 MCP server
2. リモート daemon が `resolveNotifyWindow` → **リモート tmux window ID** (`"@7"` 等) を取得
3. リモート daemon は SSE で local にイベントを push。`NotificationEvent.SessionID` を載せる (`internal/daemon/api.go:258`)
4. Local `RemoteProvider.handleSSEEvent` (`internal/daemon/remote_provider.go:158-193`):
   ```go
   case EventActivity:
       for i := range rp.sessions {
           if rp.sessions[i].ID == ev.SessionID ... {
               mirrorWindow := remapRemoteWindow(rp.sessions[i].TmuxWindow)
               rp.onSSEActivity(model.Event{ActivityNotification: &model.ActivityNotification{
                   Window: mirrorWindow, // "rm-xxxx" (mirror 名)
                   ...
               }})
           }
       }
   ```
5. `root.go:164` `activityFwd` → local broker publish
6. Local GUI → `setWindowActivity("rm-xxxx", entry)` → `windowActivity["rm-xxxx"] = entry`

### Sidebar lookup 側

`cmd/lazyclaude/root.go:530-546` `sessionToItem`:
```go
if wa, ok := windowActivity[s.TmuxWindow]; ok {
```

### `SyncWithTmux` による TmuxWindow 上書き

`internal/session/store.go:570-585`:
```go
syncSession := func(sess *Session) {
    wName := sess.WindowName()  // "lc-xxxx"
    w, found := windowByName[wName]
    if !found && sess.Host != "" {
        mirrorName := MirrorWindowName(sess.ID)  // "rm-xxxx"
        w, found = windowByName[mirrorName]
    }
    ...
    sess.TmuxWindow = w.ID  // "@42" (local tmux window ID)
```

`session.NewGC` が 2 秒周期で `SyncWithTmux` を呼ぶので、mirror 作成後ほぼ即座に `TmuxWindow` が `"@42"` (local tmux ID) に書き換わる。

### 結果

- Remote mirror session
  - `s.TmuxWindow = "@42"` (local tmux ID of mirror window)
  - `windowActivity["@42"]` → **miss** (emit 側は `"rm-xxxx"` で書き込み)
  - `activity = ActivityUnknown`
- Local session
  - `s.TmuxWindow = "@42"`
  - `windowActivity["@42"]` → hit
  - `activity = wa.State`

## Additional problems found (codex review で判明)

### 問題 A: pending popup も同様に壊れる (本 plan の scope 外として分離)

`/notify` は `resolveNotifyWindow` で tmux window ID (`"@7"` 等) を解決し、`ToolNotification.Window` にそのまま入れる。リモート `ToolNotification` は SSE `EventToolInfo` で local に流れ、`RemoteProvider.consumeSSE` が `rp.notifications` に append。

`composite_provider.PendingNotifications` (`internal/daemon/composite_provider.go:355-392`) は drain 時に `remapRemoteWindow(n.Window)` をかけるが、`remapRemoteWindow` は `"lc-"` prefix のみ変換する (`composite_provider.go:388-393`)。**`"@7"` は prefix match しないため無変換で通り抜け**、local GUI の `pending["@7"]` には存在しない key で入る。

結果:
- `sessionToItem` の `pending[s.TmuxWindow]` → `pending["@42"]` → miss
- Remote session の permission popup が reflect されない

これは Bug 4 と同根 (key space 不整合) だが、emit 側の別経路なので本 plan の修正範囲とは別に **Bug 5 として分離** する。現在の修正では activity state のみを解決し、ToolNotification は触らない。

### 問題 B: `clearUnreadActivity` も壊れる

`internal/gui/state.go:4-17` の `enterFullScreen` は `a.clearUnreadActivity(node.Session.TmuxWindow)` を呼ぶ。`node.Session.TmuxWindow` = `"@42"` (local tmux ID) だが、`windowActivity` は `"rm-xxxx"` で keying されているので `delete(windowActivity, "@42")` は no-op。

→ Idle / Error の未読 badge がリモートセッションで永遠に残る。

**本 plan の修正で同時に解決される**: 修正後は `windowActivity` の key が `"@42"` (local tmux ID) に統一されるため、既存の `clearUnreadActivity(node.Session.TmuxWindow)` 呼び出しがそのまま動く。

## Design Philosophy

- 透過性原則: リモートは local tmux mirror window として表現。activity emission の key space も local tmux window ID に統一する
- host 分岐最小化: GUI / sidebar 側の修正ゼロ。emission 側 (`activityFwd` callback) の 1 箇所で remap
- Emission path を触るのは `root.go` の `activityFwd` callback 1 箇所のみ

## Fix Strategy

### 核心

`RemoteProvider.handleSSEEvent` が SSE 経由で受け取った activity event を local broker に forward する経路で、`Window` フィールドに **local mirror session の現在の `TmuxWindow`** (= local tmux window ID `"@42"`) を入れる。

Remote daemon 側の window name ("lc-xxxx") や remote tmux window ID には依存しない。Session ID を hop として、local session.Store を参照して local mirror の TmuxWindow を解決する。

### 実装アプローチ

`remote_provider.handleSSEEvent` → `activityFwd` callback のインタフェースに **session ID** を追加する。Activity event の session ID は `NotificationEvent.SessionID` (`api.go:258`) で既に取得可能。

#### Option 1: Callback signature を変更 (採用)

`SSEActivityCallback` を `func(ev model.Event, sessionID string)` に変更し、`root.go` 側で session ID を使って local store を参照、`ev.ActivityNotification.Window` を local session の `TmuxWindow` で上書きしてから publish。

Pros:
- `model.ActivityNotification` の struct を変更せずに済む (external JSON シリアライズへの影響ゼロ)
- Remote provider が local store を知る必要なし (layering が保たれる)
- Local MCP server 側の emission は一切変更不要 (local は Window が元から正しいので)

Cons:
- `SSEActivityCallback` signature 変更 → 既存 test / 呼び出し元を更新する必要

#### Option 2: Add SessionID to model.ActivityNotification

`ActivityNotification` struct に `SessionID string` を追加。Remote emission 側で設定。GUI 側で session ID を見て lookup 先を切り替え。

Pros: future-proof、他 notification type も同じ pattern で拡張可能
Cons: model 変更により JSON シリアライズ影響範囲が大きい、local emission にも SessionID 設定が必要になる

本 plan は **Option 1 を採用** (scope 最小化、activity のみ解決)。Option 2 は Phase 2 で ToolNotification 含めた全体リファクタの候補。

## 実装ステップ

### Step 1: `SSEActivityCallback` signature を変更

ファイル: `internal/daemon/remote_provider.go:31-38`

```go
// SSEActivityCallback is called when an SSE activity event is received.
// The sessionID is the remote daemon's session ID; callers can use this to
// look up the local mirror session and resolve the correct local tmux window
// target for the published event.
type SSEActivityCallback func(ev model.Event, sessionID string)

// WithSSEActivity sets the callback for SSE activity events.
func WithSSEActivity(cb SSEActivityCallback) RemoteProviderOption {
    return func(rp *RemoteProvider) { rp.onSSEActivity = cb }
}
```

### Step 2: `handleSSEEvent` を session ID 付きで callback を呼ぶように変更

ファイル: `internal/daemon/remote_provider.go:158-193`

```go
func (rp *RemoteProvider) handleSSEEvent(ev NotificationEvent) {
    rp.mu.Lock()
    defer rp.mu.Unlock()

    switch ev.Type {
    case EventActivity:
        for i := range rp.sessions {
            if rp.sessions[i].ID == ev.SessionID || strings.HasPrefix(rp.sessions[i].ID, ev.SessionID) {
                rp.sessions[i].Activity = ev.Activity
                rp.sessions[i].ToolName = ev.ToolName
                if rp.onSSEActivity != nil {
                    // Still include the remapped window as a best-effort
                    // fallback. The callback in root.go will overwrite Window
                    // with the local mirror's current tmux window ID using
                    // the session ID as a lookup hop.
                    mirrorWindow := remapRemoteWindow(rp.sessions[i].TmuxWindow)
                    rp.onSSEActivity(model.Event{ActivityNotification: &model.ActivityNotification{
                        Window:   mirrorWindow,
                        State:    ev.Activity,
                        ToolName: ev.ToolName,
                    }}, rp.sessions[i].ID)
                }
                break
            }
        }
    ...
```

### Step 3: `root.go` の `activityFwd` で local store を参照して Window を remap

ファイル: `cmd/lazyclaude/root.go:164-166`

```go
activityFwd := func(ev model.Event, sessionID string) {
    // Replace the event's Window with the local mirror session's current
    // TmuxWindow so the GUI's windowActivity map is keyed consistently with
    // the sidebar lookup. After SyncWithTmux, a remote mirror session's
    // TmuxWindow is the local tmux window ID ("@42"), not the mirror name.
    // Using the session ID hop here avoids adding a host branch at the
    // sidebar lookup site.
    if ev.ActivityNotification != nil && sessionID != "" {
        if localSess := mgr.Store().FindByID(sessionID); localSess != nil && localSess.TmuxWindow != "" {
            ev.ActivityNotification.Window = localSess.TmuxWindow
        }
    }
    notifyBroker.Publish(ev)
}
```

**注**: `mgr` (session.Manager) は既に root.go の scope にある。`mgr.Store().FindByID(sessionID)` は thread-safe (既存 API)。

### Step 4: 既存の test の signature 更新

`SSEActivityCallback` を使っている箇所のみ更新する。codex review で確認した事実: 影響範囲は `internal/daemon/remote_provider_test.go` のみ。`cmd/lazyclaude/session_adapter_test.go` は pending-notification helper のテストで SSE callback を触らないので変更不要。

Worker が実装時に最終確認:
```bash
grep -rn "SSEActivityCallback\|WithSSEActivity\|onSSEActivity" --include='*.go' .
```
ヒットした全箇所を新 signature `func(ev model.Event, sessionID string)` に合わせる。

### Step 5: Unit tests (新規)

ファイル: `internal/daemon/remote_provider_test.go` (既存テストファイルに追記)

```go
func TestRemoteProvider_SSEActivity_PassesSessionID(t *testing.T) {
    // Scenario: handleSSEEvent for EventActivity should call the callback
    // with the session ID so that root.go's activityFwd can resolve the
    // local mirror's tmux window.
    var gotSessionID string
    var gotEvent model.Event
    rp := &RemoteProvider{
        host: "AERO",
        onSSEActivity: func(ev model.Event, sessionID string) {
            gotEvent = ev
            gotSessionID = sessionID
        },
        sessions: []SessionInfo{
            {ID: "sess-123", Host: "AERO", TmuxWindow: "lc-sess1234"},
        },
    }
    rp.handleSSEEvent(NotificationEvent{
        Type:      EventActivity,
        SessionID: "sess-123",
        Activity:  model.ActivityRunning,
        ToolName:  "Bash",
    })
    assert.Equal(t, "sess-123", gotSessionID)
    require.NotNil(t, gotEvent.ActivityNotification)
    assert.Equal(t, model.ActivityRunning, gotEvent.ActivityNotification.State)
    assert.Equal(t, "Bash", gotEvent.ActivityNotification.ToolName)
    // Window is best-effort "rm-sess1234" (may be overwritten by root.go's callback).
    assert.Equal(t, "rm-sess1234", gotEvent.ActivityNotification.Window)
}
```

ファイル: `cmd/lazyclaude/root_test.go` or similar (activityFwd の remap test)

実際の `activityFwd` は closure で root.go 内にあるため直接単体 test しにくい。代わりに:
- 同等のロジックを取り出した helper 関数として `resolveActivityWindow(mgr, ev, sessionID)` を抽出
- helper 単体で test

```go
// cmd/lazyclaude/root.go (新規 helper)
func resolveActivityWindow(store *session.Store, ev model.Event, sessionID string) model.Event {
    if ev.ActivityNotification == nil || sessionID == "" {
        return ev
    }
    localSess := store.FindByID(sessionID)
    if localSess == nil || localSess.TmuxWindow == "" {
        return ev
    }
    out := ev
    notif := *ev.ActivityNotification
    notif.Window = localSess.TmuxWindow
    out.ActivityNotification = &notif
    return out
}

// activityFwd 内で使用
activityFwd := func(ev model.Event, sessionID string) {
    notifyBroker.Publish(resolveActivityWindow(mgr.Store(), ev, sessionID))
}
```

Test:
```go
func TestResolveActivityWindow_RemapsRemoteWindow(t *testing.T) {
    store := session.NewStore("")
    store.Add(session.Session{
        ID:         "sess-123",
        Host:       "AERO",
        TmuxWindow: "@42",  // local tmux window ID for the mirror
    }, "/proj")

    ev := model.Event{ActivityNotification: &model.ActivityNotification{
        Window:   "rm-sess1234",  // best-effort from remote_provider
        State:    model.ActivityRunning,
    }}
    out := resolveActivityWindow(store, ev, "sess-123")

    assert.Equal(t, "@42", out.ActivityNotification.Window)
    assert.Equal(t, model.ActivityRunning, out.ActivityNotification.State)
    // Original event unchanged (defensive copy).
    assert.Equal(t, "rm-sess1234", ev.ActivityNotification.Window)
}

func TestResolveActivityWindow_NoSessionIDFallthrough(t *testing.T) {
    // Local MCP events have no session ID; event passes through unchanged.
    store := session.NewStore("")
    ev := model.Event{ActivityNotification: &model.ActivityNotification{
        Window: "@7",
        State:  model.ActivityIdle,
    }}
    out := resolveActivityWindow(store, ev, "")
    assert.Equal(t, "@7", out.ActivityNotification.Window)
}

func TestResolveActivityWindow_SessionNotFound(t *testing.T) {
    store := session.NewStore("")
    ev := model.Event{ActivityNotification: &model.ActivityNotification{
        Window: "rm-xxxx",
        State:  model.ActivityRunning,
    }}
    out := resolveActivityWindow(store, ev, "unknown")
    assert.Equal(t, "rm-xxxx", out.ActivityNotification.Window)
}

func TestResolveActivityWindow_NilActivityNotification(t *testing.T) {
    store := session.NewStore("")
    ev := model.Event{}
    out := resolveActivityWindow(store, ev, "sess-123")
    assert.Equal(t, model.Event{}, out)
}
```

### Step 6: Verification

1. `go build ./...` clean
2. `go vet ./...` clean
3. `go test -race ./internal/daemon/... ./cmd/lazyclaude/... ./internal/gui/...` 全 PASS
4. `/go-review` → CRITICAL/HIGH ゼロ
5. `/codex --enable-review-gate` → APPROVED
6. **手動検証** (要ユーザー):
   - [ ] Remote session で claude が実行中 → サイドバーに Running (●) 表示
   - [ ] Remote session で stop (turn 完了) → Idle (✓)
   - [ ] Remote session で fullscreen 入る → 未読 badge がクリアされる (clearUnreadActivity が動く)
   - [ ] Local session の activity (Running / NeedsInput / Idle / Error) が regression なく動く
   - [ ] Local session の permission popup (既存) が regression なく動く
   - [ ] Local session の unread badge クリアが regression なく動く

**本 plan で修正されない項目** (Bug 5 で別途扱う):
- Remote session の permission popup (`ToolNotification`) は引き続き表示されない。emission 経路が別 (`EventToolInfo` → `rp.notifications` → `CompositeProvider.PendingNotifications`) で、別 plan で解決する

## Out of Scope

- **Bug 5 (separate)**: Remote permission popup (`ToolNotification`) が同様に壊れている件。emission path が別 (EventToolInfo → rp.notifications → drainer → composite_provider.PendingNotifications) なので別 plan で扱う
- `ActivityNotification` に `SessionID` を追加する model 変更 (Option 2, Phase 2 候補)
- Bug 1 (attach) — merge 済
- Bug 2 (MCP/plugin remote), Bug 3 (copy mode remote) — 並行作業中

## Files Changed

| ファイル | 変更 |
|---------|------|
| `internal/daemon/remote_provider.go` | `SSEActivityCallback` signature を `func(ev model.Event, sessionID string)` に変更、`handleSSEEvent` の EventActivity で callback に sessionID を渡す |
| `cmd/lazyclaude/root.go` | `resolveActivityWindow` helper を追加、`activityFwd` closure を helper 経由に変更 |
| `cmd/lazyclaude/root_test.go` (新規 or 追記) | `resolveActivityWindow` の table test (remap 成功、session 見つからない、session ID 空、ActivityNotification nil) |
| `internal/daemon/remote_provider_test.go` | `handleSSEEvent` の EventActivity test で callback signature 更新、sessionID が渡ることの assertion 追加 |
| `cmd/lazyclaude/session_adapter_test.go` など | `WithSSEActivity` を使うテストがあれば signature 更新 |

## Risk Assessment

- **Low**: 変更は callback signature + activityFwd の 2 箇所のみ
- **Low**: Local emission path は一切触らない (local session 経路は regression risk なし)
- **Low**: Emission 側の defensive copy で破壊的変更なし
- **Medium**: `SSEActivityCallback` signature 変更は既存 test の signature 更新を伴うので worker が test 全体を grep して確認する必要

## Open Questions

1. Bug 5 (permission popup on remote) を Phase 2 で Option 2 (model に SessionID 追加) と同時に解決するか、別 plan で扱うか
2. `activityFwd` のロジックを `RemoteProvider` 側に移動し、`WithLocalStoreLookup` のような option で local store を注入する代替アプローチも可能。今回は root.go の wiring の方が簡潔なので Option 1 を採用
