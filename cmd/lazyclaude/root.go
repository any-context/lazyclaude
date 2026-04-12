package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/core/event"
	"github.com/any-context/lazyclaude/internal/core/lifecycle"
	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/daemon"
	"github.com/any-context/lazyclaude/internal/gui"
	"github.com/any-context/lazyclaude/internal/mcp"
	"github.com/any-context/lazyclaude/internal/plugin"
	"github.com/any-context/lazyclaude/internal/server"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/jesseduffield/gocui"
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	var debug bool
	var logFile string

	cmd := &cobra.Command{
		Use:     "lazyclaude",
		Short:   "A standalone TUI for Claude Code",
		Long:    "lazyclaude is a terminal UI for managing Claude Code sessions, inspired by lazygit.",
		Version: fmt.Sprintf("%s (%s)", version, commit),
		RunE: func(cmd *cobra.Command, args []string) error {
			lc := lifecycle.New()
			defer lc.Close()

			var logger *slog.Logger
			paths := config.DefaultPaths()
			tmuxSocket := "lazyclaude"
			if s := os.Getenv("LAZYCLAUDE_TMUX_SOCKET"); s != "" {
				tmuxSocket = s
			}
			tmuxClient := tmux.NewExecClientWithSocket(tmuxSocket)

			os.MkdirAll("/tmp/lazyclaude", 0o755)
			if debug {
				dest := logFile
				if dest == "" {
					dest = "/tmp/lazyclaude/debug.log"
				}
				f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
				if err != nil {
					return fmt.Errorf("open log file: %w", err)
				}
				defer f.Close()
				logger = slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
				logger.Info("lazyclaude.start", "version", version, "logFile", dest)

				cmdLogPath := strings.TrimSuffix(dest, ".log") + "-tmux-cmds.log"
				cmdLogFile, err := os.OpenFile(cmdLogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: open tmux cmd log: %v\n", err)
				} else {
					defer cmdLogFile.Close()
					tmuxClient.SetDebugLog(cmdLogFile)
				}
			}

			store := session.NewStore(paths.StateFile())
			mgr := session.NewManager(store, tmuxClient, paths, logger)

			if err := mgr.Load(context.Background()); err != nil {
				// Non-fatal: tmux might not be running
				fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			}

			// Skip Claude onboarding dialogs (JSON file I/O only, no subprocess)
			mgr.EnsureClaudeConfigured(".")

			// Start the MCP server: prefer in-process so the broker can be wired
			// directly to the GUI for immediate popup delivery (no 100ms poll delay).
			// Falls back to a subprocess if the server is already running as a daemon.
			//
			// The broker is created here (not inside the server) so it outlives
			// server restarts. GUI subscriptions reference this single broker
			// instance throughout the TUI lifetime.
			notifyBroker := event.NewBroker[model.Event]()
			lc.Register("notify-broker", func() { notifyBroker.Close() })

			inProcessSrv := tryStartInProcessServer(paths, tmuxClient, tmuxSocket, logger, notifyBroker)
			if inProcessSrv != nil {
				inProcessSrv.SetSessionLister(&sessionListerAdapter{mgr: mgr})
				inProcessSrv.SetSessionCreator(&sessionCreatorAdapter{mgr: mgr})
				lc.Register("mcp-server", func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					inProcessSrv.Stop(ctx) //nolint:errcheck
				})
			} else {
				// Server already running as a subprocess daemon.
				ensureMCPServer()
			}

			// Start background GC to remove dead/orphan sessions
			gc := session.NewGC(mgr, 2*time.Second)
			gc.Start()
			lc.Register("gc", gc.Stop)

			app, err := gui.NewApp(gui.ModeMain)
			if err != nil {
				return fmt.Errorf("init TUI: %w", err)
			}

			// Always use CompositeProvider so manual 'c' connect can add remotes.
			localProvider := &localDaemonProvider{mgr: mgr, tmux: tmuxClient}
			composite := daemon.NewCompositeProvider(localProvider, nil)

			ssh := &daemon.ExecSSHExecutor{}
			lifecycleMgr := daemon.NewLifecycleManager(ssh)
			clientFactory := func(addr, token string) daemon.ClientAPI {
				return daemon.NewHTTPClient(addr, token)
			}

			// remoteConns tracks active RemoteConnections for status display.
			var remoteConnsMu sync.Mutex
			remoteConns := make(map[string]*daemon.RemoteConnection)

			// Declare compositeAdapter early so connectRemoteHost can reference it.
			compositeAdapter := &guiCompositeAdapter{
				cp:       composite,
				localMgr: mgr,
				paths:    paths,
			}

			// MirrorManager: creates/deletes local tmux mirror windows for
			// remote sessions. Shared by connectRemoteHost and SessionCommandService.
			mirrorMgr := &MirrorManager{
				tmux:    tmuxClient,
				store:   store,
				onError: app.ScheduleError,
			}

			// connectRemoteHost establishes a remote connection pipeline:
			// daemon connection + provider registration + mirror windows for
			// existing sessions + SSE subscription.
			connectRemoteHost := func(host string) error {
				debugLog("connectRemoteHost: host=%q", host)
				remoteConn := daemon.NewRemoteConnection(host, lifecycleMgr, clientFactory)
				if connErr := remoteConn.Connect(context.Background()); connErr != nil {
					debugLog("connectRemoteHost: Connect failed: %v", connErr)
					return fmt.Errorf("lazyclaude is not installed on %s: %w", host, connErr)
				}
				debugLog("connectRemoteHost: Connect succeeded")

				hook := func(host, path string, resp *daemon.SessionCreateResponse) error {
					return mirrorMgr.CreateMirror(host, path, resp)
				}
				activityFwd := func(ev model.Event, sessionID string) {
					notifyBroker.Publish(resolveActivityWindow(mgr.Store(), ev, sessionID))
				}
				toolInfoFwd := func(n *model.ToolNotification, sessionID string) {
					rewriteToolNotificationWindow(mgr.Store(), n, sessionID)
				}
				remoteProvider := daemon.NewRemoteProvider(host, remoteConn,
					daemon.WithPostCreate(hook),
					daemon.WithSSEActivity(activityFwd),
					daemon.WithSSEToolInfo(toolInfoFwd),
				)
				lc.Register("remote-conn-"+host, func() {
					remoteProvider.StopSSE()
					remoteConn.Disconnect()
				})

				composite.AddRemote(host, remoteProvider)

				remoteConnsMu.Lock()
				remoteConns[host] = remoteConn
				remoteConnsMu.Unlock()

				// Create mirror windows for existing remote sessions so they
				// appear in the sidebar immediately with working preview.
				// Only mirror Running sessions — dead/orphan sessions from
				// stale state.json are skipped (GC will clean them).
				if sessions, err := remoteProvider.Sessions(); err == nil {
					var running []daemon.SessionInfo
					for _, s := range sessions {
						if s.Status == "Running" {
							running = append(running, s)
						}
					}
					if len(running) > 0 {
						mirrorMgr.RestoreExisting(host, running)
					}
				}

				// Start SSE for activity state + tool notifications.
				if err := remoteProvider.StartSSE(); err != nil {
					debugLog("connectRemoteHost: StartSSE failed (non-fatal): %v", err)
				}

				debugLog("connectRemoteHost: SUCCESS host=%q", host)
				return nil
			}

			// Auto-detect SSH host from the originating pane.
			// Connection is deferred until the user performs a remote operation.
			// Remote CWD is obtained via daemon API after connection is established.
			pendingSSHHost := gui.DetectSSHHost()
			debugLog("startup: pendingSSHHost=%q", pendingSSHHost)

			// Snapshot local project root so the adapter can distinguish
			// local-fallback paths from genuine remote paths.
			localCWD, err := filepath.Abs(".")
			if err != nil {
				localCWD = "."
			}
			localProjectRoot := session.InferProjectRoot(localCWD)
			debugLog("startup: localProjectRoot=%q", localProjectRoot)
			debugLog("startup: connectFn set=%v", connectRemoteHost != nil)

			guiUpdateFn := func() {
				app.Gui().Update(func(_ *gocui.Gui) error { return nil })
			}
			mirrorMgr.guiUpdateFn = guiUpdateFn

			// RemoteHostManager: manages lazy connections to SSH hosts.
			remoteHostMgr := NewRemoteHostManager(connectRemoteHost)

			// SessionCommandService: centralises session commands
			// so guiCompositeAdapter is a thin pass-through.
			cmdSvc := &SessionCommandService{
				localMgr:            mgr,
				cp:                  composite,
				mirrors:             mirrorMgr,
				tmux:                tmuxClient,
				onError:             app.ScheduleError,
				guiUpdateFn:         guiUpdateFn,
				ensureConnectedFn:   remoteHostMgr.EnsureConnected,
				resolveRemotePathFn: compositeAdapter.resolveRemotePath,
			}

			// Wire remaining fields now that dependencies are available.
			compositeAdapter.pendingHost = pendingSSHHost
			compositeAdapter.localProjectRoot = localProjectRoot
			compositeAdapter.windowActivityFn = app.WindowActivityMap
			compositeAdapter.currentHostFn = app.CurrentSessionHost
			compositeAdapter.onError = app.ScheduleError
			compositeAdapter.commands = cmdSvc
			app.SetSessions(compositeAdapter)

			// Wire connection status for the options bar.
			app.SetConnectionStatus(func() []gui.ConnectionStatus {
				remoteConnsMu.Lock()
				defer remoteConnsMu.Unlock()
				var statuses []gui.ConnectionStatus
				for host, conn := range remoteConns {
					mismatch := false
					if rv := conn.RemoteVersion(); rv != "" && rv != version {
						mismatch = true
					}
					statuses = append(statuses, gui.ConnectionStatus{
						Host:            host,
						State:           conn.State().String(),
						VersionMismatch: mismatch,
					})
				}
				return statuses
			})

			// Wire connect dialog handler. After a successful connection,
			// update the adapter's pending host so subsequent operations
			// (n, w, W, P, N) route to the newly connected host by default.
			app.SetConnectFn(func(host string) error {
				if err := connectRemoteHost(host); err != nil {
					return err
				}
				compositeAdapter.SetPendingHost(host)
				// Sync the lazyConn cache so EnsureConnected
				// skips the redundant connectFn call for this host.
				remoteHostMgr.MarkConnected(host)
				return nil
			})

			// Plugin manager: wraps `claude plugins` CLI (project scope only)
			var pluginOpts []plugin.Option
			if claudeAbs := findClaudeBinary(); claudeAbs != "" {
				pluginOpts = append(pluginOpts, plugin.WithClaudePath(claudeAbs))
			}
			pluginCLI := plugin.NewExecCLI(pluginOpts...)
			pluginMgr := plugin.NewManager(pluginCLI, logger)
			app.SetPlugins(&pluginAdapter{mgr: pluginMgr})

			// MCP manager: reads ~/.claude.json + project deny lists
			home, _ := os.UserHomeDir()
			userClaudeJSON := filepath.Join(home, ".claude.json")
			mcpMgr := mcp.NewManager(userClaudeJSON, ssh)
			app.SetMCP(&mcpAdapter{mgr: mcpMgr})

			// Wire the notify broker (nil-safe: falls back to file polling only).
			app.SetNotifyBroker(notifyBroker)

			// Key forwarding: all sessions (including remote mirror windows) are
			// local tmux windows, so the local forwarder handles everything.
			app.SetInputForwarder(gui.NewTmuxInputForwarder(tmuxClient))

			// Control mode for event-driven refresh.
			// The connection dies when all tmux windows are deleted.
			// controlManager monitors and reconnects automatically via ticker.
			onOutput := func(_ string) { app.NotifyOutput() }
			ctrlMgr := &controlManager{
				onOutput: onOutput,
				logger:   logger,
			}
			ctrlMgr.tryConnect()
			app.SetOnTick(func() {
				ctrlMgr.ensureConnected()
			})
			lc.Register("control-client", ctrlMgr.close)

			return app.Run()
		},
	}

	cmd.Flags().BoolVar(&debug, "debug", false, "enable debug logging")
	cmd.Flags().StringVar(&logFile, "log-file", "/tmp/lazyclaude/debug.log", "log file path (used with --debug)")

	cmd.AddCommand(newServerCmd())
	cmd.AddCommand(newSetupCmd())
	cmd.AddCommand(newSessionsCmd())
	cmd.AddCommand(newMsgCmd())
	cmd.AddCommand(newDaemonCmd())

	return cmd
}

// tryStartInProcessServer attempts to start the MCP server inside the current
// process so the notify broker can be wired directly to the GUI for immediate
// event delivery. The caller-owned broker is injected so it outlives server
// restarts. Returns the server if started successfully, or nil when an
// external server is already alive (the caller should fall back to ensureMCPServer).
func tryStartInProcessServer(paths config.Paths, tmuxClient tmux.Client, tmuxSocket string, logger *slog.Logger, broker *event.Broker[model.Event]) *server.Server {
	// If an external daemon is already alive, stop it so we can start an
	// in-process server with a wired notify broker.
	if server.IsAlive(paths.PortFile()) {
		server.StopDaemon(paths.PortFile())
		// Give the daemon a moment to release the port file and socket.
		time.Sleep(100 * time.Millisecond)
	}

	token, err := generateToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: generate server token: %v\n", err)
		return nil
	}

	// Log file lives for the process lifetime (closed on exit).
	var srvLogger *log.Logger
	if f, err := os.OpenFile("/tmp/lazyclaude/server.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		srvLogger = log.New(f, "lazyclaude-srv: ", log.LstdFlags)
	} else {
		srvLogger = log.New(os.Stderr, "lazyclaude-srv: ", log.LstdFlags)
	}

	cfg := server.Config{
		Port:       0, // random port
		Token:      token,
		IDEDir:     paths.IDEDir,
		PortFile:   paths.PortFile(),
		RuntimeDir: paths.RuntimeDir,
	}

	srv := server.New(cfg, tmuxClient, srvLogger, server.WithBroker(broker))
	port, err := srv.Start(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: start in-process MCP server: %v\n", err)
		return nil
	}

	// Remove all other lazyclaude lock files so hooks always connect to this
	// in-process server (which has the notify broker wired to the TUI).
	// Non-lazyclaude locks (VS Code, JetBrains) are left untouched.
	lockMgr := server.NewLockManager(paths.IDEDir)
	if n := lockMgr.CleanAllExcept(port); n > 0 {
		srvLogger.Printf("cleaned %d other lazyclaude lock(s)", n)
	}

	return srv
}

// ensureMCPServer starts the MCP server if not already running.
// Uses health check (TCP dial) to detect stale port files.
func ensureMCPServer() {
	paths := config.DefaultPaths()
	result, err := server.EnsureServer(server.EnsureOpts{
		Binary:   os.Args[0],
		PortFile: paths.PortFile(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		return
	}
	if result.Started {
		fmt.Fprintf(os.Stderr, "MCP server started\n")
	}
}

// sessionListerAdapter bridges session.Manager to server.SessionLister.
// It converts []session.Session to []server.SessionInfo on each call.
type sessionListerAdapter struct {
	mgr *session.Manager
}

func (a *sessionListerAdapter) Sessions() []server.SessionInfo {
	sessions := a.mgr.Sessions()
	items := make([]server.SessionInfo, len(sessions))
	for i, s := range sessions {
		items[i] = server.SessionInfo{
			ID:     s.ID,
			Name:   s.Name,
			Role:   string(s.Role),
			Path:   s.Path,
			Host:   s.Host,
			Window: s.TmuxWindow,
			Status: s.Status.String(),
		}
	}
	return items
}

// sessionCreatorAdapter bridges session.Manager to server.SessionCreator.
type sessionCreatorAdapter struct {
	mgr *session.Manager
}

func (a *sessionCreatorAdapter) FindProjectForSession(id string) *server.SessionProjectInfo {
	p := a.mgr.Store().FindProjectForSession(id)
	if p == nil {
		return nil
	}
	return &server.SessionProjectInfo{Path: p.Path}
}

func (a *sessionCreatorAdapter) CreateWorkerSession(ctx context.Context, name, prompt, projectRoot string) (*server.SessionCreateResult, error) {
	sess, err := a.mgr.CreateWorkerSession(ctx, name, prompt, projectRoot)
	if err != nil {
		return nil, fmt.Errorf("create worker session: %w", err)
	}
	return &server.SessionCreateResult{
		ID:     sess.ID,
		Name:   sess.Name,
		Role:   string(sess.Role),
		Path:   sess.Path,
		Window: sess.TmuxWindow,
	}, nil
}

// CreateLocalSession creates a plain session at projectPath and renames it
// to the caller-specified name.
func (a *sessionCreatorAdapter) CreateLocalSession(ctx context.Context, name, projectPath string) (*server.SessionCreateResult, error) {
	sess, err := a.mgr.Create(ctx, projectPath)
	if err != nil {
		return nil, fmt.Errorf("create local session: %w", err)
	}
	if name != "" && sess.Name != name {
		if renameErr := a.mgr.Rename(sess.ID, name); renameErr != nil {
			return nil, fmt.Errorf("rename local session: %w", renameErr)
		}
		sess.Name = name
	}
	return &server.SessionCreateResult{
		ID:     sess.ID,
		Name:   sess.Name,
		Role:   string(sess.Role),
		Path:   sess.Path,
		Window: sess.TmuxWindow,
	}, nil
}

// pendingWindowSet builds a set of tmux window IDs that have pending notifications.
func pendingWindowSet(notifications []*model.ToolNotification) map[string]bool {
	set := make(map[string]bool, len(notifications))
	for _, n := range notifications {
		set[n.Window] = true
	}
	return set
}

// buildProjectItems converts session.Project slice to gui.ProjectItem slice.
func buildProjectItems(projects []session.Project, pending map[string]bool, windowActivity map[string]gui.WindowActivityEntry) []gui.ProjectItem {
	items := make([]gui.ProjectItem, len(projects))
	for i, p := range projects {
		var pm *gui.SessionItem
		if p.PM != nil {
			si := sessionToItem(*p.PM, pending, windowActivity)
			pm = &si
		}
		sessions := make([]gui.SessionItem, len(p.Sessions))
		for j, s := range p.Sessions {
			sessions[j] = sessionToItem(s, pending, windowActivity)
		}
		// Derive host from any session (all sessions in a project share the same host).
		host := ""
		if pm != nil && pm.Host != "" {
			host = pm.Host
		}
		if host == "" {
			for _, s := range sessions {
				if s.Host != "" {
					host = s.Host
					break
				}
			}
		}
		items[i] = gui.ProjectItem{
			ID:       p.ID,
			Name:     p.Name,
			Path:     p.Path,
			Host:     host,
			Expanded: p.Expanded,
			PM:       pm,
			Sessions: sessions,
		}
	}
	return items
}

// sessionToItem converts a single session.Session to gui.SessionItem.
func sessionToItem(s session.Session, pending map[string]bool, windowActivity map[string]gui.WindowActivityEntry) gui.SessionItem {
	activity := model.ActivityUnknown
	toolName := ""

	// Priority 1: window activity from broker events (NeedsInput, Running, Idle, Error).
	if s.Status == session.StatusRunning {
		if wa, ok := windowActivity[s.TmuxWindow]; ok {
			activity = wa.State
			toolName = wa.ToolName
		}
	}

	// Priority 2: pending permission popup overrides to NeedsInput
	// (file-based polling fallback for when broker is not connected).
	if s.Status == session.StatusRunning && pending[s.TmuxWindow] {
		activity = model.ActivityNeedsInput
	}

	return gui.SessionItem{
		ID:         s.ID,
		Name:       s.Name,
		Path:       s.Path,
		Host:       s.Host,
		Status:     s.Status.String(),
		Flags:      s.Flags,
		TmuxWindow: s.TmuxWindow,
		Activity:   activity,
		ToolName:   toolName,
		Role:       string(s.Role),
	}
}

// buildSessionItems converts session.Session slice to gui.SessionItem slice.
func buildSessionItems(sessions []session.Session, pending map[string]bool, windowActivity map[string]gui.WindowActivityEntry) []gui.SessionItem {
	items := make([]gui.SessionItem, len(sessions))
	for i, s := range sessions {
		items[i] = sessionToItem(s, pending, windowActivity)
	}
	return items
}

type controlManager struct {
	client   *tmux.ControlClient
	onOutput func(string)
	logger   *slog.Logger
	mu       sync.Mutex
}

func (m *controlManager) tryConnect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil && !m.client.Closed() {
		return
	}
	c, err := tmux.NewControlClient("lazyclaude", "lazyclaude", m.onOutput)
	if err == nil {
		m.client = c
		if m.logger != nil {
			m.logger.Info("control: connected")
		}
	}
}

func (m *controlManager) ensureConnected() {
	m.mu.Lock()
	needReconnect := m.client == nil || m.client.Closed()
	m.mu.Unlock()
	if needReconnect {
		m.tryConnect()
	}
}

func (m *controlManager) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil {
		m.client.Close()
	}
}

// pluginAdapter converts between plugin.Manager types and gui types.
type pluginAdapter struct {
	mgr *plugin.Manager
}

func (a *pluginAdapter) SetProjectDir(dir string) {
	a.mgr.SetProjectDir(dir)
}

func (a *pluginAdapter) Refresh(ctx context.Context) error {
	return a.mgr.Refresh(ctx)
}

func (a *pluginAdapter) Installed() []gui.PluginItem {
	installed := a.mgr.Installed()
	items := make([]gui.PluginItem, len(installed))
	for i, p := range installed {
		items[i] = gui.PluginItem{
			ID:          p.ID,
			Version:     p.Version,
			Scope:       p.Scope,
			Enabled:     p.Enabled,
			InstalledAt: p.InstalledAt,
		}
	}
	return items
}

func (a *pluginAdapter) Available() []gui.AvailablePluginItem {
	available := a.mgr.Available()
	items := make([]gui.AvailablePluginItem, len(available))
	for i, p := range available {
		items[i] = gui.AvailablePluginItem{
			PluginID:        p.PluginID,
			Name:            p.Name,
			Description:     p.Description,
			MarketplaceName: p.MarketplaceName,
			InstallCount:    p.InstallCount,
		}
	}
	return items
}

func (a *pluginAdapter) Install(ctx context.Context, pluginID string) error {
	return a.mgr.Install(ctx, pluginID, "project")
}

func (a *pluginAdapter) Uninstall(ctx context.Context, pluginID string, scope string) error {
	return a.mgr.Uninstall(ctx, pluginID, scope)
}

func (a *pluginAdapter) ToggleEnabled(ctx context.Context, pluginID string, scope string) error {
	return a.mgr.ToggleEnabled(ctx, pluginID, scope)
}

func (a *pluginAdapter) Update(ctx context.Context, pluginID string) error {
	return a.mgr.Update(ctx, pluginID)
}

// mcpAdapter converts between mcp.Manager types and gui types.
type mcpAdapter struct {
	mgr *mcp.Manager
}

func (a *mcpAdapter) SetRemote(host, projectDir string) {
	a.mgr.SetRemote(host, projectDir)
}

func (a *mcpAdapter) Refresh(ctx context.Context) error {
	return a.mgr.Refresh(ctx)
}

func (a *mcpAdapter) Servers() []gui.MCPItem {
	servers := a.mgr.Servers()
	items := make([]gui.MCPItem, len(servers))
	for i, s := range servers {
		items[i] = gui.MCPItem{
			Name:    s.Name,
			Type:    s.Config.EffectiveType(),
			Scope:   s.Scope,
			Denied:  s.Denied,
			Command: s.Config.Command,
			Args:    s.Config.Args,
			URL:     s.Config.URL,
		}
	}
	return items
}

func (a *mcpAdapter) ToggleDenied(ctx context.Context, name string) error {
	return a.mgr.ToggleDenied(ctx, name)
}

// findClaudeBinary resolves the absolute path to the claude binary.
// exec.LookPath alone is insufficient because tmux display-popup inherits
// the tmux server's PATH, which typically lacks ~/.local/bin.
func findClaudeBinary() string {
	// 1. Try PATH first (works when launched from a normal shell).
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}

	// 2. Probe well-known installation directories.
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidates := []string{
		filepath.Join(home, ".local", "bin", "claude"),
		"/usr/local/bin/claude",
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	return ""
}

// resolveActivityWindow rewrites a remote activity event so its Window
// field points at the local mirror session's current tmux window ID
// ("@42"). The sidebar keys windowActivity by Session.TmuxWindow (which
// SyncWithTmux sets to the local tmux window ID for both local and remote
// sessions), so emission and lookup must share the same key space to avoid
// the "Unknown" state bug for remote sessions.
//
// Uses sessionID as a lookup hop into the local store instead of trusting
// Window (which the RemoteProvider can only fill in as a best-effort
// mirror name). Returns the event unchanged when:
//   - ActivityNotification is nil (no remap target)
//   - sessionID is empty (local MCP path — Window is already correct)
//   - the session cannot be resolved, or its TmuxWindow is unknown
//     (fall through with the best-effort Window rather than blanking it)
//
// A defensive copy of ActivityNotification is taken so callers' events
// are not mutated in place.
func resolveActivityWindow(store *session.Store, ev model.Event, sessionID string) model.Event {
	if ev.ActivityNotification == nil || sessionID == "" {
		return ev
	}
	localSess := store.FindByID(sessionID)
	if localSess == nil || localSess.TmuxWindow == "" {
		return ev
	}
	notif := *ev.ActivityNotification
	notif.Window = localSess.TmuxWindow
	out := ev
	out.ActivityNotification = &notif
	return out
}

// rewriteToolNotificationWindow rewrites a remote tool notification's
// Window field from the remote tmux window ID (e.g. "@22") to the local
// mirror session's tmux window ID (e.g. "@42"), using sessionID as a
// lookup hop into the local store. This is the ToolNotification twin of
// resolveActivityWindow and completes the SessionID hop pattern for the
// permission popup action routing path (Bug 5 Phase B).
//
// Without this rewrite, the gui layer keys pending permission popups by
// ToolNotification.Window — which for remote sessions arrives as the
// remote tmux window ID — so SendChoice is dispatched to a pane that
// does not exist on the local tmux server and Accept/Reject never reach
// the remote claude process.
//
// The notification is mutated in place (SSE pushes a fresh instance per
// event, so there is no shared-state risk). Returns without mutating
// when:
//   - n is nil (other Event variants through the same callback path),
//   - sessionID is empty (old daemon without Phase B wire format),
//   - the session is not in the local store, or
//   - the local session has no TmuxWindow yet (mirror not synced).
//
// In those cases the original Window is preserved so behavior degrades
// to the pre-fix (popup appears but action is not routed), matching the
// defensive pattern used by resolveActivityWindow.
func rewriteToolNotificationWindow(store *session.Store, n *model.ToolNotification, sessionID string) {
	if n == nil || sessionID == "" {
		return
	}
	localSess := store.FindByID(sessionID)
	if localSess == nil || localSess.TmuxWindow == "" {
		return
	}
	n.Window = localSess.TmuxWindow
}
