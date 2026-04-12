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

**解決策**: remote 時は **shell expansion を remote 側で行わせる**。SSH command は `if [ -f "$HOME/.claude.json" ]; then cat "$HOME/.claude.json"; fi` のように remote shell 内で `$HOME` を展開する。caller は path を 2 種類の形式で提供:

- **User-level path**: 定数の `"$HOME/.claude.json"` (double-quoted で remote shell が `$HOME` を展開する)
- **Project-level path**: `shell.Quote(path)` の戻り値 = `'…'` (single-quoted、shell meta を無害化)

具体的には:

```go
// Remote user config path: use $HOME expansion on the remote side.
const remoteUserConfigPath = `"$HOME/.claude.json"`

// Remote project-relative paths: derive from pluginState.projectDir,
// then shell.Quote to produce '…' safe against spaces, quotes, $ and `.
mcpJSONPath      := shell.Quote(projectDir + "/.mcp.json")
settingsLocalPath := shell.Quote(projectDir + "/.claude/settings.local.json")
```

**重要**: `sh -c '...'` の外側 wrapper は使わない (nested-quoting で壊れる)。代わりに command 文字列を直接 remote shell に渡す。SSH が 1 回だけ parse するので:

```go
// Direct command (no outer sh -c wrapper)
cmd := fmt.Sprintf("if [ -f %s ]; then cat %s; fi", remotePath, remotePath)
// Result after substitution:
//   if [ -f "$HOME/.claude.json" ]; then cat "$HOME/.claude.json"; fi
//   if [ -f '/tmp/proj/.mcp.json' ]; then cat '/tmp/proj/.mcp.json'; fi
```

Sentinel 文字列 (`__MCP_FILE_NOT_FOUND__` 等) は不要。`[ -f PATH ]` の false → `fi` に飛んで exit 0、output は空文字列になる。`sshReadFile` はこの空文字列を file-missing として扱う。

### 2. `.mcp.json` も SSH 分岐でカバー (HIGH)
Refresh の SSH 分岐で `~/.claude.json`, `<projectDir>/.mcp.json`, `<projectDir>/.claude/settings.local.json` の **3 ファイル全てを読む**。

### 3. Plugin は touch しない (HIGH)
`syncPluginProject` で remote 判定時、**plugin 側は今のまま skip** (`pluginState.remoteDisabled = true` を保持、`a.plugins.SetProjectDir` は呼ばない、`a.plugins.Refresh` も呼ばない)。MCP 側のみ `SetHost(host)` + `SetProjectDir(remotePath)` + `Refresh` する。

### 4. SetHost("") reset path (HIGH)
以下のタイミングで明示的に `SetHost("")` を呼ぶ:
- **local node branch に入った時**: local に遷移したので remote host を解除
- **真の empty-tree recovery (`len(a.cachedNodes) == 0`)**: session が全滅したので remote context を解除
- **`syncPluginProjectOnce` の CWD fallback**: 起動直後 session 無しで CWD にフォールバックする時

**重要 (codex v2 指摘)**: `node == nil` で **一律** `clearRemoteDisabled` を呼んではいけない。Phase 1 の既存挙動 (`internal/gui/app_actions.go:182-235`) は:
- `node == nil && len(a.cachedNodes) == 0` → 真の empty-tree、reset する
- `node == nil && len(a.cachedNodes) > 0` → 一時的 nil (search filter で remote row 隠れ等)、**remoteDisabled を維持して write guard を効かせる**

後者を壊すと search filter 中に plugin write が local project に誤ルーティングされる bug が再発する (既存 test `plugin_remote_disabled_test.go:472-520` がカバー)。本 Phase 2 は **この既存挙動を保持した上で**、true empty-tree recovery 時にのみ `SetHost("")` を追加で呼ぶ。

### 5. SSH helper の再利用 (HIGH)
`internal/daemon.SSHExecutor` interface を `internal/mcp` から import。`mcp.NewManager` に `SSHExecutor` を注入する (DI pattern)。これにより既存の BatchMode / ConnectTimeout / ControlMaster / IPv6 対応をそのまま享受。

### 6. Error handling 強化 (MEDIUM)
`sshReadFile` は以下を区別:
- **SSH 接続失敗** (exit 255): error を返す
- **ファイル不在**: 空文字列 + nil error (ファイルが無いのは正常)
- **その他の非ゼロ** (permission denied など): error を返す

実装:
```go
// sshReadFile runs `if [ -f PATH ]; then cat PATH; fi` on the remote.
// Returns ("", nil) when the file does not exist.
// Returns ("", error) for SSH connection failures or other read errors.
//
// IMPORTANT: the command is passed directly to ssh without an outer
// `sh -c '...'` wrapper so that nested quoting does not collide with
// shell.Quote'd paths. SSH delegates execution to the remote user's
// default shell which parses the string once.
//
// The remotePath argument must be ALREADY quoted for shell consumption:
//   - `shell.Quote(projectPath + "/.mcp.json")` for static paths (produces
//     `'/path/to/proj/.mcp.json'`, safe against shell meta).
//   - The literal string `"$HOME/.claude.json"` for the user-level path
//     (double-quoted so remote shell performs $HOME expansion).
func (m *Manager) sshReadFile(ctx context.Context, remotePath string) (string, error) {
    cmd := fmt.Sprintf("if [ -f %s ]; then cat %s; fi", remotePath, remotePath)
    out, err := m.ssh.Run(ctx, m.host, cmd)
    if err != nil {
        return "", fmt.Errorf("ssh read %s: %w", remotePath, err)
    }
    return string(out), nil
}
```

**重要 (codex v4 指摘)**: 過去の plan は `sh -c 'if [ -f %s ]; …'` と外側を single-quote で wrap していたが、`%s` に `shell.Quote(path)` (= `'…'`) を substitute すると `sh -c 'if [ -f '/tmp/proj/.mcp.json' ]; …'` となり outer single-quote が path の open quote で terminated → syntax error。

**解決策**: 外側の `sh -c '...'` wrapper を廃止する。SSH は command 文字列をそのまま remote shell に渡して実行するので、wrapper は不要。コマンド自体は `if [ -f '/tmp/proj/.mcp.json' ]; then cat '/tmp/proj/.mcp.json'; fi` の形で remote shell が 1 回だけ parse する。

### 同じく sshWriteFile も wrapper なしで実装

```go
// sshWriteFile writes content to remotePath via SSH. Uses base64 encoding
// so that the content bytes do not need any shell escaping. Parent
// directory is created with mkdir -p. remotePath must be pre-quoted as
// described above.
func (m *Manager) sshWriteFile(ctx context.Context, remotePath, content string) error {
    encoded := base64.StdEncoding.EncodeToString([]byte(content))
    // Derive parent dir via dirname command on remote side, avoiding a
    // second caller-side path mangle.
    cmd := fmt.Sprintf(
        "mkdir -p \"$(dirname %s)\" && printf %%s %s | base64 -d > %s",
        remotePath,
        shell.Quote(encoded),
        remotePath,
    )
    _, err := m.ssh.Run(ctx, m.host, cmd)
    if err != nil {
        return fmt.Errorf("ssh write %s: %w", remotePath, err)
    }
    return nil
}
```

Note:
- `$(dirname …)` は remote shell 側で展開される (double-quoted)
- `base64` の encoded 文字列は ASCII only (`A-Za-z0-9+/=`) なので `shell.Quote` で single-quote wrap して安全に埋め込める
- `mkdir -p $(dirname ...)` が既存 dir 時 no-op、存在しない dir で親ごと作成する

### 7. Shell injection 対策
`remotePath` 自体は caller 側で strict に組み立てる:
- **User-level**: 定数 `"$HOME/.claude.json"` (静的、`$HOME` を remote 側で展開するため double-quote)
- **Project-level**: `pluginState.projectDir` から組み立てる。値は内部 state (node.Project.Path / node.Session path) から来るが `$`, backtick, `'`, `\n` 等の shell meta character を含む可能性がある

**既存の `internal/core/shell/quote.go:Quote` を再利用** (codex v3 指摘):
```go
// internal/core/shell/quote.go (既存)
func Quote(s string) string {
    return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
```

これは POSIX shell の single-quote 内部に含まれる `'` を `'\''` で escape する robust なパターン。`$`, backtick, 改行、`|` などの shell meta は single-quote 内部では文字通りになるので、`Quote()` に通せば reject 不要でどんな path も安全に扱える。

使用例:
```go
import "github.com/any-context/lazyclaude/internal/core/shell"

// Remote project-level paths
mcpJSONPath := shell.Quote(projectDir + "/.mcp.json")
settingsPath := shell.Quote(projectDir + "/.claude/settings.local.json")

// User-level path: use double-quote to allow $HOME expansion on remote
userConfigPath := `"$HOME/.claude.json"`
```

**注意**: `shell.Quote` は単一引用符で wrap するため `$HOME` が文字列リテラル扱いになる。user-level path は shell.Quote を **使わず** double-quote で wrap (remote shell で `$HOME` を展開させるため)。これは remote `$HOME` が "current user's home on the remote host" を正しく解決できる唯一の方法。

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
  - `<remoteProjectPath>/.mcp.json` (project-level、**optional**)
  - `<remoteProjectPath>/.claude/settings.local.json` (denied list、**optional**)
- parse 処理は local/SSH で共有 (`parseClaudeJSON` / `parseDeniedServers` ヘルパを抽出)

**重要 (codex v2 指摘)**: `sshReadFile` が返す `""` は file-not-found を意味する。`parseClaudeJSON` / `parseDeniedServers` は空文字列を受け取ると JSON parse error になるので、**caller 側で空文字列時の skip ロジックを明示する**:

```go
// Remote refresh snippet
userJSON, err := m.sshReadFile(ctx, `"$HOME/.claude.json"`)
if err != nil {
    return fmt.Errorf("read user config: %w", err)
}
var userServers map[string]ServerConfig
if userJSON != "" {
    userServers, err = parseClaudeJSON([]byte(userJSON))
    if err != nil {
        return fmt.Errorf("parse user config: %w", err)
    }
}
// userJSON == "" → userServers remains nil, treated as empty by MergeServers

// .mcp.json is optional (local code also ignores read errors at manager.go:41-63)
projectJSON, err := m.sshReadFile(ctx, remoteMCPJSONPath)
if err != nil {
    // Log and continue — project-level config is optional.
    projectJSON = ""
}
var projectServers map[string]ServerConfig
if projectJSON != "" {
    projectServers, _ = parseClaudeJSON([]byte(projectJSON))
}

// settings.local.json also optional
settingsJSON, err := m.sshReadFile(ctx, remoteSettingsLocalPath)
if err != nil {
    settingsJSON = ""
}
var denied []string
if settingsJSON != "" {
    denied, _ = parseDeniedServers([]byte(settingsJSON))
}

merged := MergeServers(userServers, projectServers, denied)
```

これは local code の既存挙動 (`.mcp.json` / `settings.local.json` は optional、read error で空扱い) と一致する。

### Step 4: `ToggleDenied(ctx, name)` に host 分岐
- `host == ""`: 従来通り
- `host != ""`: SSH で read-modify-write
  1. `<remoteProjectPath>/.claude/settings.local.json` を `sshReadFile`
  2. **既存の JSON 全体を保持したまま** top-level の `deniedMcpServers` 配列のみ書き換え
  3. `sshWriteFile` で書き戻し
  4. `Refresh` で最新状態再読込

**重要 (codex v2/v3 指摘)**: `settings.local.json` には `deniedMcpServers` 以外にも他の key (permissions、hooks、model 等) がある可能性がある。単純に新しい JSON を作って書き戻すと **remote host のユーザー設定を clobber (上書き消失)** してしまう。

### 実際のスキーマ (`internal/mcp/config.go:18-25`)
```go
type settingsLocal struct {
    DeniedMcpServers []deniedEntry `json:"deniedMcpServers,omitempty"`
}
type deniedEntry struct {
    ServerName string `json:"serverName"`
}
```

- `deniedMcpServers` は **top-level** key (`permissions.deniedMcpjsonServers` ではない、v2 plan の誤り)
- 値は string 配列ではなく `[{"serverName": "..."}]` の **object 配列**
- 既存の `WriteDeniedServers` (`config.go:70-111`) は `map[string]json.RawMessage` で load → `deniedMcpServers` key のみ update → serialize back、という preserve-unrelated-keys 方式を実装済

### Refactor 方針
既存の `WriteDeniedServers` から **pure な in-memory 変換 helper** `updateDeniedInJSON` を切り出し、local / remote 両方から使う:

```go
// updateDeniedInJSON takes the current settings.local.json bytes and
// returns bytes with the deniedMcpServers key updated to reflect denied.
// Empty input is treated as "{}" so the function works for both existing
// files and first-time writes. Unrelated keys (permissions, hooks, model,
// etc.) are preserved byte-for-byte via map[string]json.RawMessage.
// Returns a trailing newline for POSIX cleanliness.
func updateDeniedInJSON(existing []byte, denied []string) ([]byte, error) {
    existingMap := make(map[string]json.RawMessage)
    if len(existing) > 0 {
        if err := json.Unmarshal(existing, &existingMap); err != nil {
            return nil, fmt.Errorf("parse existing settings: %w", err)
        }
    }
    if len(denied) == 0 {
        delete(existingMap, "deniedMcpServers")
    } else {
        entries := make([]deniedEntry, len(denied))
        for i, name := range denied {
            entries[i] = deniedEntry{ServerName: name}
        }
        raw, err := json.Marshal(entries)
        if err != nil {
            return nil, fmt.Errorf("marshal denied: %w", err)
        }
        existingMap["deniedMcpServers"] = raw
    }
    out, err := json.MarshalIndent(existingMap, "", "  ")
    if err != nil {
        return nil, fmt.Errorf("marshal settings: %w", err)
    }
    return append(out, '\n'), nil
}
```

Local の `WriteDeniedServers` はこれを呼び出すよう refactor:
```go
func WriteDeniedServers(path string, denied []string) error {
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
    }
    existing, err := os.ReadFile(path)
    if err != nil && !errors.Is(err, os.ErrNotExist) {
        return fmt.Errorf("read %s: %w", path, err)
    }
    out, err := updateDeniedInJSON(existing, denied)
    if err != nil {
        return err
    }
    return atomicWriteFile(path, out, 0o644)
}
```

Remote branch:
```go
existingJSON, err := m.sshReadFile(ctx, remoteSettingsLocalPath)
if err != nil {
    return fmt.Errorf("read settings: %w", err)
}
updatedJSON, err := updateDeniedInJSON([]byte(existingJSON), updatedDeniedList)
if err != nil {
    return err
}
if err := m.sshWriteFile(ctx, remoteSettingsLocalPath, string(updatedJSON)); err != nil {
    return err
}
```

既存 `WriteDeniedServers` の semantics (preserve unrelated keys) は変わらない。既存 test (`config_test.go:190-274`) もそのまま通る想定。

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
            a.mcpServers.SetHost("")  // NEW: reset host on local/empty-tree transition
        }
    }

    node := a.currentNode()
    if node == nil {
        // IMPORTANT: mirror the existing Phase 1 logic. Only reset flags
        // on a TRUE empty-tree recovery; transient nil (e.g. search
        // filter hides all rows) must preserve remoteDisabled so write
        // guards still block. See plugin_remote_disabled_test.go:472-520.
        if len(a.cachedNodes) == 0 {
            clearRemoteDisabled()
            // ... existing CWD fallback path (set projectDir to CWD for
            // plugin/MCP local refresh) unchanged from Phase 1 ...
        }
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
- `SetHost("AERO")` → `Refresh(ctx)` → mock SSHExecutor が `if [ -f "$HOME/.claude.json" ]; then cat "$HOME/.claude.json"; fi` (wrapper なし、user-level は double-quoted `$HOME`) を含むコマンドを受け取ることを assert
- `Refresh` が 3 ファイル (user, `.mcp.json`, `settings.local.json`) 全てに対する SSH command を発行することを assert (captured commands の順序と内容)
- `Refresh` で `sshReadFile` が空文字列を返しても正常動作すること (ファイル不在の optional 扱い)
- `ToggleDenied(ctx, "memory")` → sshReadFile + sshWriteFile が mock で順に呼ばれ、sshWriteFile の content が既存 JSON を preserve していることを assert
- `SetHost("")` → local file IO 経路に復帰することを確認 (mock SSH は呼ばれない)

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
- APIVersion bump (wire protocol 無変更)

### 補足: Remote ファイル自動作成の方針
`~/.claude.json` や `<proj>/.claude/settings.local.json` が remote に存在しない場合の扱い:
- **Read** (`Refresh`): 空扱い (`sshReadFile` が `""` を返し、parse を skip)
- **Write** (`ToggleDenied`): 自動作成する。`sshWriteFile` は `mkdir -p $(dirname)` で parent dir を作成後、base64 decode で content を書く。これは **in scope** (toggle 操作が成立するために必要)
- 明示的に out of scope なのは: 他のツール (plugin install の artifact dir、etc.) の自動作成

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
- **Low**: Project path に shell meta character (`$`, backtick, `'`, 改行) が含まれても `internal/core/shell.Quote` で安全にエスケープされる
- **High (operational, not code)**: Remote MCP 編集が動くには remote daemon も同じバイナリが必要 (daemon-arch HEAD 以降)。ただし wire protocol 変更なしなので APIVersion bump は不要。remote bin 更新は user の手動作業

## Open Questions (解決済)

1. ~~`~/.claude.json` が remote に存在しない場合、Toggle で空配列から始めて新規作成でよいか?~~ → **解決済**: 上記「補足: Remote ファイル自動作成の方針」参照。Write 時のみ自動作成、Read 時は空扱い
2. ~~Plugin は Phase 3 で扱うが merge 前にユーザー確認必要?~~ → **解決済**: 本 plan は MCP のみスコープ、plugin は Phase 3 plan で別途。ユーザー承認済 (Bug 2 Phase 2 ≡ MCP のみ)
3. ~~`remoteProjectPath` が `'`, `\n`, `$`, `\`` を含む場合 reject / escape?~~ → **解決済**: `internal/core/shell.Quote` を再利用して escape する。reject は不要
