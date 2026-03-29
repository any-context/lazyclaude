#!/bin/bash-real
# VHS テープの実行エントリポイント。
# テープは人間の操作のみ。テスト都合は全てここ。
# tmux + lazyclaude プラグインは bash ラッパー (Dockerfile) が自動設定。
set -euo pipefail

TAPE="${1:?Usage: entrypoint.sh <tape-file>}"
TAPE_NAME="$(basename "$TAPE" .tape)"
OUTDIR="/app/outputs/${TAPE_NAME}"
TXT="${OUTDIR}/${TAPE_NAME}.txt"
mkdir -p "$OUTDIR"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/scripts/show_frame.sh"

# --- 共通セットアップ ---
# Docker DNS が ttyd 内で効かない場合があるため、remote の IP を /etc/hosts に追加
if [ -n "${REMOTE_HOST:-}" ]; then
    REMOTE_IP=$(getent hosts "$REMOTE_HOST" 2>/dev/null | awk '{print $1}')
    if [ -n "$REMOTE_IP" ]; then
        echo "$REMOTE_IP $REMOTE_HOST" >> /etc/hosts
    fi
fi

# --- テープ固有セットアップ ---
case "$TAPE_NAME" in
    lazygit)
        # /app に git repo を用意 (lazygit が起動に必要)
        git config --global user.email "test@test.com"
        git config --global user.name "Test"
        cd /app && git init && git add go.mod && git commit -m "init" 2>/dev/null
        ;;
    diff_popup)
        cat > /tmp/test.go << 'GOEOF'
package main

import "fmt"

func hello() string {
    return "hello"
}

func main() {
    fmt.Println(hello())
}
GOEOF
        ;;
    worktree)
        cd /app
        git init . || true
        git config user.email "test@test.com"
        git config user.name "test"
        git add -A
        git commit -m "init" || true
        git worktree add .claude/worktrees/fix-popup -b fix-popup || true
        ;;
    pm_worker)
        # PM/Worker テスト: git repo を用意 (worktree 作成に必要)
        cd /app
        git init . || true
        git config user.email "test@test.com"
        git config user.name "test"
        git add -A
        git commit -m "init" || true

        # msg API テストは Claude Code セッション内から自律実行される
        # (人間はプロンプトを送るだけ。API は Claude Code が叩く)
        ;;
    plugin_mode)
        # プラグインモード E2E: プラグインの install/toggle/uninstall を実演
        # 1. 公式マーケットプレイスを追加
        claude plugins marketplace add anthropics/claude-plugins-official 2>/dev/null || true
        # 2. テスト用プラグインを2つインストール (ローカルパス参照で git clone 不要)
        claude plugins install agent-sdk-dev --scope project 2>/dev/null || true
        claude plugins install plugin-dev --scope project 2>/dev/null || true
        ;;
    paste_special)
        # Bracketed paste E2E: send ESC[200~ + multiline text + ESC[201~
        cat > /tmp/paste-text.txt << 'PASTEEOF'
七夕
持明院統の光厳天皇が後醍醐天皇によって廃位される（1333年）
ナポレオン戦争：フランスとロシア帝国がティルジットの和約を締結（1807年）
華族令制定（1884年）
フィリピンで独立運動組織カティプナンが結成（1892年）
アメリカ合衆国がハワイを併合（1898年）
盧溝橋事件（1937年）
広島高裁が加藤老事件の被告人に再審無罪判決（1977年）
ソロモン諸島が独立（1978年）
ロンドン同時爆破事件（2005年）
PASTEEOF
        (
            sleep 15
            # ESC[200~ (paste start)
            tmux send-keys -t main -H 1b 5b 32 30 30 7e
            # Text body (multiline)
            while IFS= read -r line; do
                tmux send-keys -t main -l "$line"
                tmux send-keys -t main Enter
            done < /tmp/paste-text.txt
            # ESC[201~ (paste end)
            tmux send-keys -t main -H 1b 5b 32 30 31 7e
        ) &
        ;;
esac

# --- フレーム監視 (バックグラウンド) ---
LOG="${OUTDIR}/${TAPE_NAME}.log"
source "$SCRIPT_DIR/scripts/watch_frames.sh" | tee >(LC_ALL=C sed 's/\x1b\[[0-9;]*[a-zA-Z]//g' > "$LOG") &
WATCHER_PID=$!

# --- VHS 実行 ---
VHS_RC=0
vhs -q "$TAPE" || VHS_RC=$?

sleep 1
kill "$WATCHER_PID" 2>/dev/null || true
pkill -f "tail -f $TXT" 2>/dev/null || true
wait "$WATCHER_PID" 2>/dev/null || true

# --- リモート hook ログ回収 (SSH テスト用) ---
if [ -n "${REMOTE_HOST:-}" ]; then
    ssh -o ConnectTimeout=3 "$REMOTE_HOST" "cat /tmp/lazyclaude-hook.log 2>/dev/null" \
        > "$OUTDIR/remote-hook.log" 2>/dev/null || true
fi

cleanup_frames
exit "$VHS_RC"
