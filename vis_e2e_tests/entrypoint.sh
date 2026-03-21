#!/bin/bash
# VHS テープの実行エントリポイント。
# テープは人間の操作のみ。テスト都合は全てここ。
set -euo pipefail

TAPE="${1:?Usage: entrypoint.sh <tape-file>}"
TAPE_NAME="$(basename "$TAPE" .tape)"
OUTDIR="/app/outputs/${TAPE_NAME}"
TXT="${OUTDIR}/${TAPE_NAME}.txt"
mkdir -p "$OUTDIR"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/scripts/show_frame.sh"

# --- セットアップ ---
case "$TAPE_NAME" in
    ssh_launch)
        tmux new-session -d -s main -x 125 -y 37
        /app/bin/lazyclaude setup 2>/dev/null || true
        bash-real /app/lazyclaude.tmux 2>/dev/null || true
        sleep 2
        export VHS_AUTO_TMUX=1
        ;;
esac

# --- フレーム監視 (バックグラウンド) ---
LOG="${OUTDIR}/${TAPE_NAME}.log"
source "$SCRIPT_DIR/scripts/watch_frames.sh" | tee >(sed 's/\x1b\[[0-9;]*[a-zA-Z]//g' > "$LOG") &
WATCHER_PID=$!

# --- VHS 実行 ---
VHS_RC=0
vhs -q "$TAPE" || VHS_RC=$?

sleep 1
# tail -f を含むパイプ全体を停止
kill "$WATCHER_PID" 2>/dev/null || true
pkill -f "tail -f $TXT" 2>/dev/null || true
wait "$WATCHER_PID" 2>/dev/null || true

cleanup_frames
exit "$VHS_RC"
