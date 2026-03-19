# GUI Large-Scale Refactoring Plan

## Requirements Restatement

lazyclaude の `internal/gui` パッケージを、以下の拡張に耐えられるよう再設計する:

1. **Keymapping system**: ユーザーが任意のキーマップで上書きでき、キーマップ一覧を参照できる単一実体
2. **Mode 遷移**: 状態と UI の遷移を簡潔にまとめる (散在する boolean ではなく state machine)
3. **SoC / DI**: 責務の明確な分離、テスタブルな依存注入

同時に dead code / 冗長コードを削除する。

---

## Current Pain Points

| Issue | Location | Impact |
|-------|----------|--------|
| 散在する状態ガード | keybindings.go 全ハンドラ | 全ハンドラに `if a.hasPopup() \|\| a.fullScreen` の重複 |
| Boolean で mode 管理 | app.go: `fullScreen`, `inputMode`, `hasPopup()` | 組み合わせ爆発、バグの温床 |
| context/ package 未使用 | context/base.go, context/manager.go | Dead code (テストでのみ参照) |
| notify.Write/Read 未使用 | notify/notify.go L37-71 | Enqueue/ReadAll に置換済み |
| suspendActivePopup 未使用 | popup_stack.go | テストでのみ使用、プロダクションで未使用 |
| popupViewNames 未使用 | popup_stack.go | テストでのみ使用、プロダクションで未使用 |
| setup.go placeholder | cmd/lazyclaude/setup.go | "not yet implemented" のまま |
| keybindings.go 肥大化 | 412 LOC | popup/fullscreen 用に global + view-specific 二重登録 |
| KeyMap 参照不完全 | keymap.go | `FirstRune`/`FirstKey` は最初のバインドのみ返す、一覧は未対応 |

---

## Architecture: Before vs After

### Before (Current)
```
App (God object)
├── fullScreen bool           ← scattered state
├── inputMode InputMode       ← scattered state
├── popupStack []popupEntry   ← scattered state
├── contextMgr *Manager       ← unused
├── keybindings.go (412 LOC)  ← monolithic, guards everywhere
├── keymap.go                 ← data only, no user override
└── popup.go + popup_stack.go ← mixed rendering + state
```

### After (Target)
```
App (thin coordinator)
├── state AppState              ← single enum: Main|FullInsert|FullNormal
├── popups *PopupStack          ← extracted, self-contained
├── keyRegistry *KeyRegistry    ← single source of truth for all keymaps
├── dispatcher                  ← state-aware key dispatch (no per-handler guards)
└── controllers/
    ├── SessionController       ← session list, CRUD, cursor
    ├── FullScreenController    ← fullscreen enter/exit, preview, scroll
    └── PopupController         ← popup push/dismiss/suspend, choice sending
```

---

## Phase 0: Dead Code Removal

**Goal**: コードベースを小さくしてからリファクタリングに入る

### 0-1. `internal/gui/context/` 削除
- `context/base.go` と `context/manager.go` はプロダクションコードから呼ばれていない
- `app.go` の `contextMgr` フィールドと `ContextMgr()` メソッド削除
- `keybindings.go:60-62` の `contextMgr.Depth()` チェック削除
- `app_test.go` の `ContextMgr()` テスト削除

### 0-2. `notify.Write` / `notify.Read` 削除
- L37-71: 旧シングルファイル方式、`Enqueue`/`ReadAll` に完全置換済み
- `notifyFileName` 定数、`FilePath` 関数も削除

### 0-3. 未使用メソッド削除
- `suspendActivePopup()`: プロダクションでは `suspendAllPopups()` のみ使用
- `popupViewNames()`: プロダクションで未使用
- テストは `suspendAllPopups()` に合わせて更新

### 0-4. `setup.go` 判断
- placeholder のまま残すか削除するか → **残す** (将来の keymapping setup で使用予定)

**削除予定 LOC**: ~180 LOC (context/ 124 + notify 35 + unused methods ~20)

---

## Phase 1: AppState State Machine

**Goal**: 散在する boolean を単一の状態型に統合

### 1-1. `AppState` 定義 (`internal/gui/state.go`)

```go
type AppState int

const (
    StateMain           AppState = iota  // session list + preview
    StateFullInsert                      // full-screen, keys → Claude Code
    StateFullNormal                      // full-screen, vim-like navigation
)
```

### 1-2. 状態遷移メソッド

```go
func (a *App) Transition(to AppState) {
    // exit actions for current state
    // enter actions for new state
    a.state = to
}
```

遷移表:

| From | To | Trigger | Side Effects |
|------|----|---------|--------------|
| Main | FullInsert | Enter on session | set target, clear cache |
| FullInsert | FullNormal | Ctrl+\ | (none) |
| FullNormal | FullInsert | `i` | (none) |
| FullInsert | Main | Ctrl+D | clear target |
| FullNormal | Main | `q` | clear target |

### 1-3. ハンドラからガードを除去

Before:
```go
if a.hasPopup() || a.fullScreen { return nil }
if a.mode != ModeMain { return nil }
```

After:
```go
// StateMain ハンドラにのみ登録 → ガード不要
```

### 1-4. Popup はオーバーレイ (状態外)

Popup は AppState とは独立。Popup が表示されている間はキー入力をインターセプトする。
これは dispatcher レベルで処理 (Phase 2)。

**影響ファイル**: app.go, mode.go (→ state.go に統合), keybindings.go, layout.go

---

## Phase 2: Action-Based Key Dispatch

**Goal**: keybindings.go の肥大化を解消し、ユーザーカスタマイズ可能な KeyRegistry を導入

### 2-1. `KeyRegistry` (`internal/gui/keyregistry.go`)

```go
type KeyRegistry struct {
    actions map[KeyAction]ActionDef
}

type ActionDef struct {
    Name        string       // human-readable name for help screen
    Description string       // tooltip
    Bindings    []KeyBinding // physical keys
    States      []AppState   // which states this action is active in
    Handler     ActionHandler
}

type ActionHandler func(a *App) error
```

### 2-2. 登録パターン

```go
func (a *App) registerActions() {
    r := a.keyRegistry

    r.Register(ActionQuit, ActionDef{
        Name:     "Quit",
        Bindings: []KeyBinding{{Rune: 'q'}},
        States:   []AppState{StateMain},
        Handler:  func(a *App) error { return gocui.ErrQuit },
    })

    r.Register(ActionEnterFull, ActionDef{
        Name:     "Enter Full Screen",
        Bindings: []KeyBinding{{Key: gocui.KeyEnter}},
        States:   []AppState{StateMain},
        Handler:  a.handleEnterFullScreen,
    })
    // ...
}
```

### 2-3. Dispatch Flow

```
Key event → gocui
         → App.dispatch(key, ch, mod)
            1. Popup visible? → popup handlers のみ
            2. KeyRegistry.Match(key, ch, mod, a.state) → handler
            3. FullInsert? → Editor.forwardAny()
            4. Drop
```

### 2-4. `AllBindings()` for Help Screen

```go
func (r *KeyRegistry) AllBindings() []ActionDef {
    // returns all registered actions with their bindings
    // for displaying a keybinding reference panel
}
```

### 2-5. User Override (将来)

```go
// ~/.config/lazyclaude/keymap.toml
// [overrides]
// quit = ["q", "Q"]
// enter_fullscreen = ["Enter", "l"]

func (r *KeyRegistry) LoadOverrides(path string) error
```

Phase 2 では構造だけ作り、LoadOverrides は未実装。

**影響ファイル**: keybindings.go (大幅削減), keymap.go (→ keyregistry.go に統合)

---

## Phase 3: Controller Extraction (SoC)

**Goal**: App から責務を分離し、テスタブルなコントローラーに

### 3-1. `PopupController` (`internal/gui/popup_controller.go`)

popup.go + popup_stack.go を統合:

```go
type PopupController struct {
    stack     []popupEntry
    focusIdx  int
    choiceFn  func(window string, choice Choice)  // DI: sends choice
}

// Methods: Push, Dismiss, DismissAll, Suspend, Unsuspend, ActiveEntry, etc.
// Rendering: LayoutPopups(g, maxX, maxY) error
```

App から popup 関連フィールドを全て移動。

### 3-2. `FullScreenController` (`internal/gui/fullscreen_controller.go`)

mode.go の fullscreen ロジックを抽出:

```go
type FullScreenController struct {
    target    string
    scrollY   int
    forwarder InputForwarder
    keyQueue  chan keyCmd
}

// Methods: Enter, Exit, ForwardKey, ForwardSpecial, Scroll
```

### 3-3. App の Slim 化

```go
type App struct {
    gui         *gocui.Gui
    state       AppState
    sessions    SessionProvider
    keyRegistry *KeyRegistry
    popups      *PopupController
    fullscreen  *FullScreenController
    cursor      int
    // preview 関連 (async capture)
    preview     *PreviewCache
}
```

**影響ファイル**: app.go, popup.go, popup_stack.go, mode.go, layout.go

---

## Phase 4: Layout Simplification

**Goal**: layout.go を状態ベースの dispatch に簡素化

### 4-1. State-Based Layout

```go
func (a *App) layout(g *gocui.Gui) error {
    maxX, maxY := g.Size()
    a.detectResize(maxX, maxY)

    switch a.state {
    case StateMain:
        a.layoutMain(g, maxX, maxY)
    case StateFullInsert, StateFullNormal:
        a.layoutFullScreen(g, maxX, maxY)
    }

    // Popup overlay (independent of state)
    return a.popups.Layout(g, maxX, maxY)
}
```

### 4-2. ModeDiff / ModeTool の扱い

`ModeDiff` / `ModeTool` は cmd/lazyclaude/diff.go, tool.go のサブプロセスモード。
これらは AppState の一部ではなく、別プロセスなので **そのまま維持** (AppMode として残す)。

---

## Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|------------|
| keybinding 回帰 | HIGH | E2E テスト (Docker) で全キー動作確認 |
| popup 表示崩れ | MEDIUM | popup_stack_test.go の更新 |
| fullscreen cursor 位置ずれ | MEDIUM | 手動テスト (Docker) |
| state machine 遷移漏れ | MEDIUM | 遷移テスト追加 |

---

## Execution Order

```
Phase 0 (dead code)     → 独立、すぐ実行可能
Phase 1 (state machine) → Phase 0 後
Phase 2 (key dispatch)  → Phase 1 後 (state に依存)
Phase 3 (controllers)   → Phase 2 後
Phase 4 (layout)        → Phase 3 後
```

各フェーズ後に `go test ./internal/gui/ -count=1` で回帰確認。
Phase 2 以降は E2E テスト (Docker) で動作確認。

---

## Success Criteria

- [ ] Dead code 削除: ~180 LOC 削減
- [ ] keybindings.go: 412 LOC → ~100 LOC (action registration のみ)
- [ ] App struct フィールド数: 17 → ~10
- [ ] 全ハンドラからガード条件 (`if a.hasPopup() || a.fullScreen`) 除去
- [ ] `KeyRegistry.AllBindings()` でキーマップ一覧取得可能
- [ ] 既存テスト全 PASS
- [ ] E2E テスト全 PASS
