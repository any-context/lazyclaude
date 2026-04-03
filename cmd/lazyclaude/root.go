package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/any-context/lazyclaude/internal/adapter/tmuxadapter"
	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/core/event"
	"github.com/any-context/lazyclaude/internal/core/lifecycle"
	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/gui"
	"github.com/any-context/lazyclaude/internal/mcp"
	"github.com/any-context/lazyclaude/internal/notify"
	"github.com/any-context/lazyclaude/internal/plugin"
	"github.com/any-context/lazyclaude/internal/server"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/charmbracelet/x/ansi"
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

			adapter := &sessionAdapter{mgr: mgr, tmux: tmuxClient, paths: paths}

			app, err := gui.NewApp(gui.ModeMain)
			if err != nil {
				return fmt.Errorf("init TUI: %w", err)
			}
			adapter.windowActivityFn = app.WindowActivityMap
			app.SetSessions(adapter)

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
			mcpMgr := mcp.NewManager(userClaudeJSON)
			app.SetMCP(&mcpAdapter{mgr: mcpMgr})

			// Wire the notify broker (nil-safe: falls back to file polling only).
			app.SetNotifyBroker(notifyBroker)

			// Key forwarding via subprocess
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
			app.SetOnTick(ctrlMgr.ensureConnected)
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
	sess, err := a.mgr.CreateWorkerSession(ctx, name, prompt, projectRoot, "")
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
	sess, err := a.mgr.Create(ctx, projectPath, "")
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

// sessionAdapter bridges session.Manager to gui.SessionProvider.
type sessionAdapter struct {
	mgr          *session.Manager
	tmux         tmux.Client
	paths        config.Paths
	lastResizeID string // session ID of last resize
	lastResizeW  int
	lastResizeH  int

	// cachedPending is refreshed once per layout cycle via RefreshPending,
	// then reused by Sessions() and Projects() to avoid redundant ReadAll
	// calls (each of which does an os.ReadDir + file I/O).
	cachedPending map[string]bool

	// windowActivity provides window->activity mapping from the App layer.
	// Set via SetWindowActivitySource after the App is wired.
	windowActivityFn func() map[string]gui.WindowActivityEntry
}

// RefreshPendingFrom caches the given notifications for badge rendering.
// Called from the ticker goroutine after ReadAll, before the files are
// consumed, so that Sessions() and Projects() can display badges without
// a redundant (and destructive) ReadAll call.
func (a *sessionAdapter) RefreshPendingFrom(notifications []*model.ToolNotification) {
	a.cachedPending = pendingWindowSet(notifications)
}

func (a *sessionAdapter) Sessions() []gui.SessionItem {
	sessions := a.mgr.Sessions()
	return buildSessionItems(sessions, a.cachedPending, a.getWindowActivity())
}

func (a *sessionAdapter) Projects() []gui.ProjectItem {
	projects := a.mgr.Projects()
	return buildProjectItems(projects, a.cachedPending, a.getWindowActivity())
}

func (a *sessionAdapter) getWindowActivity() map[string]gui.WindowActivityEntry {
	if a.windowActivityFn != nil {
		return a.windowActivityFn()
	}
	return nil
}

func (a *sessionAdapter) ToggleProjectExpanded(projectID string) {
	a.mgr.ToggleProjectExpanded(projectID)
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

func (a *sessionAdapter) CapturePreview(id string, width, height int) (gui.PreviewResult, error) {
	sess := a.mgr.Store().FindByID(id)
	if sess == nil {
		return gui.PreviewResult{}, nil
	}
	target := sess.TmuxWindow
	if target == "" {
		target = "lazyclaude:" + sess.WindowName()
	}
	ctx := context.Background()

	// Resize pane only when target or dimensions changed
	if width > 0 && height > 0 && (id != a.lastResizeID || width != a.lastResizeW || height != a.lastResizeH) {
		if err := a.tmux.ResizeWindow(ctx, target, width, height); err != nil {
			return gui.PreviewResult{}, err
		}
		a.lastResizeID = id
		a.lastResizeW = width
		a.lastResizeH = height
		time.Sleep(20 * time.Millisecond)
	}

	// Capture with ANSI colors
	content, err := a.tmux.CapturePaneANSI(ctx, target)
	if err != nil || width <= 0 {
		return gui.PreviewResult{Content: content}, err
	}

	// Safety truncate
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) > width {
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}

	// Fetch cursor position from tmux pane
	var cursorX, cursorY int
	if pos, err := a.tmux.ShowMessage(ctx, target, "#{cursor_x},#{cursor_y}"); err == nil {
		parts := strings.SplitN(strings.TrimSpace(pos), ",", 2)
		if len(parts) == 2 {
			cursorX, _ = strconv.Atoi(parts[0])
			cursorY, _ = strconv.Atoi(parts[1])
		}
	}

	return gui.PreviewResult{
		Content: strings.Join(lines, "\n"),
		CursorX: cursorX,
		CursorY: cursorY,
	}, nil
}

func (a *sessionAdapter) CaptureScrollback(id string, _, startLine, endLine int) (gui.PreviewResult, error) {
	sess := a.mgr.Store().FindByID(id)
	if sess == nil {
		return gui.PreviewResult{}, nil
	}
	target := sess.TmuxWindow
	if target == "" {
		target = "lazyclaude:" + sess.WindowName()
	}
	ctx := context.Background()

	// Truncation is handled by renderScrollContent (ANSI-aware).
	content, err := a.tmux.CapturePaneANSIRange(ctx, target, startLine, endLine)
	return gui.PreviewResult{Content: content}, err
}

func (a *sessionAdapter) HistorySize(id string) (int, error) {
	sess := a.mgr.Store().FindByID(id)
	if sess == nil {
		return 0, nil
	}
	target := sess.TmuxWindow
	if target == "" {
		target = "lazyclaude:" + sess.WindowName()
	}
	ctx := context.Background()
	out, err := a.tmux.ShowMessage(ctx, target, "#{history_size}")
	if err != nil {
		return 0, err
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n, nil
}

func (a *sessionAdapter) Create(path, host string) error {
	if path == "." {
		abs, err := filepath.Abs(".")
		if err != nil {
			return err
		}
		path = abs
	}
	_, err := a.mgr.Create(context.Background(), path, host)
	return err
}

func (a *sessionAdapter) Delete(id string) error {
	return a.mgr.Delete(context.Background(), id)
}

func (a *sessionAdapter) Rename(id, newName string) error {
	return a.mgr.Rename(id, newName)
}

func (a *sessionAdapter) PurgeOrphans() (int, error) {
	return a.mgr.PurgeOrphans()
}

func (a *sessionAdapter) PendingNotifications() []*model.ToolNotification {
	notifications, err := notify.ReadAll(a.paths.RuntimeDir)
	if err != nil || len(notifications) == 0 {
		return nil
	}
	return notifications
}

func (a *sessionAdapter) SendChoice(window string, c gui.Choice) error {
	return tmuxadapter.SendToPane(context.Background(), a.tmux, window, c)
}

func (a *sessionAdapter) CreateWorktree(name, prompt, projectRoot, host string) error {
	_, err := a.mgr.CreateWorktree(context.Background(), name, prompt, projectRoot, host)
	return err
}

func (a *sessionAdapter) ResumeWorktree(worktreePath, prompt, projectRoot, host string) error {
	_, err := a.mgr.ResumeWorktree(context.Background(), worktreePath, prompt, projectRoot, host)
	return err
}

func (a *sessionAdapter) CreatePMSession(projectRoot, host string) error {
	_, err := a.mgr.CreatePMSession(context.Background(), projectRoot, host)
	return err
}

func (a *sessionAdapter) CreateWorkerSession(name, prompt, projectRoot, host string) error {
	_, err := a.mgr.CreateWorkerSession(context.Background(), name, prompt, projectRoot, host)
	return err
}

func (a *sessionAdapter) ListWorktrees(projectRoot, host string) ([]gui.WorktreeInfo, error) {
	items, err := session.ListWorktrees(context.Background(), projectRoot, host)
	if err != nil {
		return nil, err
	}
	result := make([]gui.WorktreeInfo, len(items))
	for i, item := range items {
		result[i] = gui.WorktreeInfo{Name: item.Name, Path: item.Path, Branch: item.Branch}
	}
	return result, nil
}

func (a *sessionAdapter) LaunchLazygit(path, host string) error {
	if host != "" {
		bin, args := session.BuildLazygitSSHArgs(host, path)
		cmd := exec.Command(bin, args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	if _, err := exec.LookPath("lazygit"); err != nil {
		return fmt.Errorf("lazygit is not installed")
	}
	cmd := exec.Command("lazygit")
	cmd.Dir = path
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (a *sessionAdapter) AttachSession(id string) error {
	sess := a.mgr.Store().FindByID(id)
	if sess == nil {
		return fmt.Errorf("session not found: %s", id)
	}
	target := "lazyclaude:" + sess.WindowName()

	// Ensure window-size=largest so attach is not constrained by control mode.
	_ = exec.Command("tmux", "-L", "lazyclaude", "set-option", "-t", "lazyclaude", "window-size", "largest").Run()

	// Directly attach to the lazyclaude tmux session.
	cmd := exec.Command("tmux", "-L", "lazyclaude", "attach-session", "-t", target)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// controlManager handles control mode connection lifecycle.
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

// Client returns the active control client, or nil if not connected.
func (m *controlManager) Client() *tmux.ControlClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil && !m.client.Closed() {
		return m.client
	}
	return nil
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

func (a *mcpAdapter) SetProjectDir(dir string) {
	a.mgr.SetProjectDir(dir)
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
