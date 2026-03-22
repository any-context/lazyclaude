package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/server"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Register tmux keybindings and Claude Code hooks",
		Long: `Setup lazyclaude as a tmux plugin:
1. Ensures the MCP server is running
2. Installs Claude Code hooks in ~/.claude/settings.json
3. Registers tmux keybindings for launching Claude sessions`,
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

	// 2. Install Claude Code hooks
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home directory: %w", err)
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	settings, err := config.ReadClaudeSettings(settingsPath)
	if err != nil {
		return fmt.Errorf("read claude settings: %w", err)
	}

	if !config.HasLazyClaudeHooks(settings) {
		updated := config.SetLazyClaudeHooks(settings)
		if mkErr := os.MkdirAll(filepath.Dir(settingsPath), 0o700); mkErr != nil {
			return fmt.Errorf("create settings dir: %w", mkErr)
		}
		if err := config.WriteClaudeSettings(settingsPath, updated); err != nil {
			return fmt.Errorf("write claude settings: %w", err)
		}
		fmt.Fprintln(os.Stderr, "Claude Code hooks installed")
	}

	return nil
}
