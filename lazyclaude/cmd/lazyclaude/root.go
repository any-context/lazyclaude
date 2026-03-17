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
	"github.com/KEMSHlM/lazyclaude/internal/session"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
	"golang.org/x/term"
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

			adapter := &sessionAdapter{mgr: mgr, tmux: tmuxClient}

			app, err := gui.NewApp(gui.ModeMain)
			if err != nil {
				return fmt.Errorf("init TUI: %w", err)
			}
			app.SetSessions(adapter)
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
	mgr  *session.Manager
	tmux tmux.Client
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

func (a *sessionAdapter) AttachCmd(id string) (*exec.Cmd, error) {
	sess := a.mgr.Store().FindByID(id)
	if sess == nil {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	target := "lazyclaude:" + sess.WindowName()
	// Build tmux attach command with the same socket
	cmd := exec.Command("tmux", "-u", "-L", "lazyclaude", "attach-session", "-t", target)
	return cmd, nil
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

	// Resize pane to preview panel dimensions
	if width > 0 && height > 0 {
		if err := a.tmux.ResizeWindow(ctx, target, width, height); err != nil {
			return "", err
		}
		time.Sleep(150 * time.Millisecond) // wait for Claude to re-render
	}

	// Capture with ANSI colors
	content, err := a.tmux.CapturePaneANSI(ctx, target)

	// Restore to full terminal size (for Enter/attach)
	if width > 0 && height > 0 {
		if w, h, restoreErr := term.GetSize(int(os.Stdin.Fd())); restoreErr == nil && w > 0 && h > 0 {
			a.tmux.ResizeWindow(ctx, target, w, h) // best-effort restore
		}
	}

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
