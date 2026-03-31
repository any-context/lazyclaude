# lazyclaude

[English](README.md) | **日本語**

> [lazygit](https://github.com/jesseduffield/lazygit) にインスパイアされた、複数の [Claude Code](https://docs.anthropic.com/en/docs/claude-code) セッションを管理する TUI。

ライブプレビュー、権限プロンプトのポップアップ、スクロールバック閲覧、SSH リモートセッション -- すべてを一つの tmux popup から。

<p align="center">
  <img src="docs/images/hero.gif" alt="lazyclaude demo" width="800">
</p>

---

## なぜ lazyclaude？

Claude Code は強力ですが、複数セッションの管理は大変です:

- **コンテキストの切り替え** -- 他のセッションの状況を見るには `tmux select-window` が必要
- **権限プロンプトがブロックする** -- 承認するには適切なウィンドウに適切なタイミングでいる必要がある
- **全体像がない** -- どのセッションが実行中、アイドル、入力待ちなのか分からない

lazyclaude は、全セッションを一覧表示し、権限プロンプトをポップアップとして配信し、どこからでも承認できる単一の TUI でこれらを解決します。

## 機能

**セッション管理**
- Claude Code セッションの作成、リネーム、削除、アタッチ
- セッション一覧を離れずに任意のセッションの出力をライブプレビュー
- プロジェクト単位の折りたたみ可能なツリー表示

**アクティビティ追跡**
- 全セッションのリアルタイム 5 段階ステータス:
  `?` 実行中 | `!` 入力待ち | `✓` アイドル | `✗` エラー | `×` 終了
- Claude Code hooks による自動更新（設定不要）

**権限プロンプト**
- ツール承認ポップアップがオーバーレイとして表示 -- ウィンドウ切り替え不要
- ワンキー承認: `y` 許可、`a` 常に許可、`n` 拒否
- 複数セッションが入力待ちの場合、`Left`/`Right` でポップアップを切り替え
- SSH トンネル経由でも動作

**フルスクリーンモード**
- Claude Code へのキーボード直接転送（透過パススルー）
- vim ライクなスクロールバックブラウザ（`Ctrl+V` またはマウスホイール）
- 行単位のビジュアル選択とクリップボードコピー（`v` で選択、`y` でコピー）

**検索とナビゲーション**
- fzf スタイルの `/` フィルター（セッション、プラグイン、MCP サーバーの各パネル対応）
- `?` Telescope スタイルのキーバインドヘルプオーバーレイ
- `Tab` / `Shift+Tab` でパネル切り替え

**インフラ**
- `display-popup` 経由の tmux プラグイン統合（`Ctrl+\` でトグル）
- SSH リモートセッション（通知用の自動リバーストンネル）
- Claude Code IDE 自動検出用の組み込み MCP サーバー
- PM/Worker マルチエージェントオーケストレーション対応

---

## 要件

- Go 1.25+
- tmux >= 3.4（`display-popup -b rounded` に必要）
- [Claude CLI](https://docs.anthropic.com/en/docs/claude-code)

## インストール

### ソースからビルド

```bash
git clone https://github.com/any-context/lazyclaude ~/.local/share/tmux/plugins/lazyclaude
cd ~/.local/share/tmux/plugins/lazyclaude
make install PREFIX=~/.local
```

### [TPM](https://github.com/tmux-plugins/tpm) を使う場合

`.tmux.conf` に追加:

```tmux
set -g @plugin 'any-context/lazyclaude'
```

`prefix + I` でインストール。プラグインは `Ctrl+\` で lazyclaude を tmux popup として開くキーバインドを登録します。

### スタンドアロン（tmux プラグインなし）

```bash
lazyclaude
```

---

## キーバインド

### セッションパネル

| キー | アクション |
|------|-----------|
| `j` / `k` | セッション間の移動 |
| `n` | 新規セッション作成 |
| `d` | セッション削除 |
| `Enter` | フルスクリーンモード |
| `a` | アタッチ（tmux 直接接続） |
| `R` | リネーム |
| `D` | 孤立セッションの一括削除 |

### フルスクリーンモード

| キー | アクション |
|------|-----------|
| `Ctrl+\` / `Ctrl+D` | フルスクリーン終了 |
| `Ctrl+V` / マウスホイール | スクロールモード開始 |
| その他のキー | Claude Code に転送 |

### スクロールモード（フルスクリーン内）

| キー | アクション |
|------|-----------|
| `j` / `k` | 1 行ずつスクロール |
| `J` / `K` / `PgUp` / `PgDn` | 半ページスクロール |
| `g` / `G` | 先頭 / 末尾にジャンプ |
| `v` | 行単位のビジュアル選択切り替え |
| `y` | 選択範囲をクリップボードにコピー |
| `Esc` / `q` | スクロールモード終了 |

### ポップアップ（権限プロンプト）

| キー | アクション |
|------|-----------|
| `y` | 許可 |
| `a` | 常に許可 |
| `n` | 拒否 |
| `Y` | 全ての保留中を許可 |
| `j` / `k` | スクロール（diff 表示） |
| `Left` / `Right` | ポップアップ間の切り替え |
| `Esc` | ポップアップを非表示 |

### グローバル

| キー | アクション |
|------|-----------|
| `?` | キーバインドヘルプオーバーレイ |
| `/` | 現在のパネルで検索フィルター |
| `Tab` / `Shift+Tab` | パネルフォーカスの切り替え |
| `p` | 非表示のポップアップを復元 |
| `q` / `Ctrl+C` | 終了 |

---

## アーキテクチャ

```
+---------------------------+       +---------------------------+
|     ユーザーの tmux        |       |   lazyclaude tmux (-L)    |
|  (display-popup)          |       |   Claude Code セッション   |
|                           |       |                           |
|   +-------------------+   |       |   @0: session-1           |
|   | lazyclaude TUI    |<--+-------+-> @1: session-2           |
|   | (gocui)           |   |       |   @2: session-3           |
|   +--------+----------+   |       |                           |
|            |              |       +---------------------------+
|   +--------v----------+   |
|   | MCP Server        |   |       Claude Code hooks が POST:
|   | (in-process)      |<----------  /notify, /stop,
|   | 127.0.0.1:<port>  |   |        /session-start,
|   +-------------------+   |        /prompt-submit
+---------------------------+
```

hooks はセッション起動時に `claude --settings <file>` で注入されます。`~/.claude/settings.json` は変更されません。hooks は lock file スキャンで MCP サーバーを発見するため、サーバー再起動後も動作します。

---

## 開発

```bash
make build         # バイナリをビルド
make test          # 全テスト（race detector + カバレッジ）
make lint          # golangci-lint
make readme-gif    # docs/images/hero.gif を再生成（Docker 必須）
```

## 既知の問題

- **フルスクリーンモードでのペースト** -- フルスクリーンモードでのテキストペースト（Cmd+V / Ctrl+Shift+V）が正常に動作しない場合があります。tmux の `display-popup` と bracketed paste シーケンスの相互作用に起因する制限です。回避策: `a` でセッションに直接アタッチしてからペーストしてください。

## コントリビューション

コントリビューションを歓迎します！ バグ報告、機能リクエスト、プルリクエスト -- すべて大歓迎です。現在のタスクは [Issues](https://github.com/any-context/lazyclaude/issues) をご覧ください。

## ライセンス

MIT
