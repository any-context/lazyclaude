#!/bin/bash
# Verify popup stack cascade rendering.
# Writes 3 notifications, checks cascade display, focus switching,
# suspend/reopen, and dismiss.
#
# PASS: all 5 checks pass
# FAIL: any check fails

set -euo pipefail

BINARY="${1:-lazyclaude}"
UI_SOCKET="popup-test"

cleanup() {
    tmux -L "$UI_SOCKET" kill-server 2>/dev/null || true
    tmux -L lazyclaude kill-server 2>/dev/null || true
    rm -f /tmp/lazyclaude-mcp.port
    rm -f "$HOME/.local/share/lazyclaude/state.json"
}
trap cleanup EXIT

cleanup
sleep 0.5

capture() {
    tmux -L "$UI_SOCKET" capture-pane -p -t test 2>/dev/null
}

wait_for() {
    local pattern="$1" timeout="${2:-5}" i=0
    while [ $i -lt $((timeout * 10)) ]; do
        if capture | grep -q "$pattern"; then return 0; fi
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
        FAIL=$((FAIL + 1))
    fi
}

echo "Popup stack test: $BINARY" >&2
echo "" >&2

tmux -L "$UI_SOCKET" new-session -d -s test -x 60 -y 25 "$BINARY; sleep 999"
wait_for "no sessions" 5 || { echo "FAIL: TUI did not start" >&2; exit 1; }

# Enqueue 3 notifications
for i in 1 2 3; do
    TS=$(date +%s%N)
    echo "{\"tool_name\":\"Tool${i}\",\"input\":\"{\\\"a\\\":\\\"${i}\\\"}\",\"window\":\"@0\",\"timestamp\":\"2026-01-01T00:00:0${i}Z\"}" \
        > /tmp/lazyclaude-q-${TS}.json
    sleep 0.05
done
sleep 1

# 1. Cascade display
echo "--- cascade [3/3] ---" >&2
C=$(capture)
echo "$C" >&2
echo "" >&2
R=0
echo "$C" | grep -q "Tool1" && echo "$C" | grep -q "Tool2" && echo "$C" | grep -q "Tool3" && echo "$C" | grep -q "\[3/3\]" || R=1
check "cascade display (3 titles + [3/3])" $R

# 2. Arrow Up switches focus
tmux -L "$UI_SOCKET" send-keys -t test Up
sleep 0.3
echo "--- arrow up [2/3] ---" >&2
C=$(capture)
echo "$C" >&2
echo "" >&2
R=0; echo "$C" | grep -q "\[2/3\]" || R=1
check "arrow up -> [2/3]" $R

# 3. Esc suspends
tmux -L "$UI_SOCKET" send-keys -t test Escape
sleep 0.3
echo "--- esc suspended ---" >&2
C=$(capture)
echo "$C" >&2
echo "" >&2
R=0; echo "$C" | grep -q "y/a/n" && R=1
check "esc suspends (no action bar)" $R

# 4. p reopens
tmux -L "$UI_SOCKET" send-keys -t test p
sleep 0.3
echo "--- p reopened ---" >&2
C=$(capture)
echo "$C" >&2
echo "" >&2
R=0; echo "$C" | grep -q "Tool" || R=1
check "p reopens popup" $R

# 5. y dismisses focused only (Tool3 gone, Tool1+2 remain)
tmux -L "$UI_SOCKET" send-keys -t test y
sleep 0.3
echo "--- y dismiss focused ---" >&2
C=$(capture)
echo "$C" >&2
echo "" >&2
R=0
echo "$C" | grep -q "Tool1" || R=1
echo "$C" | grep -q "send choice" && R=1
check "y dismisses focused only (others remain)" $R

# 6. y to dismiss focused (Tool2 goes, Tool1 remains)
tmux -L "$UI_SOCKET" send-keys -t test y
sleep 0.3
echo "--- y dismiss Tool2 ---" >&2
C=$(capture)
echo "$C" >&2
echo "" >&2
R=0; echo "$C" | grep -q "Tool1" || R=1
check "y dismiss Tool2 (Tool1 remains)" $R

# 7. requeue 2 more to test Y (accept all)
for i in 4 5; do
    TS=$(date +%s%N)
    echo "{\"tool_name\":\"Tool${i}\",\"input\":\"{\\\"a\\\":\\\"${i}\\\"}\",\"window\":\"@0\",\"timestamp\":\"2026-01-01T00:00:0${i}Z\"}" \
        > /tmp/lazyclaude-q-${TS}.json
    sleep 0.05
done
sleep 1

echo "--- Y accept all ---" >&2
tmux -L "$UI_SOCKET" send-keys -t test Y
sleep 0.3
C=$(capture)
echo "$C" >&2
echo "" >&2
R=0
echo "$C" | grep -q "y/a/n" && R=1
echo "$C" | grep -q "Tool" && R=1
check "Y accepts all at once" $R

echo "" >&2
echo "Results: $PASS passed, $FAIL failed" >&2
[ "$FAIL" -eq 0 ]
