#!/bin/bash
# Send a test message from PM to Worker session.
# Used by ssh_msg VHS tape to avoid quoting issues.
set -euo pipefail

SESSIONS=$(/app/bin/lazyclaude sessions --json 2>/dev/null)
PM_ID=$(echo "$SESSIONS" | python3 -c "import json,sys; [print(s['id']) for s in json.load(sys.stdin) if s.get('role')=='pm']" | head -1)
W_ID=$(echo "$SESSIONS" | python3 -c "import json,sys; [print(s['id']) for s in json.load(sys.stdin) if s.get('role')=='worker']" | head -1)

echo "PM=$PM_ID W=$W_ID"

if [ -z "$PM_ID" ] || [ -z "$W_ID" ]; then
    echo "ERROR: Could not find PM or Worker session"
    exit 1
fi

/app/bin/lazyclaude msg send --from "$PM_ID" --type status "$W_ID" "hello from PM"
