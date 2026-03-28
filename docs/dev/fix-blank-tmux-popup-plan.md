# Fix: Blank tmux popup bugs (v2 -- redesign)

**Created**: 2026-03-28
**Status**: Planning
**Branch**: fix-brank-tmux-popup

---

## Overview

2 bugs in tmux display-popup integration:

1. **Bug 1**: TUI overlay で popup を消費後に quit すると、同時に spawn された tmux display-popup が露出して表示される
2. **Bug 2**: TUI 閉鎖時（hostTmux 上の display-popup）で popup を許可しても、Claude Code の pane にキーが送信されない

## 設計上の問題

### 現状: 三重配信

`dispatchToolNotification()` が 3 つの配信経路を**同時に**発火する:

```
通知到着
  ├─ 1. notify.Enqueue()          → ディスクにファイル (SSH remote 互換)
  ├─ 2. notifyBroker.Publish()    → in-process broker → TUI overlay
  └─ 3. popupOrch.SpawnToolPopup()→ tmux display-popup (別プロセス)
```

これが両バグの根本原因:
- Bug 1: (2) と (3) が同時発火 → TUI が overlay で処理しても display-popup が残る
- Bug 2: (3) の display-popup プロセスに LAZYCLAUDE_TMUX_SOCKET が渡らない

### さらに: 二重消費

TUI は broker 経由で即座に通知を受け取るが、100ms ticker でも `notify.ReadAll()` を呼ぶ。
broker と ticker が同じ通知を二重に `showToolPopup()` する可能性がある（既存バグ）。

---

## 新設計: 単一経路ディスパッチ

**核心**: broker の subscriber 状態でディスパッチを分岐する。

```
通知到着
  ├─ broker.HasSubscribers()?
  │   ├─ YES (TUI が in-process)
  │   │   → broker.Publish() のみ
  │   │   → display-popup スキップ
  │   │   → notify.Enqueue() スキップ (broker が直接配信)
  │   │
  │   └─ NO (daemon mode / TUI 未起動)
  │       → notify.Enqueue() (ディスク)
  │       → popupOrch.SpawnToolPopup() (display-popup)
```

### なぜ broker.HasSubscribers() で十分か

| 状態 | Subscriber | 正しい経路 |
|------|-----------|-----------|
| TUI open + overlay mode | YES | broker → TUI overlay |
| TUI open + fullscreen | YES | broker → TUI overlay (popup が来ると key forwarding 停止、overlay 表示) |
| TUI closed | NO | display-popup on hostTmux |
| Daemon server (TUI なし) | NO | display-popup on hostTmux |

**fullscreen 中も TUI overlay で処理できる理由**:
`state.go:23` の `resolveForwardTarget()` は `a.hasPopup()` が true なら key forwarding を停止する。
つまり fullscreen 中に通知が来ると、TUI は自動的に overlay popup を表示し、
ユーザーが dismiss するまで key forwarding を一時停止する。
TUI の `SendChoice()` は lazyclaude tmux client を直接使うため、socket 問題もない。

### TUI lock ファイルが不要になる理由

lock ファイルは「TUI が起動中か」を外部から判定するための仕組みだった。
broker の subscriber 数は同じ情報をプロセス内で正確に提供する。
lock ファイルには TOCTOU race、stale file、fullscreen 判定不能、という問題があったが、
全て解消される。

---

## Phase 1: broker.HasSubscribers() と単一経路ディスパッチ

### Step 1.1: `HasSubscribers()` を broker に追加

**File**: `internal/core/event/broker.go`

```go
func (b *Broker[T]) HasSubscribers() bool {
    b.mu.Lock()
    defer b.mu.Unlock()
    return len(b.subs) > 0
}
```

### Step 1.2: `dispatchToolNotification()` を条件分岐に変更

**File**: `internal/server/server.go`

Before:
```go
notify.Enqueue(...)       // 常に
s.notifyBroker.Publish()  // 常に
s.popupOrch.SpawnToolPopup() // 常に
```

After:
```go
s.notifyBroker.Publish(model.Event{Notification: &n})

if s.notifyBroker.HasSubscribers() {
    // TUI が in-process → broker が直接配信済み。display-popup 不要。
    s.log.Printf("notify: delivered via broker (TUI active)")
} else {
    // TUI なし → ディスクにファイル + display-popup
    notify.Enqueue(s.config.RuntimeDir, n)
    s.popupOrch.SpawnToolPopup(ctx, window, toolName, input, cwd)
}
```

### Step 1.3: TUI lock ファイルを削除

**File**: `internal/gui/app.go`

`Run()` 内の lock ファイル作成・削除コード (L176-178) を削除。
broker subscriber の有無がこの役割を果たす。

### テスト

| Test | Description |
|------|-------------|
| `TestBroker_HasSubscribers` | Subscribe 前は false、Subscribe 後は true、Cancel 後は false |
| `TestDispatch_UseBrokerWhenSubscribed` | subscriber あり → SpawnToolPopup 呼ばれない |
| `TestDispatch_UsePopupWhenNoSubscriber` | subscriber なし → SpawnToolPopup + Enqueue 呼ばれる |

---

## Phase 2: socket 伝播修正 (display-popup 経路の修正)

display-popup 経路が使われるのは TUI 不在時のみだが、正しく動作させる必要がある。

### Step 2.1: `PopupOrchestrator` に `socket` フィールド追加

**File**: `internal/adapter/tmuxadapter/orchestrator.go`

```go
type PopupOrchestrator struct {
    binary     string
    socket     string      // lazyclaude tmux socket name (e.g., "lazyclaude")
    tmux       tmux.Client
    hostTmux   tmux.Client
    ...
}

func NewPopupOrchestrator(binary, socket, runtimeDir string, ...) *PopupOrchestrator {
    ...
}
```

### Step 2.2: env 設定で `p.socket` を使用

**File**: `internal/adapter/tmuxadapter/orchestrator.go`

`spawnToolPopupBlocking()` と `SpawnDiffPopup()` の両方で:

Before:
```go
if s := os.Getenv("LAZYCLAUDE_TMUX_SOCKET"); s != "" {
    env["LAZYCLAUDE_TMUX_SOCKET"] = s
}
```

After:
```go
if p.socket != "" {
    env["LAZYCLAUDE_TMUX_SOCKET"] = p.socket
}
```

### Step 2.3: server.Config に TmuxSocket 追加

**File**: `internal/server/server.go`

`server.Config` に `TmuxSocket string` フィールド追加。
`New()` で `NewPopupOrchestrator` に socket を渡す。

**File**: `cmd/lazyclaude/server.go`

`cfg.TmuxSocket = tmuxSocket` を設定。

**File**: `cmd/lazyclaude/root.go`

`tryStartInProcessServer` でも `cfg.TmuxSocket` を設定
（in-process server でも daemon fallback 経路がある）。

### Step 2.4: `tool.go` / `diff.go` の CapturePaneANSI も socket を使用

**File**: `cmd/lazyclaude/tool.go` (L53-63), `cmd/lazyclaude/diff.go` (L44-53)

Before:
```go
client := tmux.NewExecClient() // host tmux — 間違い
```

After:
```go
var client tmux.Client
if s := os.Getenv("LAZYCLAUDE_TMUX_SOCKET"); s != "" {
    client = tmux.NewExecClientWithSocket(s)
} else {
    client = tmux.NewExecClient()
}
```

これにより CapturePaneANSI と SendToPane が同じ tmux server を参照する。

### Step 2.5: `setup.go` の extraEnv に socket 追加 (防御的)

**File**: `cmd/lazyclaude/setup.go`

```go
extraEnv = append(extraEnv, "LAZYCLAUDE_TMUX_SOCKET=lazyclaude")
```

### テスト

| Test | Description |
|------|-------------|
| `TestPopupEnv_ContainsSocket` | orchestrator が env に socket を含める |
| `TestPopupEnv_EmptySocket` | socket 空なら env キーを含めない |
| 既存テスト更新 | `NewPopupOrchestrator` シグネチャ変更に追随 |

---

## Phase 3: 二重消費の防止

### Step 3.1: broker 配信時は ticker 経由の `PendingNotifications()` をスキップ

**File**: `internal/gui/app.go` (L214-224)

broker が接続されている場合、ticker の `PendingNotifications()` 呼び出しをスキップ。
broker が nil (subprocess server fallback) の場合のみファイルポーリングを使用。

```go
case <-ticker.C:
    a.notify.OnTick()
    a.gui.Update(func(g *gocui.Gui) error {
        // broker 経由で配信されている場合はファイルポーリング不要
        if a.sessions != nil && !a.notify.HasBroker() {
            for _, n := range a.sessions.PendingNotifications() {
                a.showToolPopup(n)
            }
        }
        return nil
    })
```

**File**: `internal/gui/notify_loop.go`

```go
func (nl *NotifyLoop) HasBroker() bool {
    return nl.brokerSub != nil
}
```

### Step 3.2: `notify.ReadAll(os.TempDir())` のハードコード修正

**File**: `cmd/lazyclaude/tool.go` (L156)

Before:
```go
notify.ReadAll(os.TempDir())
```

After:
```go
notify.ReadAll(config.DefaultPaths().RuntimeDir)
```

同様に `diff.go` にも同じパターンがあれば修正。

---

## 変更ファイル一覧

| File | Phase | Change |
|------|-------|--------|
| `internal/core/event/broker.go` | 1 | `HasSubscribers()` 追加 |
| `internal/server/server.go` | 1,2 | 条件分岐ディスパッチ + Config.TmuxSocket |
| `internal/gui/app.go` | 1,3 | lock ファイル削除 + ticker ポーリング条件追加 |
| `internal/gui/notify_loop.go` | 3 | `HasBroker()` 追加 |
| `internal/adapter/tmuxadapter/orchestrator.go` | 2 | socket フィールド + env 修正 |
| `internal/adapter/tmuxadapter/orchestrator_test.go` | 2 | シグネチャ追随 + socket テスト |
| `cmd/lazyclaude/server.go` | 2 | `cfg.TmuxSocket` 設定 |
| `cmd/lazyclaude/root.go` | 2 | `tryStartInProcessServer` で TmuxSocket 設定 |
| `cmd/lazyclaude/setup.go` | 2 | extraEnv に socket 追加 |
| `cmd/lazyclaude/tool.go` | 2,3 | CapturePaneANSI socket 修正 + RuntimeDir 修正 |
| `cmd/lazyclaude/diff.go` | 2 | CapturePaneANSI socket 修正 |

---

## Phase Dependencies

```
Phase 1 (単一経路ディスパッチ)  → Phase 3 (二重消費防止)
          ↕ 独立
Phase 2 (socket 伝播修正)
```

推奨順序: Phase 1 → Phase 2 → Phase 3

## Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| broker subscriber race (Subscribe 直後に Publish) | Low | Subscribe はロック内で完了。Publish 前に subscriber 登録済み |
| Subprocess server fallback 時の notify.Enqueue 不在 | Low | HasSubscribers=false → Enqueue は実行される |
| `NewPopupOrchestrator` シグネチャ変更 | Low | コンパイラが全 caller を検出 |
| lock ファイル削除による後方互換 | None | lock ファイルは外部から参照されていない |

## Testing

```bash
go test ./internal/... -count=1 -race
go test ./cmd/... -count=1 -race
```
