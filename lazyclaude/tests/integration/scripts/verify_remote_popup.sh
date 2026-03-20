#!/bin/bash
# Full E2E: lazyclaude TUI <- MCP server <- SSH tunnel <- real Claude Code (remote)
#
# Verifies the production flow:
# 1. lazyclaude TUI starts, MCP server auto-starts
# 2. SSH reverse tunnel connects remote to local MCP
# 3. Claude Code runs interactively on remote, receives a prompt
# 4. Claude's permission dialog triggers hooks -> POST /notify via tunnel
# 5. TUI displays popup with tool name + action bar
#
# Requires:
#   REMOTE_HOST              - SSH target (e.g., "remote" in Docker Compose)
#   CLAUDE_CODE_OAUTH_TOKEN  - Claude Code auth token (from .env)
#
# Usage: verify_remote_popup.sh [binary] [--mode ssh]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/test_lib.sh"

init_test "Remote Claude Popup E2E" "${1:-lazyclaude}" "${@:2}"

REMOTE="${REMOTE_HOST:-}"
OAUTH_TOKEN="${CLAUDE_CODE_OAUTH_TOKEN:-}"

if [ -z "$REMOTE" ]; then
    echo "SKIP: REMOTE_HOST not set" >&2
    exit 0
fi
if [ -z "$OAUTH_TOKEN" ]; then
    echo "FAIL: CLAUDE_CODE_OAUTH_TOKEN not set" >&2
    exit 1
fi

# Override cleanup to also handle SSH
cleanup_test() {
    [ -n "${SSH_PID:-}" ] && kill "$SSH_PID" 2>/dev/null || true
    ssh -o BatchMode=yes "$REMOTE" "rm -f ~/.claude/ide/*.lock; tmux kill-server 2>/dev/null" 2>/dev/null || true
    cleanup_test_silent
    rm -f "$_PREV_FRAME_FILE" "$_CURR_FRAME_FILE" 2>/dev/null || true
}

# Helper: capture remote Claude Code pane via SSH
capture_remote() {
    ssh -o BatchMode=yes -o ConnectTimeout=3 "$REMOTE" \
        "tmux capture-pane -p -t claude-e2e 2>/dev/null" 2>/dev/null || echo "(remote capture failed)"
}

# Helper: display remote pane as a frame
frame_remote() {
    local step_name="$1"
    FRAME_NUM=$((FRAME_NUM + 1))
    _draw_frame "[REMOTE] $step_name" "$(capture_remote)"
}

# --- 0. Verify SSH connectivity (shown as terminal screen) ---
tmux -L "$TEST_SOCKET" new-session -d -s test -x "$TEST_WIDTH" -y "$TEST_HEIGHT"
send_keys "ssh -o BatchMode=yes -o ConnectTimeout=5 $REMOTE" Enter
sleep 3
frame "SSH to $REMOTE"
R=0; capture | grep -qE "root@|#|\\\$|Last login" || R=1
check "SSH connection to $REMOTE" $R
send_keys "hostname && uname -a" Enter
sleep 1
frame "remote shell"
send_keys "exit" Enter
sleep 1

# --- 1. Start lazyclaude TUI ---
send_keys "$BINARY; sleep 999" Enter
if ! wait_for "no sessions" 5; then
    frame "TUI startup failed"
    exit 1
fi
frame "TUI started"

# --- 2. Wait for MCP server ---
PORT_FILE="/tmp/lazyclaude-mcp.port"
for i in $(seq 1 100); do
    [ -f "$PORT_FILE" ] && break
    sleep 0.1
done
[ -f "$PORT_FILE" ] || { echo "FAIL: MCP port file not found" >&2; exit 1; }
MCP_PORT=$(cat "$PORT_FILE")
frame "MCP server ready (port $MCP_PORT)"

# --- 3. Read auth token ---
LOCK_FILE="$HOME/.claude/ide/${MCP_PORT}.lock"
for i in $(seq 1 50); do [ -f "$LOCK_FILE" ] && break; sleep 0.1; done
[ -f "$LOCK_FILE" ] || { echo "FAIL: lock file not found" >&2; exit 1; }
AUTH_TOKEN=$(node -e "console.log(JSON.parse(require('fs').readFileSync('$LOCK_FILE','utf8')).authToken)") || true
R=0; [ -n "$AUTH_TOKEN" ] || R=1
check "auth token read from lock file" $R
[ $R -eq 0 ] || { frame "auth token read failed"; exit 1; }

# --- 4. Pending window for remote ---
echo "remote-claude" > /tmp/lazyclaude-pending-window
LOCK_JSON=$(node -e "console.log(JSON.stringify({pid:0,authToken:'$AUTH_TOKEN',transport:'ws'}))")

# --- 5. SSH: setup remote + start Claude interactively ---
ssh -o BatchMode=yes -o ConnectTimeout=5 \
    -R "${MCP_PORT}:127.0.0.1:${MCP_PORT}" \
    "$REMOTE" bash <<REMOTE_SCRIPT &
set -e

mkdir -p ~/.claude/ide
printf '%s' '$LOCK_JSON' > ~/.claude/ide/${MCP_PORT}.lock

rm -rf ~/.claude/projects/ ~/.claude/statsig/ ~/.claude/todos/ 2>/dev/null || true
mkdir -p /tmp/e2e-test
cat > ~/.claude.json <<'CJSON'
{"hasCompletedOnboarding":true,"numStartups":10,"projects":{"/tmp/e2e-test":{"hasTrustDialogAccepted":true,"allowedTools":[]}}}
CJSON

tmux new-session -d -s claude-e2e -x 80 -y 24 -c /tmp/e2e-test
tmux send-keys -t claude-e2e \
    "CLAUDE_CODE_OAUTH_TOKEN='$OAUTH_TOKEN' CLAUDE_CODE_AUTO_CONNECT_IDE=true claude" Enter

sleep 10

tmux send-keys -t claude-e2e 'Run this exact bash command: for i in \$(seq 1 10); do echo "line \$i"; done && ls /tmp && ps aux | head -5 && echo "done"' Enter

# Keep SSH alive for the tunnel
sleep 60
REMOTE_SCRIPT
SSH_PID=$!

frame "SSH tunnel started (PID: $SSH_PID)"

# --- 6. Wait for Claude to initialize on remote ---
sleep 12
frame_remote "Claude Code initializing"

# --- 7. Wait for prompt to be sent + permission dialog ---
sleep 18
frame_remote "Claude Code permission dialog"

# --- 8. Wait for popup on local TUI ---
R=0
wait_for "Bash|Write|Read|Edit|command|echo" 30 || R=1
frame "local TUI after notification"
check "popup appeared with tool name" $R

if [ $R -eq 0 ]; then
    # Show remote and local side-by-side (sequentially)
    frame_remote "remote at popup time"
    frame "local popup content"

    # --- 9. Verify action bar adapts to dialog option count ---
    C=$(capture)
    if echo "$C" | grep -q "allow always"; then
        R=0; echo "$C" | grep -q "y/a/n" || R=1
        check "3-option dialog: action bar shows y/a/n" $R
    else
        R=0; echo "$C" | grep -q "y/a/n" && R=1
        check "2-option dialog: action bar does NOT show y/a/n" $R
        R=0; echo "$C" | grep -qE "y.*n|yes.*no" || R=1
        check "2-option dialog: action bar shows y/n" $R
    fi
fi

finish_test
