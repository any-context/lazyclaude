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

			runtimeDir := paths.RuntimeDir
			if runtimeDir == "" {
				runtimeDir = daemon.DaemonInfoDir()
			}

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

			// Print JSON to stdout so parseDaemonOutput can parse it.
			// daemon.json is also written to disk for file-based discovery.
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
