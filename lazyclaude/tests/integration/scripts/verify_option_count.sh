#!/bin/bash
# Verify popup action bar adapts to 2-option vs 3-option dialogs.
#
# Enqueues notifications with max_option=2 and max_option=3,
# checks that the TUI popup action bar shows y/n or y/a/n accordingly.
#
# PASS: all checks pass
# FAIL: any check fails

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/test_lib.sh"

init_test "Option Count Test" "${1:-lazyclaude}" "${@:2}"

start_lazyclaude

# --- Test 1: 2-option dialog (Bash, max_option=2) ---
TS=$(date +%s%N)
echo "{\"tool_name\":\"Bash\",\"input\":\"{\\\"command\\\":\\\"ls /tmp\\\"}\",\"window\":\"@0\",\"timestamp\":\"2026-01-01T00:00:01Z\",\"max_option\":2}" \
    > /tmp/lazyclaude-q-${TS}.json
sleep 1

wait_for "Bash" 3 || true
frame "2-option dialog (Bash)"

C=$(capture)
# Should show y/n, NOT y/a/n
R=0; echo "$C" | grep -q "y/a/n" && R=1
check "2-option: does NOT show y/a/n" $R

R=0; echo "$C" | grep -q "y/n" || R=1
check "2-option: shows y/n" $R

# Dismiss with 'y'
send_keys y
sleep 0.3
frame "2-option dismissed"

# --- Test 2: 3-option dialog (Write, max_option=3) ---
TS=$(date +%s%N)
echo "{\"tool_name\":\"Write\",\"input\":\"{\\\"file_path\\\":\\\"/tmp/hello.txt\\\",\\\"content\\\":\\\"hello\\\"}\",\"window\":\"@0\",\"timestamp\":\"2026-01-01T00:00:02Z\",\"max_option\":3}" \
    > /tmp/lazyclaude-q-${TS}.json
sleep 1

wait_for "Write" 3 || true
frame "3-option dialog (Write)"

C=$(capture)
R=0; echo "$C" | grep -q "y/a/n" || R=1
check "3-option: shows y/a/n" $R

# Dismiss
send_keys y
sleep 0.3
frame "3-option dismissed"

# --- Test 3: default (max_option=0 or missing -> treated as 3) ---
TS=$(date +%s%N)
echo "{\"tool_name\":\"Read\",\"input\":\"{\\\"file_path\\\":\\\"/tmp/test.txt\\\"}\",\"window\":\"@0\",\"timestamp\":\"2026-01-01T00:00:03Z\"}" \
    > /tmp/lazyclaude-q-${TS}.json
sleep 1

wait_for "Read" 3 || true
frame "default (no max_option)"

C=$(capture)
R=0; echo "$C" | grep -q "y/a/n" || R=1
check "default: shows y/a/n (backward compat)" $R

send_keys y
sleep 0.3
frame "default dismissed"

finish_test
