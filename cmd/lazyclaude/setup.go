package main

import (
	"fmt"
	"os"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/server"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Register tmux keybindings and ensure MCP server",
		Long: `Setup lazyclaude as a tmux plugin:
1. Ensures the MCP server is running
2. Registers tmux keybindings for launching Claude sessions

Hooks are injected at session startup via --settings flag (no settings.json modification).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup()
		},
	}
}

func runSetup() error {
	paths := config.DefaultPaths()

	// 1. Restart MCP server (kill old, start new with current binary)
	var extraEnv []string
	if hostTmux := os.Getenv("LAZYCLAUDE_HOST_TMUX"); hostTmux != "" {
		extraEnv = append(extraEnv, "LAZYCLAUDE_HOST_TMUX="+hostTmux)
	}
	extraEnv = append(extraEnv, "LAZYCLAUDE_TMUX_SOCKET=lazyclaude")
	result, err := server.RestartServer(server.EnsureOpts{
		Binary:   os.Args[0],
		PortFile: paths.PortFile(),
		ExtraEnv: extraEnv,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: MCP server: %v\n", err)
	} else if result.Started {
		fmt.Fprintln(os.Stderr, "MCP server restarted")
	}

	// Hooks are now injected at session startup via `claude --settings`,
	// so there is no need to modify ~/.claude/settings.json here.

	return nil
}
