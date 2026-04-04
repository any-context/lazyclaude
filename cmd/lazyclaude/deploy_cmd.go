package main

import (
	"context"
	"fmt"
	"time"

	"github.com/any-context/lazyclaude/internal/daemon"
	"github.com/spf13/cobra"
)

func newDeployCmd() *cobra.Command {
	var binPath string
	var remoteDir string
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "deploy <user@host>",
		Short: "Deploy lazyclaude binary to a remote host",
		Long: `Deploy the lazyclaude binary to a remote host via SSH.

Detects the remote architecture, cross-compiles if needed,
transfers the binary, and verifies it runs correctly.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host := args[0]

			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()

			cfg := daemon.DeployConfig{
				Host:       host,
				BinaryPath: binPath,
				RemoteDir:  remoteDir,
			}

			ssh := &daemon.ExecSSHExecutor{}

			fmt.Printf("Deploying lazyclaude to %s...\n", host)

			result, err := daemon.Deploy(ctx, cfg, ssh)
			if err != nil {
				return err
			}

			fmt.Printf("Deployed to %s (%s)\n", result.RemotePath, result.Arch)
			fmt.Printf("Version: %s\n", result.Version)

			if result.PathWarning != "" {
				fmt.Printf("Warning: %s\n", result.PathWarning)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&binPath, "bin", "", "path to pre-built binary (skip cross-compile)")
	cmd.Flags().StringVar(&remoteDir, "remote-dir", "", "remote install directory (default: ~/.local/bin)")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "deploy timeout")

	return cmd
}
