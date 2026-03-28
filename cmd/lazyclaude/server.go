package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/KEMSHlM/lazyclaude/internal/server"
	"github.com/spf13/cobra"
)

func newServerCmd() *cobra.Command {
	var port int
	var token string

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start MCP server daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				var err error
				token, err = generateToken()
				if err != nil {
					return err
				}
			}

			paths := config.DefaultPaths()
			binaryPath := os.Args[0]
			if b := os.Getenv("LAZYCLAUDE_POPUP_BINARY"); b != "" {
				binaryPath = b
			}

			logger := log.New(os.Stderr, "lazyclaude: ", log.LstdFlags)
			tmuxSocket := "lazyclaude"
			if s := os.Getenv("LAZYCLAUDE_TMUX_SOCKET"); s != "" {
				tmuxSocket = s
			}
			tmuxClient := tmux.NewExecClientWithSocket(tmuxSocket)

			cfg := server.Config{
				Port:       port,
				Token:      token,
				BinaryPath: binaryPath,
				IDEDir:     paths.IDEDir,
				PortFile:   paths.PortFile(),
				RuntimeDir: paths.RuntimeDir,
				TmuxSocket: tmuxSocket,
			}
			srv := server.New(cfg, tmuxClient, logger)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			actualPort, err := srv.Start(ctx)
			if err != nil {
				return err
			}

			logger.Printf("MCP server started on port %d", actualPort)

			// Wait for signal
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh

			logger.Println("shutting down...")
			shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutCancel()
			return srv.Stop(shutCtx)
		},
	}

	cmd.Flags().IntVar(&port, "port", 0, "listen port (0 = random)")
	cmd.Flags().StringVar(&token, "token", "", "auth token (auto-generated if empty)")

	return cmd
}

func generateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}