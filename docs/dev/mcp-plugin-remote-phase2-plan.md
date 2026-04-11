# Plan: Bug 2 Phase 2 — Full SSH-backed MCP editing on remote

## Context

Phase 1 (merge `acd7de6`) は remote session で MCP/plugin 編集を disable してプレースホルダー表示するだけだった。ユーザーの本来の要求は **remote で実際に MCP を編集できる** こと。Phase 2 で MCP を SSH 越しに動作させる。

**本 Phase では plugin は touch しない** (Phase 1 の disable 挙動を維持)。Plugin は `claude plugins install/uninstall/update` CLI 依存が大きく、SSH 越しの CLI 実行は Phase 3 で扱う。

## Reference commits (未 merge、参考のみ)

- `0630493` feat: support MCP toggle for SSH remote sessions via SSH commands (fix-ssh-mcp-toggle branch)
- `15fb347` fix: address security and error handling issues from code review

daemon-arch とはアーキテクチャが異なるため cherry-pick 不可。実装ロジックのみ参考。

## 重要な事前調査結果

### SSH execution helper は既存
`internal/daemon/ssh.go` に `SSHExecutor` interface と `ExecSSHExecutor` 実装が既に存在:

```go
type SSHExecutor interface {
    Run(ctx context.Context, host, command string) ([]byte, error)
    Copy(ctx context.Context, host, localPath, remotePath string) error
}
```

既に `BatchMode=yes` / `ConnectTimeout=10` / `ControlMaster=no` / `ControlPath=none` / `SplitHostPort` 対応済 (IPv6 `[::1]:22` 含む)。**これを `internal/mcp` から再利用する** (daemon → mcp への import は無いので cycle なし)。

### 既存の MCP Manager
`internal/mcp/manager.go` は 140 行、`userConfig` + `projectDir` のみ保持。Refresh は `ReadClaudeJSON(userConfig)` で user-level、`ReadClaudeJSON(<projectDir>/.mcp.json)` で project-level、`ReadDeniedServers(<projectDir>/.claude/settings.local.json)` で denied を読む。`MergeServers` で合成。

### 重要: 3 つのファイルが関係する
1. **`~/.claude.json`** (user-level、remote の HOME)
2. **`<projectDir>/.mcp.json`** (project-level MCP config)
3. **`<projectDir>/.claude/settings.local.json`** (denied list)

Phase 2 は 3 つ全てに SSH 分岐を追加する必要がある。

## Critical design points (codex review で判明)

### 1. Remote path 解決 (CRITICAL)
`mcp.NewManager` は起動時に `filepath.Join(home, ".claude.json")` を **ローカルの絶対パス** で保持する。これをそのまま remote で cat しても失敗する。

**解決策**: remote 時は **shell expansion を remote 側で行わせる**。SSH command は `sh -c 'cat "$HOME/.claude.json"'` のように remote shell 内で `$HOME` を展開する。`shellQuote` で single quote にすると展開されないので、外側 single quote の中で double quote を使う pattern にする。

具体的には:

```go
// Remote user config path: use $HOME expansion on the remote side.
const remoteUserConfigPath = `"$HOME/.claude.json"`

// Remote project-relative paths: the project dir comes from pluginState.projectDir
// which (for remote) is whatever the user's cursor resolved to on the remote side.
// These paths are pre-quoted strings that include the project dir.
```

SSH 実行:
```go
// ssh host 'cat "$HOME/.claude.json" 2>/dev/null || true'
cmd := fmt.Sprintf(`sh -c 'cat %s 2>/dev/null || echo __MCP_FILE_NOT_FOUND__'`, remotePath)
```

`remotePath` は caller 側で `"$HOME/.claude.json"` または `"/path/to/proj/.mcp.json"` のような形で渡す。

### 2. `.mcp.json` も SSH 分岐でカバー (HIGH)
Refresh の SSH 分岐で `~/.claude.json`, `<projectDir>/.mcp.json`, `<projectDir>/.claude/settings.local.json` の **3 ファイル全てを読む**。

### 3. Plugin は touch しない (HIGH)
`syncPluginProject` で remote 判定時、**plugin 側は今のまま skip** (`pluginState.remoteDisabled = true` を保持、`a.plugins.SetProjectDir` は呼ばない、`a.plugins.Refresh` も呼ばない)。MCP 側のみ `SetHost(host)` + `SetProjectDir(remotePath)` + `Refresh` する。

### 4. SetHost("") reset path (HIGH)
以下 3 箇所で明示的に `SetHost("")` を呼ぶ:
- `node == nil` branch (`clearRemoteDisabled` の中)
- local node branch (`clearRemoteDisabled` 呼び出しの後)
- `syncPluginProjectOnce` の CWD fallback

### 5. SSH helper の再利用 (HIGH)
`internal/daemon.SSHExecutor` interface を `internal/mcp` から import。`mcp.NewManager` に `SSHExecutor` を注入する (DI pattern)。これにより既存の BatchMode / ConnectTimeout / ControlMaster / IPv6 対応をそのまま享受。

### 6. Error handling 強化 (MEDIUM)
`sshReadFile` は以下を区別:
- **SSH 接続失敗** (exit 255): error を返す
- **ファイル不在**: 空文字列 + nil error (ファイルが無いのは正常)
- **その他の非ゼロ** (permission denied など): error を返す

実装:
```go
// sshReadFile runs `sh -c 'if [ -f PATH ]; then cat PATH; fi'` on the remote.
// Returns ("", nil) when the file does not exist.
// Returns ("", error) for SSH connection failures or other read errors.
func (m *Manager) sshReadFile(ctx context.Context, remotePath string) (string, error) {
    // Note: remotePath is pre-quoted by the caller; do NOT shellQuote here
    // because that would prevent $HOME expansion.
    cmd := fmt.Sprintf(`sh -c 'if [ -f %s ]; then cat %s; fi'`, remotePath, remotePath)
    out, err := m.ssh.Run(ctx, m.host, cmd)
    if err != nil {
        return "", fmt.Errorf("ssh read %s: %w", remotePath, err)
    }
    return string(out), nil
}
```

`-f` test で file-not-found を正常扱い、cat 失敗 (permission など) は exec error として伝搬する。

### 7. Shell injection 対策
`remotePath` 自体は caller 側で strict に組み立てる:
- **User-level**: 定数 `"$HOME/.claude.json"` (静的)
- **Project-level**: `pluginState.projectDir` から組み立てるが、この値は **remote の file system path** で、ユーザー入力ではなく内部の state (node.Project.Path) から来る

ただし project path はユーザーが指定したディレクトリなので、**完全に trusted ではない**。対策:
- project path に `;`, `|`, `$`, 改行、引用符を含む場合は reject (conservative validation)
- または fully shell-escape して `'"'"'` エスケープで single-quote 内に出す

シンプルな妥協:
```go
// remoteProjectPath returns a shell-safe quoted remote absolute path.
// If the path contains characters that could break quoting, returns error.
func remoteProjectPath(projectDir string) (string, error) {
    if strings.ContainsAny(projectDir, "'\n") {
        return "", fmt.Errorf("project path contains unsafe characters: %q", projectDir)
    }
    return "'" + projectDir + "'", nil
}
```

`$HOME` path は static なのでそのまま `"$HOME/.claude.json"`。project path は single-quoted で展開無効化。

## 実装ステップ

### Step 1: `internal/mcp/ssh.go` 新規作成
- `Manager` が使う SSH helper を mcp パッケージ内で定義
- 実際の SSH 実行は `daemon.SSHExecutor` interface に委譲
- `sshReadFile` / `sshWriteFile` method
- `sshWriteFile` は base64 + `base64 -d > PATH` で content injection を回避

### Step 2: `mcp.Manager` struct 拡張
```go
type Manager struct {
    mu         sync.RWMutex
    servers    []MCPServer
    userConfig string   // local の ~/.claude.json (host=="" 時のみ使用)
    projectDir string
    host       string   // NEW: "" for local, hostname for SSH
    ssh        daemon.SSHExecutor  // NEW: injected SSH executor
}
```

- `NewManager(userConfig string, ssh daemon.SSHExecutor) *Manager`
- `SetHost(host string)` method

### Step 3: `Refresh(ctx)` に host 分岐
- `host == ""`: 従来通り 3 ファイルを local で読む
- `host != ""`: SSH で 3 ファイルを読む
  - `"$HOME/.claude.json"` (user-level)
  - `<remoteProjectPath>/.mcp.json` (project-level)
  - `<remoteProjectPath>/.claude/settings.local.json` (denied list)
- parse 処理は local/SSH で共有 (`parseClaudeJSON` / `parseDeniedServers` ヘルパを抽出)

### Step 4: `ToggleDenied(ctx, name)` に host 分岐
- `host == ""`: 従来通り
- `host != ""`: SSH で read-modify-write
  1. `<remoteProjectPath>/.claude/settings.local.json` を `sshReadFile`
  2. `parseDeniedServers` で現在リスト取得
  3. リスト更新
  4. `buildDeniedJSON` で JSON 生成
  5. `sshWriteFile` で書き戻し
  6. `Refresh` で最新状態再読込

### Step 5: `MCPProvider` interface 拡張
`internal/gui/mcp_state.go`:
```go
type MCPProvider interface {
    SetProjectDir(dir string)
    SetHost(host string)  // NEW
    Refresh(ctx context.Context) error
    Servers() []MCPItem
    ToggleDenied(ctx context.Context, name string) error
}
```

### Step 6: `mcpAdapter` で forward
`cmd/lazyclaude/root.go`:
```go
func (a *mcpAdapter) SetHost(host string) { a.mgr.SetHost(host) }
```

`NewManager` 呼び出し箇所で `daemon.SSHExecutor` を注入:
```go
mcpMgr := mcp.NewManager(claudeJSON, &daemon.ExecSSHExecutor{})
```

### Step 7: `syncPluginProject` 修正
ファイル: `internal/gui/app_actions.go`

現在の実装から以下を変更:

```go
func (a *App) syncPluginProject() {
    if a.plugins == nil {
        return
    }

    clearRemoteDisabled := func() {
        a.pluginState.remoteDisabled = false
        if a.mcpServers != nil {
            a.mcpState.remoteDisabled = false
            a.mcpServers.SetHost("")  // NEW: reset host on any local transition
        }
    }

    node := a.currentNode()
    if node == nil {
        clearRemoteDisabled()
        return
    }

    host, isRemote := a.isRemoteNodeSelected()

    if isRemote {
        // Plugin: Phase 3 まで disable を維持
        a.pluginState.remoteDisabled = true
        // MCP: Phase 2 で SSH-backed で動作させる
        if a.mcpServers != nil {
            a.mcpState.remoteDisabled = false  // MCP は使用可能
            a.mcpServers.SetHost(host)

            // Resolve remote project path
            var remoteProjectPath string
            if node.Kind == ProjectNode && node.Project != nil {
                remoteProjectPath = node.Project.Path
            } else if node.Session != nil {
                remoteProjectPath = a.configDirForSession(node.Session)
            }
            if remoteProjectPath != "" {
                a.mcpState.cursor = 0
                a.mcpServers.SetProjectDir(remoteProjectPath)
                a.runMCPAsync(func(ctx context.Context) error {
                    return a.mcpServers.Refresh(ctx)
                })
            }
        }
        return  // plugin 側は処理しない
    }

    // Local node: clear remote state
    clearRemoteDisabled()

    // ... existing local path: plugins.SetProjectDir + mcpServers.SetProjectDir + Refresh ...
}
```

`syncPluginProjectOnce` の CWD fallback にも:
```go
if a.mcpServers != nil {
    a.mcpServers.SetHost("")  // NEW
    a.mcpState.remoteDisabled = false
    // ... existing CWD fallback ...
}
```

### Step 8: `MCPToggleDenied` / `MCPRefresh` の guard 削除
`internal/gui/app_actions.go` の該当 entry point から `if a.guardRemoteOp("MCP editing") { return }` を削除。

Plugin 側 (`PluginInstall/Uninstall/Toggle/Update/Refresh`) は **そのまま残す**。

### Step 9: Render 修正
`internal/gui/render_mcp.go`: `renderMCPList` / `renderMCPPreview` の `mcpState.remoteDisabled` guard を削除。MCP は remote でも通常表示する。

`internal/gui/render_plugins.go`: PluginTabMCP dispatch はそのまま (renderMCPList/renderMCPPreview に委譲)、PluginTabPlugins/PluginTabMarketplace の placeholder は残す。

### Step 10: Tests

**Unit tests** (`internal/mcp/ssh_test.go` 新規):
- SSH path のための mock `SSHExecutor` を定義
- `sshReadFile`: 正常、file not found (`-f` で false)、SSH error (255)、permission error (非ゼロ)
- `sshWriteFile`: 正常書き込み、SSH error

**Manager tests** (`internal/mcp/manager_test.go` 追記):
- `SetHost("AERO")` → `Refresh(ctx)` → mock SSHExecutor が `sh -c 'if [ -f "$HOME/.claude.json" ]; ...'` を受け取ることを assert
- `Refresh` が 3 ファイル (user, .mcp.json, settings.local.json) を全て読むこと
- `ToggleDenied(ctx, "memory")` → sshReadFile + sshWriteFile を mock で捕捉
- `SetHost("")` → local path に復帰することを確認

**GUI tests** (`internal/gui/plugin_remote_disabled_test.go` 修正):
- Remote node 選択時: `mcpState.remoteDisabled == false` に変更 (Phase 2 で remote が動くため)
- Remote node 選択時: `mockMCPProvider.SetHostCalls` に `"AERO"` が入っていることを assert
- Remote → local 遷移: `SetHost("")` が呼ばれることを assert
- `node == nil` / empty tree recovery: `SetHost("")` が呼ばれることを assert
- Plugin 側の remote disabled 挙動は regression (既存 test をそのまま残す)

### Step 11: Verification
1. `go build ./...` clean
2. `go vet ./...` clean
3. `go test -race -count=1 ./internal/mcp/... ./internal/gui/... ./cmd/lazyclaude/...` 全 PASS
4. `/go-review` → CRITICAL/HIGH ゼロ
5. `/codex --enable-review-gate` → APPROVED
6. **手動検証** (要ユーザー):
   - Local session で MCP toggle (regression)
   - Remote session にカーソル → MCP server list が remote host の設定を表示
   - Remote session で MCP toggle → 実 remote `settings.local.json` が更新される
   - Remote → local 遷移 → local の MCP list が復帰
   - Remote session で plugin 操作 → 引き続き "Plugin editing on remote is not supported" (Phase 3 まで)
   - Local plugin の regression なし

## Scope (厳守)

### In scope
- MCP の SSH-backed Refresh / ToggleDenied
- `daemon.SSHExecutor` 再利用
- 3 ファイル (`~/.claude.json`, `.mcp.json`, `settings.local.json`) 全ての remote 対応
- `SetHost("")` reset path
- GUI wiring 修正
- `MCPProvider.SetHost` interface 拡張

### Out of scope
- Plugin の SSH 対応 (Phase 3 で別 plan)
- Bug 4 / Bug 3 / Bug 1 関連の変更
- `~/.claude.json` が remote に存在しない場合の自動作成 (read時は空として扱う、write時は base64 + `mkdir -p $(dirname) && base64 -d > PATH` で親 dir 含めて作成)
- APIVersion bump (wire protocol 無変更)

## Files Changed

| ファイル | 変更 |
|---------|------|
| `internal/mcp/ssh.go` (新規) | SSH read/write helper、`daemon.SSHExecutor` に委譲 |
| `internal/mcp/manager.go` | `host` field、`ssh` field、`SetHost` method、Refresh/ToggleDenied の SSH 分岐、parse helper 抽出 |
| `internal/mcp/manager_test.go` | host 設定時の test 追加 |
| `internal/mcp/ssh_test.go` (新規) | mock SSHExecutor で ssh helper test |
| `internal/gui/mcp_state.go` | `MCPProvider.SetHost(host)` interface 拡張 |
| `cmd/lazyclaude/root.go` | `mcpAdapter.SetHost` 実装、`mcp.NewManager` に `&daemon.ExecSSHExecutor{}` を注入 |
| `internal/gui/app_actions.go` | `syncPluginProject` / `syncPluginProjectOnce` で MCP 側の SetHost 経路追加、`MCPToggleDenied` / `MCPRefresh` の guard 削除、`clearRemoteDisabled` で `SetHost("")` 呼び出し |
| `internal/gui/render_mcp.go` | `mcpState.remoteDisabled` guard 削除 |
| `internal/gui/plugin_remote_disabled_test.go` | MCP 関連 test を update、plugin 側は regression 維持 |

## Risk Assessment

- **Low**: `daemon.SSHExecutor` は既存で、BatchMode/port/IPv6 対応済なのでそこは安全
- **Low**: Plugin 側は touch しないので plugin regression は起きない
- **Medium**: SSH 実行は unit test しづらい。`SSHExecutor` interface 注入で mock 可能にして緩和
- **Medium**: Remote `$HOME/.claude.json` の format/存在は環境依存。conservative に "不在 → 空" で扱う
- **Medium**: Project path に `$`, `\``, `'` などが入るとエスケープ処理が必要。`remoteProjectPath` で fail-fast validation + single-quote wrapping
- **High (operational, not code)**: Remote MCP 編集が動くには remote daemon も同じバイナリが必要 (daemon-arch HEAD 以降)。ただし wire protocol 変更なしなので APIVersion bump は不要。remote bin 更新は user の手動作業

## Open Questions

1. `~/.claude.json` が remote に存在しない場合、Toggle で空配列から始めて新規作成でよいか? → **conservative に「作成する」方針で進める** (parent dir は `mkdir -p` で作成)
2. Plugin は Phase 3 で扱うが、merge 前に ユーザー確認必要? → **本 plan は MCP のみスコープ、plugin は Phase 3 plan で別途**
3. `remoteProjectPath` が `'`, `\n`, `$`, `\`` を含む場合 **reject** するか **escape** するか → worker は reject (conservative) で進め、必要になったら escape に拡張
