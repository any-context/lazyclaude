# lazyclaude

**English** | [日本語](README_ja.md)

> A [lazygit](https://github.com/jesseduffield/lazygit)-inspired TUI for managing multiple [Claude Code](https://docs.anthropic.com/en/docs/claude-code) sessions.

Live preview, permission prompt popups, scrollback browsing, SSH remote sessions -- all from a single tmux popup.

<p align="center">
  <img src="docs/images/hero.gif" alt="lazyclaude demo" width="800">
</p>

---

## Why lazyclaude?

Claude Code is powerful, but managing multiple sessions is painful:

- **Context switching** -- you can't see what other sessions are doing without `tmux select-window`
- **Permission prompts block** -- you have to be in the right window at the right time to approve
- **No overview** -- which sessions are running, idle, or stuck waiting for input?

lazyclaude solves this with a single TUI that shows all sessions at a glance, routes permission prompts as popups, and lets you approve from anywhere.

## Features

**Session Management**
- Create, rename, delete, and attach to Claude Code sessions
- Live preview of any session's output without leaving the session list
- Project-based grouping with collapsible trees
- Resume terminated sessions with preserved conversation history (`lazyclaude sessions resume`)

**Activity Tracking**
- Real-time 5-stage status for every session:
  `?` Running | `!` Needs Input | `✓` Idle | `✗` Error | `×` Dead
- Status updates via Claude Code hooks (injected automatically, zero config)

**Permission Prompts**
- Tool approval popups appear as overlays -- no need to switch windows
- One-key approval: `1` accept, `2` always allow, `3` reject
- Stacked popups with `Up`/`Down` navigation when multiple sessions need input
- Diff preview with dual line numbers, color-coded additions/deletions, and scrollbar
- Supports Write and Edit tool diffs (unified diff format with clean inline display)
- Works across SSH tunnels

**Fullscreen Mode**
- Direct keyboard forwarding to Claude Code (transparent passthrough)
- Scrollback browser with vim-like navigation (`Ctrl+V` or mouse wheel)
- Visual line selection and clipboard copy (`v` select, `y` copy)

**MCP Server & Plugin Management**
- View and toggle MCP servers registered in Claude Code
- Install, uninstall, and enable/disable Claude Code plugins from the TUI
- Scope-aware operations (project vs global)

**Search & Navigation**
- fzf-style `/` filter on any panel (sessions, plugins, MCP servers)
- `?` Telescope-style keybinding help overlay
- `Tab` / `Shift+Tab` panel cycling

**PM/Worker Multi-Agent**
- Spawn a PM (Project Manager) session that orchestrates multiple Worker sessions
- Workers run in isolated git worktrees with their own branches
- PM and Workers communicate via a built-in message API (`/msg/send`, `/msg/create`)
- Resume terminated workers to continue where they left off (`sessions resume`)
- PM reviews Worker pull requests and sends structured feedback
- Each Worker receives a system prompt with its role, task, and communication instructions

**Infrastructure**
- tmux plugin integration via `display-popup` (`Ctrl+\` to toggle)
- SSH remote sessions with automatic reverse tunnel for notifications
- SSH password authentication via SSH_ASKPASS integration
- Built-in MCP server for Claude Code IDE auto-discovery
- Launch [lazygit](https://github.com/jesseduffield/lazygit) directly from the TUI (optional, if installed)

---

## Requirements

- tmux >= 3.4 (for `display-popup -b rounded`)
- [Claude CLI](https://docs.anthropic.com/en/docs/claude-code)
- [lazygit](https://github.com/jesseduffield/lazygit) (optional -- for in-TUI git management)

## Installation

### Quick install (standalone binary)

```bash
curl -fsSL https://raw.githubusercontent.com/any-context/lazyclaude/prod/install.sh | sh
```

Downloads a pre-built binary to `~/.local/bin/`. No Go required. Run with `lazyclaude`.

> **Note:** This installs the binary only. For tmux plugin integration (`Ctrl+\` popup), use TPM or clone the repo instead.

### With [TPM](https://github.com/tmux-plugins/tpm) (tmux plugin)

Add to `.tmux.conf`:

```tmux
set -g @plugin 'any-context/lazyclaude'
```

Then press `prefix + I` to install. The plugin registers `Ctrl+\` to open lazyclaude as a tmux popup.

### Clone manually (tmux plugin without TPM)

```bash
git clone https://github.com/any-context/lazyclaude ~/.local/share/tmux/plugins/lazyclaude
cd ~/.local/share/tmux/plugins/lazyclaude
make install PREFIX=~/.local
```

Add to `.tmux.conf`:

```tmux
run-shell ~/.local/share/tmux/plugins/lazyclaude/lazyclaude.tmux
```

Then reload: `tmux source ~/.tmux.conf`

### Build from source

Requires Go 1.25+:

```bash
git clone https://github.com/any-context/lazyclaude
cd lazyclaude
make install PREFIX=~/.local
```

---

## Keybindings

### Sessions panel

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate sessions |
| `n` | Create new session |
| `d` | Delete session |
| `Enter` | Fullscreen mode |
| `a` | Attach (direct tmux attach) |
| `R` | Rename session |
| `D` | Purge orphan sessions |

### Fullscreen mode

| Key | Action |
|-----|--------|
| `Ctrl+\` / `Ctrl+D` | Exit fullscreen |
| `Ctrl+V` / Mouse wheel | Enter scroll mode |
| All other keys | Forwarded to Claude Code |

### Scroll mode (in fullscreen)

| Key | Action |
|-----|--------|
| `j` / `k` | Scroll line by line |
| `J` / `K` / `PgUp` / `PgDn` | Half-page scroll |
| `g` / `G` | Jump to top / bottom |
| `v` | Toggle visual line selection |
| `y` | Copy selection to clipboard |
| `Esc` / `q` | Exit scroll mode |

### Popup (permission prompt)

| Key | Action |
|-----|--------|
| `1` | Accept |
| `2` | Allow always |
| `3` | Reject |
| `Y` | Accept all pending |
| `j` / `k` | Scroll content |
| `Up` / `Down` | Switch between stacked popups |
| `Esc` | Hide popup |

### Global

| Key | Action |
|-----|--------|
| `?` | Keybinding help overlay |
| `/` | Search filter on current panel |
| `Tab` / `Shift+Tab` | Cycle panel focus |
| `p` | Restore hidden popups |
| `q` / `Ctrl+C` | Quit |

---

## Architecture

```
+---------------------------+       +---------------------------+
|     User's tmux           |       |   lazyclaude tmux (-L)    |
|  (display-popup)          |       |   Claude Code sessions    |
|                           |       |                           |
|   +-------------------+   |       |   @0: session-1           |
|   | lazyclaude TUI    |<--+-------+-> @1: session-2           |
|   | (gocui)           |   |       |   @2: session-3           |
|   +--------+----------+   |       |                           |
|            |              |       +---------------------------+
|   +--------v----------+   |
|   | MCP Server        |   |       Claude Code hooks POST to:
|   | (in-process)      |<----------  /notify, /stop,
|   | 127.0.0.1:<port>  |   |        /session-start,
|   +-------------------+   |        /prompt-submit
+---------------------------+
```

Hooks are injected at session startup via `claude --settings <file>` -- `~/.claude/settings.json` is never modified. The hooks discover the MCP server via lock file scanning, so they survive server restarts.

---

## Development

```bash
make build         # Build binary
make test          # All tests (race + coverage)
make lint          # golangci-lint
make readme-gif    # Regenerate docs/images/hero.gif (Docker required)
```

## Known Issues

- **Paste in fullscreen mode** -- Pasting text (Cmd+V / Ctrl+Shift+V) in fullscreen mode does not work reliably. This is a limitation of how tmux `display-popup` interacts with bracketed paste sequences. Workaround: use `a` to attach directly to the session, then paste.

## Roadmap

- **Multi-agent support** -- Support for agents beyond Claude Code (e.g. Codex, Gemini CLI, custom agents)
- **Chat viewer** -- Built-in viewer for inter-session message history (PM/Worker conversations)

Have a feature idea? Open an [Issue](https://github.com/any-context/lazyclaude/issues) -- we'd love to hear it.

## Contributing

We welcome contributions! Whether it's bug reports, feature requests, or pull requests -- all are appreciated. See [Issues](https://github.com/any-context/lazyclaude/issues) for current tasks or open a new one.

## License

MIT
