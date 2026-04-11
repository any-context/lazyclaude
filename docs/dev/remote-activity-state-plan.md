# Plan: Remote session activity state が常に Unknown の修正 (Bug 4)

## Context

リモートセッションの activity アイコンがサイドバーで常に `?` (ActivityUnknown) のまま。PreToolUse / Notification / Stop / SessionStart / UserPromptSubmit いずれの hook イベントも反映されない。ローカルは動く。

## Root Cause

**Activity emission の key space と sidebar lookup の key が remote mirror session で不一致。**

### Event emission 側

#### Local (動く)
1. ローカルの `claude` が hook を発火 → 127.0.0.1:<port>/notify へ POST (`internal/core/config/hooks.go`)
2. `internal/server/server.go:386` `resolveNotifyWindow(pid)` → `FindWindowForPid` → **ローカル tmux window ID** `@42`
3. `server.go:416` `notifyBroker.Publish(Event{ActivityNotification: {Window: "@42", State: ...}})`
4. GUI subscriber (`internal/gui/app.go:308-317`) が `a.setWindowActivity("@42", ...)` 呼び出し
5. `windowActivity["@42"] = entry`

#### Remote (壊れる)
1. リモート `claude` が hook を発火 → **リモート側の** MCP server へ POST
2. リモート daemon が `resolveNotifyWindow` でリモート tmux window ID を取得 → 自分のbroker に publish
3. リモート daemon が SSE で local にイベントを push、ペイロードに `SessionID` を載せる
4. Local `RemoteProvider.handleSSEEvent` (`internal/daemon/remote_provider.go:158-193`):
   ```go
   case EventActivity:
       for i := range rp.sessions {
           if rp.sessions[i].ID == ev.SessionID ... {
               mirrorWindow := remapRemoteWindow(rp.sessions[i].TmuxWindow)
               rp.onSSEActivity(model.Event{ActivityNotification: &model.ActivityNotification{
                   Window:   mirrorWindow,  // ← "rm-xxxx" (mirror 名)
                   ...
               }})
           }
       }
   ```
   - `rp.sessions[i].TmuxWindow` は daemon API の `SessionInfo.TmuxWindow` 由来 = `sess.WindowName()` = `"lc-xxxx"` (`internal/daemon/server.go:241` など)
   - `remapRemoteWindow` は `"lc-xxxx"` → `"rm-xxxx"` に変換 (`composite_provider.go:388`)
5. `root.go:164` `activityFwd` 経由で local `notifyBroker.Publish(...)`
6. Local GUI の subscriber が `setWindowActivity("rm-xxxx", entry)` → `windowActivity["rm-xxxx"] = entry`

### Sidebar lookup 側

`cmd/lazyclaude/root.go:530-540` `sessionToItem`:
```go
if s.Status == session.StatusRunning {
    if wa, ok := windowActivity[s.TmuxWindow]; ok {
        activity = wa.State
        toolName = wa.ToolName
    }
}
```

Key は `s.TmuxWindow`。

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
    if !found {
        sess.Status = StatusOrphan
        sess.TmuxWindow = ""
        return
    }
    sess.TmuxWindow = w.ID  // ← local tmux window ID "@42" で上書き
```

**Remote mirror session の `TmuxWindow` は `SyncWithTmux` 後に `"@42"` (local tmux ID) になる**。Mirror 作成直後は `"rm-xxxx"` (名) だが、sync が走ると "@42" に置き換わる。GC (`session.NewGC`) が 2 秒周期で sync を呼ぶので、ほぼ即座に "@42" になる。

### 結果: 常に Unknown

- Remote mirror session の場合
  - `s.TmuxWindow = "@42"`
  - `windowActivity["@42"]` → **miss** (emit 側は `"rm-xxxx"` で書き込んでいる)
  - `activity = ActivityUnknown`
- Local session の場合
  - `s.TmuxWindow = "@42"`
  - `windowActivity["@42"]` → hit (emit 側も `"@42"` で書き込み)
  - `activity = wa.State`

## Design Philosophy

- 透過性原則: リモートはローカル tmux の mirror window として表現 → sidebar は local/remote 問わず同じ lookup 経路で解決すべき
- host 分岐最小化: 新規 `if sess.Host != ""` を増やさない
- 既存の remote_provider / server 経路は保つ (emission 側の大規模変更は Phase 2 以降)

## Fix Strategy

### Option A: Sidebar lookup で複数 key を試す (採用)

`sessionToItem` の activity lookup を、session の複数の表現 (現在の `TmuxWindow` と `MirrorWindowName(ID)`) で順に試す。

メリット:
- 変更は `cmd/lazyclaude/root.go:sessionToItem` 1 箇所
- 新規 host 分岐なし (常に両方試す)
- Emission 側 (server / remote_provider) を一切触らない
- 既存 test への影響最小

デメリット:
- Local session の場合 2 回目 lookup は常に miss (overhead は無視できる)
- 将来 `ToolNotification` / `StopNotification` 等の他 event にも `rm-` / `@42` の不一致があれば同じ 2 段階 lookup が必要 → pending popup lookup `pending[s.TmuxWindow]` も同様に修正が必要か要検証

### Option B: SessionID ベースの keying (scope 外、Phase 2 候補)

`ActivityNotification` に `SessionID` フィールド追加、local/remote 両方の emission side で session ID を設定、GUI 側で session ID で keying する。

より principled だが:
- model 変更 + server.go / remote_provider.go 両方の emission 修正
- `windowActivity` map の key を session ID に変更 (popup の `setWindowActivity(window, ...)` 呼び出し箇所も refactoring)
- Phase 2 に切り出し

本 plan は **Option A のみ** を扱う。

## Fix 実装 (Option A)

### Step 1: Activity lookup helper を新設

ファイル: `cmd/lazyclaude/root.go`

```go
// lookupActivity resolves the window activity entry for a session, trying
// multiple candidate keys in the window activity map.
//
// Background: local sessions and remote mirror sessions have different key
// spaces in the activity map:
//   - Local: emitted with the local tmux window ID (e.g. "@42"), matches
//     sess.TmuxWindow after SyncWithTmux.
//   - Remote mirror: emitted by RemoteProvider.handleSSEEvent with the
//     mirror window NAME (e.g. "rm-abcd1234") via remapRemoteWindow, but
//     sess.TmuxWindow is overwritten to the local tmux window ID ("@42")
//     by SyncWithTmux. The two never match.
//
// The lookup tries sess.TmuxWindow first (fast path for local, and for
// remote mirrors before the first sync overwrite) and then falls back to
// the canonical mirror window name derived from the session ID. For local
// sessions the second lookup misses cheaply.
//
// This avoids adding a host branch at the call site and does not change
// the activity emission path.
func lookupActivity(
    windowActivity map[string]gui.WindowActivityEntry,
    sess session.Session,
) (gui.WindowActivityEntry, bool) {
    if sess.TmuxWindow != "" {
        if wa, ok := windowActivity[sess.TmuxWindow]; ok {
            return wa, true
        }
    }
    if wa, ok := windowActivity[session.MirrorWindowName(sess.ID)]; ok {
        return wa, true
    }
    return gui.WindowActivityEntry{}, false
}
```

**注**: `session.MirrorWindowName(sess.ID)` は sess.Host != "" かどうかに関係なく `"rm-" + ID[:8]` を返すだけの pure function。ローカル session でも呼び出しコストは無視できる (O(1))。

### Step 2: sessionToItem を helper 経由に変更

ファイル: `cmd/lazyclaude/root.go:530-546`

```go
func sessionToItem(s session.Session, pending map[string]bool, windowActivity map[string]gui.WindowActivityEntry) gui.SessionItem {
    activity := model.ActivityUnknown
    toolName := ""

    // Priority 1: window activity from broker events (NeedsInput, Running, Idle, Error).
    if s.Status == session.StatusRunning {
        if wa, ok := lookupActivity(windowActivity, s); ok {
            activity = wa.State
            toolName = wa.ToolName
        }
    }

    // Priority 2: pending permission popup overrides to NeedsInput
    // (file-based polling fallback for when broker is not connected).
    //
    // pending is keyed by the same window value used by ToolNotification.Window
    // which matches server.go's resolveNotifyWindow output. Try both keys for
    // symmetry with lookupActivity so remote mirror sessions pick up pending
    // popups as well.
    if s.Status == session.StatusRunning {
        if pending[s.TmuxWindow] || pending[session.MirrorWindowName(s.ID)] {
            activity = model.ActivityNeedsInput
        }
    }

    return gui.SessionItem{
        ID:         s.ID,
        Name:       s.Name,
        Path:       s.Path,
        Host:       s.Host,
        Status:     s.Status.String(),
        Flags:      s.Flags,
        TmuxWindow: s.TmuxWindow,
        Activity:   activity,
        ToolName:   toolName,
        Role:       string(s.Role),
    }
}
```

`pending` は `pendingWindowSet(notifications)` (`root.go:480-487`) の結果で `ToolNotification.Window` の set。remote provider の notification drain 経路 (`composite_provider.go:378-380`) でも `remapRemoteWindow` が適用されるので `pending` map の key も `"rm-xxxx"` 形式。よって同じ 2 段階 lookup で OK。

### Step 3: 既存挙動の確認

`pending` map の中身を追跡:
- Local: `ToolNotification.Window` = `server.go` が `resolveNotifyWindow` で resolve した tmux window ID (`"@42"`)
- Remote: `composite_provider.PendingNotifications` で `remapRemoteWindow(n.Window)` → `"rm-xxxx"`

従って pending key も activity key と同じ不一致問題がある。Step 2 で両方 lookup するよう修正することで pending popup も remote で動くようになる (副次効果)。

### Step 4: Unit tests

ファイル: `cmd/lazyclaude/root_test.go` (or 適切な test file)

```go
func TestSessionToItem_RemoteActivityKeyedByMirrorName(t *testing.T) {
    // Scenario: remote mirror session's TmuxWindow is "@42" (local tmux ID
    // after SyncWithTmux), but windowActivity is keyed by "rm-abcd1234"
    // (mirror window name emitted by RemoteProvider.handleSSEEvent).
    sess := session.Session{
        ID:         "abcd1234ef567890", // 8+ chars
        Host:       "AERO",
        Status:     session.StatusRunning,
        TmuxWindow: "@42",
    }
    windowActivity := map[string]gui.WindowActivityEntry{
        "rm-abcd1234": {State: model.ActivityRunning, ToolName: "Bash"},
    }
    item := sessionToItem(sess, nil, windowActivity)

    assert.Equal(t, model.ActivityRunning, item.Activity)
    assert.Equal(t, "Bash", item.ToolName)
}

func TestSessionToItem_LocalActivityKeyedByWindowID(t *testing.T) {
    // Regression: local session lookup still works with tmux window ID key.
    sess := session.Session{
        ID:         "1111222233334444",
        Host:       "",
        Status:     session.StatusRunning,
        TmuxWindow: "@7",
    }
    windowActivity := map[string]gui.WindowActivityEntry{
        "@7": {State: model.ActivityNeedsInput, ToolName: "Edit"},
    }
    item := sessionToItem(sess, nil, windowActivity)

    assert.Equal(t, model.ActivityNeedsInput, item.Activity)
}

func TestSessionToItem_RemotePendingKeyedByMirrorName(t *testing.T) {
    // pending popup lookup should also match on the mirror name for remote.
    sess := session.Session{
        ID:         "abcd1234ef567890",
        Host:       "AERO",
        Status:     session.StatusRunning,
        TmuxWindow: "@42",
    }
    pending := map[string]bool{"rm-abcd1234": true}
    item := sessionToItem(sess, pending, nil)

    assert.Equal(t, model.ActivityNeedsInput, item.Activity)
}

func TestSessionToItem_UnknownWhenNoActivity(t *testing.T) {
    sess := session.Session{ID: "x", Status: session.StatusRunning, TmuxWindow: "@1"}
    item := sessionToItem(sess, nil, nil)
    assert.Equal(t, model.ActivityUnknown, item.Activity)
}
```

### Step 5: Verification

1. `go build ./...` clean
2. `go vet ./...` clean
3. `go test -race ./cmd/lazyclaude/... ./internal/gui/...` 全 PASS
4. `/go-review` → CRITICAL/HIGH ゼロ
5. `/codex --enable-review-gate` → APPROVED
6. **手動検証** (要ユーザー):
   - [ ] Remote session で claude を実行中 → サイドバーに Running (●) 表示
   - [ ] Remote session で `Read`/`Bash`/`Write` 等の tool 確認待ちが発生 → NeedsInput (⚠) と popup
   - [ ] Remote session で stop (turn 完了) → Idle (✓)
   - [ ] Local session で各 state が regression なく動く
   - [ ] Remote session 作成直後 (sync 前) も動く (ここはもともと動いていたはず、要確認)

## Out of Scope

- Option B (SessionID ベース keying) — 別 PR
- Emission side の変更 (server.go / remote_provider.go)
- `ActivityNotification` / `ToolNotification` に `SessionID` 追加
- Bug 1 (attach) — 既に merge 済
- Bug 2 (MCP/plugin remote)、Bug 3 (copy mode remote)

## Files Changed

| ファイル | 変更 |
|---------|------|
| `cmd/lazyclaude/root.go` | `lookupActivity` helper 追加、`sessionToItem` の activity + pending lookup を helper 経由に変更 |
| `cmd/lazyclaude/root_test.go` (新規 or 追記) | Remote/local の activity lookup table test |

## Risk Assessment

- **Very Low**: 変更は lookup 側だけ、emission path は一切触らない
- **Low**: Local session でも 2 回目 lookup (MirrorWindowName(ID) = "rm-xxxx") が常に走るが、miss なので overhead は O(1) map lookup 1 回分のみ
- **Low**: `session.MirrorWindowName` export が既に存在するので新規 API 追加なし

## Open Questions

1. Option B (SessionID 全面移行) を Phase 2 で実施すべきか、それとも Option A で当面十分か
2. `ToolNotification.Window` に SessionID 相当が無いので popup の window 識別も同じ問題を抱えているか要検証 (Phase 2 候補)
3. `clearUnreadActivity(window)` (`app.go:474`) は window 文字列を受け取るが、これも remote で壊れる可能性 (切替時に stale 状態が残る) — 要別途確認
