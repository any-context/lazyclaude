#!/bin/bash
# test_lib.sh — Shared visual test library for lazyclaude E2E tests.
#
# Usage:
#   source "$(dirname "${BASH_SOURCE[0]}")/test_lib.sh"
#   init_test "Test Name" lazyclaude [--mode mock|tmux|ssh|claude]
#   ...
#   finish_test
#
# Every test step should call `frame "step name"` to capture and display
# the current UI state. This produces a fixed-size frame output like
# an animation, allowing visual verification of TUI rendering.

set -euo pipefail

# --- Constants ---
TEST_WIDTH=80
TEST_HEIGHT=24
FRAME_NUM=0
PASS=0
FAIL=0
TEST_SOCKET=""
TEST_PANE="test"
BINARY=""
MODE="mock"
_TEST_NAME=""
FRAME_DELAY="${FRAME_DELAY:-0.5}"  # seconds between frames (adjustable)
FRAME_DIFF="${FRAME_DIFF:-1}"     # 1 = show diff between consecutive frames (default: on)
_PREV_FRAME_FILE=""               # temp file for previous frame content

# --- Argument parsing ---
# Call: init_test "name" binary [--mode MODE]
init_test() {
    _TEST_NAME="$1"
    BINARY="${2:-lazyclaude}"
    shift 2 || true

    # Parse --mode from remaining args
    while [ $# -gt 0 ]; do
        case "$1" in
            --mode) MODE="${2:-mock}"; shift 2 ;;
            *) shift ;;
        esac
    done

    # Also allow MODE env var
    MODE="${MODE:-mock}"

    TEST_SOCKET="vt-$$-$(date +%s)"

    echo "" >&2
    echo "================================================================" >&2
    echo "  $_TEST_NAME" >&2
    echo "  mode=$MODE  size=${TEST_WIDTH}x${TEST_HEIGHT}  socket=$TEST_SOCKET" >&2
    echo "================================================================" >&2
    echo "" >&2

    # Temp files for diff mode
    _PREV_FRAME_FILE=$(mktemp /tmp/frame-prev-XXXX)
    _CURR_FRAME_FILE=$(mktemp /tmp/frame-curr-XXXX)

    trap cleanup_test EXIT
    cleanup_test_silent
    sleep 0.3
}

# --- Cleanup ---
cleanup_test_silent() {
    [ -n "$TEST_SOCKET" ] && tmux -L "$TEST_SOCKET" kill-server 2>/dev/null || true
    tmux -L lazyclaude kill-server 2>/dev/null || true
    rm -f /tmp/lazyclaude-mcp.port
    rm -f /tmp/lazyclaude-q-*.json
    rm -f /tmp/lazyclaude-pending-window
    rm -f "$HOME/.local/share/lazyclaude/state.json"
}

cleanup_test() {
    cleanup_test_silent
    rm -f "$_PREV_FRAME_FILE" "$_CURR_FRAME_FILE" 2>/dev/null || true
}

# --- Capture ---
capture() {
    tmux -L "$TEST_SOCKET" capture-pane -p -t "$TEST_PANE" 2>/dev/null
}

# Capture a specific target (for multi-pane tests)
capture_target() {
    local target="$1"
    tmux -L "$TEST_SOCKET" capture-pane -p -t "$target" 2>/dev/null
}

# --- Frame display ---
# Captures current pane and displays it as a numbered frame.
# Clears the screen and redraws in-place so frames animate like sl(1).
# Usage: frame "step description"
_draw_frame() {
    local step_name="$1"
    local content="$2"
    local header="[Frame $FRAME_NUM] $step_name"
    local width=$TEST_WIDTH

    # Move cursor to top-left, clear screen
    printf '\033[2J\033[H' >&2

    # Header
    printf '\033[1;36m%s\033[0m\n' "$header" >&2

    # Top border
    printf '\033[90m'  >&2
    printf '%0.s─' $(seq 1 $width) >&2
    printf '\033[0m\n' >&2

    # Content (pad/truncate to fixed height)
    local line_num=0
    while IFS= read -r line || [ -n "$line" ]; do
        if [ $line_num -lt $TEST_HEIGHT ]; then
            printf '%s\n' "$line" >&2
            line_num=$((line_num + 1))
        fi
    done <<< "$content"
    # Pad remaining lines
    while [ $line_num -lt $TEST_HEIGHT ]; do
        printf '\n' >&2
        line_num=$((line_num + 1))
    done

    # Bottom border
    printf '\033[90m' >&2
    printf '%0.s─' $(seq 1 $width) >&2
    printf '\033[0m\n' >&2

    # Status line
    printf '\033[33mPASS:%d  FAIL:%d\033[0m\n' "$PASS" "$FAIL" >&2

    # Diff mode: show changes from previous frame
    if [ "$FRAME_DIFF" = "1" ]; then
        # Lazy init for scripts that don't call init_test()
        [ -z "$_PREV_FRAME_FILE" ] && _PREV_FRAME_FILE=$(mktemp /tmp/frame-prev-XXXX)
        [ -z "$_CURR_FRAME_FILE" ] && _CURR_FRAME_FILE=$(mktemp /tmp/frame-curr-XXXX)
    fi
    if [ "$FRAME_DIFF" = "1" ] && [ -n "$_PREV_FRAME_FILE" ]; then
        printf '%s\n' "$content" > "$_CURR_FRAME_FILE"
        if [ -s "$_PREV_FRAME_FILE" ]; then
            local diff_out
            diff_out=$(diff "$_PREV_FRAME_FILE" "$_CURR_FRAME_FILE" 2>/dev/null || true)
            if [ -n "$diff_out" ]; then
                printf '\n\033[90m--- diff (Frame %d -> %d) ---\033[0m\n' "$((FRAME_NUM - 1))" "$FRAME_NUM" >&2
                while IFS= read -r dline; do
                    case "$dline" in
                        \<*) printf '\033[31m%s\033[0m\n' "$dline" >&2 ;;
                        \>*) printf '\033[32m%s\033[0m\n' "$dline" >&2 ;;
                        *)   printf '\033[90m%s\033[0m\n' "$dline" >&2 ;;
                    esac
                done <<< "$diff_out"
                printf '\033[90m--- end diff ---\033[0m\n' >&2
            else
                printf '\n\033[90m(no changes from previous frame)\033[0m\n' >&2
            fi
        fi
        cp "$_CURR_FRAME_FILE" "$_PREV_FRAME_FILE"
    fi

    sleep "$FRAME_DELAY"
}

frame() {
    local step_name="$1"
    FRAME_NUM=$((FRAME_NUM + 1))
    _draw_frame "$step_name" "$(capture)"
}

# Frame with a specific capture target
frame_target() {
    local step_name="$1"
    local target="$2"
    FRAME_NUM=$((FRAME_NUM + 1))
    _draw_frame "$step_name" "$(capture_target "$target")"
}

# --- Assertions ---
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

# Check with auto-capture on failure
check_with_capture() {
    local name="$1" result="$2"
    if [ "$result" -eq 0 ]; then
        echo "  PASS: $name" >&2
        PASS=$((PASS + 1))
    else
        echo "  FAIL: $name" >&2
        echo "  --- failure capture ---" >&2
        capture >&2
        echo "  --- end ---" >&2
        FAIL=$((FAIL + 1))
    fi
}

# Assert that capture output contains a string
assert_contains() {
    local text="$1" name="${2:-contains '$1'}"
    local content
    content=$(capture)
    local r=0
    echo "$content" | grep -q "$text" || r=1
    check "$name" $r
}

# Assert that capture output does NOT contain a string
assert_not_contains() {
    local text="$1" name="${2:-not contains '$1'}"
    local content
    content=$(capture)
    local r=0
    echo "$content" | grep -q "$text" && r=1
    check "$name" $r
}

# --- Wait ---
wait_for() {
    local pattern="$1" timeout="${2:-5}" i=0
    while [ $i -lt $((timeout * 10)) ]; do
        if capture | grep -qE "$pattern"; then return 0; fi
        sleep 0.1
        i=$((i + 1))
    done
    return 1
}

# --- Send keys ---
send_keys() {
    tmux -L "$TEST_SOCKET" send-keys -t "$TEST_PANE" "$@"
}

# Send keys to a specific target
send_keys_target() {
    local target="$1"
    shift
    tmux -L "$TEST_SOCKET" send-keys -t "$target" "$@"
}

# --- Session management ---
start_session() {
    local cmd="${1:-$BINARY; sleep 999}"
    tmux -L "$TEST_SOCKET" new-session -d -s test -x "$TEST_WIDTH" -y "$TEST_HEIGHT" "$cmd"
}

# Start lazyclaude and wait for TUI
start_lazyclaude() {
    start_session "$BINARY; sleep 999"
    if ! wait_for "no sessions" 5; then
        echo "FAIL: TUI did not start" >&2
        frame "TUI startup failed"
        exit 1
    fi
    frame "TUI started"
}

# Enqueue a mock notification JSON file
enqueue_notification() {
    local tool_name="$1"
    local window="${2:-@0}"
    local extra="${3:-}"
    local ts
    ts=$(date +%s%N)
    local json="{\"tool_name\":\"${tool_name}\",\"input\":\"{\\\"a\\\":\\\"1\\\"}\",\"window\":\"${window}\",\"timestamp\":\"2026-01-01T00:00:00Z\"${extra}}"
    echo "$json" > "/tmp/lazyclaude-q-${ts}.json"
}

# --- Finish ---
finish_test() {
    printf '\033[2J\033[H' >&2
    printf '\033[1m================================================================\033[0m\n' >&2
    printf '  %s\n' "$_TEST_NAME" >&2
    printf '  \033[32mPASS: %d\033[0m  ' "$PASS" >&2
    if [ "$FAIL" -gt 0 ]; then
        printf '\033[1;31mFAIL: %d\033[0m\n' "$FAIL" >&2
    else
        printf 'FAIL: %d\n' "$FAIL" >&2
    fi
    printf '\033[1m================================================================\033[0m\n' >&2
    [ "$FAIL" -eq 0 ]
}
