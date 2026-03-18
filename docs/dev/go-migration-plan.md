# lazyclaude — Standalone Claude Code TUI

**作成日**: 2026-03-17
**改訂日**: 2026-03-17 (v3: standalone + tmux extension 2 層分離)
**参照**: `docs/research/gocui-lazygit-architecture.md`

---

## コンセプト

lazygit が git の standalone TUI であるように、**lazyclaude** は Claude Code の standalone TUI。
tmux-claude プラグインの「移行」ではなく、新規リポジトリとしてゼロから設計する。

### 2 層アーキテクチャ

lazyclaude は **standalone コア** と **tmux 拡張 (lazyclaude.tmux)** の 2 層で構成する。

| 層 | 何ができるか | tmux 必要? |
|----|------------|-----------|
| **lazyclaude (standalone)** | セッション管理 TUI、diff/tool ビューア、MCP サーバー | tmux 内で実行する必要があるが、keybind 登録や自動 popup は行わない |
| **lazyclaude.tmux (拡張)** | keybind 登録、作業中ペインへの自動 popup 割り込み、MCP サーバー自動起動 | TPM (tmux plugin manager) で導入 |

### なぜ 2 層に分けるか

lazyclaude 単体は「自分のターミナル内」でしか動作しない。
作業中の別ペインに diff/tool popup を**割り込み表示**する機能は、
tmux の `display-popup` API に依存しており、1 CUI アプリの管理範囲を超えている。

この「管理範囲の越境」を tmux 拡張に委譲することで:
1. lazyclaude 単体はクリーンな standalone TUI として完結する
2. 割り込み popup が欲しいユーザーだけが拡張を導入する
3. 責務が明確に分離される

### popup の 2 つのコンテキスト

```
1. lazyclaude TUI 内 popup (standalone で完結)
   - lazyclaude 起動中にヘルプ/確認ダイアログを gocui で表示
   - gocui の ContextMgr スタックで管理
   - 通常の TUI アプリの範疇

2. 作業中ペインへの割り込み popup (tmux 拡張で実現)
   - Claude Code が diff 確認や tool 許可を求めるとき
   - ユーザーが作業中の任意の tmux ペイン上に popup が出現

   Claude Code → MCP WebSocket → lazyclaude server
                                       ↓
                                 tmux display-popup -E "lazyclaude diff ..."
                                       ↓
                                 tmux が popup の PTY を提供
                                       ↓
                                 lazyclaude diff (別プロセス) が gocui で描画
```

### 責務分担表

| 責務 | 担当 |
|------|------|
| popup を**いつ**出すか | lazyclaude server (MCP メッセージに応じて) |
| popup を**どこに**出すか | tmux (アクティブクライアントの上に display-popup) |
| popup の**中身**を描画 | lazyclaude diff / lazyclaude tool (独立プロセス) |
| popup の**表示・非表示**制御 | tmux (Esc で閉じる、サイズ管理等) |
| keybind (prefix+a 等) 登録 | lazyclaude.tmux (tmux 拡張) |
| MCP サーバー自動起動 | lazyclaude.tmux (tmux hook) |

### lazygit との対比

| | lazygit | lazyclaude |
|---|---------|------|
| 対象 | git | Claude Code |
| 起動 | `lazygit` | `lazyclaude` |
| 依存 | git CLI | claude CLI, tmux |
| UI | gocui (full TUI) | gocui (full TUI) |
| 配布 | シングルバイナリ | シングルバイナリ + lazyclaude.tmux (optional) |
| リポジトリ | jesseduffield/lazygit | KEMSHlM/lazyclaude |

### 旧 tmux-claude との関係

tmux-claude は tmux プラグイン (shell + node スクリプトの集合体)。
lazyclaude は独立したバイナリで、tmux-claude の全機能を吸収しつつ、
より洗練された TUI 体験を提供する。

tmux-claude は lazyclaude の完成後に archived にする。

---

## 目次

1. [アーキテクチャ概要](#1-アーキテクチャ概要)
2. [技術スタック](#2-技術スタック)
3. [パッケージ設計](#3-パッケージ設計)
4. [Phase 0: プロジェクト初期化 + PoC](#4-phase-0-プロジェクト初期化--poc)
5. [Phase 1: コア層 (tmux + process)](#5-phase-1-コア層)
6. [Phase 2: MCP サーバー](#6-phase-2-mcp-サーバー)
7. [Phase 3: TUI フレームワーク](#7-phase-3-tui-フレームワーク)
8. [Phase 4: メイン画面](#8-phase-4-メイン画面)
9. [Phase 5: Diff / Tool Popup](#9-phase-5-diff--tool-popup)
10. [Phase 6: SSH + 高度な機能](#10-phase-6-ssh--高度な機能)
11. [Phase 7: lazyclaude.tmux 拡張](#11-phase-7-lazyclaudetmux-拡張)
12. [Phase 8: 配布・CI/CD](#12-phase-8-配布cicd)
13. [リスクと対策](#13-リスクと対策)
14. [テスト戦略](#14-テスト戦略)
15. [並行作業マップ](#15-並行作業マップ)

---

## 1. アーキテクチャ概要

### 起動モデル

**standalone (lazyclaude 単体)**:

```
$ lazyclaude                    # メイン TUI (セッション一覧 + プレビュー)
$ lazyclaude server             # MCP サーバーデーモン
$ lazyclaude diff <args>        # diff ビューア (任意のターミナルで実行可能)
$ lazyclaude tool <args>        # tool 確認ビューア (任意のターミナルで実行可能)
```

**tmux 拡張 (lazyclaude.tmux) を導入した場合に追加される機能**:

```
prefix + a                      # lazyclaude TUI を popup で開く
prefix + A                      # --resume で Claude を attach
(自動)                          # Claude Code の diff/tool 要求時に作業中ペインに popup 表示
(自動)                          # MCP サーバーが tmux session 開始時に自動起動
```

### 全体構成図

```
┌────────────────────────────────────────────────────────────────────────┐
│  lazyclaude (standalone binary)                                        │
│                                                                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────────────┐  │
│  │  Main TUI    │  │  MCP Server  │  │  Popup Viewers               │  │
│  │  (gocui)     │  │  (WS + HTTP) │  │  lazyclaude diff             │  │
│  │              │  │              │  │  lazyclaude tool             │  │
│  │  session mgr │  │  Claude Code │  │  (standalone gocui プロセス) │  │
│  └──────┬───────┘  └──────┬───────┘  └─────────────┬────────────────┘  │
│         │                 │                        │                   │
│  ┌──────┴─────────────────┴────────────────────────┴────────────────┐  │
│  │                      Core Layer                                  │  │
│  │  tmux client  |  process  |  session store  |  config            │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└─────────────┬──────────────────────┬───────────────────────────────────┘
              │                      │
        ┌─────┴─────┐         ┌─────┴──────┐
        │   tmux    │         │ Claude Code│
        └───────────┘         └────────────┘

┌────────────────────────────────────────────────────────────────────────┐
│  lazyclaude.tmux (optional tmux extension)                             │
│                                                                        │
│  - keybind 登録 (prefix+a → lazyclaude popup)                         │
│  - MCP サーバー自動起動 (tmux session-created hook)                    │
│  - 作業中ペインへの割り込み popup (tmux display-popup 経由)            │
│  - claude hooks 設定 (Notification → POST /notify)                     │
└────────────────────────────────────────────────────────────────────────┘
```

### lazyclaude 単体 vs tmux 拡張 — 機能比較

| 機能 | lazyclaude 単体 | + lazyclaude.tmux |
|------|:--------------:|:-----------------:|
| セッション一覧 TUI | o | o |
| セッション作成・削除・attach | o | o |
| ライブプレビュー | o | o |
| `lazyclaude diff` (手動実行) | o | o |
| `lazyclaude tool` (手動実行) | o | o |
| MCP サーバー (手動起動) | o | o |
| prefix+a でTUI を開く | x | o |
| 作業中ペインに diff/tool 自動 popup | x | o |
| MCP サーバー自動起動 | x | o |
| Claude hooks 自動設定 | x | o |

### lazygit パターンの適用

| lazygit の概念 | lazyclaude での適用 |
|---------------|-------------|
| `Gui` struct | `App` struct — gocui.Gui + 全状態のルート |
| `View` | gocui.View — Sessions, Preview, Actions, Status 等 |
| `Context` | SessionListContext, DiffContext, ToolContext 等 |
| `ContextMgr` (stack) | メイン画面 → popup のスタック遷移 |
| `Manager.Layout()` | ターミナルサイズに応じた動的 View 配置 |
| `Binding` + options bar | アクションバー (context ごとに切り替わる) |
| `ListRenderer` + `ListCursor` | セッション一覧 + カーソル移動 |
| `controllers/` | SessionController, DiffController 等 |

---

## 2. 技術スタック

### Go 依存ライブラリ

| ライブラリ | 用途 |
|-----------|------|
| `jesseduffield/gocui` | TUI フレームワーク (lazygit 同一 fork) |
| `nhooyr.io/websocket` | MCP WebSocket サーバー |
| `alecthomas/chroma/v2` | シンタックスハイライト |
| `spf13/cobra` | CLI サブコマンド |
| stdlib | HTTP, JSON, exec, crypto, net |

### 排除される外部依存

Node.js, fzf, bat, bash/zsh スクリプト → **Go シングルバイナリ 1 つ**

---

## 3. パッケージ設計

```
lazyclaude/
├── cmd/lazyclaude/
│   ├── main.go                 エントリポイント
│   ├── root.go                 cobra root (引数なし → メイン TUI)
│   ├── server.go               `lazyclaude server` サブコマンド
│   ├── diff.go                 `lazyclaude diff` サブコマンド
│   ├── tool.go                 `lazyclaude tool` サブコマンド
│   └── setup.go                `lazyclaude setup` (tmux keybind + hook 登録)
│
├── internal/
│   ├── core/                   基盤層 (外部依存なし)
│   │   ├── tmux/
│   │   │   ├── client.go       interface TmuxClient
│   │   │   ├── types.go        ClientInfo, WindowInfo, PaneInfo
│   │   │   ├── exec.go         exec.CommandContext 実装
│   │   │   ├── pidwalk.go      PID → window 解決
│   │   │   └── mock.go         テスト用 mock
│   │   ├── process/
│   │   │   └── tree.go         プロセスツリー走査 (ps → sysctl/proc)
│   │   └── config/
│   │       └── config.go       設定 (ポート, トークン, パス)
│   │
│   ├── server/                 MCP サーバー
│   │   ├── server.go           net.Listener, 接続管理, HTTP /notify
│   │   ├── ws.go               WebSocket 接続ハンドラ
│   │   ├── jsonrpc.go          JSON-RPC 2.0 エンコード/デコード
│   │   ├── handler.go          MCP メッセージハンドラ
│   │   ├── state.go            接続状態, pendingToolInfo
│   │   ├── popup.go            popup 起動判断 + サイズ計算
│   │   └── lock.go             IDE lock ファイル + SSH リモート更新
│   │
│   ├── gui/                    TUI 層 (lazygit 構成に準拠)
│   │   ├── app.go              App struct (= lazygit の Gui)
│   │   ├── views.go            Views struct + createAllViews()
│   │   ├── layout.go           Manager.Layout() 実装
│   │   ├── keybindings.go      resetKeybindings(), Binding 型
│   │   ├── options.go          アクションバー (renderContextOptionsMap)
│   │   ├── theme.go            色定義, スタイル
│   │   │
│   │   ├── context/            Context 実装
│   │   │   ├── base.go         BaseContext (共通ロジック)
│   │   │   ├── manager.go      ContextMgr (スタック)
│   │   │   ├── sessions.go     SessionListContext (メイン画面左)
│   │   │   ├── preview.go      PreviewContext (メイン画面右)
│   │   │   ├── diff.go         DiffContext (diff popup)
│   │   │   └── tool.go         ToolContext (tool popup)
│   │   │
│   │   ├── controllers/        Controller (keybinding → action)
│   │   │   ├── session.go      n: new, d: delete, enter: attach
│   │   │   ├── diff.go         y: accept, a: allow-all, n: reject
│   │   │   ├── tool.go         y/a/n 選択
│   │   │   └── navigation.go   j/k/gg/G スクロール
│   │   │
│   │   └── presentation/       表示文字列生成
│   │       ├── sessions.go     セッション行のフォーマット
│   │       └── diff.go         diff hunk のカラーリング
│   │
│   ├── session/                セッション管理ロジック
│   │   ├── store.go            セッション永続化 (JSON state file)
│   │   ├── manager.go          セッション CRUD (store + tmux 連携)
│   │   ├── launch.go           Claude Code 起動 (local + SSH)
│   │   ├── ssh.go              SSH reverse tunnel 構築
│   │   └── attach.go           popup attach ループ + 監視
│   │
│   └── integration/            GUI 統合テストフレームワーク
│       ├── components/
│       │   ├── test_driver.go  テスト実行オーケストレーター
│       │   ├── view_driver.go  View 操作 (Focus/Press/Lines チェーン API)
│       │   ├── popup_driver.go Popup 操作 (diff/tool popup 用)
│       │   ├── matcher.go      汎用 Matcher (Contains, Equals, MatchesRegexp)
│       │   ├── text_matcher.go 文字列アサーション
│       │   └── shell.go        テスト用セッション/tmux セットアップ
│       ├── tests/
│       │   ├── session/        セッション操作テスト群
│       │   ├── diff/           diff popup テスト群
│       │   ├── tool/           tool popup テスト群
│       │   └── test_list.go    テストレジストリ (自動生成)
│       └── clients/
│           ├── go_test.go      go test 統合 (CI 用)
│           ├── cli.go          CLI テストランナー
│           └── tui.go          インタラクティブ TUI テストランナー
│
├── go.mod
├── go.sum
├── Makefile
├── .golangci.yml
├── .goreleaser.yml
└── lazyclaude.tmux             TPM エントリポイント (shell 10行以内, Go に委譲)
```

### パッケージ依存関係

```
cmd/lazyclaude
  ├── internal/gui          (メイン TUI)
  │     ├── internal/core   (tmux, process, config)
  │     └── internal/session (セッション管理)
  │
  ├── internal/server       (MCP サーバー)
  │     └── internal/core   (tmux, process, config)
  │
  └── internal/gui          (diff/tool popup — 同じ gui パッケージ)
        └── (gocui のみ)

循環依存なし:
  server → core (tmux 操作)
  server → gui  なし (popup は自バイナリの別プロセスとして起動)
  gui    → core (tmux 操作)
  gui    → server なし
```

---

## 4. Phase 0: プロジェクト初期化 + PoC

**目標**: 新規リポジトリ作成、ビルド基盤、リスク項目の PoC 検証

### タスク

| # | タスク | 成果物 |
|---|--------|--------|
| P0.1 | GitHub リポジトリ `KEMSHlM/lazyclaude` 作成 | リポジトリ |
| P0.2 | `go mod init github.com/KEMSHlM/lazyclaude` | `go.mod` |
| P0.3 | cobra サブコマンド骨格 (root, server, diff, tool) | `cmd/lazyclaude/*.go` |
| P0.4 | Makefile (build, test, lint, install) | `Makefile` |
| P0.5 | golangci-lint 設定 | `.golangci.yml` |
| P0.6 | GitHub Actions CI (build + test + lint) | `.github/workflows/ci.yml` |
| P0.7 | **PoC: gocui in tmux display-popup** | `poc/gocui-popup/main.go` |
| P0.8 | **PoC: WebSocket + Claude Code 接続** | `poc/ws-server/main.go` |

### PoC 検証項目

**PoC-A: gocui in tmux display-popup**

```go
// poc/gocui-popup/main.go
// tmux display-popup -w80% -h80% -E './poc/gocui-popup/main'
// 期待: gocui View が描画され、キー入力を受け付ける
```

検証ポイント:
- tcell.Screen が display-popup の PTY で初期化できるか
- キーバインドが正しく動作するか
- マウスイベントが取得できるか
- Esc キーで正常終了するか

**PoC-B: WebSocket + Claude Code**

```go
// poc/ws-server/main.go
// Claude Code の MCP 接続を受け付け、initialize に応答する最小サーバー
```

検証ポイント:
- Claude Code が Go WebSocket サーバーに接続するか
- JSON-RPC 2.0 のメッセージ交換が成功するか
- 認証トークンの受け渡しが動作するか

### 完了条件

- `go build ./cmd/lazyclaude` 成功
- `lazyclaude --help` でサブコマンド一覧表示
- PoC-A: gocui が tmux display-popup 内で動作
- PoC-B: Claude Code が Go サーバーに接続成功

---

## 5. Phase 1: コア層

**目標**: tmux 操作 + プロセス管理を interface で抽象化
**依存**: P0

### タスク

| # | タスク | 成果物 |
|---|--------|--------|
| P1.1 | TmuxClient interface + 型定義 | `internal/core/tmux/client.go`, `types.go` |
| P1.2 | exec 実装 | `internal/core/tmux/exec.go` |
| P1.3 | PID → window 解決 | `internal/core/tmux/pidwalk.go` |
| P1.4 | mock 実装 | `internal/core/tmux/mock.go` |
| P1.5 | プロセスツリー走査 | `internal/core/process/tree.go` |
| P1.6 | 設定管理 (ポート, トークン, パス) | `internal/core/config/config.go` |
| P1.8 | ユニットテスト | `*_test.go` |

### TmuxClient interface

```go
type TmuxClient interface {
    // クライアント
    ListClients() ([]ClientInfo, error)
    FindActiveClient() (*ClientInfo, error)

    // セッション / ウィンドウ
    HasSession(name string) (bool, error)
    NewSession(opts NewSessionOpts) error
    ListWindows(session string) ([]WindowInfo, error)
    NewWindow(opts NewWindowOpts) error
    RespawnPane(target, cmd string) error
    KillWindow(target string) error

    // ペイン
    CapturePaneContent(target string) (string, error)
    SendKeys(target string, keys ...string) error

    // popup
    DisplayPopup(opts PopupOpts) error

    // PID
    FindWindowForPid(pid int) (*WindowInfo, error)

    // 情報
    ShowMessage(target, format string) (string, error)
    GetOption(target, option string) (string, error)
}
```

### 専用 tmux ソケットによるセッション分離

lazyclaude が管理するセッションはユーザーの tmux セッションリストから分離する。
`ExecClient` が専用ソケット (`-L lazyclaude`) を使うことで実現:

```
ユーザーの tmux (default)     lazyclaude の tmux (-L lazyclaude)
┌─────────────────┐           ┌──────────────────┐
│ main: 3 windows │           │ lc-abc: claude   │
│ work: 2 windows │           │ lc-def: claude   │
└─────────────────┘           └──────────────────┘
tmux ls → 2 sessions          ユーザーには見えない
```

`ExecClient` にソケット名を設定:

```go
type ExecClient struct {
    tmuxBin string
    socket  string  // "-L lazyclaude" — 空ならデフォルトソケット
}

func (c *ExecClient) run(ctx context.Context, args ...string) (string, error) {
    if c.socket != "" {
        args = append([]string{"-L", c.socket}, args...)
    }
    // ...
}
```

利点:
- `tmux ls` にセッションが表示されない
- ユーザーが誤って lazyclaude のウィンドウを kill しない
- テスト環境と同じ隔離パターン (`-L lc-dev` → `-L lazyclaude`)

lazyclaude.tmux 拡張 (P7) では、ユーザーの tmux から `tmux -L lazyclaude` 経由で
lazyclaude セッションにアクセスする。

### 完了条件

- mock テスト 15+ ケース
- カバレッジ 80%+
- tmux コマンドの直接実行はテストに不要

---

## 6. Phase 2: MCP サーバー

**目標**: `lazyclaude server` が Claude Code からの MCP 接続を処理
**依存**: P1

### タスク

| # | タスク | 成果物 |
|---|--------|--------|
| P2.1 | TCP リスナー + HTTP /notify | `internal/server/server.go` |
| P2.2 | WebSocket ハンドラ | `internal/server/ws.go` |
| P2.3 | JSON-RPC 2.0 | `internal/server/jsonrpc.go` |
| P2.4 | MCP ハンドラ (initialize, ide_connected, openDiff) | `internal/server/handler.go` |
| P2.5 | 接続状態 + pending store | `internal/server/state.go` |
| P2.6 | popup 起動 (サイズ計算 + `lazyclaude diff`/`lazyclaude tool` 呼び出し) | `internal/server/popup.go` |
| P2.7 | lock ファイル + SSH リモート更新 | `internal/server/lock.go` |
| P2.8 | 統合テスト | `*_test.go` |

### 並行処理パターン

| 責務 | Go パターン |
|------|-----------|
| per-connection メッセージ順序 | goroutine + channel |
| SSH lock 並列更新 | `errgroup.Group` |
| pending 期限切れ | `time.AfterFunc` + `sync.Map` |
| popup 起動 | `exec.CommandContext` (自バイナリ呼び出し) |

### popup 起動

MCP サーバーが popup を起動する際、自バイナリのサブコマンドを呼ぶ:

```go
func (s *Server) triggerDiffPopup(client, window string, args DiffArgs) error {
    cmd := fmt.Sprintf("%s diff --window %s --old %s --new %s",
        s.binaryPath, window, args.OldPath, args.NewContentsFile)
    return s.tmux.DisplayPopup(PopupOpts{
        Client: client, Width: args.Width, Height: args.Height, Cmd: cmd,
    })
}
```

### 完了条件

- `lazyclaude server` → Claude Code 接続成功
- initialize → capabilities 応答
- ide_connected → PID→window 解決
- /notify POST → popup 起動
- lock ファイル管理動作
- テストカバレッジ 80%+

---

## 7. Phase 3: TUI フレームワーク

**目標**: lazygit 準拠の gocui フレームワーク層を構築
**依存**: P0 (P1, P2 と並行可能)

### タスク

| # | タスク | 成果物 |
|---|--------|--------|
| P3.1 | App struct (gocui.Gui ラッパー) | `internal/gui/app.go` |
| P3.2 | Views struct + createAllViews() | `internal/gui/views.go` |
| P3.3 | Layout (動的 View 配置) | `internal/gui/layout.go` |
| P3.4 | テーマ (色定数, スタイル) | `internal/gui/theme.go` |
| P3.5 | Binding 型 + keybinding 管理 | `internal/gui/keybindings.go` |
| P3.6 | アクションバー (renderContextOptionsMap) | `internal/gui/options.go` |
| P3.7 | BaseContext + ContextMgr | `internal/gui/context/base.go`, `manager.go` |
| P3.8 | NavigationController (j/k/gg/G/mouse) | `internal/gui/controllers/navigation.go` |
| P3.9 | テストモード (SimulationScreen + GuiDriver) | `internal/gui/test_mode.go` |
| P3.10 | テストフレームワーク (ViewDriver, Matcher) | `internal/integration/` |

### App struct (lazygit の Gui に相当)

```go
type App struct {
    gui        *gocui.Gui
    views      *Views
    contextMgr *context.ContextMgr
    tmux       tmux.TmuxClient
    config     *config.Config

    // 起動モード
    mode       AppMode  // ModeMain | ModeDiff | ModeTool
}

type AppMode int
const (
    ModeMain AppMode = iota  // lazyclaude        → メイン画面
    ModeDiff                 // lazyclaude diff   → diff popup
    ModeTool                 // lazyclaude tool   → tool popup
)
```

### Views struct

```go
type Views struct {
    // メイン画面
    Sessions *gocui.View  // 左: セッション一覧
    Preview  *gocui.View  // 右: ライブプレビュー
    Status   *gocui.View  // 下: ステータスバー
    Options  *gocui.View  // 最下行: アクションバー

    // Popup 画面 (diff/tool)
    Content  *gocui.View  // コンテンツ領域
    Actions  *gocui.View  // アクションバー
}
```

### Layout パターン

```go
func (a *App) Layout(g *gocui.Gui) error {
    maxX, maxY := g.Size()
    switch a.mode {
    case ModeMain:
        return a.layoutMain(g, maxX, maxY)
    case ModeDiff, ModeTool:
        return a.layoutPopup(g, maxX, maxY)
    }
    return nil
}

func (a *App) layoutMain(g *gocui.Gui, maxX, maxY int) error {
    splitX := maxX / 3
    // Sessions: 左 1/3
    g.SetView("sessions", 0, 0, splitX-1, maxY-3, 0)
    // Preview: 右 2/3
    g.SetView("preview", splitX, 0, maxX-1, maxY-3, 0)
    // Status: 下から 2 行目
    g.SetView("status", 0, maxY-2, maxX-1, maxY-1, 0)
    // Options: 最下行
    g.SetView("options", 0, maxY-1, maxX-1, maxY, 0)
    return nil
}

func (a *App) layoutPopup(g *gocui.Gui, maxX, maxY int) error {
    // Content: 上部
    g.SetView("content", 0, 0, maxX-1, maxY-3, 0)
    // Actions: 下部
    g.SetView("actions", 0, maxY-2, maxX-1, maxY, 0)
    return nil
}
```

### Context / ContextMgr

```go
type Context interface {
    Name() string
    Kind() ContextKind  // SIDE | MAIN | POPUP
    GetKeybindings() []Binding
    OnFocus()   // context がアクティブになった時
    OnBlur()    // context が非アクティブになった時
}

type ContextMgr struct {
    mu    sync.Mutex
    stack []Context
}

func (m *ContextMgr) Push(c Context)    { ... }
func (m *ContextMgr) Pop() Context      { ... }
func (m *ContextMgr) Current() Context  { ... }
```

### 完了条件

- 空の App が起動し、View が描画される
- j/k でリスト移動 (空リスト) が動作
- Esc / q で終了
- アクションバーが context に応じて切り替わる
- `LAZYCLAUDE_HEADLESS=true` で SimulationScreen ベースのテストが実行できる
- ViewDriver チェーン API でアサーションが書ける
- `go test ./internal/integration/clients/...` が CI で動作する

---

## 8. Phase 4: メイン画面

**目標**: `lazyclaude` 起動でセッション一覧 + プレビュー + セッション操作
**依存**: P1, P3

### 旧方式の問題点 (tmux-claude)

| 問題 | 詳細 |
|------|------|
| 不透明な命名 | `claude-a1b2c3d` — パスの MD5 ハッシュで、何のプロジェクトか分からない |
| 1:1 制約 | 1 パス = 1 セッション。同じプロジェクトで複数セッションを持てない |
| 孤児化 | ディレクトリ移動/リネームでセッションが orphan になる |
| 状態なし | tmux window の有無だけが真実。メタデータ (作成日時, 最終利用日時) がない |
| 名前変更不可 | ハッシュベースのため、ユーザーが意味のある名前を付けられない |

### 新セッション管理設計

#### セッションモデル

```go
type Session struct {
    ID        string    `json:"id"`         // UUID v4
    Name      string    `json:"name"`       // ユーザー可読名 (自動生成 or 手動)
    Path      string    `json:"path"`       // ワーキングディレクトリ
    Host      string    `json:"host"`       // "" = local, "srv1" = SSH remote
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    Flags     []string  `json:"flags"`      // "--resume" 等の起動オプション

    // 実行時状態 (永続化しない)
    TmuxWindow string   `json:"-"`          // tmux window ID (@1, @2, ...)
    Status     Status   `json:"-"`          // Running / Dead / Detached
    PID        int      `json:"-"`          // Claude Code の PID
}

type Status int
const (
    StatusDetached Status = iota  // tmux window あり、Claude 未起動
    StatusRunning                 // Claude Code が実行中
    StatusDead                    // tmux window のペインが dead
    StatusOrphan                  // state にあるが tmux window が消失
)
```

#### 名前の自動生成

ハッシュではなく、ディレクトリ名をベースに人間が読める名前を生成:

```
/Users/kenshin/projects/my-app     → "my-app"
/Users/kenshin/projects/my-app (2) → "my-app-2"   (同名の場合サフィックス)
srv1:/home/user/work               → "srv1:work"
```

ユーザーが `R` キーでリネーム可能。

#### 永続化

```
~/.local/share/lazyclaude/
├── state.json              # セッション一覧 (Session の配列)
└── logs/                   # per-session ログ (optional)
```

`state.json` の例:

```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "my-app",
    "path": "/Users/kenshin/projects/my-app",
    "host": "",
    "created_at": "2026-03-17T10:00:00Z",
    "updated_at": "2026-03-17T14:30:00Z",
    "flags": []
  },
  {
    "id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
    "name": "srv1:work",
    "path": "/home/user/work",
    "host": "srv1",
    "created_at": "2026-03-16T09:00:00Z",
    "updated_at": "2026-03-17T12:00:00Z",
    "flags": ["--resume"]
  }
]
```

#### 起動時の状態同期

`lazyclaude` 起動時に state.json と tmux の実状態を突合:

```
1. state.json を読み込み
2. tmux list-windows で lazyclaude session の全 window を取得
3. 各 Session の TmuxWindow を解決 (window name = Session.ID の先頭 8 文字)
4. tmux に存在しない Session → Status = Orphan
5. tmux にあるが state にない window → 無視 (lazyclaude 管理外)
6. pane が dead → Status = Dead
7. Claude PID が alive → Status = Running, else → Status = Detached
```

tmux window 名は `lc-550e8400` (ID 先頭 8 文字) — 内部用なので不透明でも問題ない。
ユーザーが目にするのは TUI 上の `Name` フィールド。

### タスク

| # | タスク | 成果物 | 状態 |
|---|--------|--------|------|
| P4.1 | Session 型 + Store (JSON 永続化) | `internal/session/store.go` | 完了 |
| P4.2 | SessionManager (CRUD + tmux 同期) | `internal/session/manager.go` | 完了 |
| P4.3 | セッションリスト表示 + カーソル | `internal/gui/app.go` (renderSessionList) | 完了 |
| P4.4 | キーバインド (j/k/n/d/D/q) | `internal/gui/app.go` (setupGlobalKeybindings) | 完了 |
| P4.5 | セッション行フォーマット | `internal/gui/presentation/sessions.go` | 完了 |
| P4.6 | PreviewContext (Main パネルに capture-pane) | 未実装 | **次** |
| P4.7 | launch (local) | `internal/session/manager.go` (Create) | 完了 |
| P4.8 | attach ループ (enter キー) | 未実装 | 未着手 |
| P4.9 | MCP サーバー自動起動 | 未実装 | 未着手 |

**注**: P4.3/P4.4 は当初の計画では `gui/context/sessions.go` と `gui/controllers/session.go` に分離予定だったが、lazygit と同様に `app.go` 内に統合した。

### メイン画面レイアウト

```
┌─ Sessions ─────────────┬─ Preview ─────────────────────┐
│                         │                               │
│ ▸ my-app          ⬤ R  │  $ claude                     │
│   my-lib               │  I'll help you with that...   │
│   srv1:work       ⬤    │  > Edit src/main.go           │
│   old-project     x    │  ...                          │
│                         │                               │
├─────────────────────────┴───────────────────────────────┤
│ MCP: ⬤ :7860  |  3 sessions  |  ~/projects/my-app      │
├─────────────────────────────────────────────────────────┤
│ n: new  d: del  enter: attach  r: resume  R: rename    │
└─────────────────────────────────────────────────────────┘
```

### セッション行フォーマット

```
▸ my-app          ⬤ R    # Running (green dot), Resume flag
  my-lib                  # Detached (no dot)
  srv1:work       ⬤      # Remote SSH, Running
  old-project     x      # Orphan (red x) — tmux window 消失
```

- `▸` = カーソル位置
- `⬤` = Running (green) / Dead (dim red)
- `x` = Orphan (state にあるが tmux window なし)
- `R` = --resume フラグ付き

### プレビュー

`time.Ticker(100ms)` で選択中セッションの `CapturePaneContent` をポーリング。
変更がある場合のみ View を更新 (flickering 防止)。
Orphan セッションを選択時は `"Session not found in tmux. Press 'd' to remove."` を表示。

### MCP サーバー自動起動

`lazyclaude` 起動時に MCP サーバーが未起動なら自動起動:

```go
func (a *App) ensureServer() error {
    if isServerRunning(a.config) { return nil }
    cmd := exec.Command(os.Args[0], "server")
    cmd.Start()
    return cmd.Process.Release()  // detach
}
```

### キーバインド

| キー | 動作 |
|------|------|
| `j`/`k` | セッション選択移動 |
| `enter` | 選択セッションに attach (TUI を離れる) |
| `n` | 新規セッション作成 (パス入力 or CWD) |
| `r` | --resume で attach |
| `R` | セッション名リネーム |
| `d` | セッション削除 (tmux window kill + state 削除) |
| `D` | Orphan セッションを一括削除 |
| `1`/`2`/`3` | 選択セッションに send-keys |
| `?` | ヘルプ popup |
| `q` | 終了 |

### 完了条件

- `lazyclaude` 起動でセッション一覧が表示される
- j/k でカーソル移動、右側にプレビュー表示
- enter で attach → TUI 離脱 → Claude Code 操作 → detach で TUI 復帰
- n で新規セッション作成 (名前はディレクトリ名から自動生成)
- R でリネーム
- state.json と tmux の同期が正しく動作
- Orphan 検出と一括削除
- MCP サーバーが自動起動

---

## 9. Phase 5: Diff / Tool Popup

**目標**: `lazyclaude diff`, `lazyclaude tool` が gocui popup として動作
**依存**: P3

### タスク

| # | タスク | 成果物 | 状態 |
|---|--------|--------|------|
| P5.1 | diff パーサー | `internal/gui/presentation/diff.go` | 完了 |
| P5.2 | diff gocui 画面 (スクロール + y/a/n) | `cmd/lazyclaude/diff.go` | 完了 |
| P5.3 | diff 行カラーリング (ANSI) | `cmd/lazyclaude/diff.go` | 完了 |
| P5.4 | tool 情報パーサー | `internal/gui/presentation/tool.go` | 完了 |
| P5.5 | tool gocui 画面 (y/a/n) | `cmd/lazyclaude/tool.go` | 完了 |
| P5.6 | choice ファイル書き出し | `internal/gui/choice.go` | 完了 |
| P5.7 | MCP server → display-popup → lazyclaude diff | P7 で実装 | 未着手 |

**注**: 当初は `gui/context/` と `gui/controllers/` に分離予定だったが、
diff/tool は独立サブコマンド (`cmd/lazyclaude/diff.go`, `tool.go`) として実装した。
gocui の Context/Controller パターンは使わず、サブコマンド内に直接 layout + keybinding を記述。

### Diff Popup レイアウト

```
┌─ Edit: src/main.go (~/projects/app) ───────────────────┐
│                                                         │
│   10  func main() {                                     │
│   11 -    fmt.Println("hello")                          │  ← red bg
│   11 +    fmt.Println("hello, world")                   │  ← green bg
│   12  }                                                 │
│                                                         │
├─────────────────────────────────────────────────────────┤
│  Yes: y  |  Allow always: a  |  No: n  |  cancel: Esc  │
└─────────────────────────────────────────────────────────┘
```

### Tool Popup レイアウト

```
┌─ Bash ──────────────────────────────────────────────────┐
│                                                         │
│  Command:                                               │
│  $ npm test -- --coverage                               │
│                                                         │
│  CWD: ~/projects/app                                    │
│                                                         │
├─────────────────────────────────────────────────────────┤
│  Yes: y  |  Allow always: a  |  No: n  |  cancel: Esc  │
└─────────────────────────────────────────────────────────┘
```

### スクロール (DiffContext のみ)

lazygit の `oy` 制御方式:

| キー | 動作 |
|------|------|
| `j`/`k` | 1 行 |
| `d`/`u` | 半ページ |
| `f`/`b` | 全ページ |
| `gg`/`G` | 先頭/末尾 |
| マウスホイール | 3 行 |

### 完了条件

- `lazyclaude diff` が tmux display-popup 内で動作
- `lazyclaude tool` が tmux display-popup 内で動作
- スクロール + y/a/n 選択 → choice ファイル書き出し
- MCP サーバーが popup を起動 → choice を読み取り → send-keys

---

## 10. Phase 6: SSH + 高度な機能

**目標**: SSH リモート、設定ファイル、ヘルプ
**依存**: P4

### タスク

| # | タスク | 成果物 |
|---|--------|--------|
| P6.1 | SSH reverse tunnel 構築 | `internal/session/ssh.go` |
| P6.2 | リモート lock ファイル管理 | `internal/server/lock.go` 拡張 |
| P6.3 | 設定ファイル (~/.config/lazyclaude/config.toml) | `internal/core/config/` 拡張 |
| P6.4 | ヘルプ popup (?) | `internal/gui/context/help.go` |

### 完了条件

- SSH リモートセッション作成 + attach が動作
- `~/.config/lazyclaude/config.toml` でカスタマイズ可能
- lazyclaude 単体が standalone として完結

---

## 11. Phase 7: lazyclaude.tmux 拡張

**目標**: tmux 拡張として、keybind 登録・自動 popup・MCP サーバー自動起動を提供
**依存**: P5, P6

### tmux 拡張の位置づけ

lazyclaude 単体は standalone TUI として完結する。
lazyclaude.tmux はそれに **追加機能** を載せる tmux プラグイン:

```
lazyclaude (standalone)     — ユーザーが手動で起動・操作する
lazyclaude.tmux (拡張)      — tmux が自動的に lazyclaude を呼び出す
```

### 拡張が提供する機能

| 機能 | 実現方法 |
|------|---------|
| prefix+a → lazyclaude TUI popup | `tmux bind-key a display-popup -E lazyclaude` |
| prefix+A → --resume で attach | `tmux bind-key A run-shell 'lazyclaude attach --resume'` |
| 作業中ペインに diff/tool 自動 popup | MCP server → `tmux display-popup -E "lazyclaude diff ..."` |
| MCP サーバー自動起動 | `tmux set-hook -g session-created 'run-shell "lazyclaude server --ensure"'` |
| Claude Code hooks 設定 | `~/.claude/settings.json` に Notification hook を登録 |

### タスク

| # | タスク | 成果物 |
|---|--------|--------|
| P7.1 | lazyclaude.tmux エントリポイント | `lazyclaude.tmux` (shell, TPM 用) |
| P7.2 | `lazyclaude setup` サブコマンド (手動セットアップ代替) | `cmd/lazyclaude/setup.go` |
| P7.3 | server --ensure (idempotent 起動) | `cmd/lazyclaude/server.go` 拡張 |
| P7.4 | MCP server → tmux display-popup 連携 | `internal/server/popup.go` |
| P7.5 | Claude hooks 自動設定 | `internal/core/config/hooks.go` |

### lazyclaude.tmux の実装

lazyclaude.tmux は **最小限の shell** — TPM が認識するためのエントリポイント。
全ロジックは Go バイナリに委譲する:

```bash
#!/usr/bin/env bash
# lazyclaude.tmux — TPM entry point
# This file must be shell for TPM compatibility.
# All logic is delegated to the lazyclaude binary.

LAZYCLAUDE_BIN="$(command -v lazyclaude 2>/dev/null)"
if [ -z "$LAZYCLAUDE_BIN" ]; then
    LAZYCLAUDE_BIN="$(cd "$(dirname "$0")" && pwd)/bin/lazyclaude"
fi

if [ ! -x "$LAZYCLAUDE_BIN" ]; then
    tmux display-message "lazyclaude: binary not found"
    exit 1
fi

# Delegate all setup to the Go binary
"$LAZYCLAUDE_BIN" setup --tmux-plugin
```

`lazyclaude setup --tmux-plugin` が実行する内容:

```go
// 1. tmux keybind 登録
tmux.Bind("a", "display-popup -w90% -h90% -E lazyclaude")
tmux.Bind("A", "run-shell 'lazyclaude attach --resume'")

// 2. MCP サーバー自動起動 hook
tmux.SetHook("session-created", "run-shell 'lazyclaude server --ensure'")

// 3. Claude Code hooks 設定 (optional, --setup-hooks フラグ)
writeClaudeHooks(port, token)
```

### lazyclaude setup (拡張なしの手動セットアップ)

TPM を使わないユーザー向けに `lazyclaude setup` を提供:

```
$ lazyclaude setup
lazyclaude setup complete:
  - tmux keybinds registered (prefix+a, prefix+A)
  - MCP server hook installed
  - Claude Code hooks configured

Add to ~/.tmux.conf for persistence:
  run-shell 'lazyclaude setup --quiet'
```

### 2 つのセットアップ方法

| 方法 | コマンド | 対象ユーザー |
|------|---------|-------------|
| TPM | `set -g @plugin 'KEMSHlM/lazyclaude'` | TPM ユーザー |
| 手動 | `lazyclaude setup` (tmux.conf に追記) | TPM なしのユーザー |

どちらも最終的に `lazyclaude setup --tmux-plugin` を実行するため、結果は同一。

### 完了条件

- `lazyclaude.tmux` が TPM で認識される
- prefix+a で lazyclaude TUI が popup で開く
- Claude Code の diff/tool 要求時に作業中ペインに popup が自動表示される
- MCP サーバーが tmux session 開始時に自動起動する
- `lazyclaude setup` で手動セットアップが可能
- lazyclaude.tmux の shell は **10 行以内** (ロジックは全て Go)

---

## 12. Phase 8: 配布・CI/CD

**目標**: goreleaser + Homebrew でバイナリ配布
**依存**: P7

### タスク

| # | タスク | 成果物 |
|---|--------|--------|
| P8.1 | goreleaser 設定 | `.goreleaser.yml` |
| P8.2 | GitHub Actions release | `.github/workflows/release.yml` |
| P8.3 | Homebrew Formula | `homebrew-lazyclaude/` |
| P8.4 | README.md (standalone + tmux 拡張 両方の説明) | `README.md` |

### 完了条件

- `git tag v0.1.0 && git push --tags` → macOS/Linux バイナリ自動ビルド
- `brew install KEMSHlM/tap/lazyclaude` でインストール可能
- TPM でのインストールが動作
- README に standalone 使用と tmux 拡張使用の両方を記載

---

## 13. リスクと対策

| リスク | 影響 | 確率 | 対策 |
|--------|------|------|------|
| gocui が tmux display-popup PTY で動作しない | P3-P5 全体 | 低 | P0.7 で PoC 先行。失敗時は bubbletea に切替 |
| Claude Code WebSocket 互換性 | P2 全体 | 中 | P0.8 で PoC 先行。既存 JS サーバーの通信を tcpdump で事前分析 |
| SSH reverse tunnel タイミング | P6.1 | 中 | 既存 Shell ロジックを忠実移植 + retry |
| goroutine リーク | P2 安定性 | 中 | `context.Context` + `errgroup` + `goleak` テスト |
| メイン TUI ↔ attach 切り替え | P4 UX | 中 | gocui を一時停止 → tmux attach → 復帰のフロー検証 |
| lazyclaude.tmux の shell 依存 | P7 設計原則 | 低 | TPM 互換のため最小限 (10行以内) のみ shell。ロジックは Go に委譲 |

---

## 14. テスト戦略

### 5 層テストアーキテクチャ

| 層 | 対象 | 手法 | 実行環境 | Phase |
|----|------|------|---------|-------|
| L1: Unit | core, server, session | `testing` + mock | CI + local | P0-P2 |
| L2: GUI In-Process | TUI 表示 + keybinding + 状態遷移 | lazygit 方式 (SimulationScreen + GuiDriver) | CI (headless) + local | P3 |
| L3: Component | gocui バイナリの外部駆動 | **termtest** (PTY + VT100 エミュレーション) | CI + local | P5.5 |
| L4: Integration | tmux popup + MCP サーバー + choice | **tmux scripting** (send-keys + capture-pane) | CI + local | P5.5 |
| L5: Visual Regression | UI スナップショット回帰 | **VHS** (.tape → .txt ゴールデンファイル) | CI + local | P5.5 |

### GUI 統合テスト (lazygit 方式)

lazygit の `pkg/integration/` を参考に、同じパターンで GUI テストを構築する。

#### テストモード (`internal/gui/test_mode.go`)

```go
func (a *App) handleTestMode() {
    if os.Getenv("LAZYCLAUDE_HEADLESS") != "true" {
        return
    }
    go func() {
        a.waitUntilIdle()
        a.integrationTest.Run(NewGuiDriver(a))
        a.gui.Update(func(g *gocui.Gui) error {
            return gocui.ErrQuit
        })
    }()
}
```

- `LAZYCLAUDE_HEADLESS=true` で tcell.SimulationScreen を使用
- テストは goroutine で GUI と並行実行
- `isIdleChan` で GUI の処理完了を待機してから次の操作

#### GuiDriver (`internal/gui/gui_driver.go`)

```go
type GuiDriver struct {
    gui       *App
    isIdleChan chan struct{}
}

func (d *GuiDriver) PressKey(key string) {
    tcellKey := keybindings.GetKey(key)
    d.gui.gui.ReplayedEvents.Keys <- gocui.NewTcellKeyEventWrapper(
        tcell.NewEventKey(tcellKey, 0, tcell.ModNone), 0,
    )
    d.waitTillIdle()
}

func (d *GuiDriver) Click(x, y int) {
    d.gui.gui.ReplayedEvents.MouseEvents <- gocui.NewTcellMouseEventWrapper(
        tcell.NewEventMouse(x, y, tcell.ButtonPrimary, 0), 0,
    )
    d.waitTillIdle()
}

func (d *GuiDriver) Fail(message string) {
    fullMessage := fmt.Sprintf(
        "%s\nFinal state:\n%s\nFocused view: '%s'\nLog:\n%s",
        message,
        d.gui.gui.Snapshot(),  // gocui の画面スナップショット
        d.gui.gui.CurrentView().Name(),
        strings.Join(d.gui.guiLog, "\n"),
    )
    panic(fullMessage)
}
```

#### ViewDriver (チェーン API)

```go
type ViewDriver struct {
    name    string
    getView func() *gocui.View
    t       *TestDriver
}

// チェーンメソッド — lazygit と同じ fluent API
func (v *ViewDriver) Focus() *ViewDriver
func (v *ViewDriver) IsFocused() *ViewDriver
func (v *ViewDriver) Press(key string) *ViewDriver
func (v *ViewDriver) Lines(matchers ...*TextMatcher) *ViewDriver
func (v *ViewDriver) TopLines(matchers ...*TextMatcher) *ViewDriver
func (v *ViewDriver) LineCount(matcher *IntMatcher) *ViewDriver
func (v *ViewDriver) SelectedLine(matcher *TextMatcher) *ViewDriver
func (v *ViewDriver) SelectNextItem() *ViewDriver
func (v *ViewDriver) NavigateToLine(matcher *TextMatcher) *ViewDriver
func (v *ViewDriver) Tap(f func()) *ViewDriver
```

#### Matcher パターン

```go
// TextMatcher
Contains("my-app")
Equals("my-app          Running")
DoesNotContain("orphan")
MatchesRegexp(`^▸ .+⬤`)

// IntMatcher
EqualsInt(3)
GreaterThan(0)

// 組み合わせ
Contains("my-app").IsSelected()  // 選択行のアサーション
```

#### テスト定義の例

```go
// internal/integration/tests/session/create_session.go
var CreateSession = NewIntegrationTest(NewIntegrationTestArgs{
    Description: "Create a new session and verify it appears in the list",

    SetupConfig: func(cfg *config.Config) {
        cfg.DataDir = t.TempDir()
    },

    SetupSessions: func(shell *Shell) {
        // テスト用の tmux mock セットアップ
        shell.CreateMockSession("existing-project", "/tmp/test/existing")
    },

    Run: func(t *TestDriver, keys config.KeybindingConfig) {
        // 初期状態: 既存セッション 1 件
        t.Views().Sessions().
            IsFocused().
            Lines(
                Contains("existing-project").IsSelected(),
            ).
            LineCount(EqualsInt(1))

        // n キーで新規作成
        t.Views().Sessions().Press("n")

        // パス入力 popup
        t.ExpectPopup().Prompt().
            Title(Equals("New Session")).
            Type("/tmp/test/new-project").
            Confirm()

        // セッション一覧に追加されたことを確認
        t.Views().Sessions().
            Lines(
                Contains("existing-project"),
                Contains("new-project").IsSelected(),
            ).
            LineCount(EqualsInt(2))

        // プレビューにコンテンツが表示される
        t.Views().Preview().
            Content(Contains("claude"))
    },
})

// internal/integration/tests/diff/accept_diff.go
var AcceptDiff = NewIntegrationTest(NewIntegrationTestArgs{
    Description: "Accept a diff in the popup viewer",
    Mode:        ModeDiff,  // diff popup モードで起動

    SetupDiff: func(shell *Shell) {
        shell.CreateOldFile("src/main.go", `func main() { fmt.Println("hello") }`)
        shell.CreateNewFile("src/main.go", `func main() { fmt.Println("hello, world") }`)
    },

    Run: func(t *TestDriver, keys config.KeybindingConfig) {
        // diff が表示されている
        t.Views().Content().
            IsFocused().
            Contains(Contains(`- fmt.Println("hello")`)).
            Contains(Contains(`+ fmt.Println("hello, world")`))

        // アクションバーの確認
        t.Views().Actions().
            Content(Contains("Yes: y")).
            Content(Contains("No: n"))

        // j キーでスクロール
        t.Views().Content().Press("j").Press("j")

        // y キーで accept
        t.Views().Content().Press("y")

        // choice ファイルが書き出されたことを確認
        t.AssertChoiceFile(Equals("1"))
    },
})
```

#### テスト実行の 3 モード

lazygit と同じ 3 つの実行方式:

**1. go test (CI 用)**

```bash
go test ./internal/integration/clients/... -v
```

- `LAZYCLAUDE_HEADLESS=true` で SimulationScreen 使用
- 並列実行対応 (`PARALLEL_TOTAL`, `PARALLEL_INDEX`)
- 失敗時にスナップショット出力

**2. CLI ランナー (開発用)**

```bash
go run cmd/integration_test/main.go cli [--slow] [test_name]
```

- `--slow`: 600ms 遅延でキー入力を可視化
- `--sandbox`: テストセットアップ後、GUI を手動操作 (デバッグ用)
- 特定テストのみ実行可能

**3. TUI ランナー (開発用)**

```bash
go run cmd/integration_test/main.go tui
```

- テスト一覧を TUI で表示
- `enter` で実行、`t` でスローモード、`s` でサンドボックス
- `/` でフィルタリング

#### 失敗時のデバッグ情報

テスト失敗時に `GuiDriver.Fail()` が出力する情報:

```
Test failed: expected Sessions view to contain "new-project"

Final lazyclaude state:
┌─ Sessions ─────────────┬─ Preview ──────────┐
│ ▸ existing-project     │ $ claude           │
│                        │                    │
└────────────────────────┴────────────────────┘

Focused view: 'sessions'
Log:
  [0.001s] App.Layout() called
  [0.002s] SessionListContext.OnFocus()
  [0.050s] PressKey("n")
  [0.051s] Popup.Prompt shown: "New Session"
  [0.100s] Type("/tmp/test/new-project")
  [0.150s] PressKey("enter")
  [0.151s] SessionManager.Create() error: path does not exist
```

### L3: Component Test (termtest) — Phase P5.5

gocui バイナリを **外部プロセス** として PTY で起動し、キー入力 → 画面内容アサーション。
lazygit 方式 (L2) が gocui の in-process テストなのに対し、L3 は **ビルド済みバイナリ** を
実際の PTY 環境で駆動する。これにより gocui ↔ ターミナルの統合を検証できる。

#### 依存ライブラリ

| ライブラリ | 用途 |
|-----------|------|
| `ActiveState/termtest` | PTY 起動 + VT100 エミュレーション + アサーション |
| `creack/pty` (termtest 内部) | PTY 割当 |
| `hinshun/vt10x` or `ActiveState/vt10x` (termtest 内部) | ANSI エスケープ解釈 |

#### テスト例

```go
// tests/component/diff_test.go
func TestDiffPopup_AcceptWithY(t *testing.T) {
    // Build the binary first
    bin := buildBinary(t)

    // Start lazyclaude diff in a PTY via termtest
    cp, err := termtest.New(
        termtest.CmdName(bin),
        termtest.Args("diff", "--window", "lc-test", "--old", oldFile, "--new", newFile),
        termtest.Cols(80), termtest.Rows(30),
    )
    require.NoError(t, err)
    defer cp.Close()

    // Wait for diff content to appear
    cp.Expect("func main()")
    cp.Expect("Yes: y")

    // Press 'y' to accept
    cp.SendLine("y")

    // Verify process exits
    cp.ExpectExitCode(0)

    // Verify choice file was written
    choice, err := gui.ReadChoiceFile("lc-test")
    require.NoError(t, err)
    assert.Equal(t, gui.ChoiceAccept, choice)
}
```

#### CI 環境

- `go test` で直接実行 (tmux 不要)
- termtest は PTY を自前で作成するため、X11/Wayland 不要
- GitHub Actions `ubuntu-latest` で動作確認済み

### L4: Integration Test (tmux scripting) — Phase P5.5

tmux をヘッドレスで起動し、`send-keys` + `capture-pane` で E2E フローを検証。
L3 との違いは **tmux display-popup** を含む完全なフローをテストすること。

#### tmux ヘッドレス起動

```bash
# テスト用 tmux サーバーを分離 (ソケット名で隔離)
tmux -L lazyclaude-test -f /dev/null new-session -d -s test -x 200 -y 50
```

- `-L lazyclaude-test`: 専用ソケット (他の tmux と干渉しない)
- `-f /dev/null`: 設定ファイルなし
- `-d`: デタッチ状態で起動 (ディスプレイ不要)
- `-x 200 -y 50`: 固定サイズ

#### テスト例

```go
// tests/integration/popup_test.go
func TestPopupLaunch_E2E(t *testing.T) {
    socket := "lazyclaude-test-" + t.Name()
    t.Cleanup(func() { exec.Command("tmux", "-L", socket, "kill-server").Run() })

    // Start isolated tmux server
    run(t, "tmux", "-L", socket, "-f", "/dev/null",
        "new-session", "-d", "-s", "test", "-x", "200", "-y", "50")

    // Launch lazyclaude diff in a pane
    run(t, "tmux", "-L", socket, "send-keys", "-t", "test",
        fmt.Sprintf("%s diff --window lc-test --old %s --new %s", bin, oldFile, newFile), "Enter")

    // Wait for diff to render
    waitForText(t, socket, "test", "Yes: y", 5*time.Second)

    // Press 'y'
    run(t, "tmux", "-L", socket, "send-keys", "-t", "test", "y", "")

    // Verify choice file
    waitForFile(t, gui.ChoiceFilePath("lc-test"), 2*time.Second)
    choice, _ := gui.ReadChoiceFile("lc-test")
    assert.Equal(t, gui.ChoiceAccept, choice)
}

// waitForText polls capture-pane until expected text appears or timeout.
func waitForText(t *testing.T, socket, target, text string, timeout time.Duration) {
    t.Helper()
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        out, _ := exec.Command("tmux", "-L", socket,
            "capture-pane", "-p", "-t", target).Output()
        if strings.Contains(string(out), text) {
            return
        }
        time.Sleep(50 * time.Millisecond)
    }
    t.Fatalf("timeout waiting for %q in pane %s", text, target)
}
```

#### CI 環境

- tmux は GitHub Actions `ubuntu-latest` にプリインストール
- X11/Wayland **不要** (tmux は純粋な PTY マルチプレクサ)
- `TERM=xterm-256color` を設定するだけで動作

### L5: Visual Regression (VHS) — Phase P5.5

VHS `.tape` ファイルでユーザー操作をスクリプト化し、`.txt` 出力を
ゴールデンファイルと比較。**UI の意図しない退行** を検出する。

#### VHS の限界と用途

| | 可能 | 不可能 |
|---|------|--------|
| 操作スクリプト | Type, Enter, Ctrl+C, 矢印キー | tmux popup への直接介入 |
| 出力形式 | .gif, .mp4, .txt, .ascii | |
| アサーション | ゴールデンファイル比較 (外部 diff) | インライン assert |
| CI | headless 対応 | |

#### テスト例

```tape
# tests/visual/diff_popup.tape
Require lazyclaude
Set Shell bash
Set Width 80
Set Height 30

Type "lazyclaude diff --old testdata/old.go --new testdata/new.go --window test"
Enter
Sleep 1s
Type "j"
Sleep 200ms
Type "j"
Sleep 200ms
Type "y"
Sleep 500ms

Output tests/visual/diff_popup.txt
```

```bash
# CI: generate + compare
vhs tests/visual/diff_popup.tape
diff tests/visual/diff_popup.txt tests/visual/golden/diff_popup.txt
```

#### 更新フロー

UI を意図的に変更した場合:
```bash
vhs tests/visual/diff_popup.tape
cp tests/visual/diff_popup.txt tests/visual/golden/diff_popup.txt
git add tests/visual/golden/
```

---

### Phase P5.5: TUI 自動テスト基盤 (新設)

**目標**: L3/L4/L5 のテスト基盤を構築し、gocui TUI + tmux popup の自動テストを CI で実行可能にする
**依存**: P3, P5

#### タスク

| # | タスク | 成果物 |
|---|--------|--------|
| P5.5.1 | termtest 依存追加 + ヘルパー関数 | `tests/component/helpers_test.go` |
| P5.5.2 | diff popup の component test | `tests/component/diff_test.go` |
| P5.5.3 | tool popup の component test | `tests/component/tool_test.go` |
| P5.5.4 | tmux ヘッドレス起動ヘルパー | `tests/integration/tmux_test.go` |
| P5.5.5 | popup E2E test (tmux send-keys + capture-pane) | `tests/integration/popup_test.go` |
| P5.5.6 | VHS tape + ゴールデンファイル | `tests/visual/*.tape`, `tests/visual/golden/` |
| P5.5.7 | GitHub Actions CI に L3/L4/L5 を追加 | `.github/workflows/ci.yml` 更新 |

#### ディレクトリ構成

```
tests/
├── component/              L3: termtest ベース
│   ├── helpers_test.go     buildBinary(), newTermtest()
│   ├── diff_test.go        diff popup テスト
│   └── tool_test.go        tool popup テスト
│
├── integration/            L4: tmux scripting ベース
│   ├── tmux_test.go        ヘッドレス tmux ヘルパー
│   ├── popup_test.go       popup 起動 + choice テスト
│   └── server_test.go      MCP サーバー + popup 統合
│
└── visual/                 L5: VHS ゴールデンファイル
    ├── diff_popup.tape
    ├── tool_popup.tape
    ├── main_screen.tape
    └── golden/             ゴールデンファイル
        ├── diff_popup.txt
        ├── tool_popup.txt
        └── main_screen.txt
```

#### CI ワークフロー

```yaml
# .github/workflows/ci.yml
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: go test -race -cover ./internal/...

  component:
    runs-on: ubuntu-latest
    needs: unit
    env:
      TERM: xterm-256color
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: go build -o bin/lazyclaude ./cmd/lazyclaude
      - run: go test -v ./tests/component/...

  integration:
    runs-on: ubuntu-latest
    needs: unit
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: go build -o bin/lazyclaude ./cmd/lazyclaude
      - run: tmux -V  # verify tmux available
      - run: go test -v ./tests/integration/...

  visual:
    runs-on: ubuntu-latest
    needs: component
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: go build -o bin/lazyclaude ./cmd/lazyclaude
      - run: go install github.com/charmbracelet/vhs@latest
      - run: |
          for tape in tests/visual/*.tape; do
            vhs "$tape"
          done
      - run: diff -r tests/visual/ tests/visual/golden/ --exclude='*.tape'
```

#### 完了条件

- `go test ./tests/component/...` が CI で動作 (termtest, tmux 不要)
- `go test ./tests/integration/...` が CI で動作 (ヘッドレス tmux)
- VHS ゴールデンファイル比較が CI で動作
- diff popup の y/a/n 全パスがテスト済み
- tool popup の y/a/n 全パスがテスト済み

### カバレッジ目標

| パッケージ | テスト種別 | カバレッジ目標 |
|-----------|----------|-------------|
| `internal/core/tmux/` | Unit (mock) | 90% |
| `internal/server/` | Unit + Integration | 80% |
| `internal/gui/` | Unit + L2 + L3 | 80% |
| `internal/session/` | Unit (mock) | 80% |
| `tests/component/` | L3 (termtest) | — |
| `tests/integration/` | L4 (tmux scripting) | — |
| `tests/visual/` | L5 (VHS golden) | — |

---

## 15. 並行作業マップ

```
Timeline →

P0    ████                                                  (初期化 + PoC)
P1        ████████                                          (コア層)
P3        ████████████                                      (TUI フレームワーク — P1 と並行)
P2                ████████████                              (MCP サーバー — P1 後)
P4                        ████████████                      (メイン画面 — P1,P3 後)
P5                ████████████                              (popup — P3 後、P2 と並行)
P5.5                      ████████████                      (TUI 自動テスト基盤 — P3,P5 後)
P6                                ████████                  (SSH — P4 後)
P7                                        ████████          (tmux 拡張 — P5,P6 後)
P8                                                ████      (配布 — P7 後)
```

### 依存グラフ

```
P0 ─┬─→ P1 ─┬─→ P2 ──────────┐
    │        └─→ P4 ───→ P6 ──┤
    └─→ P3 ─┬─→ P4            ├─→ P7 ─→ P8
             ├─→ P5 ──────────┘
             └─→ P5.5 (P3+P5 後)
```

- P0-P6: **lazyclaude standalone** が完成 (手動操作で全機能使える)
- P7: **lazyclaude.tmux 拡張** が追加 (自動 popup、keybind、サーバー自動起動)
- P8: 配布

### マイルストーン

| # | マイルストーン | Phase | 達成基準 | 層 |
|---|--------------|-------|---------|-----|
| M0 | PoC 完了 | P0 | gocui + WebSocket が動作確認済み | — |
| M1 | コア完成 | P1 | TmuxClient mock テスト全パス | standalone |
| M2 | サーバー稼働 | P2 | Claude Code → Go サーバー接続成功 | standalone |
| M3 | TUI 骨格 | P3 | 空の gocui App が起動・終了 | standalone |
| M4 | メイン画面 | P4 | セッション一覧 + attach が動作 | standalone |
| M5 | popup 動作 | P5 | diff/tool popup が gocui で動作 | standalone |
| M5.5 | TUI 自動テスト | P5.5 | termtest + tmux E2E + Docker が CI で動作 | **testing** |
| M6 | standalone 完成 | P6 | SSH + 設定。`lazyclaude` 単体で全機能動作 | **standalone** |
| M7 | tmux 拡張完成 | P7 | 自動 popup + keybind + サーバー自動起動 | **extension** |
| M8 | 公開リリース | P8 | v0.1.0 バイナリ + TPM + Homebrew | 配布 |

---

## 16. 実装チェックリスト

### Phase 0: プロジェクト初期化

- [x] P0.1 Go module 初期化 (`go.mod`)
- [x] P0.2 cobra サブコマンド骨格 (root, server, diff, tool, setup)
- [x] P0.3 Makefile
- [ ] P0.4 golangci-lint 設定 (`.golangci.yml`)
- [ ] P0.5 GitHub Actions CI
- [x] P0.6 PoC: gocui in tmux (Docker 内で検証済み)
- [x] P0.7 PoC: WebSocket + Claude Code (MCP サーバー統合テスト)

### Phase 1: コア層

- [x] P1.1 TmuxClient interface + 型定義
- [x] P1.2 ExecClient (exec.CommandContext 実装)
- [x] P1.3 PID → window 解決 (pidwalk)
- [x] P1.4 MockClient
- [ ] P1.5 プロセスツリー走査 (`internal/core/process/tree.go`) — pidwalk 内に統合
- [x] P1.6 config.Paths (本番/テスト隔離)
- [x] P1.7 ユニットテスト
- [x] P1.8 ExecClient ソケットオプション (`-L lazyclaude` でセッション分離)

### Phase 2: MCP サーバー

- [x] P2.1 TCP リスナー + HTTP /notify
- [x] P2.2 WebSocket ハンドラ (nhooyr.io/websocket)
- [x] P2.3 JSON-RPC 2.0 (パース + バージョン検証)
- [x] P2.4 MCP ハンドラ (initialize, ide_connected, openDiff)
- [x] P2.5 接続状態 + PendingStore (TTL 付き)
- [ ] P2.6 popup 自動起動 → P5.7 で実装
- [x] P2.7 IDE lock ファイル管理
- [x] P2.8 統合テスト (実 WebSocket 接続)

### Phase 3: TUI フレームワーク

- [x] P3.1 App struct (gocui.Gui ラッパー)
- [x] P3.2 Views (sessions, server, main, options)
- [x] P3.3 Layout (Main モード + Popup モード)
- [x] P3.4 テーマ (色定数)
- [x] P3.5 Binding 型 + keybinding ヘルパー
- [x] P3.6 アクションバー (RenderActionBar)
- [x] P3.7 BaseContext + ContextMgr (lazygit パターン)
- [x] P3.8 タブバー (SideTabs, TabBar)
- [x] P3.9 NewAppHeadless (テスト用 SimulationScreen)
- [x] P3.10 isUnknownView ヘルパー (go-errors Wrap 対応)

### Phase 4: メイン画面

- [x] P4.1 Session 型 + Store (JSON 永続化, atomic write)
- [x] P4.2 SessionManager (Create/Delete/Rename/PurgeOrphans/Sync)
- [x] P4.3 セッションリスト表示 (カーソル + ステータスインジケータ)
- [x] P4.4a キーバインド: j/k (カーソル移動)
- [x] P4.4b キーバインド: n (新規セッション作成 → claude 起動)
- [x] P4.4c キーバインド: d (セッション削除)
- [x] P4.4d キーバインド: D (orphan 一括削除)
- [x] P4.4e キーバインド: q (終了)
- [x] P4.4f キーバインド: enter (attach — gocui Suspend → tmux attach → Resume)
- [x] P4.4g キーバインド: r (--resume で attach — 構造完成、フラグ伝搬 TODO)
- [x] P4.4h キーバインド: R (リネーム — 構造完成、入力 popup は TODO)
- [x] P4.5 セッション行フォーマット (`presentation/sessions.go`)
- [x] P4.6 PreviewContext (capture-pane → Main パネル表示、detach 後に動作確認済み)
- [x] P4.7 launch (local) — SessionManager.Create → tmux NewSession + claude 起動
- [x] P4.8 attach ループ (gocui Suspend/Resume)
- [x] P4.9 MCP サーバー自動起動 (ensureMCPServer)
- [x] P4.10 専用 tmux ソケット分離 (`-L lazyclaude`)

### Phase 5: Diff / Tool Popup

- [x] P5.1 diff パーサー (ParseUnifiedDiff + FormatDiffLine)
- [x] P5.2 diff gocui 画面 (cmd/lazyclaude/diff.go — スクロール j/k/d/u)
- [x] P5.3 diff 行カラーリング (add=green, del=red, hunk=cyan)
- [x] P5.4 tool 情報パーサー (ParseToolInput + FormatToolLines)
- [x] P5.5 tool gocui 画面 (cmd/lazyclaude/tool.go — y/a/n/Esc)
- [x] P5.6 choice ファイル書き出し (config.Paths 経由)
- [ ] P5.7 MCP サーバーとの接続 (server → display-popup → lazyclaude diff) → P7

### Phase 5.5: TUI 自動テスト基盤

- [x] P5.5.1 termtest 依存 + ヘルパー
- [x] P5.5.2 component test (help, version, diff missing args)
- [x] P5.5.3 integration test (tmux ヘッドレス — diff, tool, server, help)
- [x] P5.5.4 Docker テスト環境 (Dockerfile.test)
- [x] P5.5.5 Docker 内 Claude Code 認証 (CLAUDE_CODE_OAUTH_TOKEN)
- [x] P5.5.6 Docker 内 gocui capture-pane 検証
- [ ] P5.5.7 VHS ゴールデンファイル
- [ ] P5.5.8 GitHub Actions CI ワークフロー

### Phase 6: SSH + 高度な機能

- [ ] P6.1 SSH reverse tunnel
- [ ] P6.2 リモート lock ファイル管理
- [ ] P6.3 設定ファイル (~/.config/lazyclaude/config.toml)
- [ ] P6.4 ヘルプ popup (?)

### Phase 7: lazyclaude.tmux 拡張

- [ ] P7.1 lazyclaude.tmux (TPM エントリポイント, shell 10行以内)
- [ ] P7.2 `lazyclaude setup` サブコマンド
- [ ] P7.3 server --ensure (idempotent 起動)
- [ ] P7.4 MCP server → tmux display-popup 連携
- [ ] P7.5 Claude hooks 自動設定

### Phase 8: 配布・CI/CD

- [ ] P8.1 goreleaser 設定
- [ ] P8.2 GitHub Actions release
- [ ] P8.3 Homebrew Formula
- [ ] P8.4 README.md
