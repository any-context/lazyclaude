# Plan: Remote session の fullscreen copy mode (Ctrl+V) 診断 + 修正 (Bug 3)

## Context

fullscreen mode で Ctrl+V で起動する lazyclaude 自前 copy mode が remote session で正しく機能しない。local では動く。

## 既知の事実 (コード調査済)

### 経路
1. Ctrl+V → `ActionScrollEnter` → `App.ScrollModeEnter` (`internal/gui/app_actions.go:927`)
2. `a.scroll.Enter(viewH)` → `a.scroll.BumpGeneration()` → `a.captureScrollbackWithHistorySize()`
3. `captureScrollbackWithHistorySize` (`app_actions.go:1067`):
   ```go
   target := a.fullscreen.Target()          // session ID (string)
   ...
   histSize, _ := a.sessions.HistorySize(target)
   result, _  := a.sessions.CaptureScrollback(target, viewW, startLine, endLine)
   ```
4. → `guiCompositeAdapter` → `CompositeProvider.HistorySize/CaptureScrollback`
5. → `providerForSession(id)` は local Store にあれば local provider を返す (remote mirror も local Store 登録されている)
6. → `localDaemonProvider.CaptureScrollback` (`cmd/lazyclaude/local_provider.go:117`):
   ```go
   target := sess.TmuxWindow
   if target == "" {
       target = "lazyclaude:" + sess.WindowName()
   }
   content, err := p.tmux.CapturePaneANSIRange(ctx, target, startLine, endLine)
   ```
7. → `tmux capture-pane -t <target> -ep -S <start> -E <end>`

### codex review で確認された重要事実

1. **`fullscreen.Target()` は session ID を返す** (`internal/gui/fullscreen.go:17-55`)、window 名ではない
2. **`sess.TmuxWindow` は既に valid** ——
   - `SyncWithTmux` が local/remote mirror 両方に tmux window ID (`@42` 等) を格納 (`internal/session/store.go:570-610`, 特に L585 `sess.TmuxWindow = w.ID`)
   - `CaptureScrollback`/`HistorySize`/`CapturePreview` は既に `sess.TmuxWindow` を優先して使う (`cmd/lazyclaude/local_provider.go:74-139`)
   - つまり **Bug 1 (attach) の原因 (`sess.WindowName()` hardcode) は capture 系には影響しない**
3. 従って **Bug 1 の `Session.TmuxTarget()` helper で Bug 3 が直る保証はない**。codex レビュー明示指摘

## Root Cause Hypothesis (要検証)

仮説は複数あり、どれが正しいか **Phase 1 で実動作の診断が必要**:

### 仮説 A: Mirror window の scrollback buffer が空 (または極めて小さい)
Mirror window は local tmux が管理するローカル window で、その中で `ssh -t host tmux attach` が走る。local tmux の scrollback buffer には **SSH 接続後にその pane に流れた内容のみ** が残る。つまり:
- Remote tmux session に蓄積された本来の scrollback (たとえば 2000 行) は **local tmux 側からは見えない**
- `tmux capture-pane -S -1000 -t <mirror>` が返すのは、せいぜい SSH 接続開始以降の数十行
- `history_size` も同様に mirror window の local buffer しか反映しない
- ユーザーが Ctrl+V を押したとき、`HistorySize` が 0 または小さすぎ、`CaptureScrollback` の戻り値が空または不完全

**もしこれが root cause なら**: mirror window ベースのアーキテクチャの限界であり、**fullscreen copy mode を remote で完全に動かすには SSH 越しの capture-pane 経路 (remote daemon / remote tmux に直接問い合わせる path) が必要**。実装工数大。

### 仮説 B: `sess.TmuxWindow` が未 sync 状態
- Mirror 作成直後は `TmuxWindow = "rm-xxxx"` (name)、`SyncWithTmux` 実行後に `@42` (ID) に更新
- 未 sync のタイミングで `capture-pane -t rm-xxxx` が実行されると、grouped session 環境で target 解決が曖昧
- → 空の content が返る、または違う window の内容が返る

**もしこれが root cause なら**: capture 系でも `lazyclaude:` prefix を強制する (Bug 1 と同様の helper 化) ことで解決する可能性

### 仮説 C: `fullscreen.Target()` / session ID 不一致
- Remote mirror の場合、GUI が参照する session ID と store の session ID がずれている可能性
- → `CaptureScrollback(wrong-id)` で `sess == nil` となり空が返る (`local_provider.go:119-121`)

### 仮説 D: スクロール state の `generation` 管理が remote で race
- Remote の capture は goroutine 経由で遅延があり、その間に `BumpGeneration` が走って結果が捨てられる
- 結果表示ゼロ

### 仮説 E: 非同期 goroutine のエラー握りつぶし
- `captureScrollbackWithHistorySize` では `histErr` / `scrollErr` を無視して `if histErr == nil { ... }` / `if scrollErr == nil { ... }` の分岐のみ。エラー時は silent fail
- Remote 特有のエラーがここで埋もれている可能性

## Phase 1: 実動作診断 (this plan の中心)

### Step 1: debug logging 追加 (一時的、merge しない)

ファイル: `cmd/lazyclaude/local_provider.go`
- `CaptureScrollback` / `HistorySize` / `CapturePreview` で:
  - session の ID, Host, TmuxWindow, 計算された target を log 出力
  - 戻り値 (content の長さ、err の内容) を log 出力

ファイル: `internal/gui/app_actions.go`
- `captureScrollbackWithHistorySize` で:
  - target (session ID), viewW, startLine, endLine を log
  - histSize, histErr, scrollErr, content 長 を log
  - generation 比較結果を log

ログ出力先: `debugLog(...)` 既存関数 (cmd/lazyclaude/debug.go) を流用。user は `~/.local/share/lazyclaude/debug.log` (または既存 path) で確認できる。

**注**: `slog.Default()` は gocui TUI で terminal rendering を壊すため使用禁止 (`.claude/CLAUDE.md:131`)。既存の `debugLog` 経由で書く。

### Step 2: ユーザーに再現手順を依頼

手順:
1. debug log 有効化版 lazyclaude を install
2. TUI 起動、remote session 作成、しばらく claude code で操作して scrollback を貯める
3. fullscreen + Ctrl+V を押す
4. 何が起きたか目視観察 (空画面 / 一部表示 / 間違った内容 / フリーズ)
5. `~/.local/share/lazyclaude/debug.log` (あるいは `/tmp/lazyclaude/*.log`) を収集
6. 平行して手動で `tmux -L lazyclaude list-windows` と `tmux -L lazyclaude capture-pane -t rm-<short-id> -p -S -100` を実行し、結果を収集
7. debug log + 目視観察 + tmux 手動実行結果を PM に報告

### Step 3: 診断結果の分類と判定

| 観察された症状 | 該当仮説 | 判定 |
|---------------|---------|------|
| histSize = 0 かつ CaptureScrollback content が数行だけ | A (mirror buffer が限定的) | 仮説 A 確定 |
| target が `rm-xxxx` のまま、手動 `capture-pane -t rm-xxxx` が曖昧エラー | B (未 sync) | 仮説 B 確定 |
| target の session ID が store に存在しない (`sess == nil` log) | C (ID 不一致) | 仮説 C 確定 |
| generation 不一致で結果が毎回捨てられる | D (race) | 仮説 D 確定 |
| histErr / scrollErr が非 nil (tmux エラー) | E (error 握りつぶし) | 仮説 E 確定 |
| 手動 tmux capture-pane は正常、lazyclaude 経由で空 | 追加調査 (GUI layer の処理) | 不明、追加調査 |

### Step 4: Phase 2 への分岐

診断結果に応じて、それぞれ別 plan / 別 PR を作成:

| 仮説 | Phase 2 の方針 | 実装工数 |
|------|---------------|---------|
| A | (a) mirror-window limit を受け入れ status message 表示、(b) remote daemon / SSH 経由で直接 scrollback 取得する経路を追加 | 大 |
| B | capture 系でも `lazyclaude:` prefix を強制する helper 化 (Bug 1 の `TmuxTarget()` に capture-pane 用 variant を足すか別 helper) | 小 |
| C | `currentSession()` / `fullscreen.Target()` の ID 同期を修正 | 中 |
| D | `captureScrollbackWithHistorySize` の generation check を緩和、または BumpGeneration タイミング調整 | 中 |
| E | エラーを `showError`/`setStatus` 経由で GUI に上げる (attach error と同じ問題) | 小 |

## Phase 2 (後続、診断結果次第)

Phase 1 完了後、仮説が確定したら本 plan を終了し、適切な fix plan を別 md で作成する。

## Out of Scope

- Bug 1 (remote attach) の修正
- Bug 2 (MCP/plugin remote) の修正
- copy mode UX 全般の改善

## Dependencies

- Bug 1 の merge を待つ必要はない (独立調査)
- 実機での user 検証が必要

## Verification (Phase 1)

1. debug logging 追加後 `go build ./...` / `go vet ./...` clean
2. 既存 test suite に regression なし (`go test -race ./internal/... ./cmd/lazyclaude/...`)
3. `/go-review` → CRITICAL/HIGH ゼロ
4. `/codex --enable-review-gate` → APPROVED
5. ユーザーが:
   - [ ] debug 版バイナリを install
   - [ ] 再現手順実行
   - [ ] debug log + 目視観察 + 手動 tmux 実行結果を PM に送付
6. PM は Step 3 の分類表で仮説を確定し、次 plan を作成

## Files Changed (Phase 1)

| ファイル | 変更 |
|---------|------|
| `cmd/lazyclaude/local_provider.go` | `CaptureScrollback` / `HistorySize` / `CapturePreview` に一時 debug log |
| `internal/gui/app_actions.go` | `captureScrollbackWithHistorySize` に一時 debug log |

**重要**: 本 Phase 1 の debug log は診断完了後に **必ず revert** する。merge 用ではなく診断専用。PR のタイトルに `[DIAGNOSTIC, do not merge]` を付けて明示、または worktree 上で commit せずに一時検証する運用も可。

## Risk Assessment

- **Very Low**: debug log のみ、production code の振る舞いは変わらない
- **Medium**: ユーザーの手動検証に依存するため、情報が不完全なら診断結論が出ず Phase 2 着手できない

## Open Questions

1. **debug log を merge するか、それとも worktree 上で install → verify して捨てるか**: 後者のほうが clean だが再現性が低い。前者は revert 作業が増える
2. **ユーザーが Docker 環境で再現手順を実行できるか**: 実 remote SSH host 前提だが、mock SSH 環境で代替可能か
3. **仮説 A が確定した場合の方針**: mirror limit を受け入れる (disable + status) か、本格的に SSH 越し capture を実装するか。後者は大工事
