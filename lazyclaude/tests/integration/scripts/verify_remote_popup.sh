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
# Usage: verify_remote_popup.sh [binary]

set -euo pipefail

BINARY="${1:-lazyclaude}"
REMOTE="${REMOTE_HOST:-}"
OAUTH_TOKEN="${CLAUDE_CODE_OAUTH_TOKEN:-}"
UI_SOCKET="remote-popup-e2e"

if [ -z "$REMOTE" ]; then
    echo "SKIP: REMOTE_HOST not set" >&2
    exit 0
fi
if [ -z "$OAUTH_TOKEN" ]; then
    echo "FAIL: CLAUDE_CODE_OAUTH_TOKEN not set" >&2
    exit 1
fi

cleanup() {
    tmux -L "$UI_SOCKET" kill-server 2>/dev/null || true
    tmux -L lazyclaude kill-server 2>/dev/null || true
    rm -f /tmp/lazyclaude-mcp.port
    rm -f "$HOME/.local/share/lazyclaude/state.json"
    rm -f /tmp/lazyclaude-pending-window
    # Kill background SSH
    [ -n "${SSH_PID:-}" ] && kill "$SSH_PID" 2>/dev/null || true
    # Clean remote state
    ssh -o BatchMode=yes "$REMOTE" "rm -f ~/.claude/ide/*.lock; tmux kill-server 2>/dev/null" 2>/dev/null || true
}
trap cleanup EXIT

cleanup
sleep 0.5

capture() {
    tmux -L "$UI_SOCKET" capture-pane -p -t test 2>/dev/null
}

wait_for() {
    local pattern="$1" timeout="${2:-10}" i=0
    while [ $i -lt $((timeout * 10)) ]; do
        if capture | grep -qE "$pattern"; then return 0; fi
        sleep 0.1
        i=$((i + 1))
    done
    return 1
}

PASS=0
FAIL=0

check() {
    local name="$1" result="$2"
    if [ "$result" -eq 0 ]; then
        echo "  PASS: $name" >&2
        PASS=$((PASS + 1))
    else
        echo "  FAIL: $name" >&2
        echo "  --- capture ---" >&2
        capture >&2
        echo "  --- end ---" >&2
        FAIL=$((FAIL + 1))
    fi
}

echo "=== Remote Claude popup E2E ===" >&2
echo "  binary: $BINARY" >&2
echo "  remote: $REMOTE" >&2
echo "" >&2

# --- 1. Start lazyclaude TUI ---
echo "--- Step 1: Start TUI ---" >&2
tmux -L "$UI_SOCKET" new-session -d -s test -x 120 -y 40 "$BINARY; sleep 999"
wait_for "no sessions" 10 || { echo "FAIL: TUI did not start" >&2; exit 1; }
check "TUI started" 0

# --- 2. Wait for MCP server ---
echo "--- Step 2: Wait for MCP server ---" >&2
PORT_FILE="/tmp/lazyclaude-mcp.port"
for i in $(seq 1 100); do
    [ -f "$PORT_FILE" ] && break
    sleep 0.1
done
[ -f "$PORT_FILE" ] || { echo "FAIL: MCP port file not found" >&2; exit 1; }
MCP_PORT=$(cat "$PORT_FILE")
echo "  MCP port: $MCP_PORT" >&2

# --- 3. Read auth token ---
echo "--- Step 3: Read auth token ---" >&2
LOCK_FILE="$HOME/.claude/ide/${MCP_PORT}.lock"
for i in $(seq 1 50); do
    [ -f "$LOCK_FILE" ] && break
    sleep 0.1
done
[ -f "$LOCK_FILE" ] || { echo "FAIL: lock file not found" >&2; exit 1; }
AUTH_TOKEN=$(node -e "console.log(JSON.parse(require('fs').readFileSync('$LOCK_FILE','utf8')).authToken)")
echo "  auth token: ${AUTH_TOKEN:0:8}..." >&2
check "MCP server ready" 0

# --- 4. Pending window for remote ---
echo "--- Step 4: Prepare remote connection ---" >&2
echo "remote-claude" > /tmp/lazyclaude-pending-window
LOCK_JSON=$(node -e "console.log(JSON.stringify({pid:0,authToken:'$AUTH_TOKEN',transport:'ws'}))")

# --- 5. SSH: setup remote + start Claude interactively ---
echo "--- Step 5: Start Claude interactively on remote ---" >&2
ssh -o BatchMode=yes -o ConnectTimeout=5 \
    -R "${MCP_PORT}:127.0.0.1:${MCP_PORT}" \
    "$REMOTE" bash <<REMOTE_SCRIPT &
set -e

# Write lock file
mkdir -p ~/.claude/ide
printf '%s' '$LOCK_JSON' > ~/.claude/ide/${MCP_PORT}.lock

# Skip Claude onboarding, clear cached sessions so nothing is auto-approved
rm -rf ~/.claude/projects/ ~/.claude/statsig/ ~/.claude/todos/ 2>/dev/null || true
mkdir -p /tmp/e2e-test
cat > ~/.claude.json <<'CJSON'
{"hasCompletedOnboarding":true,"numStartups":10,"projects":{"/tmp/e2e-test":{"hasTrustDialogAccepted":true,"allowedTools":[]}}}
CJSON

# Start Claude interactively inside a tmux session on the remote.
# Use a fresh working dir (no session history) so permission dialog appears.
tmux new-session -d -s claude-e2e -x 80 -y 24 -c /tmp/e2e-test

# Launch Claude inside the tmux session
tmux send-keys -t claude-e2e \
    "CLAUDE_CODE_OAUTH_TOKEN='$OAUTH_TOKEN' CLAUDE_CODE_AUTO_CONNECT_IDE=true claude" Enter

# Wait for Claude to initialize
sleep 10

# Send a prompt that triggers Write tool (not auto-approved unlike echo)
tmux send-keys -t claude-e2e "Create a new file called hello.txt containing hello world" Enter

# Debug: show what Claude is doing
sleep 15
echo "=== REMOTE CLAUDE PANE ===" >&2
tmux capture-pane -p -t claude-e2e >&2
echo "=== END REMOTE PANE ===" >&2

# Check if lock file exists
ls -la ~/.claude/ide/ >&2 2>/dev/null || echo "no lock files" >&2

# Keep SSH alive for the tunnel
sleep 45
REMOTE_SCRIPT
SSH_PID=$!
echo "  SSH PID: $SSH_PID" >&2

# --- 6. Wait for popup ---
echo "--- Step 6: Wait for popup (up to 60s) ---" >&2
R=0
wait_for "Bash|Write|Read|Edit|command|echo" 60 || R=1
check "popup appeared with tool name" $R

if [ $R -eq 0 ]; then
    echo "" >&2
    echo "--- popup content ---" >&2
    capture >&2
    echo "--- end ---" >&2

    # --- 7. Action bar ---
    R=0; capture | grep -q "y/a/n" || R=1
    check "popup action bar visible" $R
fi

echo "" >&2
echo "Results: $PASS passed, $FAIL failed" >&2
[ "$FAIL" -eq 0 ]
