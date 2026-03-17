# Session State Bugs — 修正計画

**作成日**: 2026-03-17
**対象**: lazyclaude セッション管理の状態不整合バグ群

---

## 報告されたバグ

| # | 症状 |
|---|------|
| B1 | j/k を押すとセッションが消える |
| B2 | n でセッションが表示されない (バックグラウンドには存在) |
| B3 | /exit すると大量の claude が残っている |
| B4 | TUI 表示と tmux 実状態が一致しない |

---

## 確定バグ (静的解析で証明済み — ログ不要で修正可能)

### C1: Sync の orphan 判定がコピーに書く (manager.go:54-61)

```go
if !exists {
    sessions := m.store.All()      // コピーを返す
    for i := range sessions {
        sessions[i].Status = StatusOrphan  // コピーを変更 → Store に反映されない
    }
    return nil
}
```

`store.All()` は値コピーを返すため、Status 変更が Store に到達しない。
tmux セッション全体が存在しない場合、orphan 検出が完全に機能しない。

**修正**: Store に `MarkAllOrphan()` メソッドを追加し、内部の `s.sessions` を直接変更する。

### C2: GC が Create 直後のセッションを Orphan 判定 (gc.go:55-65)

Create の流れ:
1. `tmux new-session` (非同期 — tmux が返ってもウィンドウがすぐには list-windows に出ない)
2. `store.Add(sess)` — Store に追加
3. GC (2秒ごと) が `Sync` → `list-windows` → 新ウィンドウがまだ見えない → Orphan → Delete

GC と Create の間に排他制御がない。

**修正**: Manager に `sync.Mutex` を追加。Create 中は GC の collect をブロックする。

### C3: Delete が orphan セッションの tmux window を kill できない (manager.go:178-181)

```go
if sess.TmuxWindow != "" {       // Sync が Orphan 時に TmuxWindow="" に設定済み
    _ = m.tmux.KillWindow(...)   // 呼ばれない → claude プロセスが残留
}
```

SyncWithTmux が Orphan 判定時に `TmuxWindow=""` にクリアするため、
Delete 時に KillWindow がスキップされ、tmux ウィンドウと claude プロセスが残る。

**修正**: `TmuxWindow` ではなく `sess.WindowName()` (lc-xxxxxxxx) でウィンドウを kill する。

---

## 追加バグ (レビューで発見)

### C4: NewWindow に -f /dev/null がない (exec.go:169)

NewSession は `-f /dev/null` でユーザー tmux.conf を無視するが、
NewWindow にはない。2 回目以降の `n` で作成されるウィンドウは
ユーザー tmux.conf の影響を受け、ウィンドウ名が `lc-xxxxxxxx` から
上書きされる可能性がある → SyncWithTmux で名前不一致 → Orphan。

**修正**: NewWindow の tmux コマンドに `-f /dev/null` は使えない (new-window のオプションにない)。
代わりに `-f /dev/null` は new-session でサーバー起動時に一度だけ適用され、
以降のウィンドウにも効く。ただし `set-option` の `automatic-rename off` を
PostCommands で設定して名前の上書きを防ぐ。

### C5: Sessions() が毎フレーム 2 回呼ばれる TOCTOU (app.go)

j/k ハンドラと layout の `renderSessionList` がそれぞれ `Sessions()` を呼ぶ。
2 回の呼び出しの間に GC が状態を変更する可能性がある。

**修正**: layout 関数の先頭で一度だけ `Sessions()` を呼び、その snapshot を
そのフレーム内で使い回す。

---

## 修正フェーズ

### Phase 1: 確定バグ修正 (C1-C5)

依存順:
1. **C2** (GC と Create の排他) — 他の修正前に GC の暴走を止める
2. **C3** (Delete で WindowName を使う) — C2 修正後もフォールバックが必要
3. **C1** (Sync の orphan コピーバグ) — 正しい状態管理の前提
4. **C4** (automatic-rename off) — 名前不一致を防止
5. **C5** (Sessions snapshot) — レンダリングの安定性

### Phase 2: ログ基盤

確定バグ修正後、残りの微妙なバグ (pane_pid の問題等) を追跡するために導入。

- `log/slog` で Manager, GC, Store の主要操作にログ追加
- `lazyclaude --debug` フラグで有効化
- 出力先: stderr (gocui と干渉しない)

### Phase 3: Docker 検証

全修正後に Docker で E2E 検証:

```bash
# n 3回 → j/k → 状態確認
docker run --rm --env-file .env lazyclaude-test bash -c '
  tmux -f /dev/null new-session -d -s ui -x 100 -y 25 \
    "lazyclaude --debug 2>/tmp/lc.log; sleep 999"
  sleep 3
  tmux send-keys -t ui n; sleep 3
  tmux send-keys -t ui n; sleep 3
  tmux send-keys -t ui n; sleep 3
  echo "=== TUI ==="
  tmux capture-pane -p -t ui | head -8
  echo "=== tmux windows ==="
  tmux -L lazyclaude list-windows -t lazyclaude 2>&1
  echo "=== store ==="
  cat ~/.local/share/lazyclaude/state.json 2>/dev/null
  echo "=== log ==="
  cat /tmp/lc.log
  tmux kill-server 2>/dev/null
  tmux -L lazyclaude kill-server 2>/dev/null
'
```

検証項目:
- TUI のセッション数 = tmux のウィンドウ数 = store のセッション数
- j/k でセッションが消えない
- d でのみセッションが消える
- /exit 後に tmux ウィンドウが残っていない

### Phase 4: テスト追加

- `TestGC_DoesNotDeleteDuringCreate` — Create と GC の並行テスト
- `TestDelete_KillsByWindowName` — TmuxWindow="" でも kill される
- `TestSync_OrphanWritesBackToStore` — orphan 状態が Store に反映される
- `TestLayout_StableSnapshot` — 1 フレーム内で Sessions が安定

---

## 作業順序

```
Phase 1 (C2→C3→C1→C4→C5) → Phase 2 (ログ) → Phase 3 (Docker検証) → Phase 4 (テスト)
```
