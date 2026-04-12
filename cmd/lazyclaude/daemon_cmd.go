package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/core/event"
	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/daemon"
	"github.com/any-context/lazyclaude/internal/server"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Start the lazyclaude daemon server",
		Long:  "Runs the daemon HTTP server in the foreground. Manages sessions and provides a REST API.",
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := daemon.GenerateDaemonToken()
			if err != nil {
				return err
			}

			paths := config.DefaultPaths()
			tmuxSocket := "lazyclaude"
			if s := os.Getenv("LAZYCLAUDE_TMUX_SOCKET"); s != "" {
				tmuxSocket = s
			}
			tmuxClient := tmux.NewExecClientWithSocket(tmuxSocket)

			logger := log.New(os.Stderr, "lazyclaude-daemon: ", log.LstdFlags)
			slogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

			store := session.NewStore(paths.StateFile())
			mgr := session.NewManager(store, tmuxClient, paths, slogger)

			if err := mgr.Load(context.Background()); err != nil {
				fmt.Fprintf(os.Stderr, "warning: load sessions: %v\n", err)
			}

			broker := event.NewBroker[model.Event]()
			defer broker.Close()

			// Start MCP server in-process so Claude Code hooks are received
			// by the existing server code and published to the shared broker.
			mcpLogger := log.New(os.Stderr, "lazyclaude-mcp: ", log.LstdFlags)
			mcpCfg := server.Config{
				Port:       0,
				Token:      token,
				IDEDir:     paths.IDEDir,
				PortFile:   paths.PortFile(),
				RuntimeDir: paths.RuntimeDir,
			}
			mcpSrv := server.New(mcpCfg, tmuxClient, mcpLogger, server.WithBroker(broker))
			mcpSrv.SetSessionLister(&sessionListerAdapter{mgr: mgr})
			mcpSrv.SetSessionCreator(&sessionCreatorAdapter{mgr: mgr})
			if _, err := mcpSrv.Start(context.Background()); err != nil {
				return fmt.Errorf("start MCP server: %w", err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := mcpSrv.Stop(ctx); err != nil {
					logger.Printf("warning: MCP server stop: %v", err)
				}
			}()

			// Start background GC to sync tmux window state and clean dead sessions.
			gc := session.NewGC(mgr, 2*time.Second)
			gc.Start()
			defer gc.Stop()

			runtimeDir := daemon.DaemonInfoDir()

			cfg := daemon.DaemonConfig{
				Port:       port,
				Token:      token,
				RuntimeDir: runtimeDir,
			}

			srv := daemon.NewDaemonServer(cfg, mgr, broker, tmuxClient, logger, daemon.WithVersion(version))
			actualPort, err := srv.Start(context.Background())
			if err != nil {
				return fmt.Errorf("start daemon: %w", err)
			}

			// Print JSON to stdout. daemon.json is also written to
			// disk for file-based discovery.
			if err := json.NewEncoder(os.Stdout).Encode(daemon.DaemonInfo{Port: actualPort, Token: token}); err != nil {
				return fmt.Errorf("write daemon info to stdout: %w", err)
			}

			// Wait for signal or shutdown request
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			select {
			case sig := <-sigCh:
				logger.Printf("received signal %s, shutting down...", sig)
			case <-srv.ShutdownCh():
				logger.Println("shutdown requested via API")
			}

			shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutCancel()
			return srv.Stop(shutCtx)
		},
	}

	cmd.Flags().IntVar(&port, "port", 0, "listen port (0 = random)")

	return cmd
}
