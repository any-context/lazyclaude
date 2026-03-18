package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/KEMSHlM/lazyclaude/internal/gui"
	"github.com/KEMSHlM/lazyclaude/internal/notify"
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
			var logger *slog.Logger
			paths := config.DefaultPaths()
			tmuxClient := tmux.NewExecClientWithSocket("lazyclaude")

			if debug {
				dest := logFile
				if dest == "" {
					dest = "/tmp/lazyclaude-debug.log"
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

			// Ensure MCP server is running
			ensureMCPServer()

			// Start background GC to remove dead/orphan sessions
			gc := session.NewGC(mgr, 2*time.Second)
			gc.Start()
			defer gc.Stop()

			adapter := &sessionAdapter{mgr: mgr, tmux: tmuxClient, paths: paths}

			app, err := gui.NewApp(gui.ModeMain)
			if err != nil {
				return fmt.Errorf("init TUI: %w", err)
			}
			app.SetSessions(adapter)

			// Forwarder with subprocess fallback — always works
			fwd := &fallbackInputForwarder{
				fallback: gui.NewTmuxInputForwarder(tmuxClient),
			}
			app.SetInputForwarder(fwd)

			// Connect control mode for event-driven refresh + fast key forwarding.
			// If tmux session doesn't exist yet, defer until first session is created.
			connectControl := func() {
				c, err := tmux.NewControlClient("lazyclaude", "lazyclaude", func(_ string) {
					app.NotifyOutput()
				})
				if err == nil {
					fwd.SetControl(c)
				}
			}

			ctrl, ctrlErr := tmux.NewControlClient("lazyclaude", "lazyclaude", func(_ string) {
				app.NotifyOutput()
			})
			if ctrlErr == nil {
				defer ctrl.Close()
				fwd.SetControl(ctrl)
			} else {
				// Connect after first session is created (no polling)
				app.SetOnSessionCreated(func() {
					time.Sleep(100 * time.Millisecond) // brief wait for tmux session to initialize
					connectControl()
				})
			}

			return app.Run()
		},
	}

	cmd.Flags().BoolVar(&debug, "debug", false, "enable debug logging")
	cmd.Flags().StringVar(&logFile, "log-file", "/tmp/lazyclaude-debug.log", "log file path (used with --debug)")

	cmd.AddCommand(newServerCmd())
	cmd.AddCommand(newDiffCmd())
	cmd.AddCommand(newToolCmd())
	cmd.AddCommand(newSetupCmd())

	return cmd
}

// ensureMCPServer starts the MCP server if not already running.
func ensureMCPServer() {
	// Check if server is running by reading port file
	paths := config.DefaultPaths()
	portFile := paths.PortFile()
	if _, err := os.Stat(portFile); err == nil {
		return // port file exists, server likely running
	}

	// Start server in background
	cmd := exec.Command(os.Args[0], "server", "--port", "0")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: start MCP server: %v\n", err)
		return
	}
	cmd.Process.Release() // detach
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

func (a *sessionAdapter) CapturePreview(id string, width, height int) (string, error) {
	sess := a.mgr.Store().FindByID(id)
	if sess == nil {
		return "", nil
	}
	target := sess.TmuxWindow
	if target == "" {
		target = "lazyclaude:" + sess.WindowName()
	}
	ctx := context.Background()

	// Resize pane only when target or dimensions changed (skip 100ms sleep otherwise)
	if width > 0 && height > 0 && (id != a.lastResizeID || width != a.lastResizeW || height != a.lastResizeH) {
		if err := a.tmux.ResizeWindow(ctx, target, width, height); err != nil {
			return "", err
		}
		a.lastResizeID = id
		a.lastResizeW = width
		a.lastResizeH = height
		time.Sleep(100 * time.Millisecond) // wait for Claude to re-render after resize
	}

	// Capture with ANSI colors
	content, err := a.tmux.CapturePaneANSI(ctx, target)

	if err != nil || width <= 0 {
		return content, err
	}

	// Safety truncate: clip lines that didn't fit after resize
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) > width {
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n"), nil
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

// fallbackInputForwarder tries control mode first, falls back to subprocess.
type fallbackInputForwarder struct {
	ctrl     *tmux.ControlClient
	fallback gui.InputForwarder
}

func (f *fallbackInputForwarder) ForwardKey(target string, key string) error {
	if f.ctrl != nil {
		if err := f.ctrl.SendKeys(target, key); err == nil {
			return nil
		}
		f.ctrl = nil
	}
	// Subprocess path: run async to avoid blocking gocui event loop (~5ms per call)
	go f.fallback.ForwardKey(target, key)
	return nil
}

func (f *fallbackInputForwarder) SetControl(ctrl *tmux.ControlClient) {
	f.ctrl = ctrl
}

func (a *sessionAdapter) PendingNotification() *notify.ToolNotification {
	n, err := notify.Read(a.paths.RuntimeDir)
	if err != nil || n == nil {
		return nil
	}
	return n
}

// choiceToKey maps a GUI choice to the key Claude Code expects.
// Claude Code's permission dialog shows numbered options (1=Yes, 2=Allow, 3=No).
// Single-key press selects immediately (no Enter needed).
var choiceToKey = map[gui.Choice]string{
	gui.ChoiceAccept: "1",
	gui.ChoiceAllow:  "2",
	gui.ChoiceReject: "3",
	gui.ChoiceCancel: "Escape",
}

func (a *sessionAdapter) SendChoice(window string, choice gui.Choice) error {
	key, ok := choiceToKey[choice]
	if !ok {
		key = "Escape"
	}
	// window is a bare tmux window ID (e.g., "@3") from State.WindowForPID.
	// Prepend session name only if not already present.
	target := window
	if !strings.Contains(window, ":") {
		target = "lazyclaude:" + window
	}
	return a.tmux.SendKeys(context.Background(), target, key)
}
