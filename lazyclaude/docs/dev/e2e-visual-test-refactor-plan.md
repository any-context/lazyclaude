# E2E Visual Test Refactoring Plan

## Goal

`tests/integration/scripts/` の全テストスクリプトを統一された視覚テストフレームワークにリファクタリングする。

**原則**:
- 既存スクリプトは削除しない。リファクタリングのみ。
- 全テストで毎ステップ `capture-pane` UI 出力を表示する (動画のようにフレームが切り替わる)
- 固定サイズ (80x24) で統一
- `test_lib.sh` で共通ヘルパーを提供
- mock / tmux / ssh / claude モードを切り替え可能にする

## Phase 1: test_lib.sh 共通ライブラリ

### 提供する関数

```bash
# --- Environment ---
init_test "テスト名" [mode]     # 初期化 + モード判定 + 固定サイズセッション起動
cleanup_test                     # tmux kill-server + ポートファイル等の削除

# --- Capture & Display ---
frame "ステップ名"               # capture-pane + フレーム番号付き表示
                                 # === [Frame N] ステップ名 ===
                                 # (capture-pane output)
                                 # ==============================

# --- Assertion ---
check "チェック名" $result       # PASS/FAIL + capture表示(FAIL時)
assert_contains "$text"          # capture に $text が含まれるか
assert_not_contains "$text"      # capture に $text が含まれないか

# --- Wait ---
wait_for "pattern" [timeout]     # capture-pane を polling

# --- Session ---
capture                          # tmux capture-pane -p -t $TEST_PANE
send_keys [keys...]              # tmux send-keys -t $TEST_PANE

# --- Mode ---
start_lazyclaude                 # モードに応じた起動方法
                                 # mock: 直接バイナリ実行
                                 # tmux: TPM プラグイン経由
                                 # ssh: Docker Compose SSH 経由
```

### 固定変数

```bash
TEST_WIDTH=80
TEST_HEIGHT=24
TEST_SOCKET="test-$$"            # PID でユニーク化
TEST_PANE="test"
FRAME_NUM=0                      # フレーム番号 (frame() で自動インクリメント)
PASS=0
FAIL=0
```

### frame() の出力フォーマット

```
=== [Frame 1] cascade display (3 popups) ===
┌─────────────────────────────────────────────────────────────────────────────────┐
│ Sessions                                                                        │
│                                                                                 │
│ (capture-pane の生出力がそのまま表示される)                                       │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
=================================================
```

### モード切り替え

```bash
# 使い方:
#   ./verify_popup_stack.sh lazyclaude          # デフォルト (mock)
#   ./verify_popup_stack.sh lazyclaude --mode tmux
#   ./verify_popup_stack.sh lazyclaude --mode ssh
#   MODE=ssh ./verify_popup_stack.sh lazyclaude

# init_test 内部で MODE を判定:
# - mock:  BINARY を直接 tmux で起動
# - tmux:  lazyclaude.tmux を source して TPM 経由で起動
# - ssh:   REMOTE_HOST に SSH して起動
# - claude: mock と同じだが CLAUDE_CODE_OAUTH_TOKEN を要求
```

## Phase 2: 各スクリプトのリファクタリング

各スクリプトの変更内容。**全チェックの前後で `frame` を呼ぶ**。

### verify_popup_stack.sh (7 checks)

現状: 既に毎ステップ capture 出力あり。パターンとして最も良い。
変更: test_lib.sh を source。cleanup/capture/wait_for/check を共通化。frame() で統一フォーマット。サイズを 80x24 に統一。

```
Frame 1: TUI起動 (no sessions)
Frame 2: 3通知投入後 → cascade [3/3]        → check "cascade display"
Frame 3: Arrow Up → [2/3]                    → check "arrow up"
Frame 4: Esc → suspended (no action bar)     → check "esc suspend"
Frame 5: p → reopened                        → check "p reopen"
Frame 6: y → focused dismissed               → check "y dismiss focused"
Frame 7: y → Tool2 dismissed                 → check "y dismiss Tool2"
Frame 8: 2通知追加 + Y → all dismissed       → check "Y accept all"
```

### verify_option_count.sh (4 checks)

現状: 毎ステップ capture 出力あり。
変更: test_lib.sh を source。frame() で統一フォーマット。

```
Frame 1: TUI起動
Frame 2: 2択通知 → y/n 表示                  → check "2-option: y/n"
Frame 3: y で dismiss
Frame 4: 3択通知 → y/a/n 表示                → check "3-option: y/a/n"
Frame 5: y で dismiss
Frame 6: デフォルト通知 → y/a/n 表示          → check "default: y/a/n"
```

### verify_setup.sh (6 checks)

現状: UI capture なし (PASS/FAIL のみ)。
変更: test_lib.sh を source。setup 実行前後の状態を frame で表示。settings.json の内容も frame 内に表示。

```
Frame 1: setup 実行前 (tmux session)
Frame 2: setup 実行後 → ポートファイル        → check "port file"
Frame 3: settings.json 内容表示              → check "settings.json"
Frame 4: hooks 内容 (/notify, PreToolUse)    → check "hooks"
Frame 5: 冪等性テスト (再実行)               → check "idempotent"
```

### verify_key_order.sh (1 check)

現状: 最後に 1 回 capture のみ。
変更: test_lib.sh を source。各ステップを frame 化。

```
Frame 1: TUI起動
Frame 2: n でセッション作成
Frame 3: Enter でフルスクリーン
Frame 4: Claude Code プロンプト待ち
Frame 5: あいうえお 送信後                   → check "key order"
```

### verify_tmux_popup.sh (6 checks)

現状: script replay + capture。既に視覚的。
変更: test_lib.sh を source。frame() で統一フォーマット。

```
Frame 1: display-popup 実行
Frame 2: script log 確認                     → check "script log"
Frame 3: replay + capture → popup content     → check "tool context"
Frame 4: Command: 確認                       → check "Command:"
Frame 5: action bar 確認                     → check "action bar"
Frame 6: border 確認                         → check "border"
```

### verify_remote_popup.sh (4-5 checks)

現状: 最後のみ capture。
変更: test_lib.sh を source。各ステップを frame 化。

```
Frame 1: TUI起動
Frame 2: MCP server ready (ポート表示)
Frame 3: SSH tunnel 確立
Frame 4: Claude Code 起動 (remote pane capture)
Frame 5: プロンプト送信後 (remote pane capture)
Frame 6: TUI popup 表示                      → check "popup appeared"
Frame 7: action bar 判定                     → check "option count"
```

### verify_2option_detect.sh (3-4 checks)

現状: 最後のみ capture。
変更: test_lib.sh を source。各ステップを frame 化。

```
Frame 1: TUI起動
Frame 2: MCP server ready
Frame 3: Claude Code 起動 (Claude pane capture)
Frame 4: プロンプト送信後 (Claude pane capture)
Frame 5: Claude permission dialog 表示
Frame 6: TUI popup → y/n or y/a/n 判定       → check "option detect"
```

### measure_latency.sh

現状: ベンチマーク。capture はあるが数値出力が主。
変更: test_lib.sh を source。frame() で測定中の画面を表示。

```
Frame 1: 1st launch 開始
Frame 2: セッション作成後
Frame 3: フルスクリーン
Frame 4: 測定中 (chars 表示)
Frame 5: 1st 結果
Frame 6: 2nd launch (同構造)
Frame 7: 比較結果                            → check "ratio"
```

### verify_tmux_popup_runner.sh

変更なし。ランナーのまま。test_lib.sh は不要 (verify_tmux_popup.sh が使う)。

## Phase 3: Makefile ターゲット

```makefile
## Visual E2E tests (Docker, capture-pane UI output)
test-visual:
	docker build -f Dockerfile.test -t lazyclaude-test .
	docker run --rm lazyclaude-test bash -c '\
		for f in tests/integration/scripts/verify_*.sh; do \
			echo ""; echo "========== $$(basename $$f) =========="; \
			bash "$$f" lazyclaude || exit 1; \
		done'

## Visual E2E: single script
test-visual-%:
	docker build -f Dockerfile.test -t lazyclaude-test .
	docker run --rm lazyclaude-test bash tests/integration/scripts/verify_$*.sh lazyclaude

## Visual E2E: SSH mode
test-visual-ssh:
	docker compose -f docker-compose.ssh.yml build
	docker compose -f docker-compose.ssh.yml run --rm local \
		"ssh-keyscan -H remote >> /root/.ssh/known_hosts 2>/dev/null && \
		 for f in tests/integration/scripts/verify_*.sh; do \
			echo ''; echo '========== $$(basename $$f) =========='; \
			MODE=ssh REMOTE_HOST=remote bash \"$$f\" lazyclaude || exit 1; \
		 done"
	docker compose -f docker-compose.ssh.yml down
```

## Implementation Order

1. `test_lib.sh` を作成 (共通ヘルパー)
2. `verify_popup_stack.sh` をリファクタリング (最もパターンが良いので最初に)
3. `verify_option_count.sh` をリファクタリング
4. `verify_setup.sh` をリファクタリング (視覚化追加)
5. `verify_key_order.sh` をリファクタリング (視覚化追加)
6. `verify_tmux_popup.sh` をリファクタリング
7. `verify_remote_popup.sh` をリファクタリング (視覚化追加)
8. `verify_2option_detect.sh` をリファクタリング (視覚化追加)
9. `measure_latency.sh` をリファクタリング
10. Makefile にターゲット追加
11. Docker 内で全スクリプト実行して視覚的に確認
