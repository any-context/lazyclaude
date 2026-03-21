#!/bin/bash
# VHS .txt のフレーム表示 + diff。
# stdout: プレーンテキスト (ログ用)
# stderr: 画面クリア (TTY 用)
#
# 必要な変数:
#   TAPE_NAME — テープ名

PREV_FRAME="/tmp/prev_frame.txt"
CURR_FRAME="/tmp/curr_frame.txt"
: > "$PREV_FRAME"
FRAME_N=0

show_frame() {
    local block="$1"

    echo "$block" | sed '/^[[:space:]]*$/d' > "$CURR_FRAME"

    if [ -s "$PREV_FRAME" ] && diff -q "$PREV_FRAME" "$CURR_FRAME" >/dev/null 2>&1; then
        return
    fi

    FRAME_N=$((FRAME_N + 1))

    # 画面クリアは stderr (TTY) のみ
    printf '\033[2J\033[H' >&2

    # フレーム内容は stdout (プレーンテキスト)
    printf '[Frame %d] %s\n' "$FRAME_N" "$TAPE_NAME"
    printf '%0.s─' $(seq 1 80)
    printf '\n'

    cat "$CURR_FRAME"

    printf '%0.s─' $(seq 1 80)
    printf '\n'

    if [ -s "$PREV_FRAME" ]; then
        local d
        d=$(diff "$PREV_FRAME" "$CURR_FRAME" 2>/dev/null || true)
        if [ -n "$d" ]; then
            echo "--- diff (Frame $((FRAME_N - 1)) -> $FRAME_N) ---"
            echo "$d"
            echo "--- end diff ---"
        fi
    fi
    cp "$CURR_FRAME" "$PREV_FRAME"
}

cleanup_frames() {
    rm -f "$PREV_FRAME" "$CURR_FRAME"
}
