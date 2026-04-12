<!-- Last Updated: 2026-04-11 | Added SessionCommandService, MirrorManager, RemoteHostManager, and routing integration tests -->

# Backend

**Last Updated:** 2026-04-11 (daemon-arch)

## CLI Commands (Cobra)

```
lazyclaude           -- Interactive TUI (default), flags: --debug, --log-file
lazyclaude server    -- MCP daemon, flags: --port, --token
lazyclaude setup     -- Install hooks + keybindings (--settings flag)
lazyclaude daemon    -- SSH daemon management (internal, used for remote hosts)
lazyclaude diff      -- Diff viewer popup (internal use)
lazyclaude tool      -- Tool confirmation popup (internal use)
```

## MCP Server (internal/server/)

HTTP routes (mux in server.go):
```
POST   /notify               -> handleNotify              (permission prompt from hooks)
POST   /prompt-submit        -> handlePromptSubmit        (prompt entry detection)
POST   /msg/send             -> handleMsgSend             (send message to session)
GET    /msg/sessions         -> handleMsgSessions         (list active sessions)
POST   /msg/create           -> handleMsgCreate           (API-driven session spawning)
UPGRADE /                    -> WebSocket MCP              (JSON-RPC protocol)
```

WebSocket MCP methods (handler.go):
```
initialize                  -> protocol handshake (version + capabilities)
notifications/initialized   -> async ack (no response)
ide_connected               -> IDE connection event
openDiff                    -> diff viewer popup
```

Activity & Notification Events (server.go):
```
PreToolUse hook              -> publish ActivityNotification (Running state + tool name)
/prompt-submit endpoint      -> publish ActivityNotification (NeedsInput state)
Window death                 -> publish ActivityNotification (Dead state)
Idle detection               -> publish ActivityNotification (Idle state)
Tool error                   -> publish ActivityNotification (Error state)
```

Key files:
```
server/server.go       (676 lines) -- HTTP/WS server lifecycle + activity tracking
server/handler.go      (188 lines) -- MCP message dispatch
server/handler_msg.go  (428 lines) -- message-specific handlers + /msg/create
server/state.go        (182 lines) -- connection/session state
server/lock.go         (182 lines) -- IDE lock file management
server/ensure.go       (169 lines) -- health checks + startup
server/jsonrpc.go      -- JSON-RPC protocol utilities
```

## Daemon Architecture (internal/daemon/)

**Remote session management via HTTP API with mirror window strategy**

```
composite_provider.go  -- Unified local+remote provider routing via CompositeProvider
remote_provider.go     -- HTTP client for remote daemon API
  PostCreateHook      -- Called after remote session creation for mirror setup
  ConnectionManager   -- SSH tunnel + daemon connection lifecycle
server.go            -- Remote daemon HTTP server (runs on remote host)
api.go               -- SessionCreateResponse (includes Role field), SessionCreateRequest
client.go            -- HTTP session client (local TUI <-> remote daemon)
http_client.go       -- Low-level HTTP utilities
```

**Key patterns:**
- Functional option: `WithPostCreate(hook PostCreateHook)` for side-effects
- Remote daemon API includes: POST /session/create, DELETE /session/{id}, POST /session/{id}/rename, GET /sessions, GET /cwd
- Response includes: ID, Name, Path, TmuxWindow, Role ("pm", "worker", or empty)

## Session Management (internal/session/)

```
manager.go     (631 lines) -- CRUD, tmux sync, project grouping, role management
store.go       (613 lines) -- JSON persistence (~/.local/share/lazyclaude/state.json)
  Fields       -- ID, Name, Path, Host (remote hostname), TmuxWindow (local mirror), Role
  SyncWithTmux -- Detects rm- and lc- windows, ignores external tmux changes
service.go     -- SessionService interface
project.go     -- project grouping by git root
role.go        -- PM/Worker role management + session creation
worktree.go    -- git worktree operations
ssh.go         -- SSH remote session setup
gc.go          -- orphan session garbage collector
```

## MCP Config (internal/mcp/)

```
config.go   (177 lines) -- read/write ~/.claude.json + project settings
manager.go  (140 lines) -- MCP server operations
cli.go      -- `claude mcp` CLI wrapper
model.go    -- data models
```

## Plugin Management (internal/plugin/)

```
cli.go      (197 lines) -- `claude plugins` subprocess wrapper
manager.go  (156 lines) -- plugin operations coordinator
model.go    -- data models
```

## Notification (internal/notify/)

```
notify.go   (100 lines) -- file-based queue (/tmp/lazyclaude-q-*.json)
```

## Core Libraries (internal/core/)

```
tmux/exec.go      (432 lines) -- tmux command execution
tmux/control.go   (380 lines) -- multiplexed control mode client
tmux/mock.go      (207 lines) -- mock for testing

config/hooks.go   (165+ lines) -- Claude Code hook installation + --settings flag
config/config.go  (76 lines)  -- path management (IDEDir, DataDir, RuntimeDir)

event/broker.go   (124 lines) -- generic pub/sub broker

lifecycle/lifecycle.go (83 lines) -- LIFO cleanup

model/notification.go   (140+ lines) -- ToolNotification, ActivityNotification, ActivityState enum
model/notification_test.go -- comprehensive notification + state tests

shell/quote.go    -- shell escaping
choice/choice.go  -- Choice enum (1/2/3)
```

## State Models (internal/core/model/)

ActivityState enum values:
```go
const (
    ActivityStateRunning   // Tool execution in progress
    ActivityStateNeedsInput // User prompt awaiting response
    ActivityStateIdle      // Idle, awaiting commands
    ActivityStateError     // Last operation errored
    ActivityStateDead      // Window terminated
)
```

Events published by server:
- `ActivityNotification` (ActivityState + tool name + window ID)
- `ToolNotification` (permission prompts)
- Generic `Event` for pubsub

## GUI Composite Adapter & Command Routing (cmd/lazyclaude/)

**Bridge between GUI and dual local/remote session management with operation routing**

```
gui_adapter.go         (333 lines) -- guiCompositeAdapter implements gui.SessionProvider
  Routes to CompositeProvider or SessionCommandService
  resolveHost()        -- Resolves current operation host (cached or pending)
  ensureRemoteConnected() -- Lazy remote connection (sync.Once per host)
  currentHostFn()      -- Cached cursor position for routing decisions (n vs N)

session_command.go     (345 lines) -- SessionCommandService: command routing dispatcher
  Create/Delete/Rename -- Route to local or remote provider based on host
  CreateWorktree       -- Git worktree operations with role assignment
  CreatePMSession      -- Spawn PM role session with optional mirror
  CreateWorkerSession  -- Spawn Worker role session (for PM/Worker multi-agent)
  LaunchLazygit        -- Route lazygit to local or remote

local_provider.go      (227 lines) -- LocalDaemonProvider implements daemon.SessionProvider
  Routes to session.Manager for local-only operations

mirror.go              (197 lines) -- MirrorManager: creates local mirror windows
  CreateMirror()       -- Creates rm-prefixed tmux window with SSH attach command
  SSH command encoding -- base64 escaping for shell injection prevention
  Grouped sessions     -- tmux new-session -t lazyclaude -s {name}

remote_host.go         (78 lines)  -- RemoteHostManager: SSH connection state
  ConnectionHost       -- Lazy connection wrapper (sync.Once per host)
  ensureConnected()    -- Establishes SSH tunnel on first use

Lazy connection pattern:
  lazyConn struct with sync.Once ensures exactly one connectFn call per host
  Subsequent callers see cached result without retrying
```

**Command Routing Hierarchy:**
```
GUI Input (n/N/w/W/P/d/R)
  ↓
guiCompositeAdapter (resolveHost dispatcher)
  ↓
SessionCommandService (operation routing)
  ├─ host == "" → LocalProvider
  └─ host != "" → RemoteProvider (via CompositeProvider)
       ├─ ensureRemoteConnected (sync.Once per host)
       ├─ resolveRemotePath (daemon GET /cwd)
       └─ PostCreateHook (MirrorManager.CreateMirror)
```

**Testing:**
- `routing_integration_test.go` (980 lines) -- End-to-end behavior tests for all 30 routing cases
  - Tests real SessionCommandService + CompositeProvider + MirrorManager stack
  - Fakes: network-facing APIs + tmux.MockClient
  - Verifies final session.Store state after each command

**Key behavioral patterns:**
- **Optimistic UI**: Create shows placeholder immediately, finishes in background
- **Lazy remote connection**: First remote operation triggers connection (not on connect dialog alone)
- **Path resolution**: resolveRemotePath() calls daemon GET /cwd after connection established
- **PostCreateHook**: RemoteProvider calls hook after API response to set up mirror
- **Grouped sessions**: Mirror windows use `new-session -t lazyclaude -s {localWindowName}` for independent selection
- **Host resolution**: n (cursor-based) vs N (pane-based, pendingHost only)
