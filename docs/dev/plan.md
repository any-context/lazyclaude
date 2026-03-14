# Plan: Simplify Permission Prompt Detection

## Goal

Simplify the permission prompt detection feature. Remove unnecessary complexity
while keeping the feature working for both local and remote SSH sessions.

## Current State (Problems)

1. **`ACTIVE_DIR` (`/tmp/tmux-claude-active/`)** — file-based mirror of the
   in-memory `pidToWindow` map. Redundant. Every `ide_connected` writes a file;
   every disconnect deletes it. The map already holds this data.

2. **`claude-notify-hook.js`** — external script living in the plugin's
   `scripts/` dir. Wrong location (hooks belong in `~/.claude/`). Reads the
   lock file and walks the PID tree *client-side*, then sends only a top-level
   PID to `/notify`. Server then can't match it because it has no tree-walking.

3. **`/notify` handler** — receives a PID from the hook, does a direct
   `pidToWindow.get(pid)` lookup. Fails if the hook sends a wrong PID because
   tree-walking happens on the wrong side.

4. **`settings.json` hook command** — references the external script path.
   Breaks if the plugin is installed elsewhere or on a remote host.

## Requirements

- No server on remote host (ever).
- SSH reverse tunnel already forwards the local MCP port to the remote.
- The lock file (`~/.claude/ide/<port>.lock`) is written on remote by
  `claude-launch.sh`, so the hook can discover port + token on the remote side.
- Hook must be a simple HTTP POST — all logic lives in the server.
- Always keep everything as simple as possible.

## Changes

### Phase 1 — mcp-server.js

1. Delete `ACTIVE_DIR` constant declaration (line near top of file).
2. Remove all `ACTIVE_DIR`-related file ops — 3 locations:
   - `ide_connected`: `fs.mkdirSync(ACTIVE_DIR, ...)` + `fs.writeFileSync(...)`
   - socket `close`: `fs.unlinkSync(path.join(ACTIVE_DIR, String(pid)))`
   - Keep `pidToWindow.delete(pid)` in the close handler — it is NOT `ACTIVE_DIR` code.
3. Fix `/notify` HTTP handler: the inline hook sends only `process.ppid`.
   Walk the process tree upward from that PID until a PID is found in
   `pidToWindow` (use `.has()` — window can be `null` for remote sessions,
   which is valid). Trigger popup with that window.

   ```javascript
   // /notify handler — replace direct pidToWindow.get() with tree walk
   let current = Number(data.pid);
   for (let i = 0; i < 15; i++) {
     if (pidToWindow.has(current)) {
       triggerPopupForWindow(pidToWindow.get(current)); // null is ok
       break;
     }
     try {
       const ppid = execSync(`ps -o ppid= -p ${current} 2>/dev/null`, { encoding: 'utf8' }).trim();
       if (!ppid || ppid === '1' || ppid === '0' || ppid === String(current)) break;
       current = Number(ppid);
     } catch { break; }
   }
   ```

   Note: `pidToWindow` stores Claude CLI process PIDs (from `ide_connected`).
   The hook fires from a child process of Claude, so walking upward from
   `process.ppid` will reach the Claude PID stored in the map.

### Phase 2 — settings.json (inline hook)

Replace the external-script command with a self-contained inline `node -e '...'`
command that:
1. Reads `~/.claude/ide/*.lock` (first match) to find port + authToken.
2. Only fires on `notification_type === 'permission_prompt'`.
3. POSTs `{ pid: process.ppid }` to `http://127.0.0.1:<port>/notify`.
4. Exits silently if server unreachable or no lock file found.

The command is written as a `node -e` string — multi-line logic compressed into
a single JSON string value. No file path dependency. Works on local and remote
identically (remote Claude sees the same port via SSH reverse tunnel).

### Phase 3 — Delete claude-notify-hook.js

Delete `scripts/claude-notify-hook.js`. All logic is now inline + server.

### Phase 4 — README update

Update the Hooks section to show the new inline command and remove references
to the external script.

## Constraints

- No server on remote.
- No new files added.
- `tmux-claude.zsh` and `~/.claude` hooks stay simple.
- Remove all dead code — do not leave `ACTIVE_DIR` or the old hook path.

## File Checklist

| File | Action |
|------|--------|
| `scripts/mcp-server.js` | Remove ACTIVE_DIR; fix /notify PID tree walk |
| `scripts/claude-notify-hook.js` | Delete |
| `~/.claude/settings.json` | Replace hook command with inline node.js |
| `README.md` | Update Hooks section |
