package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
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

// envKeyPattern matches valid POSIX environment variable names.
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validateEnvKey rejects env keys that are not valid POSIX identifiers.
func validateEnvKey(k string) error {
	if !envKeyPattern.MatchString(k) {
		return fmt.Errorf("env key %q is not a valid identifier", k)
	}
	return nil
}

// ExecClient implements Client by executing tmux commands.
type ExecClient struct {
	tmuxBin  string
	socket   string   // tmux -L socket name (empty = default server)
	debugLog *os.File // optional debug log file
}

// NewExecClient creates an ExecClient using the default tmux server.
func NewExecClient() *ExecClient {
	return &ExecClient{tmuxBin: "tmux"}
}

// NewExecClientWithSocket creates an ExecClient using a dedicated tmux socket.
func NewExecClientWithSocket(socket string) *ExecClient {
	return &ExecClient{tmuxBin: "tmux", socket: socket}
}

// SetDebugLog enables command logging to a file.
func (c *ExecClient) SetDebugLog(f *os.File) {
	c.debugLog = f
}

func (c *ExecClient) logCmd(prefix string, args []string, output string, err error) {
	if c.debugLog == nil {
		return
	}
	if err != nil {
		fmt.Fprintf(c.debugLog, "%s: tmux %s → ERR: %v (out: %s)\n", prefix, strings.Join(args, " "), err, strings.TrimSpace(output))
	} else {
		fmt.Fprintf(c.debugLog, "%s: tmux %s → OK (out: %s)\n", prefix, strings.Join(args, " "), strings.TrimSpace(output))
	}
}

// Socket returns the configured socket name (empty = default).
func (c *ExecClient) Socket() string {
	return c.socket
}

func (c *ExecClient) prependSocket(args []string) []string {
	prefix := []string{"-u"} // force UTF-8
	if c.socket != "" {
		// Use -S for absolute paths, -L for socket names
		if strings.HasPrefix(c.socket, "/") {
			prefix = append(prefix, "-S", c.socket)
		} else {
			prefix = append(prefix, "-L", c.socket)
		}
	}
	return append(prefix, args...)
}

func (c *ExecClient) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	fullArgs := c.prependSocket(args)
	cmd := exec.CommandContext(ctx, c.tmuxBin, fullArgs...)

	// Use Output() (stdout only) — CombinedOutput() mixes stderr into stdout
	// which corrupts parseWindows/parsePanes parsing.
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()

	c.logCmd("run", fullArgs, string(out), err)
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w (stderr: %s)", strings.Join(fullArgs, " "), err, strings.TrimSpace(stderr.String()))
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
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	fullArgs := c.prependSocket([]string{"has-session", "-t", name})
	cmd := exec.CommandContext(ctx, c.tmuxBin, fullArgs...)

	var stderr strings.Builder
	cmd.Stderr = &stderr

	err := cmd.Run()
	c.logCmd("hasSession", fullArgs, "", err)
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			// Non-exit error (e.g., binary not found, context cancelled).
			return false, fmt.Errorf("tmux has-session: %w", err)
		}
		// Exit code 1: distinguish "session not found" from transient errors
		// by checking stderr. tmux writes "can't find session" when the
		// session genuinely does not exist.
		stderrStr := stderr.String()
		if strings.Contains(stderrStr, "can't find session") ||
			strings.Contains(stderrStr, "no session") ||
			strings.Contains(stderrStr, "no server running") ||
			strings.Contains(stderrStr, "no current target") {
			return false, nil
		}
		// Any other stderr (e.g., "error connecting")
		// is a transient error — propagate it.
		return false, fmt.Errorf("tmux has-session transient error: %s", strings.TrimSpace(stderrStr))
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

	args := []string{"-f", "/dev/null", "new-session", "-s", opts.Name}
	if opts.WindowName != "" {
		args = append(args, "-n", opts.WindowName)
	}
	if opts.StartDir != "" {
		args = append(args, "-c", opts.StartDir)
	}
	if opts.Detached {
		args = append(args, "-d")
	}
	if opts.Width > 0 {
		args = append(args, "-x", fmt.Sprintf("%d", opts.Width))
	}
	if opts.Height > 0 {
		args = append(args, "-y", fmt.Sprintf("%d", opts.Height))
	}
	// Pass environment variables via tmux -e flag (reaches the shell inside tmux)
	for k, v := range opts.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	if opts.Command != "" {
		args = append(args, opts.Command)
	}

	// Chain post-creation commands (e.g. set-option, unbind-key)
	for _, postCmd := range opts.PostCommands {
		args = append(args, ";")
		args = append(args, postCmd...)
	}

	ctx2, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	fullArgs := c.prependSocket(args)
	cmd := exec.CommandContext(ctx2, c.tmuxBin, fullArgs...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	c.logCmd("NewSession", fullArgs, string(out), err)
	if err != nil {
		return fmt.Errorf("new-session: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
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
	if opts.StartDir != "" {
		args = append(args, "-c", opts.StartDir)
	}
	for k, v := range opts.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	if opts.Command != "" {
		args = append(args, opts.Command)
	}

	ctx2, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	fullArgs := c.prependSocket(args)
	cmd := exec.CommandContext(ctx2, c.tmuxBin, fullArgs...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	c.logCmd("NewWindow", fullArgs, string(out), err)
	if err != nil {
		return fmt.Errorf("new-window: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (c *ExecClient) RespawnPane(ctx context.Context, target, command string) error {
	_, err := c.run(ctx, "respawn-pane", "-t", target, "-k", command)
	return err
}

func (c *ExecClient) KillWindow(ctx context.Context, target string) error {
	_, err := c.run(ctx, "kill-window", "-t", target)
	return err
}

func (c *ExecClient) ResizeWindow(ctx context.Context, target string, width, height int) error {
	_, err := c.run(ctx, "resize-window", "-t", target,
		"-x", fmt.Sprintf("%d", width), "-y", fmt.Sprintf("%d", height))
	return err
}

func (c *ExecClient) ListPanes(ctx context.Context, session string) ([]PaneInfo, error) {
	args := []string{"list-panes", "-F", "#{pane_id}\t#{window_id}\t#{pane_pid}\t#{pane_dead}"}
	if session != "" {
		// -s lists panes across ALL windows in the session.
		// Without -s, -t targets only the active window.
		args = append(args, "-s", "-t", session)
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

// CapturePaneANSI captures pane content with ANSI color escape codes preserved.
func (c *ExecClient) CapturePaneANSI(ctx context.Context, target string) (string, error) {
	return c.run(ctx, "capture-pane", "-t", target, "-ep")
}

// CapturePaneANSIRange captures a range of pane content with ANSI escape codes.
func (c *ExecClient) CapturePaneANSIRange(ctx context.Context, target string, start, end int) (string, error) {
	return c.run(ctx, "capture-pane", "-t", target, "-ep",
		"-S", strconv.Itoa(start), "-E", strconv.Itoa(end))
}

func (c *ExecClient) SendKeys(ctx context.Context, target string, keys ...string) error {
	args := append([]string{"send-keys", "-t", target}, keys...)
	_, err := c.run(ctx, args...)
	return err
}

func (c *ExecClient) SendKeysLiteral(ctx context.Context, target string, text string) error {
	// tmux's global argument parser treats a standalone ";" argument as a
	// command separator, even in exec mode. This happens before individual
	// command parsing, so "--" does not prevent it. Multi-character strings
	// like "hello;" are safe because ";" must be the entire argument.
	// Escape the bare ";" → "\;" so tmux passes it through to send-keys.
	escaped := text
	if escaped == ";" {
		escaped = `\;`
	}
	args := []string{"send-keys", "-l", "-t", target, "--", escaped}
	_, err := c.run(ctx, args...)
	return err
}

func (c *ExecClient) PasteToPane(ctx context.Context, target string, text string) error {
	ctx2, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	loadArgs := c.prependSocket([]string{"load-buffer", "-"})
	loadCmd := exec.CommandContext(ctx2, c.tmuxBin, loadArgs...)
	loadCmd.Stdin = strings.NewReader(text)
	if out, err := loadCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("load-buffer: %w (out: %s)", err, strings.TrimSpace(string(out)))
	}
	if _, err := c.run(ctx, "paste-buffer", "-t", target, "-d", "-p"); err != nil {
		return fmt.Errorf("paste-buffer: %w", err)
	}
	return nil
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