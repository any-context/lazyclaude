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
    mcp_toggle)
        # MCP トグル E2E: MCP タブの表示・on/off 切替を実演
        cat > /root/.claude.json << 'MCPEOF'
{
  "hasCompletedOnboarding": true,
  "numStartups": 10,
  "projects": {"/": {"hasTrustDialogAccepted": true, "allowedTools": []}},
  "mcpServers": {
    "github": {
      "command": "echo",
      "args": ["github-mcp"]
    },
    "memory": {
      "command": "echo",
      "args": ["memory-mcp"]
    },
    "vercel": {
      "type": "http",
      "url": "https://mcp.vercel.com"
    }
  }
}
MCPEOF
        mkdir -p /app/.claude
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
        # Bracketed paste E2E: write ESC[200~ + text + ESC[201~ directly
        # to the tmux client TTY. This is the exact path a real Cmd+V
        # takes: terminal → tmux → popup process (lazyclaude) → tcell.
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
            # Wait for a tmux client to attach (VHS bash wrapper attaches).
            for i in $(seq 1 30); do
                TTY=$(tmux list-clients -F '#{client_tty}' 2>/dev/null | head -1)
                [ -n "$TTY" ] && break
                sleep 1
            done
            if [ -z "$TTY" ]; then
                echo "paste_special: no tmux client TTY found after 30s" >&2
                exit 1
            fi
            echo "paste_special: found TTY=$TTY" >> /tmp/lazyclaude/paste-debug.log
            # Inject bracketed paste via TIOCSTI ioctl on the client TTY.
            # TIOCSTI pushes bytes into the TTY input queue — identical to
            # a real terminal paste. tmux reads them and routes to the
            # active popup (lazyclaude) → tcell EventPaste → gocui.
            python3 -c "
import fcntl, termios, os, sys
payload = b'\x1b[200~' + open('/tmp/paste-text.txt','rb').read() + b'\x1b[201~'
fd = os.open('$TTY', os.O_RDWR)
for b in payload:
    fcntl.ioctl(fd, termios.TIOCSTI, bytes([b]))
os.close(fd)
print(f'paste_special: injected {len(payload)} bytes via TIOCSTI')
" >> /tmp/lazyclaude/paste-debug.log 2>&1
            RC=$?
            echo "paste_special: python3 exit=$RC" >> /tmp/lazyclaude/paste-debug.log
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
