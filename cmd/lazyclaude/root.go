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

	"github.com/KEMSHlM/lazyclaude/internal/adapter/tmuxadapter"
	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/core/event"
	"github.com/KEMSHlM/lazyclaude/internal/core/lifecycle"
	"github.com/KEMSHlM/lazyclaude/internal/core/model"
	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/KEMSHlM/lazyclaude/internal/gui"
	"github.com/KEMSHlM/lazyclaude/internal/notify"
	"github.com/KEMSHlM/lazyclaude/internal/server"
	"github.com/KEMSHlM/lazyclaude/internal/session"
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
			var notifyBroker *event.Broker[model.Event]
			inProcessSrv := tryStartInProcessServer(paths, tmuxClient, logger)
			if inProcessSrv != nil {
				notifyBroker = inProcessSrv.NotifyBroker()
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
			app.SetSessions(adapter)

			// Popup mode: tmux display-popup when launched from tmux plugin,
			// gocui overlay fallback otherwise.
			if pm := os.Getenv("LAZYCLAUDE_POPUP_MODE"); pm != "" {
				app.SetPopupMode(config.ParsePopupMode(pm))
			}

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
	cmd.AddCommand(newDiffCmd())
	cmd.AddCommand(newToolCmd())
	cmd.AddCommand(newSetupCmd())

	return cmd
}

// tryStartInProcessServer attempts to start the MCP server inside the current
// process so the notify broker can be wired directly to the GUI for immediate
// event delivery. Returns the server if started successfully, or nil when an
// external server is already alive (the caller should fall back to ensureMCPServer).
func tryStartInProcessServer(paths config.Paths, tmuxClient tmux.Client, logger *slog.Logger) *server.Server {
	// If an external server is already alive, do not start a second one.
	// Only check health — do NOT call EnsureServer which would spawn a subprocess.
	if server.IsAlive(paths.PortFile()) {
		return nil
	}

	token, err := generateToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: generate server token: %v\n", err)
		return nil
	}

	binaryPath := os.Args[0]
	if b := os.Getenv("LAZYCLAUDE_POPUP_BINARY"); b != "" {
		binaryPath = b
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
		BinaryPath: binaryPath,
		IDEDir:     paths.IDEDir,
		PortFile:   paths.PortFile(),
		RuntimeDir: paths.RuntimeDir,
	}

	srv := server.New(cfg, tmuxClient, srvLogger)
	if _, err := srv.Start(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "warning: start in-process MCP server: %v\n", err)
		return nil
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

// sessionAdapter bridges session.Manager to gui.SessionProvider.
type sessionAdapter struct {
	mgr          *session.Manager
	tmux         tmux.Client
	paths        config.Paths
	lastResizeID string // session ID of last resize
	lastResizeW  int
	lastResizeH  int
}

func (a *sessionAdapter) Sessions() []gui.SessionItem {
	sessions := a.mgr.Sessions()
	items := make([]gui.SessionItem, len(sessions))
	for i, s := range sessions {
		items[i] = gui.SessionItem{
			ID:         s.ID,
			Name:       s.Name,
			Path:       s.Path,
			Host:       s.Host,
			Status:     s.Status.String(),
			Flags:      s.Flags,
			TmuxWindow: s.TmuxWindow,
		}
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

	return gui.PreviewResult{
		Content: strings.Join(lines, "\n"),
	}, nil
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

func (a *sessionAdapter) CreateWorktree(name, prompt, projectRoot string) error {
	_, err := a.mgr.CreateWorktree(context.Background(), name, prompt, projectRoot)
	return err
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

func (m *controlManager) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil {
		m.client.Close()
	}
}
