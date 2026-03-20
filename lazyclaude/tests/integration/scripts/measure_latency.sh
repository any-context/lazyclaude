#!/bin/bash
# Measure lazyclaude full-screen display latency.
# Compares 1st launch (no tmux session) vs 2nd launch (existing session).
#
# Method: send unique characters to Claude Code via lazyclaude full-screen,
# measure time until the character appears in the outer tmux pane (= gocui rendered).
#
# PASS: both launches have similar latency (< 2x difference)
# FAIL: 1st launch is significantly slower

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/test_lib.sh"

init_test "Latency Measurement" "${1:-lazyclaude}" "${@:2}"

SAMPLES=5
MAX_WAIT_MS=3000

now_ms() {
    python3 -c 'import time; print(int(time.time()*1000))'
}

# measure_key: send a unique char, measure until it appears in outer pane
measure_key() {
    local char="$1"
    local start elapsed

    start=$(now_ms)
    send_keys "$char"

    while true; do
        elapsed=$(( $(now_ms) - start ))
        if [ "$elapsed" -ge "$MAX_WAIT_MS" ]; then
            echo "$MAX_WAIT_MS"
            return
        fi
        if capture | grep -qF "$char"; then
            echo "$elapsed"
            return
        fi
        sleep 0.005
    done
}

run_measurement() {
    local label="$1"
    echo "" >&2
    echo "--- $label launch ---" >&2

    # Start lazyclaude in outer tmux
    start_session "$BINARY"
    sleep 2

    if capture | grep -q "> "; then
        echo "  Existing session found" >&2
    else
        echo "  Creating session..." >&2
        send_keys "n"
        sleep 1
    fi

    wait_for ">" 15 || { echo "FAIL: no session" >&2; return 1; }
    frame "$label: session list"

    # Enter full-screen
    send_keys "Enter"
    sleep 2
    wait_for "INSERT" 10 || { echo "FAIL: no full-screen" >&2; return 1; }
    frame "$label: full-screen entered"

    # Wait for Claude Code prompt (the > or input area)
    echo "  Waiting for Claude Code ready..." >&2
    sleep 5

    # Measure: send unique characters and time their appearance
    local total=0 measurements=()
    local chars=("Z" "X" "Q" "W" "J")

    for i in $(seq 0 $((SAMPLES - 1))); do
        local ch="${chars[$i]}"
        local ms
        ms=$(measure_key "$ch")
        measurements+=("$ms")
        total=$((total + ms))
        sleep 0.5
    done

    local avg=$((total / SAMPLES))
    echo "  Measurements: ${measurements[*]}" >&2
    echo "  Average: ${avg}ms" >&2
    frame "$label: measurement complete (avg ${avg}ms)"

    # Send Escape to cancel any partial input, then exit
    send_keys "Escape"
    sleep 0.3
    send_keys "C-\\"
    sleep 0.3
    send_keys "q"
    sleep 0.5
    send_keys "q"
    sleep 1
    tmux -L "$TEST_SOCKET" kill-server 2>/dev/null || true
    sleep 1

    echo "$avg"
}

# 1st launch: kill lazyclaude tmux to ensure clean state
cleanup_test_silent
sleep 1

avg1=$(run_measurement "1st")

# 2nd launch: lazyclaude tmux session still alive
avg2=$(run_measurement "2nd")

echo "" >&2
echo "==============================" >&2
echo "  1st launch: ${avg1}ms" >&2
echo "  2nd launch: ${avg2}ms" >&2

if [ "$avg2" -eq 0 ]; then
    echo "  ERROR: 2nd measurement is 0" >&2
    exit 1
fi

ratio=$((avg1 * 100 / avg2))
echo "  Ratio: ${ratio}%" >&2

if [ "$ratio" -le 200 ]; then
    check "latency ratio <= 200% (${ratio}%)" 0
else
    check "latency ratio <= 200% (${ratio}%)" 1
fi

finish_test
