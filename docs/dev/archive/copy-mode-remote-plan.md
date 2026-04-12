# Plan: Remote session の fullscreen copy mode (Ctrl+V) 診断 + 修正 (Bug 3)

## Context

fullscreen mode で Ctrl+V で起動する lazyclaude 自前 copy mode が remote session で正しく機能しない。local では動く。

## 既知の事実 (コード調査済)

### 経路
1. Ctrl+V → `ActionScrollEnter` → `App.ScrollModeEnter` (`internal/gui/app_actions.go:927`)
2. `a.scroll.Enter(viewH)` → `a.scroll.BumpGeneration()` → `a.captureScrollbackWithHistorySize()`
3. `captureScrollbackWithHistorySize` (`app_actions.go:1067-1092`):
   ```go
   target := a.fullscreen.Target()          // session ID (string)
   ...
   histSize, histErr := a.sessions.HistorySize(target)
   result, scrollErr := a.sessions.CaptureScrollback(target, viewW, startLine, endLine)
   ```
   エラーは `if histErr == nil { ... }` / `if scrollErr == nil { ... }` で分岐され **無視**
4. → `guiCompositeAdapter` → `CompositeProvider.HistorySize/CaptureScrollback`
5. → `providerForSession(id)` が local Store から session を見つけて local provider を返す (remote mirror も local Store 登録済み、設計通り)
6. → `localDaemonProvider.CaptureScrollback` (`cmd/lazyclaude/local_provider.go:117-128`):
   ```go
   target := sess.TmuxWindow
   if target == "" {
       target = "lazyclaude:" + sess.WindowName()
   }
   content, err := p.tmux.CapturePaneANSIRange(ctx, target, startLine, endLine)
   ```
7. → `tmux capture-pane -t <target> -ep -S <start> -E <end>` を実行

### codex review で確認された重要事実

1. `fullscreen.Target()` は **session ID を返す** (`internal/gui/fullscreen.go:17-55`)、window 名ではない
2. `sess.TmuxWindow` は既に valid:
   - `SyncWithTmux` が local / remote mirror 両方に tmux window ID (`@42` 等) を格納 (`internal/session/store.go:570-610`, 特に `sess.TmuxWindow = w.ID` at L585)
   - `CaptureScrollback` / `HistorySize` / `CapturePreview` は既に `sess.TmuxWindow` 優先で使う (`local_provider.go:74-139`)
   - つまり Bug 1 (attach) の原因 (`sess.WindowName()` hardcode、常に `lc-`) は capture 系には影響しない
3. 従って **Bug 1 の `Session.TmuxTarget()` helper で Bug 3 が直るとは限らない** (codex レビューで指摘、仮説破棄)

## Root Cause Hypothesis (要検証)

原因は以下 5 仮説のいずれか。**Phase 1 で実動作診断して確定する**。

### 仮説 A: Mirror window の scrollback buffer が限定的
Mirror window は local tmux が管理するローカル window で、その中で `ssh -t host tmux attach` が走る。local tmux の scrollback buffer には SSH 接続後にその pane に流れた内容のみ蓄積される。
- Remote tmux session に蓄積された本来の scrollback (たとえば 2000 行) は local tmux 側からは見えない
- `tmux capture-pane -S -1000 -t <mirror>` は SSH 接続開始以降の数十行しか返せない
- `history_size` も mirror window の local buffer しか反映しない
- ユーザーが Ctrl+V を押すと `HistorySize` が 0 または小さすぎ、`CaptureScrollback` が空 / 不完全

**確定した場合の方針**: mirror-window アーキテクチャの本質的限界。選択肢は (a) 限界を受け入れ status message で明示、(b) remote daemon / SSH 直接 capture-pane を経由する独立パスを実装 (大工事)。

### 仮説 B: `sess.TmuxWindow` の未同期
- Mirror 作成直後 `TmuxWindow = "rm-xxxx"` (name)、`SyncWithTmux` 実行後 `@42` (ID)
- 未 sync のタイミングで `capture-pane -t rm-xxxx` が grouped session 環境で target 解決失敗
- 空 content or 違う window 内容

**確定した場合の方針**: capture 系でも `lazyclaude:` prefix 強制 (Bug 1 `TmuxTarget` helper の capture 用 variant、または `CapturePaneANSIRange` 呼出し側で prefix を担保)。

### 仮説 C: session ID 不一致
- `a.fullscreen.Target()` が返す session ID と store の session ID がずれている
- `CaptureScrollback(wrong-id)` で `sess == nil` → 空が返る (`local_provider.go:119-121`)

**確定した場合の方針**: `currentSession()` / `fullscreen.SetTarget()` の同期修正。

### 仮説 D: `generation` 管理の race
- Remote の capture は goroutine 経由で遅延
- その間に `BumpGeneration` が走って結果が discard される (`captureScrollbackWithHistorySize` L1080)

**確定した場合の方針**: generation check を緩和 or BumpGeneration タイミング調整。

### 仮説 E: エラー握りつぶし
- `captureScrollbackWithHistorySize` は `histErr` / `scrollErr` を無視 (`if err == nil` 分岐のみ)
- Remote 特有のエラーが埋もれる

**確定した場合の方針**: エラーを `showError`/`setStatus` 経由で GUI に上げる (Bug 1 の attach error と同様)。

## Phase 1: 実動作診断 (this plan の中心)

### 診断ツールの確認 (codex レビューで判明)

`internal/core/debuglog/debuglog.go`:
- `debuglog.Log(format, args...)` が診断用の関数
- **環境変数 `LAZYCLAUDE_DEBUG` が non-empty の時のみ動作**
- 出力先: **`/tmp/lazyclaude-debug.log`** (flat file、dash 区切り、`/tmp/lazyclaude/` 配下ではない)

既存の `debugLog` / `debuglog.Log` を使う (slog.Default() は gocui 下で terminal rendering を壊すため禁止、`.claude/CLAUDE.md:131`)。

### Step 1: 診断 logging 追加

ファイル: `cmd/lazyclaude/local_provider.go`

各関数の入口と戻り値で以下を記録:

```go
func (p *localDaemonProvider) CaptureScrollback(id string, _, startLine, endLine int) (*daemon.ScrollbackResponse, error) {
    sess := p.mgr.Store().FindByID(id)
    if sess == nil {
        debuglog.Log("localProvider.CaptureScrollback: sess nil for id=%q", id)
        return &daemon.ScrollbackResponse{}, nil
    }
    target := sess.TmuxWindow
    if target == "" {
        target = "lazyclaude:" + sess.WindowName()
    }
    debuglog.Log("localProvider.CaptureScrollback: id=%q host=%q tmuxWindow=%q target=%q start=%d end=%d",
        sess.ID, sess.Host, sess.TmuxWindow, target, startLine, endLine)
    content, err := p.tmux.CapturePaneANSIRange(context.Background(), target, startLine, endLine)
    debuglog.Log("localProvider.CaptureScrollback: id=%q bytes=%d err=%v", sess.ID, len(content), err)
    return &daemon.ScrollbackResponse{Content: content}, err
}
```

同様に:
- `CapturePreview` (L74): target と len(content) と err
- `HistorySize` (L130): target と histSize と err

ファイル: `internal/gui/app_actions.go`

```go
func (a *App) captureScrollbackWithHistorySize() {
    target := a.fullscreen.Target()
    if target == "" {
        return
    }
    gen := a.scroll.Generation()
    startLine, endLine := a.scroll.CaptureRange()
    viewW := a.scrollViewWidth()
    debuglog.Log("captureScrollbackWithHistorySize: target=%q gen=%d startLine=%d endLine=%d viewW=%d",
        target, gen, startLine, endLine, viewW)

    go func() {
        histSize, histErr := a.sessions.HistorySize(target)
        result, scrollErr := a.sessions.CaptureScrollback(target, viewW, startLine, endLine)
        a.gui.Update(func(g *gocui.Gui) error {
            currentGen := a.scroll.Generation()
            debuglog.Log("captureScrollbackWithHistorySize.callback: target=%q gen=%d currentGen=%d histSize=%d histErr=%v scrollErr=%v bytes=%d",
                target, gen, currentGen, histSize, histErr, scrollErr, len(result.Content))
            if currentGen != gen {
                return nil
            }
            if histErr == nil && histSize > 0 {
                a.scroll.SetMaxOffset(histSize)
            }
            if scrollErr == nil {
                a.scroll.SetLines(splitLines(result.Content))
            }
            return nil
        })
    }()
}
```

`debuglog` import を追加。

### Step 2: 診断ブランチ運用

**重要**: Step 1 の logging は **merge 対象ではない**。診断専用ブランチ (`diag-copy-mode-remote`) を切り、以下の commit message 規約:
- `diag: add copy-mode remote logging (DO NOT MERGE)`

Worker は diag ブランチで実装 → install → 診断終了後 revert または branch 破棄。

### Step 3: ユーザーへの再現手順

以下を PM からユーザーに依頼:

1. Diag 版 lazyclaude を install:
   ```bash
   cd /Users/kenshin/.local/share/tmux/plugins/lazyclaude && make install PREFIX=$HOME/.local
   ```
   (branch: `diag-copy-mode-remote`)

2. Debug logging 有効化で TUI 起動:
   ```bash
   LAZYCLAUDE_DEBUG=1 lazyclaude
   ```
   または tmux プラグイン経由の場合は shell rc に `export LAZYCLAUDE_DEBUG=1` を追加してから `Ctrl+\`

3. Remote session 作成、claude code で数分間の操作を行い scrollback を貯める

4. Fullscreen + Ctrl+V を押し、**何が起きるか目視観察**:
   - [ ] 画面が真っ黒 (何も表示されない)
   - [ ] 数行だけ表示される
   - [ ] 違うセッションの内容が表示される
   - [ ] キー入力に反応しない
   - [ ] エラー表示
   - [ ] その他 (詳細を記述)

5. 平行して別 tmux pane で手動 capture-pane 実行し比較:
   ```bash
   tmux -L lazyclaude list-windows -a
   tmux -L lazyclaude capture-pane -t <rm-xxxx or @ID> -p -S -100 | head -50
   tmux -L lazyclaude show-message -t <rm-xxxx or @ID> -p "#{history_size}"
   ```

6. 収集した情報を PM に送付:
   - `/tmp/lazyclaude-debug.log` のスニペット (Ctrl+V 前後)
   - 目視観察の内容
   - 手動 `capture-pane` 実行結果

### Step 4: 診断結果の分類

| debug log で観察される内容 | 手動 tmux の結果 | 該当仮説 | 次アクション |
|---------------------------|----------------|---------|--------|
| `histSize=0` かつ `bytes` が小さい (<500) | manual capture-pane も同様に短い | **A** (mirror buffer 限定) | Phase 2 方針: mirror 限界受容 or SSH 直接 capture |
| `tmuxWindow="rm-xxxx"` (ID でない) で `scrollErr != nil` (can't find target) | manual capture-pane `-t rm-xxxx` も同じエラー | **B** (未 sync) | Phase 2: capture 側 target 正規化 |
| `sess nil for id=...` | (lazyclaude store の ID と fullscreen target の ID が一致しない) | **C** (ID 不一致) | Phase 2: `currentSession()` 同期修正 |
| `currentGen != gen` が毎回 true で `SetLines` が呼ばれない | (関係なし) | **D** (race) | Phase 2: generation 制御修正 |
| `histErr != nil` or `scrollErr != nil` で エラー文字列が判定可能 | (エラー内容で判断) | **E** (握りつぶし) | Phase 2: error propagation 追加 |
| 上記いずれも該当しない | manual が正常、lazyclaude 経由で空 | **X** (不明) | 追加調査必要 |

### Step 5: 診断後の判定と次 plan 作成

- Phase 1 の goal は **仮説確定 + Phase 2 の方向性決定**
- 仮説確定後、`docs/dev/copy-mode-fix-plan.md` (新規) に確定した root cause と具体的な fix を記述
- Phase 1 の diag コードは revert or branch 破棄

## Phase 2 (診断結果次第、別 PR / 別 plan)

Phase 1 完了後、確定した仮説に応じた fix を別 plan で扱う。Phase 2 の plan では:
- 確定した root cause
- 対応する具体的なコード修正
- 既存の挙動を壊さない契約
- 十分な unit test / 必要なら integration test
- 手動検証手順

を記述する。

## Out of Scope

- Bug 1 (remote attach) の修正
- Bug 2 (MCP/plugin remote) の修正
- copy mode UX 全般の改善
- Phase 1 の diag コードを `stg` / `daemon-arch` に merge すること (診断後 revert)

## Dependencies

- Bug 1 の merge を待つ必要はない (独立調査)
- 実機での user 検証が必須 (Docker で代替可能か未確定、SSH host への実接続が理想)

## Verification (Phase 1)

1. Diag 版 branch でビルド: `go build ./...` clean
2. `go vet ./...` clean
3. 既存 test suite regression なし: `go test -race ./internal/... ./cmd/lazyclaude/...`
4. `/go-review` → CRITICAL/HIGH ゼロ (diag コード由来の指摘のみ許容、ただし目的は diagnostic なので小さく保つ)
5. `/codex --enable-review-gate` → APPROVED (diag コードとして適切か確認)
6. ユーザー手順:
   - [ ] Diag 版を install
   - [ ] `LAZYCLAUDE_DEBUG=1 lazyclaude` で起動
   - [ ] 再現手順実行
   - [ ] `/tmp/lazyclaude-debug.log` + 目視観察 + 手動 tmux 結果を PM に送付
7. PM は Step 4 の分類表で仮説確定し、Phase 2 plan を作成

## Files Changed (Phase 1)

| ファイル | 変更 |
|---------|------|
| `cmd/lazyclaude/local_provider.go` | `CaptureScrollback` / `HistorySize` / `CapturePreview` に `debuglog.Log` 追加 (一時) |
| `internal/gui/app_actions.go` | `captureScrollbackWithHistorySize` に `debuglog.Log` 追加 (一時) |

**重要**:
- 本 Phase 1 の diag コードは **merge しない**
- Branch 名: `diag-copy-mode-remote`
- 診断完了後 revert or branch 破棄
- `debuglog` package を import する追加は最小限

## Risk Assessment

- **Very Low**: `debuglog.Log` は `LAZYCLAUDE_DEBUG=""` 時は no-op、production 実行には影響しない
- **Low**: log 内容に sensitive data (session content) が入る可能性 → ユーザーに log 送付時の注意喚起
- **Medium**: 実機での再現に依存、ユーザー環境なしでは進められない

## Open Questions (要ユーザー判断)

1. **診断に協力できる環境があるか**: 実 SSH remote host + lazyclaude daemon がインストール済、TUI での再現が可能か
2. **仮説 A (mirror 限界) が確定した場合の対応方針**: disable + status message で即解決 vs 大工事で SSH 直接 capture 経路を追加
3. **診断結果の待ち時間**: Phase 2 着手まで Phase 1 で diag 依頼 → ユーザー操作 → 結果送付 → PM 判定のラウンドトリップが発生するので、他の bug (Bug 1, Bug 2 Phase 1) を並行進行するのが現実的
