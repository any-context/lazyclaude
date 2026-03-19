#!/bin/bash
# Measure lazyclaude full-screen display latency.
# Compares 1st launch (no tmux session) vs 2nd launch (existing session).
#
# Method: send unique characters to Claude Code via lazyclaude full-screen,
# measure time until the character appears in the outer tmux pane (= gocui rendered).
#
# PASS: both launches have similar latency (< 2x difference)
# FAIL: 1st launch is significantly slower

set -euo pipefail

BINARY="${1:-lazyclaude}"
SAMPLES=5
MAX_WAIT_MS=3000
UI_SOCKET="perf-ui"

cleanup() {
    tmux -L "$UI_SOCKET" kill-server 2>/dev/null || true
    tmux -L lazyclaude kill-server 2>/dev/null || true
    rm -f /tmp/lazyclaude-mcp.port
    rm -f "$HOME/.local/share/lazyclaude/state.json"
}
trap cleanup EXIT

capture() {
    tmux -L "$UI_SOCKET" capture-pane -p -t perf 2>/dev/null
}

wait_for() {
    local pattern="$1" timeout="${2:-15}" i=0
    while [ $i -lt $((timeout * 10)) ]; do
        if capture | grep -q "$pattern"; then return 0; fi
        sleep 0.1
        i=$((i + 1))
    done
    echo "TIMEOUT waiting for '$pattern'" >&2
    capture >&2
    return 1
}

now_ms() {
    python3 -c 'import time; print(int(time.time()*1000))'
}

# measure_key: send a unique char, measure until it appears in outer pane
measure_key() {
    local char="$1"
    local start elapsed

    start=$(now_ms)
    tmux -L "$UI_SOCKET" send-keys -t perf "$char"

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
    echo "=== $label launch ===" >&2

    # Start lazyclaude in outer tmux
    tmux -L "$UI_SOCKET" new-session -d -s perf -x 60 -y 25 "$BINARY"
    sleep 2

    if capture | grep -q "> "; then
        echo "  Existing session found" >&2
    else
        echo "  Creating session..." >&2
        tmux -L "$UI_SOCKET" send-keys -t perf "n"
        sleep 1
    fi

    wait_for ">" 15 || { echo "FAIL: no session" >&2; return 1; }

    # Enter full-screen
    tmux -L "$UI_SOCKET" send-keys -t perf "Enter"
    sleep 2
    wait_for "INSERT" 10 || { echo "FAIL: no full-screen" >&2; return 1; }

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
    echo "  --- render ---" >&2
    capture >&2
    echo "" >&2

    # Send Escape to cancel any partial input, then exit
    tmux -L "$UI_SOCKET" send-keys -t perf "Escape"
    sleep 0.3
    tmux -L "$UI_SOCKET" send-keys -t perf "C-\\"
    sleep 0.3
    tmux -L "$UI_SOCKET" send-keys -t perf "q"
    sleep 0.5
    tmux -L "$UI_SOCKET" send-keys -t perf "q"
    sleep 1
    tmux -L "$UI_SOCKET" kill-server 2>/dev/null || true
    sleep 1

    echo "$avg"
}

echo "Latency measurement: $BINARY" >&2
echo "" >&2

# 1st launch: kill lazyclaude tmux to ensure clean state
cleanup
sleep 1

avg1=$(run_measurement "1st")
echo "" >&2

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
    echo "  PASS" >&2
    exit 0
else
    echo "  FAIL: 1st launch ${ratio}% slower" >&2
    exit 1
fi
