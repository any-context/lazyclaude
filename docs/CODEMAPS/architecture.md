<!-- Last Updated: 2026-04-11 | Added daemon package to dependency graph, covering CompositeProvider and command routing -->

# Architecture

**Last Updated:** 2026-04-11 (daemon-arch)

## System Overview

Go TUI application for managing Claude Code sessions as a tmux plugin.
Two-tier tmux architecture with built-in MCP WebSocket/HTTP server.
Rich sidebar status with 5-stage activity state tracking (Running, NeedsInput, Idle, Error, Dead).

## High-Level Data Flow

```
Claude Code (local/SSH)
    |
    +---> POST /notify (permission prompt)
    +---> POST /prompt-submit (prompt entry detection)
    |         |
    +---> MCP Server (WebSocket + HTTP, random port)
    |     |-- event.Broker (in-process pub/sub)
    |     +-- notify queue (file-based, SSH fallback)
    |     +-- activity state tracking (ActivityState enum)
    |         |
    +---> TUI (gocui)
    |     |-- sidebar with activity icons + tool names
    |     |-- popup (permission prompt overlay)
    |     +-- fullscreen mode with scrollback browser
    |     +-- search filtering (fzf-style "/" key)
    |         |
    +---> tmuxadapter.SendToPane
    |         |
    +---> Claude Code (receives keystroke)
```

## Two-Tier Tmux

1. **User tmux** (default socket) -- `display-popup` shows lazyclaude TUI
2. **lazyclaude tmux** (`-L lazyclaude` socket) -- manages Claude Code session windows

## Package Dependency Graph

```
cmd/lazyclaude (CLI entry, Cobra)
  +-- gui           (TUI rendering, gocui)
  |    +-- keydispatch (key event routing)
  |    +-- keyhandler  (per-view + panel handlers)
  |    +-- keymap      (configurable keybinding registry)
  |    +-- presentation (formatting, styling)
  +-- daemon         (remote session API + provider routing)
  |    +-- CompositeProvider (local/remote provider dispatch)
  |    +-- RemoteProvider (SSH-backed remote daemon client)
  +-- session        (session CRUD, tmux sync, persistence)
  +-- server         (MCP WebSocket/HTTP server)
  |    +-- activity state + notifications
  |    +-- IDE lock file management
  +-- mcp            (MCP server config management)
  +-- plugin         (claude plugins CLI wrapper)
  +-- notify         (file-based notification queue)
  +-- adapter/tmuxadapter (sendkeys, detect)
  +-- core/
       +-- tmux      (tmux client interface, exec + control)
       +-- config    (paths, hooks)
       +-- event     (generic pub/sub broker)
       +-- lifecycle (LIFO cleanup)
       +-- model     (ToolNotification, ActivityState, Event)
       +-- choice    (Choice enum)
       +-- shell     (quoting)
```

## Key Design Patterns

- **Pub/Sub:** `core/event.Broker` for in-process notification dispatch
- **Activity State Machine:** `model.ActivityState` (Running, NeedsInput, Idle, Error, Dead) for precise session status
- **Adapter:** SessionAdapter, PluginAdapter, MCPAdapter bridge internal types to GUI
- **Interface-based testing:** Tmux client interface with mock implementation
- **LIFO Cleanup:** `core/lifecycle` for ordered resource teardown

## Entry Points

- `cmd/lazyclaude/main.go` -- Cobra root command
- `scripts/lazyclaude-launch.sh` -- tmux plugin entry (display-popup)
- `lazyclaude setup` -- install hooks + keybindings (--settings flag injection)
- `lazyclaude server` -- start MCP daemon (manual)

## Recent Enhancements

**daemon-arch branch (Apr 2026):**
- **Mirror Windows** -- SSH-attached tmux windows for transparent remote session interaction
- **Grouped Tmux Sessions** -- Each mirror gets own grouped session (new-session -t lazyclaude -s {name})
- **PostCreateHook Pattern** -- Side-effect hook called after remote session creation
- **Session Role Propagation** -- PM/Worker roles displayed with [PM]/[W] prefixes in sidebar
- **Lazy Remote Connection** -- First remote operation triggers connection (sync.Once per host)
- **Optimistic UI** -- Create shows placeholder immediately, finishes in background
- **Path Resolution** -- resolveRemotePath() bridges local and remote CWDs via daemon API

**stg branch (earlier Apr 2026):**
- **Rich Sidebar Status** -- 5-stage ActivityState with icons + tool name display
- **Scrollback Browser** -- vim-like navigation (j/k, g/G, v for select, y to copy) in fullscreen
- **Search Filtering** -- fzf-style "/" key for session/plugin/MCP filtering with visual indicator
- **Prompt Submit Detection** -- POST /prompt-submit hook + UserPromptSubmit event for activity tracking
- **Settings Flag Injection** -- --settings flag replaces JSON file modification for hook injection
- **Mouse Wheel Scroll** -- enter scroll mode on mouse wheel in fullscreen
