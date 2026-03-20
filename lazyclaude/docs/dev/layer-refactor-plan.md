# レイヤー再配置リファクタリング計画

**作成日**: 2026-03-21
**ブランチ**: `refactor/crush-patterns` (継続)

---

## 背景

crush 比較リファクタリング (R1-R5) で導入した設計パターンにより、
コード品質は大幅に向上した。しかし、レイヤー責務の配置に問題が残っている。

**拡張シナリオに対する現状の弱点:**

| シナリオ | 現状 | ボトルネック |
|---------|------|------------|
| 新 popup 型 (Confirm, Search) | 弱い | `Push(*ToolNotification)` が notification に結合 |
| 新セッション backend (zellij, screen) | 弱い | `core/choice` が `core/tmux` に依存 |
| 新通知チャネル (desktop notification) | 普通 | `ToolNotification` が infra 層にある |
| TUI 差し替え (bubbletea) | 良い | `presentation/` は framework 非依存 |

---

## 目標パッケージ構成

```
internal/
├── core/                          # 純粋なドメイン型 + 基盤ユーティリティ
│   ├── choice/                    # Choice enum + file I/O (tmux 非依存)
│   │   ├── choice.go              # Choice type, WriteFile, ReadFile
│   │   └── choice_test.go
│   ├── config/                    # Paths, PopupMode, hooks (変更なし)
│   ├── event/                     # Broker[T] (変更なし)
│   ├── lifecycle/                 # Lifecycle (変更なし)
│   ├── model/                     # NEW: ドメインモデル
│   │   ├── notification.go        # ToolNotification, Event
│   │   └── notification_test.go
│   ├── shell/                     # Quote (変更なし)
│   └── tmux/                      # Client interface + Exec/Control/Mock (変更なし)
│
├── adapter/                       # NEW: 外部システムアダプター
│   └── tmuxadapter/               # tmux 固有の choice 配送
│       ├── detect.go              # DetectMaxOption (pane content 解析)
│       ├── detect_test.go
│       ├── sendkeys.go            # SendToPane (tmux send-keys)
│       └── sendkeys_test.go
│
├── gui/                           # TUI レイヤー
│   ├── keymap/                    # KeyBinding, Registry (変更なし)
│   ├── presentation/              # ANSI styling + formatting (変更なし)
│   ├── app.go                     # App struct (import 更新のみ)
│   ├── popup_types.go             # Popup interface (Push 修正)
│   ├── popup_controller.go        # PushPopup のみ (Push 削除)
│   └── (choice/ 削除)             # shim 不要 — 全 caller を core/choice に直接変更
│
├── notify/                        # ファイルベース通知キュー (infra のみ)
│   └── queue.go                   # Enqueue, ReadAll (model.ToolNotification を使用)
│
├── popup/                         # NEW: Popup オーケストレーション
│   ├── orchestrator.go            # PopupOrchestrator (queue + spawn)
│   ├── orchestrator_test.go
│   └── size.go                    # EstimatePopupSize
│
├── server/                        # MCP プロトコル + HTTP (popup 分離後)
│   ├── server.go                  # HTTP/WebSocket (popup.Orchestrator を注入)
│   ├── handler.go                 # JSON-RPC handler (core/choice を直接 import)
│   └── state.go                   # 接続状態 (変更なし)
│
└── session/                       # セッション管理
    ├── manager.go                 # CRUD + Sync (transport 分離後)
    ├── service.go                 # Service interface (変更なし)
    ├── store.go                   # JSON 永続化 (変更なし)
    ├── gc.go                      # GC (変更なし)
    └── transport/                 # NEW: セッション起動の transport 層
        ├── ssh.go                 # buildSSHCommand, buildRemoteCommand, splitHostPort
        ├── local.go               # buildClaudeCommand, claudeEnv, cleanSessionCommands
        └── launcher.go            # SessionLauncher interface
```

---

## Phase 計画

### Phase L1: `gui/choice/` shim 削除

**目標**: 不要な間接層を除去し、`server/ → gui/` の依存パスを断つ

| # | タスク | ファイル |
|---|--------|---------|
| L1.1 | `gui/choice/` の全 caller を `core/choice` に直接変更 | `cmd/lazyclaude/tool.go`, `diff.go`, `root.go` |
| L1.2 | `server/` の import を `core/choice` に変更 | `server/server.go`, `server/handler.go` |
| L1.3 | `gui/choice.go` (gui パッケージ内 alias) を `core/choice` に変更 | `gui/popup.go` |
| L1.4 | `gui/choice/` ディレクトリ削除 | |

**検証**: `go build ./... && go test ./internal/... -count=1`
**リスク**: 低 — import パス変更のみ

---

### Phase L2: `core/choice/` から tmux 依存を分離

**目標**: `core/choice` を純粋なドメイン型に。tmux 操作は adapter 層に移動

| # | タスク | ファイル |
|---|--------|---------|
| L2.1 | `internal/adapter/tmuxadapter/` 作成 | 新規 |
| L2.2 | `DetectMaxOption` を `tmuxadapter/detect.go` に移動 | `core/choice/detect.go` → `adapter/` |
| L2.3 | `SendToPane` を `tmuxadapter/sendkeys.go` に移動 | `core/choice/sendkeys.go` → `adapter/` |
| L2.4 | テストも移動 | `detect_test.go`, `sendkeys_test.go` |
| L2.5 | `core/choice/choice.go` から tmux import 削除 | `core/choice/choice.go` |
| L2.6 | 全 caller の import 更新 | `cmd/`, `server/` |

**検証**: `core/choice` の import に `core/tmux` がないことを確認
**リスク**: 中 — 6 ファイルの import 変更

---

### Phase L3: `ToolNotification` を `core/model/` に移動

**目標**: ドメインモデルを infra 層から分離。popup 拡張の基盤

| # | タスク | ファイル |
|---|--------|---------|
| L3.1 | `internal/core/model/notification.go` 作成 | 新規 |
| L3.2 | `ToolNotification` を `notify/` から `core/model/` に移動 | `notify/notify.go` → `core/model/` |
| L3.3 | `Event` を `core/model/` に移動 | `notify/notify.go` → `core/model/` |
| L3.4 | `notify/` は queue 機能のみに (Enqueue, ReadAll) | `notify/notify.go` (import 更新) |
| L3.5 | 全 caller の import 更新 | `gui/`, `server/`, `cmd/`, `notify/` |
| L3.6 | `PopupController.Push(*ToolNotification)` 削除 | `gui/popup_controller.go` |

**検証**: `notify/` が `core/model` を import、逆方向がないこと
**リスク**: 中 — 機械的だが影響ファイル数が多い (10+)

---

### Phase L4: `PopupOrchestrator` を `server/` から分離

**目標**: HTTP/MCP 層と popup ライフサイクル管理を分離

| # | タスク | ファイル |
|---|--------|---------|
| L4.1 | `internal/popup/` パッケージ作成 | 新規 |
| L4.2 | `PopupOrchestrator` を `popup/orchestrator.go` に移動 | `server/popup.go` → `popup/` |
| L4.3 | `EstimatePopupSize` を `popup/size.go` に移動 | `server/popup.go` → `popup/` |
| L4.4 | テストを移動 | `server/popup_test.go`, `server/popup_queue_test.go` → `popup/` |
| L4.5 | `server.Server` に `*popup.Orchestrator` を注入 | `server/server.go` (constructor 変更) |
| L4.6 | `server/handler.go` の popup 参照を更新 | |

**検証**: `server/` に popup 制御コードがないこと
**リスク**: 中 — server の constructor signature が変わる

---

### Phase L5: `session/transport/` 抽出 (将来)

**目標**: SSH/local transport ロジックを manager から分離

**YAGNI 適用**: 現時点では SSH のみ。2 つ目の transport (container, WSL) が
必要になった時点で実施する。現在は `ssh.go` が既に分離されているため、
構造は準備済み。

| # | タスク | ファイル |
|---|--------|---------|
| L5.1 | `SessionLauncher` interface 定義 | `session/transport/launcher.go` |
| L5.2 | `buildSSHCommand` 等を `transport/ssh.go` に移動 | |
| L5.3 | `buildClaudeCommand`, `claudeEnv` を `transport/local.go` に移動 | |
| L5.4 | `Manager.Create` が `SessionLauncher` を使用 | |

**実施時期**: 2 つ目の transport が必要になった時

---

### Phase L6: `presentation/` パーサー分離 (将来)

**目標**: diff/tool パーサーを presentation から分離し、framework 非依存にする

**YAGNI 適用**: 現在 gocui 以外の rendering backend はない。
bubbletea 移行時に実施する。

| # | タスク | ファイル |
|---|--------|---------|
| L6.1 | `core/diff/parse.go` — `ParseUnifiedDiff`, `DiffLine` | |
| L6.2 | `core/toolinfo/parse.go` — `ParseToolInput`, `ToolDisplay` | |
| L6.3 | `presentation/` は formatting のみに | |

**実施時期**: TUI framework 差し替え時

---

## 依存関係の変化

### Before (問題あり)

```
core/choice ──→ core/tmux       (choice が tmux に依存)
server/ ──→ gui/choice/         (server が gui を import)
notify/ ──→ (ToolNotification 定義元)
gui/ ──→ notify/ToolNotification
server/ ──→ notify/ToolNotification
```

### After (L1-L4 完了後)

```
core/model  ← 全パッケージが依存 (ドメインモデル)
core/choice ← enum + file I/O のみ (tmux 非依存)

adapter/tmuxadapter → core/tmux, core/choice  (tmux 固有の配送)
popup/              → core/tmux, core/model    (popup ライフサイクル)
notify/             → core/model               (queue infra のみ)
server/             → core/*, popup/, notify/  (gui/ 非依存)
gui/                → core/*, notify/          (server/ 非依存)
session/            → core/*                   (変更なし)
```

---

## 実施順序と依存関係

```
L1 (shim 削除) ─→ L2 (tmux 分離) ─→ L3 (model 抽出) ─→ L4 (popup 分離)
                                                           ↓
                                                    L5, L6 (将来)
```

- L1 は独立、最も低リスク
- L2 は L1 完了後 (gui/choice 削除済みが前提)
- L3 は L2 と並行可能だが、L2 完了後が理想
- L4 は L3 完了後 (model.ToolNotification を使用するため)
- L5, L6 は YAGNI — 必要になるまで延期

---

## メトリクス目標

| 指標 | 現在 | 目標 (L4 完了後) |
|------|------|-----------------|
| `core/` パッケージ数 | 6 | 7 (+model) |
| `core/choice` の import 数 | 2 (config, tmux) | 1 (config のみ) |
| `server/` の gui import | 1 (gui/choice) | 0 |
| `notify/` のドメイン型 | 2 (ToolNotification, Event) | 0 (queue のみ) |
| `server/popup.go` 行数 | ~200 | 0 (popup/ に移動) |
| `gui/choice/` ファイル数 | 6 | 0 (削除) |

---

## リスクと対策

| リスク | 影響 | 対策 |
|--------|------|------|
| import 変更漏れ | ビルド失敗 | 各 Phase 後に `go build ./...` |
| テスト移動時の package 名不一致 | テスト失敗 | `_test` suffix の確認 |
| circular import | ビルド失敗 | 各 Phase で `go list -deps` 確認 |
| `PopupController.Push` 削除の影響 | 既存テスト失敗 | テスト内の `Push` を `PushPopup(NewToolPopup(n))` に置換 |
| VHS tape / E2E script の破壊 | CI 失敗 | L1-L4 は internal のみ、外部 I/F 不変 |
