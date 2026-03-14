# tmux-claude

A tmux plugin that manages Claude AI sessions with popup windows, SSH remote support, and live notifications via a local MCP server.

## Features

- Launch Claude in a dedicated tmux session with a single keybind
- Reuse existing windows per working directory (local) or per SSH host/path (remote)
- Respawn dead panes automatically on re-launch
- Persistent MCP server that detects file writes and raises a popup notification
- SSH remote support: reverse tunnel connects remote Claude back to the local MCP server
- Session switcher with fzf preview

## Requirements

- tmux >= 3.0
- Node.js (for the MCP server)
- [claude CLI](https://claude.ai/code)
- fzf (for the session switcher)

## Installation

### With [TPM](https://github.com/tmux-plugins/tpm)

```tmux
set -g @plugin 'KEMSHlM/tmux-claude'
```

Press `prefix + I` to install.

### Manual

```bash
git clone https://github.com/KEMSHlM/tmux-claude ~/.local/share/tmux/plugins/tmux-claude
```

Add to `~/.tmux.conf`:

```tmux
run-shell ~/.local/share/tmux/plugins/tmux-claude/claude.tmux
```

### MCP server (zsh)

Add to `~/.zshrc`:

```zsh
source ~/.local/share/tmux/plugins/tmux-claude/tmux-claude.zsh
```

This starts a persistent WebSocket MCP server on login. Claude CLI auto-connects to it via the lock file at `~/.claude/ide/<port>.lock`.

## Keybindings

<!-- AUTO-GENERATED from claude.tmux -->
| Key | Action |
|-----|--------|
| `prefix + a` | Launch Claude in current directory |
| `prefix + A` | Launch Claude with `--resume` |
| `prefix + O` | Open session switcher |

Keys are configurable — see [Configuration](#configuration).
<!-- END AUTO-GENERATED -->

## Configuration

Set options in `~/.tmux.conf` before loading the plugin:

| Option | Default | Description |
|--------|---------|-------------|
| `@claude-launch-key` | `a` | Key to launch Claude |
| `@claude-resume-key` | `A` | Key to launch Claude with `--resume` |
| `@claude-switch-key` | `O` | Key to open session switcher |
| `@claude-suppress-keys` | `w` | Space-separated keys to suppress inside Claude popup |
| `@claude-notify-type` | `popup` | Notification style: `popup` or `menu` |

Example:

```tmux
set -g @claude-launch-key 'Space'
set -g @claude-notify-type 'menu'
```

## Hooks (permission prompt detection)

tmux-claude can detect when Claude shows a tool permission dialog and automatically raise a popup — even when you're in another window.

### Setup

Add the following to `~/.claude/settings.json`:

```json
{
  "hooks": {
    "Notification": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "node -e \"let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const i=JSON.parse(d);if(i.notification_type!=='permission_prompt')return;const fs=require('fs'),path=require('path'),http=require('http'),home=require('os').homedir();const lockDir=path.join(home,'.claude','ide');const locks=fs.readdirSync(lockDir).filter(f=>f.endsWith('.lock'));if(!locks.length)return;const lock=JSON.parse(fs.readFileSync(path.join(lockDir,locks[0]),'utf8'));const port=parseInt(locks[0],10);const body=JSON.stringify({pid:process.ppid});const req=http.request({hostname:'127.0.0.1',port,path:'/notify',method:'POST',timeout:2000,headers:{'Content-Type':'application/json','Content-Length':Buffer.byteLength(body),'X-Claude-Code-Ide-Authorization':lock.authToken}});req.on('error',()=>{});req.on('timeout',()=>{req.destroy()});req.write(body);req.end()}catch{}});\""
          }
        ]
      }
    ]
  }
}
```

### How it works

When Claude shows a permission dialog, the `Notification` hook fires with `notification_type: "permission_prompt"`. The inline hook command:

1. Reads `~/.claude/ide/<port>.lock` to find the MCP server port and auth token
2. Exits silently if no lock file exists (MCP server not running)
3. Sends `POST /notify` to `127.0.0.1:<port>` with `{ pid: process.ppid }`
4. The MCP server walks the process tree upward from that PID to find the matching Claude session, then opens a popup

If the user is already inside the Claude popup, no new popup is opened.

### Remote support

For remote SSH sessions, the same hook works without any changes. The SSH reverse tunnel set up by tmux-claude forwards the MCP server port to the remote host, so `localhost:<port>` on the remote machine reaches the local MCP server. The lock file at `~/.claude/ide/<port>.lock` is also created on the remote host automatically.

Copy `~/.claude/settings.json` to the remote host to enable permission prompt detection there.

## OSC 7 integration

tmux-claude uses [OSC 7](https://iterm2.com/documentation-escape-codes.html) (`file://hostname/path`) to track the working directory in remote shells. When your shell emits OSC 7 on each prompt, tmux exposes it as `#{pane_path}`.

This is used in two places:

- **Remote directory**: when launching Claude over SSH, OSC 7 lets tmux-claude `cd` into the correct remote directory automatically.
- **Session switcher**: the fzf switcher shows `[hostname] /remote/path` labels instead of opaque window names.

Enable OSC 7 in your remote shell (add to `~/.zshrc` on the remote host):

```zsh
# zsh — emit OSC 7 on each prompt
_osc7_cwd() {
  printf '\e]7;file://%s%s\e\\' "$HOST" "${PWD// /%20}"
}
precmd_functions+=(_osc7_cwd)
```

For iTerm2, Kitty, and WezTerm this is often enabled automatically. Without OSC 7, remote launches still work but Claude opens in the SSH home directory rather than the current path.

## How it works

### Local sessions

`prefix + a` calls `claude-launch.sh` which:

1. Derives a window name from an MD5 of the current directory
2. Creates (or reuses) a window in the `claude` session
3. Respawns the window if its pane is dead
4. Opens a popup showing that window via `claude-popup.sh`

### Remote (SSH) sessions

When the active pane is running `ssh`, `claude-launch.sh`:

1. Parses the SSH hostname from the process args
2. Uses OSC 7 (`#{pane_path}`) to determine the current remote directory (if available)
3. Sets up a reverse tunnel (`-R port:127.0.0.1:port`) back to the local MCP server
4. Creates a lock file on the remote host with the tunnel port so Claude can discover it
5. Launches Claude on the remote host with `CLAUDE_CODE_AUTO_CONNECT_IDE=true`

### MCP server notifications

The MCP server (`scripts/mcp-server.js`) runs locally and:

1. Listens on a random TCP port (written to `/tmp/tmux-claude-mcp.port`)
2. Accepts WebSocket connections from Claude CLI (authenticated via token)
3. On `openDiff` tool calls (file writes), triggers a tmux popup or menu so you can review Claude's work

### Session switcher

`prefix + O` opens an fzf-based switcher listing all windows in the `claude` session with a live preview pane. Labels use OSC 7 paths for remote sessions when available. Keybinds in the switcher:

| Key | Action |
|-----|--------|
| `Enter` | Switch to selected session |
| `1` / `2` / `3` | Send that number to the selected pane |
| `Ctrl-x` | Kill the selected window |

## Runtime files

| File | Contents |
|------|----------|
| `/tmp/tmux-claude-mcp.pid` | MCP server PID |
| `/tmp/tmux-claude-mcp.port` | MCP server port |
| `/tmp/tmux-claude-mcp.token` | MCP auth token |
| `/tmp/tmux-claude-mcp.log` | MCP server log |
| `~/.claude/ide/<port>.lock` | Claude IDE discovery lock file |
