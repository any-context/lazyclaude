# Project Hierarchy Plan

## Overview

Session のフラットリストを 3 階層構造に変更する。

```
Project (git repo root)
├─ PM Session (Project に 1 つ、属性として保持)
├─ Session (Worktree or non-Worktree)
├─ Session
└─ ...
```

## Design Decisions

| 項目 | 決定 |
|------|------|
| 上の階層 | Project (git リポジトリルート単位) |
| 下の階層 | Session (Worktree ベース、非 Worktree も可) |
| PM の位置付け | Project の属性 (`Project.PM *Session`) |
| UI スタイル | ツリー表示 (展開/折りたたみ) |
| Project が 1 つの場合 | 常に Project 階層を表示 (スキップしない) |
| Project 作成 | 暗黙的 (Session 作成時に Path から自動推定・作成) |
| Enter キー | Project 行 = 展開/折りたたみ、Session 行 = fullscreen |
| マイグレーション | リセット (新フォーマットで空から開始) |

## Phase 1: Model Layer

### 1.1 Project struct 追加

```go
// internal/session/project.go
type Project struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`        // リポジトリ名 (e.g. "lazyclaude")
    Path      string    `json:"path"`        // git repo root の絶対パス
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`

    PM       *Session  `json:"pm,omitempty"`       // PM Session (0 or 1)
    Sessions []Session `json:"sessions,omitempty"` // Worker / 通常 Session

    // Runtime state
    Expanded bool `json:"-"` // UI での展開状態
}
```

### 1.2 Store 変更

- `state.json` のフォーマットを `[]Session` → `[]Project` に変更
- 起動時にフォーマットチェック、旧フォーマットなら空の `[]Project` でリセット
- `Add(session)` → Project を Path から推定し、なければ自動作成
- `FindProjectByPath(path) *Project` 追加
- `All()` → `[]Project` を返すように変更
- `AllSessions()` → 全 Project の全 Session をフラットに返す (互換用)

### 1.3 Project 推定ロジック

```go
func InferProjectRoot(sessionPath string) string {
    // Worktree の場合: .claude/worktrees/ より前がプロジェクトルート
    if IsWorktreePath(sessionPath) {
        idx := strings.Index(sessionPath, "/.claude/worktrees/")
        return sessionPath[:idx]
    }
    // 非 Worktree: git rev-parse --show-toplevel で取得
    // fallback: sessionPath そのまま
}
```

## Phase 2: Manager Layer

### 2.1 Manager API 変更

- `Create()` → Session 作成時に Project を自動解決
- `CreatePMSession()` → 対象 Project の PM フィールドにセット
- `CreateWorkerSession()` → 対象 Project の Sessions に追加
- `Delete()` → Session 削除後、Project が空なら Project も削除
- `Projects()` → `[]Project` を返す新メソッド

### 2.2 Sync 変更

- `SyncWithTmux()` は全 Project の全 Session を走査して同じロジックを適用

## Phase 3: GUI Layer

### 3.1 ツリー表示データモデル

```go
// internal/gui/app.go
type TreeNode struct {
    Kind      TreeNodeKind // ProjectNode or SessionNode
    ProjectID string
    Session   *SessionItem // nil if ProjectNode
    Depth     int          // 0 = Project, 1 = Session
}

type TreeNodeKind int
const (
    ProjectNode TreeNodeKind = iota
    SessionNode
)
```

`App` のカーソルは `[]TreeNode` のインデックスで管理。

### 3.2 レンダリング

```
▼ lazyclaude                    ← Project (expanded)
  ● pm                  [PM]    ← PM Session
  ● feat-auth           [W]     ← Worker
  ○ fix-bug             [W]     ← Worker (Detached)
▶ my-api                        ← Project (collapsed)
```

- `▼` / `▶` で展開/折りたたみ状態を表示
- Session は 2 スペースインデント
- PM Session には `[PM]` アイコン
- Worktree Session には `[W]` アイコン

### 3.3 キーバインド変更

| キー | Project 行 | Session 行 |
|------|-----------|------------|
| Enter | 展開/折りたたみ | fullscreen |
| n | 新 Session をこの Project に作成 | 同 Project に新 Session |
| d | Project 削除 (全 Session) | Session 削除 |
| e | (なし) | fullscreen |

### 3.4 SessionProvider インターフェース変更

```go
type SessionProvider interface {
    Projects() []ProjectItem    // 新規: Project 一覧
    Sessions() []SessionItem    // 既存: 互換用 (全セッションフラット)
    // ... 既存メソッド
}
```

```go
type ProjectItem struct {
    ID       string
    Name     string
    Path     string
    PM       *SessionItem
    Sessions []SessionItem
}
```

## Phase 4: Adapter Layer

### 4.1 sessionAdapter 変更 (cmd/root.go)

- `Projects()` 実装: `Manager.Projects()` → `[]gui.ProjectItem` 変換
- `buildProjectItems()` ヘルパー追加

## Phase 5: Tests

- `Store` のテスト: Project CRUD、Session の自動 Project 振り分け
- `Manager` のテスト: Create/Delete で Project が正しく管理されるか
- GUI レンダリングのテスト: ツリー表示の正しさ
- Project 推定ロジックのテスト: Worktree / 非 Worktree パス

## Implementation Order

1. Phase 1 (Model) → Phase 5 (Tests for Model)
2. Phase 2 (Manager) → Phase 5 (Tests for Manager)
3. Phase 3 + 4 (GUI + Adapter) → Phase 5 (Tests for GUI)

各 Phase は TDD: テスト先行で実装する。

## Out of Scope

- SSH セッションの Project 推定 (リモートの git root 取得は複雑)
  - 暫定: Host + Path で Project 名を生成
- Project 間のセッション移動
- Project のリネーム UI
