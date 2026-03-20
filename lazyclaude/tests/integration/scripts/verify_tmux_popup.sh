#!/bin/bash
# Verify tmux display-popup E2E: MCP server spawns display-popup on /notify,
# popup process sends choice key to target pane.
#
# This script MUST run inside a tmux session (display-popup needs an attached client).
# The outer runner (verify_tmux_popup_runner.sh) handles this.
#
# Since tmux send-keys cannot target display-popup, we use LAZYCLAUDE_POPUP_BINARY
# to point the server at an auto-accept wrapper that sends '1' immediately.
# This tests: server /notify → display-popup spawn → send-keys to cat pane.

set -euo pipefail

BINARY="${1:-lazyclaude}"
SOCKET="${LAZYCLAUDE_TMUX_SOCKET:-}"

if [ -z "$SOCKET" ]; then
    echo "FAIL: LAZYCLAUDE_TMUX_SOCKET not set" >&2
    exit 1
fi

TMPDIR=$(mktemp -d /tmp/lazyclaude-popup-e2e-XXXX)
SERVER_PID=""
cleanup() {
    [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
    tmux -L "$SOCKET" kill-window -t lazyclaude:cat-listener 2>/dev/null || true
    rm -rf "$TMPDIR"
}
trap cleanup EXIT

PASS=0
FAIL=0

check() {
    local name="$1" result="$2"
    if [ "$result" -eq 0 ]; then
        echo "  PASS: $name" >&2
        PASS=$((PASS + 1))
    else
        echo "  FAIL: $name" >&2
        FAIL=$((FAIL + 1))
    fi
}

echo "=== tmux display-popup E2E ===" >&2

# --- Create auto-accept wrapper ---
WRAPPER="$TMPDIR/auto-accept"
cat > "$WRAPPER" <<'WEOF'
#!/bin/bash
# Auto-accept wrapper: simulates lazyclaude tool --send-keys choosing Accept
WINDOW=""
while [ $# -gt 0 ]; do
    case "$1" in
        tool) shift ;;
        --window) WINDOW="$2"; shift 2 ;;
        --send-keys) shift ;;
        *) shift ;;
    esac
done
if [ -n "$WINDOW" ]; then
    SOCK="${LAZYCLAUDE_TMUX_SOCKET:-}"
    if [ -n "$SOCK" ]; then
        tmux -L "$SOCK" send-keys -t "lazyclaude:$WINDOW" 1
    fi
fi
WEOF
chmod +x "$WRAPPER"

# --- 1. Start MCP server with wrapper as popup binary ---
echo "--- Step 1: Start MCP server ---" >&2
mkdir -p "$TMPDIR"/{data,run,ide}
TOKEN="popup-e2e-token"

LAZYCLAUDE_DATA_DIR="$TMPDIR/data" \
LAZYCLAUDE_RUNTIME_DIR="$TMPDIR/run" \
LAZYCLAUDE_IDE_DIR="$TMPDIR/ide" \
LAZYCLAUDE_TMUX_SOCKET="$SOCKET" \
LAZYCLAUDE_POPUP_BINARY="$WRAPPER" \
"$BINARY" server --port 0 --token "$TOKEN" &
SERVER_PID=$!

PORT_FILE="$TMPDIR/run/lazyclaude-mcp.port"
for i in $(seq 1 50); do [ -f "$PORT_FILE" ] && break; sleep 0.1; done
[ -f "$PORT_FILE" ] || { echo "FAIL: port file" >&2; exit 1; }
PORT=$(cat "$PORT_FILE")
echo "  MCP port: $PORT" >&2
check "MCP server started" 0

# --- 2. Create cat listener window ---
echo "--- Step 2: Create cat listener ---" >&2
CURRENT_SESSION=$(tmux -L "$SOCKET" display-message -p '#{session_name}')
tmux -L "$SOCKET" rename-session -t "$CURRENT_SESSION" lazyclaude 2>/dev/null || true
tmux -L "$SOCKET" new-window -t lazyclaude -n cat-listener
tmux -L "$SOCKET" send-keys -t lazyclaude:cat-listener "cat" Enter
sleep 0.3

WIN_ID=$(tmux -L "$SOCKET" display-message -t lazyclaude:cat-listener -p '#{window_id}')
echo "  cat window: $WIN_ID" >&2
check "cat listener created" 0

# --- 3. Write pending-window ---
echo "--- Step 3: Write pending-window ---" >&2
echo "$WIN_ID" > "$TMPDIR/run/lazyclaude-pending-window"
check "pending-window written" 0

# --- 4. POST /notify ---
echo "--- Step 4: POST /notify ---" >&2
tmux -L "$SOCKET" select-window -t "lazyclaude:cat-listener"
sleep 0.3

STATUS1=$(curl -s -o /dev/null -w '%{http_code}' \
  -X POST -H 'Content-Type: application/json' -H "X-Auth-Token: $TOKEN" \
  -d "{\"type\":\"tool_info\",\"pid\":99999,\"tool_name\":\"Bash\",\"tool_input\":{\"command\":\"ls\"},\"cwd\":\"/tmp\"}" \
  "http://127.0.0.1:$PORT/notify")

STATUS2=$(curl -s -o /dev/null -w '%{http_code}' \
  -X POST -H 'Content-Type: application/json' -H "X-Auth-Token: $TOKEN" \
  -d "{\"pid\":99999,\"message\":\"Allow Bash?\"}" \
  "http://127.0.0.1:$PORT/notify")

echo "  tool_info=$STATUS1 permission_prompt=$STATUS2" >&2
R=0; [ "$STATUS1" = "200" ] && [ "$STATUS2" = "200" ] || R=1
check "/notify both 200" $R

# --- 5. Wait for auto-accept popup ---
echo "--- Step 5: Wait (3s) ---" >&2
sleep 3

# --- 6. Verify cat received '1' ---
echo "--- Step 6: Verify ---" >&2
CAT_CONTENT=$(tmux -L "$SOCKET" capture-pane -t "lazyclaude:cat-listener" -p)
echo "  cat pane:" >&2
echo "$CAT_CONTENT" | head -3 >&2

R=0; echo "$CAT_CONTENT" | grep -q "1" || R=1
check "cat received '1' via display-popup chain" $R

echo "" >&2
echo "Results: $PASS passed, $FAIL failed" >&2
[ "$FAIL" -eq 0 ]
