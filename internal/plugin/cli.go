package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// Runner executes a command and returns its stdout.
type Runner interface {
	// Run executes a command with the given working directory.
	// dir may be empty to use the current process's working directory.
	Run(ctx context.Context, dir string, args ...string) (string, error)
}

// execRunner is the default Runner that spawns OS processes.
type execRunner struct {
	claudePath string
}

func (r *execRunner) Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.claudePath, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	// Force non-interactive mode to prevent ANSI escape sequences in output.
	cmd.Env = append(os.Environ(), "TERM=dumb", "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude %v: %w (stderr: %s)", args, err, stderr.String())
	}
	return stdout.String(), nil
}

// ExecCLI implements plugin CLI operations by spawning `claude plugins` subprocesses.
type ExecCLI struct {
	runner     Runner
	projectDir string // working directory for CLI commands (project root)
}

// SetProjectDir sets the working directory for all CLI commands.
// When set, `claude plugins` runs in the context of this project,
// so project-scoped plugins and settings are used.
func (c *ExecCLI) SetProjectDir(dir string) {
	c.projectDir = dir
}

// Option configures ExecCLI.
type Option func(*ExecCLI)

// WithRunner injects a custom Runner (for testing).
func WithRunner(r Runner) Option {
	return func(c *ExecCLI) {
		c.runner = r
	}
}

// WithClaudePath sets the path to the claude binary.
func WithClaudePath(path string) Option {
	return func(c *ExecCLI) {
		c.runner = &execRunner{claudePath: path}
	}
}

// NewExecCLI creates a new ExecCLI with the given options.
func NewExecCLI(opts ...Option) *ExecCLI {
	c := &ExecCLI{
		runner: &execRunner{claudePath: "claude"},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ListInstalled returns installed plugins via `claude plugins list --json`.
func (c *ExecCLI) ListInstalled(ctx context.Context) ([]InstalledPlugin, error) {
	out, err := c.runner.Run(ctx, c.projectDir, "plugins", "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("list installed: %w", err)
	}

	var plugins []InstalledPlugin
	if err := json.Unmarshal([]byte(out), &plugins); err != nil {
		return nil, fmt.Errorf("parse installed plugins: %w", err)
	}
	return plugins, nil
}

// ListAll returns installed and available plugins via `claude plugins list --available --json`.
func (c *ExecCLI) ListAll(ctx context.Context) (*ListResult, error) {
	out, err := c.runner.Run(ctx, c.projectDir, "plugins", "list", "--available", "--json")
	if err != nil {
		return nil, fmt.Errorf("list all: %w", err)
	}

	var result ListResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return nil, fmt.Errorf("parse list result: %w", err)
	}
	return &result, nil
}

// ListMarketplaces returns configured marketplaces via `claude plugins marketplace list --json`.
func (c *ExecCLI) ListMarketplaces(ctx context.Context) ([]MarketplaceInfo, error) {
	out, err := c.runner.Run(ctx, c.projectDir, "plugins", "marketplace", "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("list marketplaces: %w", err)
	}

	var markets []MarketplaceInfo
	if err := json.Unmarshal([]byte(out), &markets); err != nil {
		return nil, fmt.Errorf("parse marketplaces: %w", err)
	}
	return markets, nil
}

// Install installs a plugin via `claude plugins install <plugin> --scope <scope>`.
func (c *ExecCLI) Install(ctx context.Context, pluginID string, scope string) error {
	_, err := c.runner.Run(ctx, c.projectDir, "plugins", "install", pluginID, "--scope", scope)
	if err != nil {
		return fmt.Errorf("install %s: %w", pluginID, err)
	}
	return nil
}

// Uninstall removes a plugin via `claude plugins uninstall <plugin> --scope <scope>`.
func (c *ExecCLI) Uninstall(ctx context.Context, pluginID string, scope string) error {
	_, err := c.runner.Run(ctx, c.projectDir, "plugins", "uninstall", pluginID, "--scope", scope)
	if err != nil {
		return fmt.Errorf("uninstall %s: %w", pluginID, err)
	}
	return nil
}

// Enable activates a disabled plugin via `claude plugins enable <plugin> --scope <scope>`.
func (c *ExecCLI) Enable(ctx context.Context, pluginID string, scope string) error {
	_, err := c.runner.Run(ctx, c.projectDir, "plugins", "enable", pluginID, "--scope", scope)
	if err != nil {
		return fmt.Errorf("enable %s: %w", pluginID, err)
	}
	return nil
}

// Disable deactivates a plugin via `claude plugins disable <plugin> --scope <scope>`.
func (c *ExecCLI) Disable(ctx context.Context, pluginID string, scope string) error {
	_, err := c.runner.Run(ctx, c.projectDir, "plugins", "disable", pluginID, "--scope", scope)
	if err != nil {
		return fmt.Errorf("disable %s: %w", pluginID, err)
	}
	return nil
}

// Update updates a plugin via `claude plugins update <plugin>`.
func (c *ExecCLI) Update(ctx context.Context, pluginID string) error {
	_, err := c.runner.Run(ctx, c.projectDir, "plugins", "update", pluginID)
	if err != nil {
		return fmt.Errorf("update %s: %w", pluginID, err)
	}
	return nil
}

// MarketplaceAdd adds a marketplace via `claude plugins marketplace add <source>`.
func (c *ExecCLI) MarketplaceAdd(ctx context.Context, source string) error {
	_, err := c.runner.Run(ctx, c.projectDir, "plugins", "marketplace", "add", source)
	if err != nil {
		return fmt.Errorf("marketplace add %s: %w", source, err)
	}
	return nil
}

// MarketplaceRemove removes a marketplace via `claude plugins marketplace remove <name>`.
func (c *ExecCLI) MarketplaceRemove(ctx context.Context, name string) error {
	_, err := c.runner.Run(ctx, c.projectDir, "plugins", "marketplace", "remove", name)
	if err != nil {
		return fmt.Errorf("marketplace remove %s: %w", name, err)
	}
	return nil
}

// MarketplaceUpdate updates marketplaces via `claude plugins marketplace update [name]`.
func (c *ExecCLI) MarketplaceUpdate(ctx context.Context, name string) error {
	args := []string{"plugins", "marketplace", "update"}
	if name != "" {
		args = append(args, name)
	}
	_, err := c.runner.Run(ctx, c.projectDir, args...)
	if err != nil {
		return fmt.Errorf("marketplace update: %w", err)
	}
	return nil
}
