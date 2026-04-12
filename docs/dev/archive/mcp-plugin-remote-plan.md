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

これら 4 commit は `fix-ssh-mcp-toggle` / `fix-ssh-plugins` branch にのみ存在し、**`daemon-arch` の ancestry に含まれていない**。現在の `daemon-arch` には MCP/plugin manager が host 概念を一切持たず、remote 用の disable guard もない。

### 2. Silent "ローカルファイル IO" の発生経路

`syncPluginProject` (`internal/gui/app_actions.go:155-187`) は cursor 移動時に毎回:
```go
projectPath = node.Project.Path もしくは node.Session path
a.plugins.SetProjectDir(projectPath)
a.plugins.Refresh(ctx)                    // ローカルファイル read/write
a.mcpServers.SetProjectDir(projectPath)
a.mcpServers.Refresh(ctx)                 // 同上
```
→ remote の path がローカル `~/.claude.json` / `<path>/.claude/settings.local.json` として読み書きされる。

### 3. Cached list と fallback の罠

codex レビューで判明:
- `a.plugins.Installed()` / `a.mcpServers.Servers()` は **前回の Refresh 結果を保持**。render layer はこれを使うので、remote node で Refresh を止めても **stale local data が表示され続ける**
- `syncPluginProjectOnce` (`app_actions.go:125-151`) は `pluginState.projectDir == ""` の時に走り、session tree が無い / projectDir がセットされなかった場合 **process CWD に対して Refresh を実行**。`pluginState.projectDir` を `""` にリセットすると fallback が再起動し、remote 用 guard の意味がなくなる
- `plugin.ExecCLI` は `dir == ""` の場合 process CWD で claude plugins を実行する動作。`SetProjectDir("")` は無害ではない

## Design Philosophy

- 透過性原則: リモートは mirror window で表現 (local tmux 経由)。MCP/plugin の設定ファイル IO は SSH 越しが必要な例外領域
- host 分岐最小化: remote 判定は `a.currentSessionHost()` (既存) に集約、新規 host 分岐は作らない
- Phase 分割: Phase 1 (disable + placeholder、小変更) → Phase 2 (full SSH 対応、別 PR)

## Phase 1 (this plan): Disable + Placeholder

### コアアイデア

`pluginState.projectDir` や provider の `SetProjectDir` は **触らない** (残す)。代わりに新フラグ `remoteDisabled bool` を導入し:
1. `syncPluginProject` が remote node を検出したら `remoteDisabled = true`、通常の Refresh 処理は skip
2. `syncPluginProjectOnce` も remote node の場合は fallback に落ちず return
3. Render layer は `remoteDisabled` true の場合 "Remote editing disabled" placeholder を表示
4. 全 write entry point (`PluginInstall/Uninstall/Toggle/Update/Refresh` + `MCPToggleDenied/Refresh`) は `remoteDisabled` 確認 → status message 表示

**projectDir は維持** するので:
- 最後に居た local project の cached list は残るが、`remoteDisabled=true` により UI には見えない
- cursor が同じ local project に戻った場合: `projectPath == pluginState.projectDir` により通常 Refresh は skip、placeholder が消えて cached list が再度見える (挙動互換)
- cursor が別の local project に移った場合: `projectPath != pluginState.projectDir` により通常通り Refresh、`remoteDisabled = false` に戻す

### API 確認 (codex レビューで判明)

- Status 表示: `a.showStatus` は **存在しない**。`a.setStatus(g, msg)` (private, `app.go:539-569`) を `a.gui.Update(func(g) { a.setStatus(g, msg); return nil })` でラップ
- Host 判定: `a.currentSessionHost() (string, bool)` (`app_actions.go:268-286`) を再利用
- `SetProjectDir("")` は安全ではないので **remote 時は呼ばない**

### Step 1: state フラグ追加

ファイル: `internal/gui/plugin_state.go`
```go
type PluginState struct {
    tabIdx          int
    installedCursor int
    marketCursor    int
    loading         bool
    projectDir      string
    remoteDisabled  bool // true when cursor is on a remote node (SSH host != "")
}
```

ファイル: `internal/gui/mcp_state.go`
```go
type MCPState struct {
    cursor         int
    loading        bool
    remoteDisabled bool // true when cursor is on a remote node
}
```

### Step 2: syncPluginProject に remote guard

ファイル: `internal/gui/app_actions.go:155-187`

```go
func (a *App) syncPluginProject() {
    if a.plugins == nil {
        return
    }

    // clearRemoteDisabled resets both remoteDisabled flags. Used by the
    // local-node branch AND the no-node early return so the panels do not
    // stay stuck in "remote disabled" state after a remote session is
    // closed and there is no node to drive the local branch.
    clearRemoteDisabled := func() {
        a.pluginState.remoteDisabled = false
        if a.mcpServers != nil {
            a.mcpState.remoteDisabled = false
        }
    }

    node := a.currentNode()
    if node == nil {
        // No selection: treat as "back to local defaults". Without this
        // reset, closing the last remote session leaves the panels stuck
        // rendering the placeholder forever.
        clearRemoteDisabled()
        return
    }

    // Remote node: mark panels as disabled without touching provider state.
    // We intentionally do NOT clear pluginState.projectDir or call
    // SetProjectDir("") here:
    //   - Clearing projectDir re-triggers syncPluginProjectOnce fallback
    //     which runs Refresh against the process CWD.
    //   - SetProjectDir("") makes plugin.ExecCLI run in the process CWD.
    // Instead, flip remoteDisabled so the render layer shows a placeholder
    // and write entry points bail out with a status message.
    if host, isRemote := a.isRemoteNodeSelected(); isRemote {
        _ = host
        a.pluginState.remoteDisabled = true
        if a.mcpServers != nil {
            a.mcpState.remoteDisabled = true
        }
        return
    }

    // Local node: clear the remote flag and proceed with the existing refresh.
    clearRemoteDisabled()

    var projectPath string
    if node.Kind == ProjectNode && node.Project != nil {
        projectPath = node.Project.Path
    } else if node.Session != nil {
        projectPath = a.configDirForSession(node.Session)
    }
    if projectPath == "" || projectPath == a.pluginState.projectDir {
        return
    }
    a.pluginState.projectDir = projectPath
    a.pluginState.installedCursor = 0
    a.pluginState.marketCursor = 0
    a.plugins.SetProjectDir(projectPath)
    a.runPluginAsync(func(ctx context.Context) error {
        return a.plugins.Refresh(ctx)
    })
    if a.mcpServers != nil {
        a.mcpState.cursor = 0
        a.mcpServers.SetProjectDir(projectPath)
        a.runMCPAsync(func(ctx context.Context) error {
            return a.mcpServers.Refresh(ctx)
        })
    }
}
```

**重要**: `clearRemoteDisabled` は以下 3 箇所で呼ばれる:
1. `node == nil` のとき (no-selection fallback、remote session 消失時の復帰経路)
2. local node を検出したとき (通常の local 処理)
3. `syncPluginProjectOnce` の CWD fallback path (下記 Step 3)

これで「remote session を閉じた後、local session がなくても placeholder が残り続ける」bug を防ぐ。

### Step 3: syncPluginProjectOnce の fallback をガード

ファイル: `internal/gui/app_actions.go:125-151`

```go
func (a *App) syncPluginProjectOnce() {
    if a.plugins == nil || a.pluginState.projectDir != "" {
        return
    }

    node := a.currentNode()
    if node != nil {
        // If the startup node is remote, short-circuit before the CWD
        // fallback. syncPluginProject marks the panels as remote-disabled;
        // we must not run Refresh against the process CWD in that case.
        if _, isRemote := a.isRemoteNodeSelected(); isRemote {
            a.syncPluginProject()
            return
        }
        a.syncPluginProject()
        if a.pluginState.projectDir != "" {
            return
        }
    }

    // Fallback: no sessions yet — use process CWD so plugins load immediately.
    // Explicitly clear remoteDisabled: we are going to refresh against local
    // data, so the panels must leave any prior "remote disabled" state even
    // if the caller had a remote node selected before landing here.
    a.pluginState.remoteDisabled = false
    if a.mcpServers != nil {
        a.mcpState.remoteDisabled = false
    }
    a.runPluginAsync(func(ctx context.Context) error {
        return a.plugins.Refresh(ctx)
    })
    if a.mcpServers != nil {
        cwd, _ := filepath.Abs(".")
        a.mcpServers.SetProjectDir(cwd)
        a.runMCPAsync(func(ctx context.Context) error {
            return a.mcpServers.Refresh(ctx)
        })
    }
    a.pluginState.projectDir = "."
}
```

### Step 4: isRemoteNodeSelected helper + guardRemoteOp helper

ファイル: `internal/gui/app_actions.go`

```go
// isRemoteNodeSelected reports whether the cursor is on a remote (SSH) node.
// Returns (host, true) when the cursor is on a remote session/project,
// ("", false) otherwise. Wraps currentSessionHost() so callers do not need
// to interpret its (host, onNode) return shape.
func (a *App) isRemoteNodeSelected() (string, bool) {
    host, onNode := a.currentSessionHost()
    if !onNode || host == "" {
        return "", false
    }
    return host, true
}

// guardRemoteOp short-circuits a write handler when the cursor is on a
// remote node, showing a status message. Returns true if the caller should
// return early.
func (a *App) guardRemoteOp(feature string) bool {
    host, isRemote := a.isRemoteNodeSelected()
    if !isRemote {
        return false
    }
    msg := fmt.Sprintf("%s on remote (%s) is not supported yet", feature, host)
    a.gui.Update(func(g *gocui.Gui) error {
        a.setStatus(g, msg)
        return nil
    })
    return true
}
```

### Step 5: write entry points に guard

対象 (`internal/gui/app_actions.go`):
- `PluginInstall` (L703)
- `PluginUninstall` (L717)
- `PluginToggleEnabled` (L738)
- `PluginUpdate` (L752)
- `PluginRefresh` (L766)
- `MCPToggleDenied` (L821)
- `MCPRefresh` (L835)

各関数の冒頭に追加:
```go
func (a *App) PluginToggleEnabled() {
    if a.guardRemoteOp("Plugin editing") { return }
    // ... existing code ...
}

func (a *App) MCPToggleDenied() {
    if a.guardRemoteOp("MCP editing") { return }
    // ... existing code ...
}
```

文言統一: 全ての plugin 操作は `"Plugin editing"`、MCP 操作は `"MCP editing"` で揃える。

### Step 6: Render layer の placeholder 対応 (list + preview 両方)

**重要**: list renderer だけでなく **preview pane** にも guard が必要。list を隠しても preview が stale local plugin/MCP 詳細を表示し続けると bug 解決にならない。

#### 6-a: Plugin panel (plugins / marketplace tabs only)
ファイル: `internal/gui/render_plugins.go`

**重要 (codex review 反映)**: `renderPluginPanel` / `renderPluginPreview` は MCP タブも dispatch する (`switch a.pluginState.tabIdx`, `case keymap.PluginTabMCP:` で `renderMCPList` / `renderMCPPreview` を呼ぶ)。ここで `pluginState.remoteDisabled` を関数冒頭で早期 return すると、**MCP タブ時も plugin-flavor の placeholder が出てしまい、`render_mcp.go` 側の guard (6-b) が dead code になる**。

従って guard は **tab-aware** にし、plugin/marketplace タブでのみ plugin placeholder を出し、MCP タブは素通し (renderMCPList/renderMCPPreview の中で guard を効かせる):

```go
func (a *App) renderPluginPanel(v *gocui.View, maxWidth int) {
    v.Tabs = keymap.PluginTabLabels()
    v.TabIndex = a.pluginState.tabIdx
    v.SelFgColor = gocui.ColorWhite

    focused := a.panelManager.ActivePanel().Name() == "plugins"

    switch a.pluginState.tabIdx {
    case keymap.PluginTabMCP:
        // MCP tab dispatches to render_mcp.go which has its own
        // mcpState.remoteDisabled guard; do not early-return here.
        a.renderMCPList(v, maxWidth, focused)
        return
    case keymap.PluginTabPlugins:
        // fall through to plugin rendering below
    case keymap.PluginTabMarketplace:
        // Marketplace: guard remote before rendering available plugins.
        if a.pluginState.remoteDisabled {
            renderRemoteDisabledPlaceholder(v, "Plugin editing on remote hosts is not supported in this build.\n\nSwitch cursor to a local session to manage plugins.")
            return
        }
        a.renderAvailableList(v, maxWidth, focused)
        return
    }

    // Tab 1: Plugins (installed)
    if a.pluginState.remoteDisabled {
        renderRemoteDisabledPlaceholder(v, "Plugin editing on remote hosts is not supported in this build.\n\nSwitch cursor to a local session to manage plugins.")
        return
    }
    if a.pluginState.loading {
        fmt.Fprintln(v, "")
        fmt.Fprintln(v, presentation.Dim+"  Loading..."+presentation.Reset)
        return
    }
    if a.plugins == nil {
        fmt.Fprintln(v, "")
        fmt.Fprintln(v, presentation.Dim+"  No plugin provider"+presentation.Reset)
        return
    }
    a.renderInstalledList(v, maxWidth, focused)
}

func (a *App) renderPluginPreview(v *gocui.View) {
    switch a.pluginState.tabIdx {
    case keymap.PluginTabMCP:
        // MCP preview has its own mcpState.remoteDisabled guard.
        a.renderMCPPreview(v)
        return
    case keymap.PluginTabPlugins:
        if a.pluginState.remoteDisabled {
            v.Title = " Preview "
            fmt.Fprintln(v, "")
            fmt.Fprintln(v, presentation.Dim+"  Remote session — plugin editing not supported"+presentation.Reset)
            return
        }
        // ... existing plugin preview logic ...
    case keymap.PluginTabMarketplace:
        if a.pluginState.remoteDisabled {
            v.Title = " Preview "
            fmt.Fprintln(v, "")
            fmt.Fprintln(v, presentation.Dim+"  Remote session — plugin editing not supported"+presentation.Reset)
            return
        }
        // ... existing marketplace preview logic ...
    }
}
```

**鉄則**: `renderPluginPanel` / `renderPluginPreview` で `pluginState.remoteDisabled` を「関数冒頭で問答無用 return」にしないこと。必ず **tab switch の後** か **PluginTabMCP 以外のブランチ内** で guard する。そうしないと MCP タブ時の `renderMCPList` / `renderMCPPreview` が呼ばれず、6-b の guard が dead code になる。

#### 6-b: MCP panel
ファイル: `internal/gui/render_mcp.go`

対象関数:
1. `renderMCPList` — cached MCP list を描画する前に `mcpState.remoteDisabled` 確認、true なら placeholder
2. `renderMCPPreview` — 選択中 MCP server の詳細を描画する前に `mcpState.remoteDisabled` 確認、true なら placeholder

対応方針:
```go
func (a *App) renderMCPList(v *gocui.View, maxWidth int, focused bool) {
    if a.mcpState.remoteDisabled {
        renderRemoteDisabledPlaceholder(v, "MCP editing on remote hosts is not supported in this build.\n\nSwitch cursor to a local session to manage MCP servers.")
        return
    }
    // ... existing render logic ...
}

func (a *App) renderMCPPreview(v *gocui.View) {
    if a.mcpState.remoteDisabled {
        v.Title = " Preview "
        fmt.Fprintln(v, "")
        fmt.Fprintln(v, presentation.Dim+"  Remote session — editing not supported"+presentation.Reset)
        return
    }
    // ... existing preview logic ...
}
```

#### 6-c: 共通 helper

`internal/gui/render_plugins.go` か新規ファイルに共通関数を追加:

```go
// renderRemoteDisabledPlaceholder renders a two-line placeholder for the
// plugin/MCP panels when the cursor is on a remote session. The message
// is passed by the caller so plugin and MCP panels can share this helper.
func renderRemoteDisabledPlaceholder(v *gocui.View, msg string) {
    fmt.Fprintln(v, "")
    for _, line := range strings.Split(msg, "\n") {
        fmt.Fprintln(v, "  "+presentation.Dim+line+presentation.Reset)
    }
    v.SetCursor(0, 0)
}
```

この helper を list / preview の両方で使用。

**注意**: 既存の cursor/scroll state を壊さないよう、placeholder 表示時は `SetCursor(0, 0)` で初期化。`scrollToCursor` など focused 時の動きを呼び出さない。既存の "No plugins installed" 空状態と整合するトーンで。

### Step 7: Unit tests

ファイル: `internal/gui/app_actions_test.go` (既存なら追記、無ければ新設)

Mock を使って以下を検証:
1. `syncPluginProject`:
   - remote node 選択時: `plugins.SetProjectDir` / `plugins.Refresh` / `mcpServers.SetProjectDir` / `mcpServers.Refresh` が **呼ばれないこと**、`pluginState.remoteDisabled == true`、`mcpState.remoteDisabled == true`
   - local node 選択時 (regression): 従来通り SetProjectDir + Refresh が呼ばれる、`remoteDisabled == false`
   - remote → local に cursor 移動時: `remoteDisabled` が false に戻る
   - **remote → no-node (currentNode() == nil) 遷移時**: `remoteDisabled` が false に戻る (recovery 経路。remote session を全削除したシナリオ)
   - 初期状態で `currentNode() == nil` な場合も `remoteDisabled == false` が保たれる
2. `syncPluginProjectOnce`:
   - 初回 startup で remote node が選択されている場合: fallback の CWD Refresh が **呼ばれないこと**、`remoteDisabled == true`
   - Session tree 空で fallback を通る場合: 従来通り CWD Refresh が動き、`remoteDisabled == false`
   - 直前まで remoteDisabled == true だった状態で fallback に入った場合: `remoteDisabled == false` にリセット後、CWD Refresh が動く
3. Write guards:
   - `PluginInstall/Uninstall/Toggle/Update/Refresh` / `MCPToggle/Refresh`: remote node 時に mock provider の method が **呼ばれず**、`setStatus` が呼ばれる
   - local node 時: 従来通り provider method が呼ばれる
4. Render guards:
   - `pluginState.remoteDisabled == true` 時に `renderPluginPanel` / `renderPluginPreview` が placeholder を書き込むこと (buffer assert)
   - `mcpState.remoteDisabled == true` 時に `renderMCPList` / `renderMCPPreview` が placeholder を書き込むこと

#### Mock 構築ガイド (codex review 反映)

他 package の `mockActions` (`internal/gui/keydispatch/dispatcher_test.go`) / `mockFullScreenActions` (`internal/gui/keyhandler/mock_actions_test.go`) は **別 _test package に属するため import 不可**。新しい mock を `internal/gui` package 内の `_test.go` ファイルに **自分で書く** 必要がある。

既存の `PluginProvider` / `MCPProvider` interface (`internal/gui/plugin_state.go`, `internal/gui/mcp_state.go`) を満たす small stub を新設:

```go
// internal/gui/plugin_remote_disabled_test.go (新規)
package gui

import (
    "context"
    "sync"
)

type mockPluginProvider struct {
    mu              sync.Mutex
    setProjectCalls []string
    refreshCount    int
    installed       []PluginItem
    available       []AvailablePluginItem
}

func (m *mockPluginProvider) SetProjectDir(dir string) {
    m.mu.Lock(); defer m.mu.Unlock()
    m.setProjectCalls = append(m.setProjectCalls, dir)
}
func (m *mockPluginProvider) Refresh(_ context.Context) error {
    m.mu.Lock(); defer m.mu.Unlock()
    m.refreshCount++
    return nil
}
func (m *mockPluginProvider) Installed() []PluginItem             { return m.installed }
func (m *mockPluginProvider) Available() []AvailablePluginItem    { return m.available }
func (m *mockPluginProvider) Install(_ context.Context, _ string) error                { return nil }
func (m *mockPluginProvider) Uninstall(_ context.Context, _, _ string) error           { return nil }
func (m *mockPluginProvider) ToggleEnabled(_ context.Context, _, _ string) error       { return nil }
func (m *mockPluginProvider) Update(_ context.Context, _ string) error                 { return nil }

// internal/gui/mcp_remote_disabled_test.go (新規、同一 package)
type mockMCPProvider struct {
    mu              sync.Mutex
    setProjectCalls []string
    refreshCount    int
    toggleCalls     []string
    servers         []MCPItem
}

func (m *mockMCPProvider) SetProjectDir(dir string) {
    m.mu.Lock(); defer m.mu.Unlock()
    m.setProjectCalls = append(m.setProjectCalls, dir)
}
func (m *mockMCPProvider) Refresh(_ context.Context) error {
    m.mu.Lock(); defer m.mu.Unlock()
    m.refreshCount++
    return nil
}
func (m *mockMCPProvider) Servers() []MCPItem { return m.servers }
func (m *mockMCPProvider) ToggleDenied(_ context.Context, name string) error {
    m.mu.Lock(); defer m.mu.Unlock()
    m.toggleCalls = append(m.toggleCalls, name)
    return nil
}
```

両 stub は `internal/gui` package の `_test.go` 内に配置。sync.Mutex 付きなので並行 test でも安全。`MCPItem` / `PluginItem` / `AvailablePluginItem` / `PluginProvider` / `MCPProvider` interface 定義は `internal/gui/plugin_state.go` と `internal/gui/mcp_state.go` を参照。

Render test は gocui を起動せず、`*gocui.View` を `gocui.NewGui` mock で作るか、`io.Writer` interface で置き換えるシンプルな buffer test を worker が選択。既存の `internal/gui/render_test.go` の pattern を参考にする。

`a.currentSessionHost()` の挙動を制御するには、`a.cursor` + `a.sessions` (mock) + tree node を組み立てる必要がある。既存の test (`render_internal_test.go` 等) で App 構築の helper があるならそれを流用、無ければ minimal App を手で組む。

`a.gui.Update` を伴う test: `setStatus` が呼ばれたことの検証は `a.gui` を nil / mock にして Update コールバックの呼び出しを記録する pattern。既存 test pattern が不明なら worker が test を "async で呼ばれる" 側ではなく "同期で state が変わる" 側に寄せる (guardRemoteOp が true を返したことだけ assert、setStatus 呼び出し自体はスモークテスト対象外) のも許容。

#### Integration-style smoke test

既存の routing integration test (`cmd/lazyclaude/routing_integration_test.go`) のような実スタックで full flow を回すテストは不要。本件は GUI layer の振る舞いが中心なので **gui package の unit test で完結** させる。

### Step 8: Verification

1. `go build ./...` clean
2. `go vet ./...` clean
3. `go test -race ./internal/gui/... ./cmd/lazyclaude/...` 全 PASS
4. `/go-review` → CRITICAL/HIGH ゼロ
5. `/codex --enable-review-gate` → APPROVED
6. **手動検証** (要ユーザー):
   - [ ] local session で Plugin Install/Uninstall/Toggle/Update/Refresh 動作 (regression なし)
   - [ ] local session で MCP Toggle/Refresh 動作 (regression なし)
   - [ ] remote session にカーソル移動 → plugin / MCP panel が placeholder 表示
   - [ ] remote session で write key → "Plugin/MCP editing on remote (<host>) is not supported yet" 表示
   - [ ] remote → 元の local project に戻る → 通常表示復帰 (再 Refresh なし)
   - [ ] remote → 別の local project に移動 → 新しい project で Refresh が走る
   - [ ] 起動直後に remote session しか無い状態 → CWD fallback が走らず placeholder 表示
   - [ ] **remote session を全て削除 → placeholder が解除され通常表示に復帰** (recovery 経路、remoteDisabled flag が消えることを確認)

## Phase 2 (Out of Scope, separate PR)

完全な SSH 越し MCP/plugin 編集を復活する。必要な作業:

### Interface 拡張
- `internal/gui/plugin_state.go` の `PluginProvider` に `SetHost(host string)` 追加
- `internal/gui/mcp_state.go` の `MCPProvider` に `SetHost(host string)` 追加
- `cmd/lazyclaude/root.go` の `pluginAdapter` / `mcpAdapter` に `SetHost` forwarding 追加

### Manager 側の復活
- `internal/mcp/manager.go`: `host string` field + `SetHost` + `Refresh` / `ToggleDenied` の SSH 分岐
- `internal/mcp/ssh.go`: `shellQuote` + `sshReadFile` + `sshWriteFile` + `splitHostPort` (`fix-ssh-mcp-toggle` branch からの re-apply、daemon-arch の `RemoteHostManager` / SSH connection を活用)
- `internal/plugin/manager.go` + `internal/plugin/cli.go`: 同等の host 対応 (SSH 越し claude plugins 実行)
- 関連 test

### GUI wiring
- `syncPluginProject` で remote node 時に `SetHost(host)` + `SetProjectDir(remotePath)` → `Refresh` が SSH 経由で実行される
- Phase 1 で追加した `remoteDisabled` guard と placeholder を **削除** (feature available)
- Write entry point の `guardRemoteOp` 呼び出しも削除

### 検証
- remote plugin install/uninstall/toggle が SSH 越しに動作
- MCP toggle が remote `~/.claude.json` / `.claude/settings.local.json` を更新
- local 動作 regression なし

Phase 2 は別 plan ファイルで扱う。Phase 1 の guard / placeholder を外す作業も Phase 2 に含む。

## Files Changed (Phase 1)

| ファイル | 変更 |
|---------|------|
| `internal/gui/plugin_state.go` | `PluginState.remoteDisabled` フィールド追加 |
| `internal/gui/mcp_state.go` | `MCPState.remoteDisabled` フィールド追加 |
| `internal/gui/app_actions.go` | `isRemoteNodeSelected` / `guardRemoteOp` helper 追加、`syncPluginProject` / `syncPluginProjectOnce` に remote guard、7 write entry point に `guardRemoteOp` 呼び出し |
| `internal/gui/render_plugins.go` | `renderPluginPanel` / `renderPluginPreview` の plugins / marketplace タブ分岐で `pluginState.remoteDisabled` guard (MCP タブは素通し)、共通 helper `renderRemoteDisabledPlaceholder` 追加 |
| `internal/gui/render_mcp.go` | `renderMCPList` / `renderMCPPreview` の関数先頭で `mcpState.remoteDisabled` guard |
| `internal/gui/app_actions_test.go` (or 該当 test) | 上記の unit test |

## Risk Assessment

- **Low**: フラグ追加と guard 挿入のみ、既存 local 動作には影響しない
- **Low**: `pluginState.projectDir` を触らないので syncPluginProjectOnce の fallback が暴走しない
- **Medium**: render layer の placeholder 実装で plugin panel の cursor/scroll state を壊さないか要注意。worker は既存の empty-state 処理を参考にすること
- **Medium**: Phase 1 の guard は Phase 2 で削除される前提。Phase 2 作業者が忘れないよう plan に明記済

## Open Questions

1. **Phase 2 (full SSH 対応) をすぐやるか**: Phase 1 で silent fail を解消してから Phase 2 を別 PR にするのが推奨
2. **Placeholder 文言**: "Plugin editing on remote hosts is not supported in this build" で良いか。英日併記にするか
3. **MCP panel render file の場所**: `internal/gui/` 配下のどのファイルか、worker が調査して plan 実装時に特定
