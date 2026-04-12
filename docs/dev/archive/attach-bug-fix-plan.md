# Plan: Fix remote attach bug via Session.TmuxTarget() helper

## Context

リモートセッションにカーソルを合わせて `a` (attach) を押しても何も起きない (エラーも出ない、画面も変化しない)。ローカル attach は正常。

## Root Cause

1. `'a'` → `App.AttachSession()` → `guiCompositeAdapter.AttachSession(id)` → `CompositeProvider.AttachSession(id)`
2. `CompositeProvider.providerForSession(id)` は `local.HasSession(id)` を先にチェック
3. remote mirror session は local Store にも存在する (`MirrorManager.CreateMirror` が追加) → local provider が選ばれる (設計通り)
4. `localDaemonProvider.AttachSession` (`cmd/lazyclaude/local_provider.go:151-164`) が以下で target を組み立てる:
   ```go
   target := "lazyclaude:" + sess.WindowName()
   ```
5. `Session.WindowName()` は常に `"lc-" + ID[:8]` を返す (`internal/session/store.go:62-67`)
6. remote mirror の実 window 名は `"rm-" + ID[:8]` (`MirrorWindowName`)
7. → `tmux attach-session -t lazyclaude:lc-xxxx` が `can't find window` で失敗
8. エラーは `cmd.Run()` で返るが GUI に表示されない (`cmd.Stderr = os.Stderr` で端末直書き)

## Design Principle

**lazyclaude の透過性の原則**: リモートはローカル tmux の「ミラーウィンドウ」として表現し、ランタイム操作 (attach/capture/send-keys) はローカル tmux に一本化する。実装内の `if sess.Host != ""` 分岐は最小化する。

**教訓 (codex review より)**:
- `tmux attach-session` は session target (`session:window`) を要求。window ID (`@42`) や bare window name (`rm-xxxx`) は不可
- `tmux capture-pane` は window/pane target を受け付けるので `@42` / `rm-xxxx` で動く
- → attach と capture で target 文法が違うため、単純に同じ pattern に揃えることはできない
- desync 時には `TmuxWindow` が空になる (`internal/session/store.go:573-585`) ので、その fallback も host を意識する必要がある
- → **host 分岐を 1 箇所 (`Session.TmuxTarget()` helper) に封じ込める** のが最適解

## Fix Strategy

### Step 1: `Session.TmuxTarget()` helper を導入

ファイル: `internal/session/store.go`

```go
// TmuxTarget returns the tmux target string for runtime operations
// (attach-session, capture-pane, send-keys, kill-window).
//
// Encapsulates the local/remote distinction in ONE place so that callers
// do not need to branch on sess.Host. Returns a fully-qualified target
// of the form "lazyclaude:<window>" suitable for tmux -L lazyclaude
// commands that require a session:window target (e.g. attach-session).
//
// Resolution order:
//  1. If TmuxWindow is non-empty, use it (may be tmux window ID "@42"
//     for local, or mirror window name "rm-xxxx" for remote).
//  2. Otherwise fall back to the canonical window name:
//     - Remote (Host != ""): MirrorWindowName(ID) -> "rm-xxxx"
//     - Local (Host == ""):  WindowName()         -> "lc-xxxx"
//  3. If the resulting target does not contain ':', prefix with
//     "lazyclaude:" so tmux parses it as a session:window target.
func (s *Session) TmuxTarget() string {
    target := s.TmuxWindow
    if target == "" {
        if s.Host != "" {
            target = MirrorWindowName(s.ID)
        } else {
            target = s.WindowName()
        }
    }
    if !strings.Contains(target, ":") {
        target = tmuxSessionName + ":" + target
    }
    return target
}
```

**注**: `tmuxSessionName` は既存定数 (`"lazyclaude"`) を使う。存在しなければ新設。

### Step 2: AttachSession を helper に置換

ファイル: `cmd/lazyclaude/local_provider.go:151-164`

```go
func (p *localDaemonProvider) AttachSession(id string) error {
    sess := p.mgr.Store().FindByID(id)
    if sess == nil {
        return fmt.Errorf("session not found: %s", id)
    }
    target := sess.TmuxTarget()

    _ = exec.Command("tmux", "-L", "lazyclaude", "set-option", "-t", "lazyclaude", "window-size", "largest").Run()

    cmd := exec.Command("tmux", "-L", "lazyclaude", "attach-session", "-t", target)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}
```

### Step 3: CapturePreview / CaptureScrollback / HistorySize も helper に置換

ファイル: `cmd/lazyclaude/local_provider.go`

既存の 3 メソッド (L74, L117, L130) は `target := sess.TmuxWindow; if target == "" { target = "lazyclaude:" + sess.WindowName() }` パターン。これらを `target := sess.TmuxTarget()` に置換して統一。

**注意**: capture-pane は `@42` / `rm-xxxx` の bare form でも動くが、`TmuxTarget()` は常に prefix 付きを返す。tmux の capture-pane は prefix 付きも受け付けるので問題なし (要検証)。

### Step 4: Delete の host 分岐を helper に置換

ファイル: `internal/session/manager.go:428-438`

現状:
```go
windowName := sess.WindowName()
if sess.Host != "" {
    windowName = MirrorWindowName(sess.ID)
}
target := tmuxSessionName + ":" + windowName
```

変更:
```go
target := sess.TmuxTarget()
```

これで既存の host 分岐が helper に吸収される。desync 時のフォールバック挙動 (TmuxWindow 空 → MirrorWindowName) は `TmuxTarget()` が同じロジックを持つので挙動互換。

### Step 5 (Scope 外 / 別 PR): Attach error の GUI propagation

現状 `cmd.Stderr = os.Stderr` のため tmux エラーが端末に流れるだけで GUI に上がらない。これは別 PR で `onError` 経由に変更する。本 PR の scope 外。

## Tests

### 新規: `internal/session/store_test.go` に `TmuxTarget` のテーブルテスト

```go
func TestSession_TmuxTarget(t *testing.T) {
    cases := []struct {
        name string
        sess Session
        want string
    }{
        {"local with TmuxWindow ID",
            Session{ID: "0123456789abcdef", TmuxWindow: "@42"},
            "lazyclaude:@42"},
        {"local with TmuxWindow already prefixed",
            Session{ID: "0123456789abcdef", TmuxWindow: "lazyclaude:@42"},
            "lazyclaude:@42"},
        {"local fallback (empty TmuxWindow)",
            Session{ID: "0123456789abcdef"},
            "lazyclaude:lc-01234567"},
        {"remote mirror with TmuxWindow name",
            Session{ID: "0123456789abcdef", Host: "AERO", TmuxWindow: "rm-01234567"},
            "lazyclaude:rm-01234567"},
        {"remote fallback (empty TmuxWindow, desync)",
            Session{ID: "0123456789abcdef", Host: "AERO"},
            "lazyclaude:rm-01234567"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            assert.Equal(t, tc.want, tc.sess.TmuxTarget())
        })
    }
}
```

### 既存テストの確認

- `cmd/lazyclaude/routing_integration_test.go` の d (Delete) テスト (#27, #28) が passing を維持すること — delete の target 置換後も KillWindow が `lazyclaude:rm-xxxx` / `lazyclaude:lc-xxxx` を正しく受け取る
- 既存の unit test suite 全体が green

### 手動検証 (要ユーザー)

再起動後に以下を確認:
- [ ] ローカルセッションで `a` が正常 attach (regression なし)
- [ ] リモートセッションで `a` が mirror window に attach
- [ ] ローカルの `d` (delete) が動く
- [ ] リモートの `d` (delete) が動く

## Out of Scope

- Attach error の GUI propagation (別 PR)
- Bug 2: MCP が remote で動かない (設計レベルの検討必要、別途)
- Bug 3: Ctrl+V copy mode が remote で動かない (調査継続、別途)

## Files Changed

| ファイル | 変更 |
|---------|------|
| `internal/session/store.go` | `TmuxTarget()` method 追加 |
| `internal/session/store_test.go` | `TestSession_TmuxTarget` 追加 |
| `cmd/lazyclaude/local_provider.go` | AttachSession / CapturePreview / CaptureScrollback / HistorySize を `sess.TmuxTarget()` に統一 |
| `internal/session/manager.go` | Delete の target 組み立てを `sess.TmuxTarget()` に置換 |

## Verification

1. `go build ./...`
2. `go vet ./...`
3. `go test -race ./internal/... ./cmd/lazyclaude/...`
4. `/go-review` → CRITICAL/HIGH ゼロ
5. 手動検証 (上記、ユーザーが Docker で確認)

## Risk Assessment

**Low risk**:
- `TmuxTarget()` の semantics は既存の `AttachSession` が意図していた動作 (prefix 付き session target) と一致
- Delete の desync fallback (`MirrorWindowName(ID)`) は既存挙動をそのまま helper に移しただけ
- CapturePreview 等は bare form (`@42`, `rm-xxxx`) でも prefix 付きでも tmux が受け付けるので挙動互換

**Medium risk**:
- tmux の `capture-pane -t lazyclaude:@42` が動くか要確認 (理論上 valid だが actual tmux で verify)
- もし動かないなら CapturePreview 系は bare form を保つ必要あり → helper を 2 種類にする (attach 用と capture 用)

**この risk は Step 3 を実施する前に tmux コマンドで actual 確認する**:
```bash
tmux -L test new-session -d -s test
tmux -L test list-windows -t test -F '#{window_id}'  # e.g. @0
tmux -L test capture-pane -t test:@0 -p  # これが動けば OK
```
