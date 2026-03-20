#!/bin/bash
# Verify popup stack cascade rendering.
# Writes 3 notifications, checks cascade display, focus switching,
# suspend/reopen, and dismiss.
#
# PASS: all 7 checks pass
# FAIL: any check fails

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/test_lib.sh"

init_test "Popup Stack Test" "${1:-lazyclaude}" "${@:2}"

start_lazyclaude

# Enqueue 3 notifications
for i in 1 2 3; do
    enqueue_notification "Tool${i}"
    sleep 0.05
done
sleep 1

# 1. Cascade display
wait_for "\\[3/3\\]" 5 || true
frame "cascade [3/3]"
C=$(capture)
R=0
echo "$C" | grep -q "Tool1" && echo "$C" | grep -q "Tool2" && echo "$C" | grep -q "Tool3" && echo "$C" | grep -q "\[3/3\]" || R=1
check "cascade display (3 titles + [3/3])" $R

# 2. Arrow Up switches focus
send_keys Up
sleep 0.3
frame "arrow up [2/3]"
C=$(capture)
R=0; echo "$C" | grep -q "\[2/3\]" || R=1
check "arrow up -> [2/3]" $R

# 3. Esc suspends
send_keys Escape
sleep 0.3
frame "esc suspended"
C=$(capture)
R=0; echo "$C" | grep -q "y/a/n" && R=1
check "esc suspends (no action bar)" $R

# 4. p reopens
send_keys p
sleep 0.3
frame "p reopened"
C=$(capture)
R=0; echo "$C" | grep -q "Tool" || R=1
check "p reopens popup" $R

# 5. y dismisses focused only (Tool3 gone, Tool1+2 remain)
send_keys y
sleep 0.3
frame "y dismiss focused"
C=$(capture)
R=0
echo "$C" | grep -q "Tool1" || R=1
echo "$C" | grep -q "send choice" && R=1
check "y dismisses focused only (others remain)" $R

# 6. y to dismiss focused (Tool2 goes, Tool1 remains)
send_keys y
sleep 0.3
frame "y dismiss Tool2"
C=$(capture)
R=0; echo "$C" | grep -q "Tool1" || R=1
check "y dismiss Tool2 (Tool1 remains)" $R

# 7. Requeue 2 more to test Y (accept all)
for i in 4 5; do
    enqueue_notification "Tool${i}"
    sleep 0.05
done
sleep 1

send_keys Y
sleep 0.3
frame "Y accept all"
C=$(capture)
R=0
echo "$C" | grep -q "y/a/n" && R=1
echo "$C" | grep -q "Tool" && R=1
check "Y accepts all at once" $R

finish_test
