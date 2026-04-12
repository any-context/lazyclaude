<!-- Last Updated: 2026-04-08 | daemon-arch: added Session.Role display, mirror window support, remote path resolution -->

# Frontend (TUI)

**Last Updated:** 2026-04-08 (daemon-arch)

## GUI Package (internal/gui/)

gocui-based terminal UI with keybinding-driven navigation, rich activity visualization, and search filtering.

## View Hierarchy

```
Main Layout (layout.go, 672 lines)
+-- projectListView     (project tree sidebar with activity icons + tool names)
+-- sessionListView     (session list per project, filterable with fzf-style search)
+-- previewView         (live session preview / scrollback browser)
+-- statusBarView       (bottom status bar with filter indicator)
+-- Overlays:
    +-- popupView       (permission prompt dialog, stacked)
    +-- diffView        (diff viewer popup)
    +-- worktreeDialog  (worktree selection)
    +-- confirmView     (confirmation dialog)
    +-- inputView       (text input)
    +-- helpView        (keybinding help overlay - Telescope-style)
```

## Activity State Visualization and Role Display

Sidebar icons reflect ActivityState:
- `[Running]` spinner icon + tool name (e.g., "[Running] code_execution")
- `[NeedsInput]` exclamation icon (user prompt pending)
- `[Idle]` checkmark icon (idle, awaiting commands)
- `[Error]` cross icon (last operation failed)
- `[Dead]` dash icon (window terminated)

Session roles displayed with prefix:
- `[PM]` -- Project Manager session (pm role)
- `[W]` -- Worker session (worker role)
- No prefix for plain sessions

Badge cleared on fullscreen entry and session selection to prevent stale indicators.

**daemon-arch addition:** Role field propagated from SessionCreateResponse to session.Role, persisted in state.json, displayed in sidebar formatting

## Key Input Pipeline

```
1. View-specific bindings (popup, dialog) - highest priority
2. Search filter mode (/) - when active, "/" key + text input
3. Editor.Edit() -- Editable=true views only
4. Global bindings -- rune keys skipped in Editable views
```

Dispatched via:
```
keydispatch/dispatcher.go  -- key event routing
keyhandler/handler.go      -- per-view handlers
keyhandler/panel.go        -- panel navigation
keyhandler/plugins.go      -- plugin view keys
keymap/registry.go (491 lines) -- configurable keybinding registry
```

## Fullscreen Mode Features

- **Direct Keyboard Forwarding** -- all keys except Ctrl+\ / Ctrl+D forwarded to Claude Code
- **Scrollback Browser** -- enter with mouse wheel or Ctrl+N in fullscreen
  - `j/k` -- line scroll
  - `Ctrl+F/B` -- half-page scroll (Ctrl+D/U consumed by PTY)
  - `J/K` (Shift) -- half-page scroll alternative
  - `PgUp/PgDn` -- half-page scroll
  - `g/G` -- jump to start / end
  - `v` -- toggle visual selection
  - `y` -- copy selection (ANSI-aware)
- **Mouse Wheel Support** -- enter scroll mode on mouse wheel in fullscreen

## Core GUI Files

```
app.go            (352+ lines) -- App state machine, lifecycle, activity tracking
app_actions.go    (719 lines) -- session/panel action handlers
layout.go         (672 lines) -- view creation, layout calculation
render.go         (294+ lines) -- main rendering pipeline, activity icon rendering
keybindings.go    (321 lines) -- default keybinding setup
popup.go          (249 lines) -- permission prompt overlay
fullscreen.go     (234+ lines) -- direct keyboard forwarding + scroll mode entry
scroll.go         -- scrollback browsing state + navigation
search.go         -- fzf-style search filtering state
input.go          (211 lines) -- text input component
state.go          -- app state (focused view, sessions, projects, search filter)
```

## Search Filtering

- Activate with `/` key on any pane (projects, sessions, plugins, MCP)
- Prefix-based fuzzy matching (similar to fzf)
- Visual indicator at panel bottom while filter is active
- `Esc` to clear filter and return to normal navigation

## Presentation Layer (gui/presentation/)

```
sessions.go  -- session list formatting with activity icons + tool names
diff.go      -- diff rendering
style.go     (40+ lines) -- ANSI styling for activity states
tool.go      -- tool notification display
```

Activity state styling:
```
Running   -> bright green spinner + tool name
NeedsInput -> bright yellow exclamation + "needs input"
Idle      -> green checkmark
Error     -> red cross
Dead      -> gray dash
```

## Adapters (cmd/lazyclaude/)

GUI interfaces are satisfied by adapters in root.go:
```
SessionProvider -> sessionAdapter (wraps session.Manager)
PluginProvider  -> pluginAdapter  (wraps plugin.Manager)
MCPProvider     -> mcpAdapter     (wraps mcp.Manager)
NotificationCacher -> sessionAdapter (notification badge cache management)
```

## Key State Machines

**App Mode**
- `ModeMain` -- normal TUI display (sessions, preview, sidebar)

**Popup Manager**
- Stack-based popup management
- Multiple stacked popups (swappable with Left/Right)
- Restoring hidden popups with `p` key

**Search Filter State**
- Active flag + current filter text
- Filtered list of projects/sessions/plugins maintained
- Visual indicator on panel bottom
