# Implementation Plan: Dialog Focus Restore After Popup Dismiss

## Overview

ポップアップが入力ダイアログ中に到着した場合、フォーカスをポップアップに渡す。ポップアップを消費した後、render loop が自動的にダイアログにフォーカスを復帰する。

## 経緯

### `25c474e` 以前の挙動 (元々のバグ)
- ダイアログ中にポップアップが来ると **フォーカスを奪う** (これ自体は正しい)
- ポップアップを消費した後、**フォーカスがダイアログに戻らない**
- 入力ダイアログが画面に残留したまま操作不能になる (ゾンビ状態)

### `25c474e` の修正 (暫定対処)
- `HasActiveDialog()` ガードで **ポップアップにフォーカスを奪わせない** ようにした
- ダイアログは常にフォーカスを保持するが、ポップアップに操作できない

### 問題点
- `25c474e` は根本解決ではなく回避策
- ユーザーは通知に応答するためにダイアログを一度閉じる必要がある

## 設計方針: render-loop driven focus

gocui は毎 render で layout 関数を呼ぶ。フォーカスは `(hasPopup, activeDialog, panel)` の
3つの状態から **導出** できるため、明示的な save/restore は不要。

`layoutMain` のフォーカス決定ロジック:
```
if hasPopup → layoutToolPopup がフォーカス管理 (何もしない)
else if hasActiveDialog → dialogFocusView() にフォーカス
else → panelManager のアクティブパネルにフォーカス
```

唯一の追加状態: `worktreeActiveField string` — Tab で切り替えた worktree ダイアログの
フォーカス位置を記憶 (2つの view を持つため `activeDialog` だけでは特定できない)。

## 変更ファイル

| File | Change |
|------|--------|
| `app.go` | `savedDialogView` → `worktreeActiveField` に変更 |
| `dialog.go` | `dialogFocusView()` のみ残す。`saveDialogFocus()` / `restoreDialogFocus()` 削除 |
| `popup.go` | `!HasActiveDialog()` ガード削除。常にポップアップにフォーカスを渡す |
| `layout.go` | `layoutMain` のフォーカスロジックを 3 分岐に変更。Tab handler で `worktreeActiveField` 設定 |

## Phase 1: `dialogFocusView()` の修正

`worktreeActiveField` を使って worktree ダイアログのフォーカス先を決定:

```go
case DialogWorktree:
    if a.worktreeActiveField != "" {
        return a.worktreeActiveField
    }
    return "worktree-branch"
```

`saveDialogFocus()` / `restoreDialogFocus()` は削除。

## Phase 2: render-loop フォーカス制御

### popup.go
`!HasActiveDialog()` ガードを削除。ポップアップが常にフォーカスを奪う:
```go
// 削除: if !a.HasActiveDialog() {
if _, err := g.SetCurrentView(activeViewName); err != nil && !isUnknownView(err) {
    return err
}
g.Cursor = false
// 削除: }
```

### layout.go (`layoutMain`)
フォーカス決定を 3 分岐に変更:
```go
if a.hasPopup() {
    // layoutToolPopup が管理 — ここでは何もしない
} else if a.HasActiveDialog() {
    viewName := a.dialogFocusView()
    if viewName != "" {
        g.SetCurrentView(viewName)
        if a.activeDialog != DialogWorktreeChooser {
            g.Cursor = true
        }
    }
} else {
    g.SetCurrentView(focusedName)
}
```

## Phase 3: Tab handler で `worktreeActiveField` 更新

`keybindings.go` の worktree Tab バインディング:
```go
// worktree-branch → worktree-prompt
a.worktreeActiveField = "worktree-prompt"

// worktree-prompt → worktree-branch
a.worktreeActiveField = "worktree-branch"
```

`closeWorktreeDialog` で `a.worktreeActiveField = ""` にリセット。

## Phase 4: 不要コード削除

- `savedDialogView` フィールド削除
- `saveDialogFocus()` メソッド削除
- `restoreDialogFocus()` メソッド削除
- `app_actions.go` の restore 呼び出し（もしあれば）削除

## Edge Case

| シナリオ | 挙動 |
|---------|------|
| ポップアップ到着中にダイアログ close | `activeDialog = DialogNone` → 次の render で panel にフォーカス。自動 |
| suspend (Esc) | popup 非表示 → `hasPopup()` false → `layoutMain` がダイアログにフォーカス復帰 |
| unsuspend (p) | popup 再表示 → `layoutToolPopup` がフォーカス奪取 |
| 複数ポップアップ → 1つずつ消費 | 最後の1つを消すまで `hasPopup()` true → popup がフォーカス保持 |

全て render loop が自動処理。明示的な save/restore 不要。

## Success Criteria

- [ ] ポップアップが全4種のダイアログからフォーカスを奪える
- [ ] ポップアップ消費後、ダイアログにフォーカス自動復帰
- [ ] worktree ダイアログで Tab 切替後もフォーカス位置が正確
- [ ] Esc (suspend) でフォーカス復帰、p (unsuspend) で再奪取
- [ ] Editable view ではカーソル点滅、chooser ではカーソルなし
- [ ] ダイアログ close 中にポップアップがあっても問題なし
- [ ] 既存テスト全通過
