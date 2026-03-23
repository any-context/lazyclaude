# Implementation Plan: Lazygit Launch ('g' Key)

## Overview

SessionsPanel の `g` キーで、選択中セッションのパスで lazygit を起動する。ローカルセッションでは直接 `lazygit` を実行、リモート (SSH) セッションではリモートホスト上で lazygit を起動する。`a` (AttachSession) と同じ Suspend/Resume パターンを採用する。

## Requirements

- SessionsPanel で `g` キーを押すと、選択中セッションの Path で lazygit を起動
- ローカルセッション: `lazygit` を Path ディレクトリで実行
- リモートセッション: `ssh -t host 'cd path && lazygit'` で実行
- gocui の Suspend/Resume を使い、lazygit 終了後に lazyclaude TUI に復帰
- LogsPanel の `g` (cursor to top) は既存のまま維持 (panel dispatch で分離済み)

## Implementation Steps

### Phase 1: Interface Layer (2 files)

1. **`AppActions` に `LaunchLazygit()` を追加** (`internal/gui/keyhandler/types.go`)
   - key handler が GUI 実装に依存せず action を呼び出すため

2. **`SessionProvider` に `LaunchLazygit(path, host string) error` を追加** (`internal/gui/app.go`)
   - lazygit 起動の実装詳細を GUI 層から分離

### Phase 2: Key Binding (1 file)

3. **SessionsPanel に `g` キーハンドラを追加** (`internal/gui/keyhandler/sessions.go`)
   - `HandleKey` の switch に `case ev.Rune == 'g': actions.LaunchLazygit(); return Handled`
   - `OptionsBar()` に `presentation.StyledKey("g", "lazygit")` を追加
   - LogsPanel の `g` (cursor to top) とは panel dispatch で分離されているため競合しない

### Phase 3: Action Implementation (2 files)

4. **`LaunchLazygit()` を App に実装** (`internal/gui/app_actions.go`)
   - `AttachSession()` と同じ Suspend/Resume パターン:
     1. `a.sessions` nil チェック
     2. `items[a.cursor]` で選択中セッションの `Path` と `Host` を取得
     3. `a.gui.Suspend()` で gocui を一時停止
     4. `a.sessions.LaunchLazygit(path, host)` を呼び出し
     5. `a.gui.Resume()` で復帰
     6. エラー時はステータスバーに表示

5. **`sessionAdapter.LaunchLazygit` を実装** (`cmd/lazyclaude/root.go`)
   - ローカル: `exec.Command("lazygit")` を `Dir: path` で実行、stdin/stdout/stderr を接続
   - リモート: `exec.Command("ssh", "-t", host, fmt.Sprintf("cd %s && lazygit", path))` を実行
     - `splitHostPort` を再利用してポート指定にも対応
     - `-t` フラグで TTY 割り当て (lazygit の TUI に必要)

### Phase 4: Unit Tests (4 files)

6. **mock 更新** (`internal/gui/keyhandler/mock_actions_test.go`)
   - `func (m *mockActions) LaunchLazygit() { m.record("LaunchLazygit") }`

7. **mock 更新** (`internal/gui/keydispatch/dispatcher_test.go`)
   - `func (m *mockActions) LaunchLazygit() { m.record("LaunchLazygit") }`

8. **mock 更新** (`internal/gui/app_integration_test.go`)
   - `mockSessionProvider` に `LaunchLazygit` スタブ追加

9. **テストケース追加** (`internal/gui/keyhandler/handler_test.go`)
   - `TestSessionsPanel_Keys` に `{keyhandler.KeyEvent{Rune: 'g'}, "LaunchLazygit"}` を追加

### Phase 5: VHS 可視化 E2E テスト (3 files)

10. **tape 作成** (`vis_e2e_tests/tapes/lazygit.tape`)
    - ローカルセッションで `g` を押して lazygit が起動することを確認
    - lazygit の画面要素 (e.g. `Status`, `Branches`) が表示されることを `Wait+Screen` で検証
    - `q` で lazygit を終了し、lazyclaude TUI に復帰することを確認
    - SSH リモートセッションでの lazygit 起動も検証 (ssh_launch と同様の構成)

11. **entrypoint.sh 更新** (`vis_e2e_tests/entrypoint.sh`)
    - `lazygit` tape 用のセットアップが必要な場合に `case` ブロックを追加
    - lazygit がコンテナ内にインストールされていることを前提 (Dockerfile 更新)

12. **TEST_CATALOG.md 更新** (`vis_e2e_tests/TEST_CATALOG.md`)
    - `lazygit` テープのエントリを追加:
      - 前提: TUI 起動済み、セッションが存在
      - 操作: `g` を押す
      - 期待: lazygit が起動、`q` で終了後 TUI に復帰
      - SSH: リモートセッションでも同様に動作

13. **Dockerfile 更新** (`vis_e2e_tests/Dockerfile`)
    - lazygit パッケージをインストール

## Key Design Decisions

### Why Suspend/Resume instead of tmux display-popup

lazygit は完全な TUI アプリケーションなので、gocui を一時停止してターミナルを完全に渡す必要がある。lazyclaude の popup は別の tmux サーバー (lazyclaude ソケット) で管理されるため、Suspend/Resume が最もシンプル。

### Why `SessionProvider.LaunchLazygit` instead of GUI 直接実行

`exec.Command` の構築を `sessionAdapter` に委譲することで:
- GUI 層のテストが容易 (mock で済む)
- リモート/ローカルの分岐ロジックが GUI に漏れない
- `AttachSession` と同じアーキテクチャパターンを維持

### `g` key の重複について

`g` は LogsPanel で `LogsCursorToTop` にバインドされている。dispatcher は `ActivePanel.HandleKey` を呼ぶため、フォーカス中のパネルのハンドラのみが実行される。両方のバインドは共存可能。

## Risks & Mitigations

| Risk | Level | Mitigation |
|------|-------|------------|
| lazygit 未インストール | Medium | `exec.LookPath("lazygit")` で事前チェック、ステータスバーにメッセージ |
| リモートに lazygit 未インストール | Low | SSH エラーをステータスバーに表示 |
| Suspend/Resume 間の例外 | Medium | `AttachSession` と同じ defer パターンで Resume を保証 |
| リモートパスの特殊文字 | Low | Go の `%q` フォーマットでエスケープ |

## Files Changed (Summary)

| File | Change |
|------|--------|
| `internal/gui/keyhandler/types.go` | `LaunchLazygit()` を `AppActions` に追加 |
| `internal/gui/app.go` | `LaunchLazygit(path, host string) error` を `SessionProvider` に追加 |
| `internal/gui/keyhandler/sessions.go` | `g` キーハンドラ + options bar 更新 |
| `internal/gui/app_actions.go` | `LaunchLazygit()` 実装 (Suspend/Resume) |
| `cmd/lazyclaude/root.go` | `sessionAdapter.LaunchLazygit` 実装 (local/remote) |
| `internal/gui/keyhandler/mock_actions_test.go` | mock スタブ追加 |
| `internal/gui/keydispatch/dispatcher_test.go` | mock スタブ追加 |
| `internal/gui/app_integration_test.go` | mock スタブ追加 |
| `internal/gui/keyhandler/handler_test.go` | テストケース追加 |
| `vis_e2e_tests/tapes/lazygit.tape` | VHS E2E tape 作成 |
| `vis_e2e_tests/entrypoint.sh` | lazygit tape セットアップ追加 |
| `vis_e2e_tests/TEST_CATALOG.md` | lazygit エントリ追加 |
| `vis_e2e_tests/Dockerfile` | lazygit インストール追加 |

## Success Criteria

- [ ] SessionsPanel で `g` を押すとローカルセッションの Path で lazygit が起動する
- [ ] lazygit 終了後に lazyclaude TUI に正常に復帰する
- [ ] リモートセッションで `g` を押すと SSH 経由でリモートホストの lazygit が起動する
- [ ] LogsPanel の `g` (cursor to top) が影響を受けない
- [ ] 全既存ユニットテストが通る
- [ ] 新規テストケースが追加されている
- [ ] VHS E2E テスト (`make test-vhs TAPE=lazygit`) が通る
