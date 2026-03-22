# Implementation Plan: Worktree Command

## Overview

`w` キーで worktree 作成ダイアログを表示。2つの入力ダイアログ (ブランチ名 + プロンプト) を上下に同時表示し、Tab で切り替え。Enter で確定すると `.claude/worktrees/<name>/` を作成し、Claude Code を初期プロンプト付きで起動。

前提として、既存の入力ダイアログ (rename-input) に通知ポップアップとの競合バグがあるため、先にダイアログ管理を修正する。

## 既知バグ: 入力ダイアログとポップアップの競合

### 症状
rename-input 表示中に通知ポップアップが来ると:
1. `layoutToolPopup` が `SetCurrentView` をポップアップ view に変更
2. rename-input のフォーカスが奪われる
3. ポップアップ dismiss 後、`layoutMain` が `SetCurrentView("sessions")` に戻す
4. rename-input が画面に残留するがフォーカスが来ない (ゾンビ状態)

### 根本原因
`layoutMain` のフォーカス制御が `renameSessionID` フラグのみでガードしているが、`layoutToolPopup` がフォーカスを上書きする。入力ダイアログの存在を統一的に管理する仕組みがない。

### 修正方針 (Phase 0)
`DialogKind` 型定数で統一管理:
```go
type DialogKind int
const (
    DialogNone DialogKind = iota
    DialogRename
    DialogWorktree
)
```

- `layoutMain` と `layoutToolPopup` が `activeDialog` を参照
- `activeDialog != DialogNone` の場合、フォーカスはダイアログ view に留まる
- ポップアップは背後に描画されるが操作不可 (ダイアログが優先)

### 既知の制約 (ドキュメント化)
ダイアログの view-specific keybinding は gocui が直接処理するため、Dispatcher の priority chain (popup > fullscreen > panel > global) をバイパスする。rename-input も同じ。将来 Dispatcher にダイアログレベルハンドラを追加する場合に要検討。

## 挙動

1. Sessions パネルで `w` 押下 → 2つの入力ダイアログ表示
2. 上: ブランチ名 (例: `fix-popup`)、下: 初期プロンプト (ユーザーのタスク説明)
3. Tab でダイアログ間を切り替え
4. **Enter** で確定 (Enter はプロンプト内で改行入力に使用):
   - `{project-root}/.claude/worktrees/fix-popup/` を `MkdirAll`
   - システムプロンプト + ユーザープロンプトを結合して `--initial-prompt` に渡す
   - Claude Code セッション起動
5. Esc でキャンセル
6. セッションリストに `[W]` アイコン、プレビュータイトルに `[worktree]` 表示

## 初期プロンプト (システム部分 + ユーザー入力)

```
You are working in an isolated worktree at {worktree-path}.
Your task is scoped to this directory only.
NEVER modify files outside this worktree — {project-root} must remain untouched.
Be careful that any commands you run do not interfere with other worktrees.

---

{user-prompt}
```

### クォート戦略
プロンプトは一時ファイルに書き出して渡す:
1. `os.CreateTemp` で `/tmp/lazyclaude-prompt-XXXX.txt` 作成
2. システム + ユーザープロンプトを書き込み
3. Claude 起動コマンド: `claude --initial-prompt "$(cat /tmp/lazyclaude-prompt-XXXX.txt)"`
4. セッション起動後にファイルは自動削除不要 (Claude が読み取り済み、TTL で GC)

これによりシェルクォート問題 (複数行、日本語、特殊文字) を完全回避。

## 入力ダイアログ UI

```
╭─ Branch ──────────────────────────────╮
│ fix-popup                             │  ← 1行
╰───────────────────────────────────────╯
╭─ Prompt ──────────────────────────────╮
│ Fix the notification popup so that    │
│ it displays correctly when multiple   │  ← 複数行 (高さ 6行程度)
│ popups are stacked                    │
│                                       │
│                                       │
╰───────────────────────────────────────╯
  Enter: create  Tab: switch  Esc: cancel
```

- 2つの独立した gocui view を上下に配置
  - `worktree-branch`: 上段 (高さ1行), `Editable = true`
  - `worktree-prompt`: 下段 (高さ6行), `Editable = true`, `Wrap = true`
- Tab で `SetCurrentView` を他方に切り替え
- **Enter**: プロンプト内で改行挿入 (DefaultEditor が処理)
- **Enter**: 両方の view から content を読み取り確定
- **Esc**: 両方削除してキャンセル
- フォーカス中の view は `FrameColor = Cyan`、非フォーカスは `Default`
- `g.Cursor = true` をダイアログ表示時に設定、`false` を閉じ時に設定

### Tab dispatch 順序の検証 (実装前に確認)
jesseduffield/gocui fork で view-specific binding と Editor.Edit() の優先順:
- view-specific が先 → Tab は正しくフィールド切り替え
- Editor.Edit() が先 → Tab が文字挿入されるため、カスタム Editor が必要
実装前に実機テストで確認する。

## 変更ファイル

| File | Change |
|------|--------|
| `app.go` | `renameSessionID` 維持 + `activeDialog DialogKind` 追加、`SessionProvider.CreateWorktree` |
| `app_actions.go` | `StartWorktreeInput()`, `CreateWorktreeSession(name, prompt)` |
| `layout.go` | ダイアログフォーカス制御統一、`showWorktreeDialog()`/`closeWorktreeDialog()`, Cursor 管理 |
| `popup.go` | `layoutToolPopup` で `activeDialog` チェック追加 |
| `keybindings.go` | `w` rune 登録、`worktree-branch`/`worktree-prompt` view bindings (Enter/Tab/Esc) |
| `keyhandler/actions.go` | `StartWorktreeInput()` 追加 |
| `keyhandler/sessions.go` | `w` キー + OptionsBar 更新 |
| `render.go` | セッションリストに `[W]` アイコン |
| `presentation/style.go` | `IconWorktree` 定数 |
| `session/manager.go` | `CreateWorktree(ctx, name, prompt, projectRoot)` + 一時ファイル書き出し |
| `cmd/lazyclaude/root.go` | `sessionAdapter.CreateWorktree()` |

## Phase 0: 入力ダイアログとポップアップの競合修正

1. `DialogKind` 型定数を追加 (`DialogNone`, `DialogRename`, `DialogWorktree`)
2. `App` に `activeDialog DialogKind` フィールド追加
3. `showRenameInput`: `activeDialog = DialogRename` を設定
4. `closeRenameInput`: `activeDialog = DialogNone` を設定
5. `layoutMain`: `activeDialog != DialogNone` の場合、`SetCurrentView` をダイアログ view に設定
6. `layoutToolPopup`: `activeDialog != DialogNone` の場合、ポップアップにフォーカスを奪わせない
7. `renameSessionID` は対象セッション ID 保持用に維持 (フラグ判定は `activeDialog` に統一)
8. テスト: rename 中に通知が来てもフォーカスが維持されることを確認

## Phase 1: Domain (session/manager.go)

`CreateWorktree(ctx, name, prompt, projectRoot)`:
- 名前バリデーション:
  - 空文字、空白のみ → 拒否
  - `/`, `\`, `..`, `~`, `^`, `:`, `?`, `*`, `[` 含む → 拒否
  - `.lock` で終わる → 拒否
  - `-` で始まる → 拒否
- `os.MkdirAll(worktreePath, 0o755)`
- 既存ディレクトリの場合: 許可 (再利用可能)
- プロンプト一時ファイル書き出し → `--initial-prompt "$(cat /tmp/...)"` で渡す
- 既存の tmux window 作成ロジックを再利用

## Phase 2: GUI Interface

- `SessionProvider.CreateWorktree(name, prompt, projectRoot string) error`
- `AppActions.StartWorktreeInput()`
- `sessionAdapter.CreateWorktree()` (cmd/lazyclaude/root.go)

## Phase 3: Input Dialog UI

- `showWorktreeDialog()`:
  - `worktree-branch`: 上段 (1行), Title = " Branch "
  - `worktree-prompt`: 下段 (6行), Title = " Prompt ", Wrap = true
  - 下部にヒント view (frameless)
  - `g.Cursor = true`
  - `activeDialog = DialogWorktree`
- `closeWorktreeDialog()`:
  - 全 view 削除
  - `g.Cursor = false`
  - `activeDialog = DialogNone`

## Phase 4: Key Bindings

- `w` → `StartWorktreeInput()` (SessionsPanel、rune リストに追加)
- `worktree-branch` view:
  - Tab → `SetCurrentView("worktree-prompt")`
  - Enter → 確定 (両 view 読み取り)
  - Esc → キャンセル
  - Enter → `gocui.DefaultEditor` が処理 (Branch は1行なので no-op)
- `worktree-prompt` view:
  - Tab → `SetCurrentView("worktree-branch")`
  - Enter → 確定
  - Esc → キャンセル
  - Enter → `gocui.DefaultEditor` が処理 (改行挿入)

## Phase 5: Visual Distinction

- `IconWorktree = "\x1b[38;5;214m[W]\x1b[0m"` (オレンジ)
- セッションリスト: `/.claude/worktrees/` in path → `[W]` prefix
- プレビュー: `[worktree] <name>` タイトル

## バリデーション

- ブランチ名: 空、空白のみ、git 不正文字 (`/\..~^:?*[-`) → 拒否
- プロンプト: 空でも可 (システムプロンプトのみで起動)

## Success Criteria

- [ ] rename 中に通知ポップアップが来てもフォーカスが維持される (Phase 0)
- [ ] `w` で 2つの入力ダイアログ表示 (上下)
- [ ] Tab でブランチ名 ↔ プロンプト切り替え
- [ ] Enter でプロンプト内に改行挿入
- [ ] Enter で worktree 作成 + セッション起動
- [ ] Esc でキャンセル
- [ ] `[W]` アイコンがセッションリストに表示
- [ ] プレビュータイトルに `[worktree]` 表示
- [ ] 日本語 + 改行を含むプロンプトが正しく渡される
- [ ] 不正なブランチ名は拒否
- [ ] 既存テスト全通過
