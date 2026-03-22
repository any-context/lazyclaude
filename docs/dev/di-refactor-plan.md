# DI Refactoring: Break the App God Object

**Created**: 2026-03-23

## Overview

App struct (25 fields) を focused components に分解。既存テスト全通過を保証しつつ、各コンポーネントを独立テスト可能にする。

## Target Architecture

```
App (coordinator, ~200 lines)
  +-- gui *gocui.Gui              (retained)
  +-- mode, cursor, quitRequested, renameSessionID (retained)
  +-- sessions SessionProvider    (retained, interface)
  +-- fullscreen *FullScreenState (NEW)
  +-- logs *LogsState             (NEW)
  +-- popups PopupManager         (NEW interface)
  +-- preview *PreviewCache       (retained)
  +-- notify *NotifyLoop          (NEW)
  +-- dispatcher, panelManager, keyRegistry (retained)
```

## Phases

### Phase 1: Extract LogsState (低リスク)

4フィールド (`logsCursorY`, `logsSelecting`, `logsSelAnchor`, `logsLineCount`) を `LogsState` に分離。gocui 依存なし。

**新規**: `logs_state.go` (~80行), `logs_state_test.go` (~100行)
**変更**: `app.go` (4フィールド → `logs *LogsState`), `app_actions.go` (delegate), `layout.go` (accessor)

### Phase 2: Extract FullScreenState (最大価値)

5フィールド + 散在ロジック (state.go, input.go, app.go, layout.go) を1ファイルに集約。

**新規**: `fullscreen.go` (~150行), `fullscreen_test.go` (~120行)
**変更**: `app.go` (5フィールド → `fullscreen *FullScreenState`), `state.go` (削除 or 30行), `app_actions.go`, `input.go`, `layout.go`, `export_test.go`

### Phase 3: Interface-ify PopupController (低リスク)

`PopupManager` インターフェースを追加。concrete → interface 型変更のみ。

**変更**: `popup_controller.go` (interface 追加), `app.go` (型変更)

### Phase 4: Extract NotifyLoop (中リスク)

4フィールド (`outputNotify`, `notifyBroker`, `notifyBrokerSub`, `onTick`) + Run() 内ゴルーチンを分離。

**新規**: `notify_loop.go` (~100行)
**変更**: `app.go` (4フィールド → `notify *NotifyLoop`, Run() 簡素化)

### Phase 5: Decouple Layout Rendering (低リスク)

render 関数を App メソッドからパッケージレベル関数に変換。

**新規**: `render.go` (~200行)
**変更**: `layout.go` (557行 → ~330行), `popup.go` (270行 → ~170行)

## File Size Projection

| File | Before | After |
|------|--------|-------|
| app.go | 353 | ~200 |
| layout.go | 557 | ~330 |
| popup.go | 270 | ~170 |
| state.go | 101 | ~30 or deleted |
| **logs_state.go** | - | ~80 |
| **fullscreen.go** | - | ~150 |
| **notify_loop.go** | - | ~100 |
| **render.go** | - | ~200 |

## Success Criteria

- [ ] App struct <= 15 fields (from 25)
- [ ] No file > 400 lines
- [ ] LogsState, FullScreenState independently testable
- [ ] PopupController accessed via PopupManager interface
- [ ] All existing tests pass
- [ ] VHS smoke tape passes
