# lazyclaude — Standalone Claude Code TUI

**作成日**: 2026-03-17
**改訂日**: 2026-03-20 (v4: 実装状況を反映、アーキテクチャ変更を記録)

---

## コンセプト

lazygit が git の standalone TUI であるように、**lazyclaude** は Claude Code の standalone TUI。

### 2 層アーキテクチャ

| 層                          | 何ができるか                                                            | tmux 必要? |
| --------------------------- | ----------------------------------------------------------------------- | ---------- |
| **lazyclaude (standalone)** | セッション管理 TUI、diff/tool ビューア、MCP サーバー                    | Yes        |
| **lazyclaude.tmux (拡張)**  | keybind 登録、作業中ペインへの自動 popup 割り込み、MCP サーバー自動起動 | TPM        |

### popup の実現方式 (設計変更)

当初計画では `tmux display-popup` で別プロセスとして popup を表示する予定だった。
実装では **gocui オーバーレイ popup** を採用:

```
Claude Code → MCP server → notification queue (file-based)
                                    ↓
                        GUI ticker (100ms) polling
                                    ↓
                        PopupController.Push() → cascade overlay
                                    ↓
                        User choice → choice file → MCP server → Claude Code
```

利点:
- 別プロセス起動のオーバーヘッドがない
- 複数 popup をスタックして同時表示できる (cascade)
- gocui の state machine で一貫管理

---

## アーキテクチャ概要

### 起動モデル

```
lazyclaude                    # メイン TUI (セッション一覧 + プレビュー + full-screen)
lazyclaude server             # MCP サーバーデーモン
lazyclaude diff <args>        # diff ビューア (サブプロセスモード)
lazyclaude tool <args>        # tool 確認ビューア (サブプロセスモード)
lazyclaude setup              # tmux keybind + hook 登録 (未実装)
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
│  │  tmux client  |  config  |  notify (file-based queue)            │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└─────────────┬──────────────────────┬───────────────────────────────────┘
              │                      │
        ┌─────┴─────┐         ┌─────┴──────┐
        │   tmux    │         │ Claude Code│
        └───────────┘         └────────────┘
```

---

## 技術スタック

| ライブラリ             | 用途                                   |
| ---------------------- | -------------------------------------- |
| `jesseduffield/gocui`  | TUI フレームワーク (lazygit 同一 fork) |
| `nhooyr.io/websocket`  | MCP WebSocket サーバー                 |
| `spf13/cobra`          | CLI サブコマンド                       |
| `charmbracelet/x/ansi` | ANSI カラー処理                        |
| stdlib                 | HTTP, JSON, exec, crypto, net          |

---

## パッケージ設計 (現在の実装)

```
lazyclaude/
├── cmd/lazyclaude/
│   ├── main.go                 エントリポイント
│   ├── root.go                 cobra root (引数なし → メイン TUI)
│   ├── server.go               `lazyclaude server` サブコマンド
│   ├── diff.go                 `lazyclaude diff` サブコマンド
│   ├── tool.go                 `lazyclaude tool` サブコマンド
│   └── setup.go                `lazyclaude setup` (未実装 placeholder)
│
├── internal/
│   ├── core/
│   │   ├── tmux/
│   │   │   ├── client.go       interface Client (9 methods)
│   │   │   ├── types.go        ClientInfo, WindowInfo, PaneInfo
│   │   │   ├── exec.go         ExecClient (subprocess)
│   │   │   ├── control.go      ControlClient (persistent socket)
│   │   │   ├── pidwalk.go      PID → window 解決
│   │   │   └── mock.go         テスト用 MockClient
│   │   └── config/
│   │       └── config.go       Paths (データディレクトリ, ランタイムディレクトリ)
│   │
│   ├── server/
│   │   ├── server.go           HTTP/WebSocket サーバー
│   │   ├── handler.go          MCP リクエストハンドラ
│   │   ├── jsonrpc.go          JSON-RPC 2.0 プロトコル
│   │   ├── state.go            接続状態, PID→Window マッピング
│   │   └── lock.go             IDE lock ファイル管理
│   │
│   ├── gui/
│   │   ├── choice/             Choice 型 + ファイル I/O
│   │   ├── keymap/             AppState, KeyAction, KeyBinding, Registry
│   │   ├── presentation/       diff/tool/session フォーマット
│   │   ├── app.go              App struct, NewApp, Run, event loop
│   │   ├── state.go            transition(), enterFullScreen, forwardKey
│   │   ├── keybindings.go      gocui キー登録 (state-aware)
│   │   ├── layout.go           レイアウト + SideTab/TabBar
│   │   ├── popup.go            popup rendering + App 委譲
│   │   ├── popup_controller.go PopupController (独立テスト可能)
│   │   └── input.go            InputForwarder + inputEditor
│   │
│   ├── session/
│   │   ├── store.go            JSON 永続化 (state.json)
│   │   ├── manager.go          セッション CRUD + tmux 同期
│   │   └── gc.go               バックグラウンド GC (orphan 検出)
│   │
│   └── notify/
│       └── notify.go           通知キュー (file-based, timestamp 順)
│
├── tests/
│   ├── cli/                    CLI 出力テスト (termtest)
│   ├── integration/            TUI tmux テスト (Go + bash)
│   │   ├── scripts/            E2E bash スクリプト
│   │   ├── fullscreen_test.go  フルスクリーン + popup E2E
│   │   ├── popup_test.go       diff/tool サブコマンド E2E
│   │   └── tmux_test.go        tmux ヘルパー
│   └── testdata/               テスト用データファイル
│
├── Dockerfile.test             Docker テスト環境
├── Makefile
├── go.mod
└── go.sum
```

### 計画からの主要な設計変更

| 計画 | 実装 | 理由 |
|------|------|------|
| `gui/context/` (ContextMgr スタック) | 削除 (dead code) | popup は PopupController で独立管理。Context の OnFocus/OnBlur は不要だった |
| `gui/controllers/` (Controller 分離) | App メソッドとして統合 | Go ではサブパッケージ分離で circular import が発生。App メソッドの方がシンプル |
| `gui/theme.go`, `gui/options.go` | 未実装 | 現時点では不要。layout.go 内に直接記述 |
| `server/ws.go` (別ファイル) | `server/server.go` に統合 | WebSocket は HTTP ハンドラの一部として統合 |
| `server/popup.go` (popup 起動) | `notify/notify.go` (file-based queue) | display-popup ではなく gocui overlay を採用 |
| `session/launch.go`, `ssh.go`, `attach.go` | `session/manager.go` に統合 | SSH 未実装、launch は Create に統合 |
| `internal/integration/` (lazygit 方式テスト) | `tests/integration/` + `tests/cli/` | tmux scripting + termtest の方がシンプル |
| `KeyMap` (data-only) | `keymap.Registry` (state-aware dispatch) | 状態ごとの有効キーを宣言的に管理 |
| `fullScreen` + `inputMode` (boolean) | `AppState` enum (state machine) | 組み合わせ爆発を防止、transition() で副作用一元管理 |

---

## Phase 進捗一覧

| Phase | 目標 | 状態 | 備考 |
|-------|------|------|------|
| **P0** | プロジェクト初期化 + PoC | **完了** | go mod, cobra, Dockerfile.test |
| **P1** | コア層 (tmux + config) | **完了** | Client interface, ExecClient, ControlClient, MockClient, pidwalk |
| **P2** | MCP サーバー | **完了** | WebSocket, JSON-RPC, handler, state, lock |
| **P3** | TUI フレームワーク | **完了** | App, layout, keybindings, state machine, KeyRegistry |
| **P4** | メイン画面 | **完了** | セッション一覧, プレビュー, full-screen, cursor sync, MCP 自動起動 |
| **P5** | Diff / Tool Popup | **完了** | diff.go, tool.go, popup stack, cascade, dismiss |
| **P5+** | Popup 拡張 | **完了** | 通知キュー, popup stack, suspend/reopen, Y accept-all |
| **P6** | SSH + 高度な機能 | **一部完了** | SSH コマンド構築 + reverse tunnel + Docker テスト基盤 |
| **P7** | lazyclaude.tmux 拡張 | **未着手** | setup.go は placeholder |
| **P8** | 配布・CI/CD | **未着手** | goreleaser, GitHub Actions なし |

---

## Phase 4: 残タスク

| # | タスク | 状態 |
|---|--------|------|
| P4.1 | Session 型 + Store (JSON 永続化) | **完了** |
| P4.2 | SessionManager (CRUD + tmux 同期) | **完了** |
| P4.3 | セッションリスト表示 + カーソル | **完了** |
| P4.4 | キーバインド (state-aware KeyRegistry) | **完了** |
| P4.5 | セッション行フォーマット | **完了** |
| P4.6 | プレビュー (async capture-pane) | **完了** |
| P4.7 | Full-screen モード (INSERT/NORMAL) | **完了** |
| P4.8 | Cursor sync (tmux pane cursor → gocui) | **完了** |
| P4.9 | IME 入力の順序保証 (serial key queue) | **完了** |
| P4.10 | MCP サーバー自動起動 | **完了** |
| P4.11 | Control mode (event-driven refresh) | **完了** |

### 追加実装 (計画外)

| 機能 | 実装 |
|------|------|
| Popup stack (複数 popup 同時管理) | PopupController + cascade overlay |
| Notification queue (file-based) | notify.Enqueue/ReadAll (nanosecond timestamp) |
| Suspend/Reopen popup (Esc/p) | PopupController.SuspendAll/UnsuspendAll |
| Accept-all (Y) | PopupController.DismissAll |
| Mouse scroll in full-screen | fullScreenScrollY + SetOrigin |
| Steady block cursor (no blink) | ANSI `\033[2 q` |
| EnsureServer (health check) | server.EnsureServer + TCP alive check |
| Config env var overrides | LAZYCLAUDE_DATA_DIR, RUNTIME_DIR, IDE_DIR |
| Server E2E tests | tests/integration/server_test.go (5 tests) |

---

## Phase 6: SSH + 高度な機能

| # | タスク | 状態 | 成果物 |
|---|--------|------|--------|
| P6.1 | SSH reverse tunnel 構築 | **完了** | `internal/session/ssh.go` |
| P6.2 | リモート lock ファイル管理 | **完了** | ssh.go 内 buildRemoteCommand (自動作成 + trap 削除) |
| P6.3 | Docker SSH テスト基盤 | **完了** | `Dockerfile.ssh-test`, `docker-compose.ssh.yml`, `tests/integration/ssh_test.go` |
| P6.4 | 設定ファイル (~/.config/lazyclaude/config.toml) | **未着手** | `internal/core/config/` 拡張 |
| P6.5 | ヘルプ popup (?) | **未着手** | 未定 |
| P6.6 | Keybinding ユーザーカスタマイズ | **未着手** | `keymap.Registry.LoadOverrides()` |

### SSH 実装の詳細

```
lazyclaude (local)                          remote host
┌─────────────────────┐                    ┌──────────────────────┐
│ MCP Server :PORT    │◄── SSH -R PORT ────│ Claude Code          │
│                     │    reverse tunnel   │ reads ~/.claude/ide/ │
│ GUI polls notify    │                    │ connects to :PORT    │
└─────────────────────┘                    └──────────────────────┘
```

- `buildSSHCommand()`: SSH + PTY + reverse tunnel + keepalive
- `buildRemoteCommand()`: lock file 作成 → claude 起動 → trap で lock 削除
- `splitHostPort()`: `user@host:port` を分離 (IPv6 対応)
- `Manager.readMCPInfo()`: port file + lock file から MCP 接続情報を取得

**P6.6 の基盤**: KeyRegistry は `AllActions()` でバインド一覧を返せるので、
TOML/JSON からオーバーライドを読み込む拡張は容易。

---

## Phase 7: lazyclaude.tmux 拡張 (未着手)

| # | タスク | 成果物 |
|---|--------|--------|
| P7.1 | lazyclaude.tmux エントリポイント | `lazyclaude.tmux` (shell, TPM 用) |
| P7.2 | `lazyclaude setup` サブコマンド | `cmd/lazyclaude/setup.go` |
| P7.3 | server --ensure (idempotent 起動) | `cmd/lazyclaude/server.go` 拡張 |
| P7.4 | Claude hooks 自動設定 | `internal/core/config/hooks.go` |

---

## Phase 8: 配布・CI/CD (未着手)

| # | タスク | 成果物 |
|---|--------|--------|
| P8.1 | goreleaser 設定 | `.goreleaser.yml` |
| P8.2 | GitHub Actions CI | `.github/workflows/ci.yml` |
| P8.3 | GitHub Actions release | `.github/workflows/release.yml` |
| P8.4 | golangci-lint 設定 | `.golangci.yml` |
| P8.5 | Homebrew Formula | `homebrew-lazyclaude/` |
| P8.6 | README.md | standalone + tmux 拡張 両方 |

---

## テスト戦略 (現在の実装)

| 層 | 対象 | 手法 | 実行環境 |
|----|------|------|----------|
| L1: Unit | core, server, session, gui | `go test` + mock | Host + Docker |
| L2: GUI headless | TUI 状態遷移 + keybinding | gocui headless (SimulationScreen) | Host + Docker |
| L3: CLI | サブコマンド出力 | termtest (PTY emulation) | Docker |
| L4: Integration | tmux popup + MCP + choice | tmux scripting (send-keys + capture-pane) | Docker |
| L5: E2E scripts | 全体動作 (popup stack, IME, latency) | bash (tmux + capture-pane) | Docker |
| L6: Server E2E | MCP サーバーバイナリ起動 + WS + notify | Go binary + WebSocket client | Host + Docker |
| L7: SSH E2E | SSH reverse tunnel + MCP 通信 | docker-compose 2 コンテナ | Docker only |

### テストカバレッジ (2026-03-20 v2)

| パッケージ | カバレッジ |
|-----------|-----------|
| core/config | 93.8% |
| gui/keymap | 91.7% |
| gui/presentation | 91.6% |
| gui/choice | 90.9% |
| server | 85.2% |
| session | 80.0% |
| notify | 78.8% |
| gui (main) | 43.8% |
| core/tmux | 35.5% |

---

## コードメトリクス (2026-03-20 v2)

| 指標 | 値 |
|------|-----|
| Go ファイル数 | 79 |
| 総 LOC | ~11,500 |
| gui/ ファイル数 (直下) | 10 ソース + 8 テスト |
| gui/ サブパッケージ | choice, keymap, presentation |
| ユニットテスト数 | 250+ |
| Server E2E テスト数 | 5 (Go binary lifecycle) |
| SSH E2E テスト数 | 5 (Docker Compose, REMOTE_HOST) |
| TUI E2E テスト数 | 12 (Go) + 9 (bash) |

---

## リスクと対策

| リスク | 影響 | 状態 | 対策 |
|--------|------|------|------|
| gocui が tmux display-popup で動作しない | P5 | **解決** | display-popup は不使用、gocui overlay で実装 |
| Claude Code WebSocket 互換性 | P2 | **解決** | MCP サーバー動作確認済み |
| SSH reverse tunnel タイミング | P6 | **解決** | buildSSHCommand + Docker 2 コンテナ E2E テスト |
| goroutine リーク | P2 | **対策済み** | context.Context + done channel パターン |
| メイン TUI ↔ full-screen 切り替え | P4 | **解決** | AppState state machine + transition() |
| IME 入力の順序破壊 | P4 | **解決** | serial key forwarding queue (buffered channel) |
| Popup の notification loss | P5 | **解決** | file-based queue (nanosecond timestamp) |
