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

# --- デバッグログ有効化 ---
export LAZYCLAUDE_DEBUG=1

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
    # smoke: セットアップ不要
    smoke) ;;
    # hero: README 用デモ。セットアップ不要
    hero) ;;
    # worktree_pm: w/W/P キーの動作確認。git repo 必要
    worktree_pm)
        cd /app
        git config --global user.email "test@test.com"
        git config --global user.name "test"
        git init -q
        git add go.mod
        git commit -q -m "init"
        ;;
    # ssh_worktree_pm: SSH経由でw/W/Pキーの動作確認
    # ユーザー環境の模擬: 既存セッションがありlazyclaude tmuxセッションが存在する状態
    ssh_msg)
        # リモート: クリーンアップ + トークン + プロジェクト準備
        ssh -o ConnectTimeout=10 remote 'tmux -L lazyclaude kill-server 2>/dev/null; rm -rf /tmp/lazyclaude-* /tmp/tmux-*/lazyclaude ~/.local/share/lazyclaude/state.json 2>/dev/null; true'
        if [ -n "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]; then
            ssh -o ConnectTimeout=10 remote "echo 'export CLAUDE_CODE_OAUTH_TOKEN=${CLAUDE_CODE_OAUTH_TOKEN}' >> /root/.bashrc"
        fi
        ssh -o ConnectTimeout=10 remote 'mkdir -p /root/app/test-app && cd /root/app/test-app && git init -q && git commit -q --allow-empty -m init 2>/dev/null; true'
        ;;
    ssh_notify)
        # リモート: 古い daemon/state をクリーンアップ
        ssh -o ConnectTimeout=10 remote 'tmux -L lazyclaude kill-server 2>/dev/null; rm -rf /tmp/lazyclaude-* /tmp/tmux-*/lazyclaude ~/.local/share/lazyclaude/state.json 2>/dev/null; true'
        # リモート: .env のトークンを SSH セッションで使えるようにする
        if [ -n "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]; then
            ssh -o ConnectTimeout=10 remote "echo 'export CLAUDE_CODE_OAUTH_TOKEN=${CLAUDE_CODE_OAUTH_TOKEN}' >> /root/.bashrc"
        fi
        # リモート: プロジェクト準備
        ssh -o ConnectTimeout=10 remote 'mkdir -p /root/app/test-app && cd /root/app/test-app && git init -q && git commit -q --allow-empty -m init 2>/dev/null; true'
        ;;
    ssh_worktree_pm)
        # リモート: 古い daemon/state をクリーンアップ
        ssh -o ConnectTimeout=10 remote 'tmux -L lazyclaude kill-server 2>/dev/null; rm -rf /tmp/lazyclaude-* /tmp/tmux-*/lazyclaude ~/.local/share/lazyclaude/state.json 2>/dev/null; true'
        # リモート: .env のトークンを SSH セッションで使えるようにする
        if [ -n "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]; then
            ssh -o ConnectTimeout=10 remote "echo 'export CLAUDE_CODE_OAUTH_TOKEN=${CLAUDE_CODE_OAUTH_TOKEN}' >> /root/.bashrc"
        fi
        # リモート: /root/app/test-app にプロジェクトを作成
        ssh -o ConnectTimeout=10 remote 'mkdir -p /root/app/test-app && cd /root/app/test-app && git init -q && git commit -q --allow-empty -m init'
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
