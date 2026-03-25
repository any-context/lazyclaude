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
