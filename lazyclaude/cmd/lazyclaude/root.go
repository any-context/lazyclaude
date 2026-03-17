package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/KEMSHlM/lazyclaude/internal/gui"
	"github.com/KEMSHlM/lazyclaude/internal/session"
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	var debug bool

	cmd := &cobra.Command{
		Use:     "lazyclaude",
		Short:   "A standalone TUI for Claude Code",
		Long:    "lazyclaude is a terminal UI for managing Claude Code sessions, inspired by lazygit.",
		Version: fmt.Sprintf("%s (%s)", version, commit),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Logger: --debug writes to stderr (gocui uses stdout)
			logLevel := slog.LevelError
			if debug {
				logLevel = slog.LevelDebug
			}
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

			paths := config.DefaultPaths()
			tmuxClient := tmux.NewExecClientWithSocket("lazyclaude")

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

	cmd.Flags().BoolVar(&debug, "debug", false, "enable debug logging to stderr")

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
	cmd := exec.Command("tmux", "-L", "lazyclaude", "attach-session", "-t", target)
	return cmd, nil
}

func (a *sessionAdapter) CapturePreview(id string) (string, error) {
	sess := a.mgr.Store().FindByID(id)
	if sess == nil {
		return "", nil
	}
	// Try TmuxWindow first, fall back to window name
	target := sess.TmuxWindow
	if target == "" {
		target = "lazyclaude:" + sess.WindowName()
	}
	return a.tmux.CapturePaneContent(context.Background(), target)
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
