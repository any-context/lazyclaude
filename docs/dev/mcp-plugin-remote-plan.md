# Plan: Remote session の MCP/plugin 編集対応 (Bug 2)

## Context

リモートセッションで MCP toggle や plugin 管理の UI 操作が無反応になる。ローカルでは動く。

## Root Cause

### 1. Branch merge の抜け
git 履歴調査で判明:

- `0630493 feat: support MCP toggle for SSH remote sessions via SSH commands`
- `15fb347 fix: address security and error handling issues from code review`
- `e1e1178 fix: disable plugin management for SSH remote sessions`
- `6dc0d0d fix: disable MCP toggle for SSH remote sessions`

これら 4 commit は `fix-ssh-mcp-toggle` / `fix-ssh-plugins` branch にのみ存在し、**`daemon-arch` の ancestry に含まれていない** (`git merge-base --is-ancestor` で NOT 確認済)。現在の `daemon-arch` には:
- MCP/plugin manager が host 概念を一切持たない
- Remote session 用の disable guard もない

### 2. Silent "ローカルファイルで読み書き" の根本

`internal/gui/app_actions.go:155-187` の `syncPluginProject()` は、カーソル移動 (`MoveCursorDown/Up`) の度に以下を実行する:

```go
func (a *App) syncPluginProject() {
    ...
    node := a.currentNode()
    var projectPath string
    if node.Kind == ProjectNode && node.Project != nil {
        projectPath = node.Project.Path
    } else if node.Session != nil {
        projectPath = a.configDirForSession(node.Session)
    }
    ...
    a.plugins.SetProjectDir(projectPath)
    a.runPluginAsync(func(ctx context.Context) error {
        return a.plugins.Refresh(ctx)
    })
    if a.mcpServers != nil {
        a.mcpServers.SetProjectDir(projectPath)
        a.runMCPAsync(func(ctx context.Context) error {
            return a.mcpServers.Refresh(ctx)
        })
    }
}
```

→ Remote session に cursor が移っただけで、**remote 側の projectPath をローカルファイル (`~/.claude.json`, `<projectPath>/.claude/settings.local.json`) として読み書き** する。ユーザーから見れば「remote project の MCP 一覧が間違って出る」または「ローカル側の不要な書き込みが起きる」状態。

ボタン (MCPToggleDenied 等) だけ guard しても、cursor 移動時の自動 refresh は止まらない。

### 3. Write entry point の全列挙

以下全てが `syncPluginProject` 経由でセットされた `projectDir` を元に file IO する:
- `PluginInstall` (L703)
- `PluginUninstall` (L717)
- `PluginToggleEnabled` (L738)
- `PluginUpdate` (L752)
- `PluginRefresh` (L766)
- `MCPToggleDenied` (L821)
- `MCPRefresh` (L835)

これら全てと `syncPluginProject` 本体で remote guard が必要。

## Design Philosophy

- 透過性原則: リモートは local tmux の mirror window として表現。しかし MCP/plugin 設定は SSH 越しの file IO 領域で、tmux では扱えない例外
- host 分岐最小化: guard logic は 1 箇所の helper (`shouldSkipPluginOpsForRemote(host string) bool` 相当) にまとめ、各 entry point は helper を呼ぶだけ
- Phase 分割: 機能 disable (小、即効) → 完全 SSH 対応 (大、別 PR)

## Phase 1 (this plan): Disable + Status Message

### API 確認

**Status 表示の正しい API** (codex レビューで判明):
- `a.showStatus(...)` は**存在しない**
- 使うべきは `a.setStatus(g *gocui.Gui, msg string)` (private, `app.go:539-569`)
- 非 Update callback のコンテキストから呼ぶには `a.gui.Update(func(g *gocui.Gui) error { a.setStatus(g, "..."); return nil })` でラップする

### Step 1: host-awareness helper を追加

ファイル: `internal/gui/app_actions.go`

```go
// isRemoteNodeSelected reports whether the current cursor node points at a
// remote (SSH) session/project. Returns (host, true) when the cursor is on a
// remote node, ("", false) otherwise (local node or no node).
//
// Used by plugin/MCP operations to short-circuit remote editing which is not
// yet supported via SSH in this branch. Wraps CurrentSessionHost().
func (a *App) isRemoteNodeSelected() (string, bool) {
    host, onNode := a.currentSessionHost()
    if !onNode || host == "" {
        return "", false
    }
    return host, true
}
```

既存の `currentSessionHost()` (app_actions.go:268-286) を再利用するので新ロジックはゼロ。

### Step 2: syncPluginProject に guard

ファイル: `internal/gui/app_actions.go:155-187`

```go
func (a *App) syncPluginProject() {
    if a.plugins == nil {
        return
    }
    node := a.currentNode()
    if node == nil {
        return
    }

    // Remote sessions: SSH-based MCP/plugin editing is not supported yet.
    // Avoid calling SetProjectDir(remotePath) + Refresh() because the
    // managers would read/write local files at the remote project path,
    // producing incorrect state. Clear the panel's project context so the
    // UI does not display stale local data.
    if host, isRemote := a.isRemoteNodeSelected(); isRemote {
        _ = host
        if a.pluginState.projectDir != "" {
            a.pluginState.projectDir = ""
            a.pluginState.installedCursor = 0
            a.pluginState.marketCursor = 0
            a.plugins.SetProjectDir("")
        }
        if a.mcpServers != nil {
            a.mcpState.cursor = 0
            a.mcpServers.SetProjectDir("")
        }
        return
    }

    // ... 既存の local 処理をそのまま ...
}
```

**注**: `SetProjectDir("")` は既存 interface が空文字を安全に扱える前提。そうでなければ worker が `manager.go` の `SetProjectDir` を調整する必要あり (plan 実装時に確認)。

### Step 3: 各 write entry point に guard

対象: `PluginInstall`, `PluginUninstall`, `PluginToggleEnabled`, `PluginUpdate`, `PluginRefresh`, `MCPToggleDenied`, `MCPRefresh`

共通パターン (各関数の冒頭に追加):

```go
func (a *App) PluginToggleEnabled() {
    if host, isRemote := a.isRemoteNodeSelected(); isRemote {
        a.gui.Update(func(g *gocui.Gui) error {
            a.setStatus(g, fmt.Sprintf("Plugin editing on remote (%s) is not supported yet", host))
            return nil
        })
        return
    }
    // ... 既存処理 ...
}
```

MCP 側は "MCP" に文言を差し替える。関数数が多いため共通化を検討:

```go
// guardRemoteOp returns true when the current node is remote. Callers
// short-circuit their handler after this returns true.
func (a *App) guardRemoteOp(feature string) bool {
    host, isRemote := a.isRemoteNodeSelected()
    if !isRemote {
        return false
    }
    a.gui.Update(func(g *gocui.Gui) error {
        a.setStatus(g, fmt.Sprintf("%s on remote (%s) is not supported yet", feature, host))
        return nil
    })
    return true
}
```

使用例:

```go
func (a *App) PluginToggleEnabled() {
    if a.guardRemoteOp("Plugin editing") { return }
    // ...
}
func (a *App) MCPToggleDenied() {
    if a.guardRemoteOp("MCP editing") { return }
    // ...
}
```

### Step 4: Unit tests

ファイル: `internal/gui/app_actions_test.go` (存在しなければ新設、あれば追記)

table test で:
- `syncPluginProject`: remote node (Session.Host="AERO") 選択時に `plugins.SetProjectDir` が `""` で呼ばれ `Refresh` が呼ばれないこと
- `syncPluginProject`: local node 選択時に従来通り `SetProjectDir(path)` + `Refresh` が呼ばれること
- 各 write entry point: remote guard で short-circuit し、mock plugin/mcp manager の method が呼ばれないこと
- local node では従来通り呼ばれること
- `setStatus` がコールされた (status message 設定) ことの検証 (mock gui.Update)

既存 mock の活用を worker に委ねる。既に `mockActions` / `PluginProvider` の mock が `internal/gui/keydispatch/` 等にある模様。

### Step 5: Verification

1. `go build ./...`
2. `go vet ./...`
3. `go test -race ./internal/gui/... ./cmd/lazyclaude/...` → 新旧テスト全 PASS
4. `/go-review` → CRITICAL/HIGH ゼロ
5. `/codex --enable-review-gate` → APPROVED
6. **手動検証** (要ユーザー):
   - [ ] local session で PluginInstall/Uninstall/Toggle/Update/Refresh 動作 (regression なし)
   - [ ] local session で MCPToggleDenied/Refresh 動作 (regression なし)
   - [ ] remote session にカーソル移動 → plugin/MCP 一覧がクリアされる (stale local data を表示しない)
   - [ ] remote session で plugin 操作キー → "Plugin editing on remote (hostname) is not supported yet" 表示
   - [ ] remote session で MCP 操作キー → "MCP editing on remote (hostname) is not supported yet" 表示
   - [ ] local に戻ると正常表示が復帰

## Phase 2 (Out of Scope, separate PR)

完全な SSH 越し MCP/plugin 編集を復活する。必要な作業:

### Interface 拡張
- `internal/gui/plugin_state.go` の `PluginProvider` interface に `SetHost(host string)` 追加
- `internal/gui/mcp_state.go` の `MCPProvider` interface に `SetHost(host string)` 追加
- `cmd/lazyclaude/root.go` の `pluginAdapter` / `mcpAdapter` に `SetHost` forwarding 追加

### Manager 側の復活
- `internal/mcp/manager.go`: `host string` field + `SetHost` + `Refresh` / `ToggleDenied` の SSH 分岐
- `internal/mcp/ssh.go`: `shellQuote` + `sshReadFile` + `sshWriteFile` + `splitHostPort` (`fix-ssh-mcp-toggle` branch からの再 apply、daemon-arch で追加された RemoteHostManager / SSH config を利用)
- `internal/plugin/manager.go`: 同等の host 対応
- `internal/plugin/cli.go`: SSH 越しで plugin CLI 実行
- 関連 test

### GUI wiring
- `syncPluginProject` で remote node 時に `SetHost(host)` + `SetProjectDir(remotePath)` → `Refresh` が SSH 経由で実行される
- Phase 1 で追加した disable guard を削除 (feature available)

### 検証
- remote plugin install/uninstall/toggle が SSH 越しに動作
- MCP toggle が remote `.claude/settings.local.json` を更新
- local 動作 regression なし

Phase 2 は別 plan ファイルで扱う。Phase 1 の guard を外す作業も含む。

## Files Changed (Phase 1)

| ファイル | 変更 |
|---------|------|
| `internal/gui/app_actions.go` | `isRemoteNodeSelected()` + `guardRemoteOp()` helper 追加、`syncPluginProject` に remote guard、7 entry points に guard 呼び出し |
| `internal/gui/app_actions_test.go` | 上記 guard の unit test |

## Risk Assessment

- **Low**: guard 追加のみ、既存 local 動作には影響しない
- **Low**: `currentSessionHost()` / `setStatus` / `gui.Update` 既存 API を使うので新規バグ混入リスク小
- **Medium**: `SetProjectDir("")` の挙動が manager に依存。`"".Refresh()` が error を吐くなら `Refresh` を呼ばないように順序を調整する必要あり → worker が実装時に確認
- **Medium**: `pluginState.projectDir = ""` の reset で「再度 local node に戻った時にちゃんと再 load されるか」を確認 (既存の projectDir 変化検出 `if projectPath == a.pluginState.projectDir` ロジックに依存)

## Open Questions (要ユーザー判断)

1. **Phase 2 (full SSH 対応) をすぐやるか**: Phase 1 で silent fail を解消してから Phase 2 を別 PR にするのが推奨だが、ユーザーが最終形を一気に欲しいなら Phase 1 と Phase 2 を結合することも可能
2. **文言**: "Plugin/MCP editing on remote (<host>) is not supported yet" で良いか。"Remote plugin management is disabled in this build" のような別案もあり
3. **pluginState.projectDir reset のタイミング**: cursor が remote に移った瞬間クリアするか、それとも「最後の local state を残す」か (後者は UX 混乱の可能性)
