package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const defaultTimeout = 5 * time.Second

// validateShellSafe rejects strings containing shell metacharacters.
func validateShellSafe(s, field string) error {
	for _, c := range s {
		switch c {
		case ';', '&', '|', '`', '$', '(', ')', '{', '}', '<', '>', '\n', '\r', '\x00':
			return fmt.Errorf("%s contains unsafe character %q", field, c)
		}
	}
	return nil
}

// validateEnvKey rejects env keys containing = or newlines.
func validateEnvKey(k string) error {
	if strings.ContainsAny(k, "=\n\r\x00") {
		return fmt.Errorf("env key %q contains invalid character", k)
	}
	return nil
}

// ExecClient implements Client by executing tmux commands.
type ExecClient struct {
	tmuxBin string
	socket  string // tmux -L socket name (empty = default server)
}

// NewExecClient creates an ExecClient using the default tmux server.
func NewExecClient() *ExecClient {
	return &ExecClient{tmuxBin: "tmux"}
}

// NewExecClientWithSocket creates an ExecClient using a dedicated tmux socket.
// Sessions on this socket are invisible to the user's default `tmux ls`.
func NewExecClientWithSocket(socket string) *ExecClient {
	return &ExecClient{tmuxBin: "tmux", socket: socket}
}

// Socket returns the configured socket name (empty = default).
func (c *ExecClient) Socket() string {
	return c.socket
}

func (c *ExecClient) prependSocket(args []string) []string {
	if c.socket != "" {
		return append([]string{"-L", c.socket}, args...)
	}
	return args
}

func (c *ExecClient) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.tmuxBin, c.prependSocket(args)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (c *ExecClient) ListClients(ctx context.Context) ([]ClientInfo, error) {
	out, err := c.run(ctx, "list-clients", "-F",
		"#{client_name}\t#{client_session}\t#{client_width}\t#{client_height}\t#{client_activity}")
	if err != nil {
		return nil, err
	}
	return parseClients(out), nil
}

func (c *ExecClient) FindActiveClient(ctx context.Context) (*ClientInfo, error) {
	clients, err := c.ListClients(ctx)
	if err != nil {
		return nil, err
	}
	if len(clients) == 0 {
		return nil, nil
	}
	best := clients[0]
	for _, cl := range clients[1:] {
		if cl.Activity > best.Activity {
			best = cl
		}
	}
	return &best, nil
}

func (c *ExecClient) HasSession(ctx context.Context, name string) (bool, error) {
	_, err := c.run(ctx, "has-session", "-t", name)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *ExecClient) NewSession(ctx context.Context, opts NewSessionOpts) error {
	if err := validateShellSafe(opts.Name, "session name"); err != nil {
		return err
	}
	// opts.Command is not validated — it's built by the application and may
	// contain shell constructs like "cd /path && claude"
	for k := range opts.Env {
		if err := validateEnvKey(k); err != nil {
			return err
		}
	}

	args := []string{"new-session", "-s", opts.Name}
	if opts.WindowName != "" {
		args = append(args, "-n", opts.WindowName)
	}
	if opts.Detached {
		args = append(args, "-d")
	}
	if opts.Command != "" {
		args = append(args, opts.Command)
	}

	ctx2, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx2, c.tmuxBin, c.prependSocket(args)...)
	if len(opts.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range opts.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	return cmd.Run()
}

func (c *ExecClient) ListWindows(ctx context.Context, session string) ([]WindowInfo, error) {
	out, err := c.run(ctx, "list-windows", "-t", session, "-F",
		"#{window_id}\t#{window_index}\t#{window_name}\t#{session_name}\t#{window_active}")
	if err != nil {
		return nil, err
	}
	return parseWindows(out), nil
}

func (c *ExecClient) NewWindow(ctx context.Context, opts NewWindowOpts) error {
	for k := range opts.Env {
		if err := validateEnvKey(k); err != nil {
			return err
		}
	}

	args := []string{"new-window", "-t", opts.Session}
	if opts.Name != "" {
		args = append(args, "-n", opts.Name)
	}
	if opts.Command != "" {
		args = append(args, opts.Command)
	}

	ctx2, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx2, c.tmuxBin, c.prependSocket(args)...)
	if len(opts.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range opts.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	return cmd.Run()
}

func (c *ExecClient) RespawnPane(ctx context.Context, target, command string) error {
	_, err := c.run(ctx, "respawn-pane", "-t", target, "-k", command)
	return err
}

func (c *ExecClient) KillWindow(ctx context.Context, target string) error {
	_, err := c.run(ctx, "kill-window", "-t", target)
	return err
}

func (c *ExecClient) ListPanes(ctx context.Context, session string) ([]PaneInfo, error) {
	args := []string{"list-panes", "-F", "#{pane_id}\t#{window_id}\t#{pane_pid}\t#{pane_dead}"}
	if session != "" {
		args = append(args, "-t", session)
	} else {
		args = append(args, "-a")
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parsePanes(out), nil
}

func (c *ExecClient) CapturePaneContent(ctx context.Context, target string) (string, error) {
	return c.run(ctx, "capture-pane", "-t", target, "-p")
}

func (c *ExecClient) SendKeys(ctx context.Context, target string, keys ...string) error {
	args := append([]string{"send-keys", "-t", target}, keys...)
	_, err := c.run(ctx, args...)
	return err
}

func (c *ExecClient) DisplayPopup(ctx context.Context, opts PopupOpts) error {
	if err := validateShellSafe(opts.Cmd, "popup command"); err != nil {
		return err
	}

	args := []string{"display-popup"}
	if opts.Client != "" {
		args = append(args, "-c", opts.Client)
	}
	if opts.Width > 0 {
		args = append(args, fmt.Sprintf("-w%d%%", opts.Width))
	}
	if opts.Height > 0 {
		args = append(args, fmt.Sprintf("-h%d%%", opts.Height))
	}
	args = append(args, "-E", opts.Cmd)
	_, err := c.run(ctx, args...)
	return err
}

func (c *ExecClient) ShowMessage(ctx context.Context, target, format string) (string, error) {
	args := []string{"display-message", "-t", target, "-p", format}
	return c.run(ctx, args...)
}

func (c *ExecClient) GetOption(ctx context.Context, target, option string) (string, error) {
	args := []string{"show-option", "-gqv"}
	if target != "" {
		args = []string{"show-option", "-t", target, "-qv"}
	}
	args = append(args, option)
	return c.run(ctx, args...)
}

// --- Parsers ---

func parseClients(out string) []ClientInfo {
	if out == "" {
		return nil
	}
	var clients []ClientInfo
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 5 {
			continue
		}
		w, _ := strconv.Atoi(parts[2])
		h, _ := strconv.Atoi(parts[3])
		a, _ := strconv.ParseInt(parts[4], 10, 64)
		clients = append(clients, ClientInfo{
			Name:     parts[0],
			Session:  parts[1],
			Width:    w,
			Height:   h,
			Activity: a,
		})
	}
	return clients
}

func parseWindows(out string) []WindowInfo {
	if out == "" {
		return nil
	}
	var windows []WindowInfo
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 5 {
			continue
		}
		idx, _ := strconv.Atoi(parts[1])
		windows = append(windows, WindowInfo{
			ID:      parts[0],
			Index:   idx,
			Name:    parts[2],
			Session: parts[3],
			Active:  parts[4] == "1",
		})
	}
	return windows
}

func parsePanes(out string) []PaneInfo {
	if out == "" {
		return nil
	}
	var panes []PaneInfo
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		pid, _ := strconv.Atoi(parts[2])
		panes = append(panes, PaneInfo{
			ID:     parts[0],
			Window: parts[1],
			PID:    pid,
			Dead:   parts[3] == "1",
		})
	}
	return panes
}