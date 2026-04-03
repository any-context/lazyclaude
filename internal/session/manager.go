package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/core/shell"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/google/uuid"
)

const tmuxSessionName = "lazyclaude"

// syncFailThreshold is the number of consecutive HasSession failures
// required before marking all sessions as orphans. This prevents a single
// transient tmux error from cascading into full session teardown.
const syncFailThreshold = 3

// Manager handles session lifecycle (CRUD + tmux synchronization).
type Manager struct {
	store         *Store
	tmux          tmux.Client
	paths         config.Paths
	log           *slog.Logger
	mu            sync.Mutex // guards Create/Delete/Sync against concurrent GC
	syncFailCount int        // consecutive Sync calls where HasSession returned false
}

// NewManager creates a session manager.
func NewManager(store *Store, tmuxClient tmux.Client, paths config.Paths, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &Manager{
		store: store,
		tmux:  tmuxClient,
		paths: paths,
		log:   log,
	}
}

// Store returns the underlying store.
func (m *Manager) Store() *Store {
	return m.store
}

// Load reads sessions from disk and syncs with tmux.
func (m *Manager) Load(ctx context.Context) error {
	if err := m.store.Load(); err != nil {
		return fmt.Errorf("load store: %w", err)
	}
	return m.Sync(ctx)
}

// Sync updates runtime state by comparing store with tmux windows.
// Acquires the manager mutex to prevent races with Create/Delete.
func (m *Manager) Sync(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	exists, err := m.tmux.HasSession(ctx, tmuxSessionName)
	if err != nil {
		m.log.Warn("sync.hasSession.error", "err", err)
		return fmt.Errorf("check session: %w", err)
	}
	m.log.Debug("sync.hasSession", "exists", exists)
	if !exists {
		m.syncFailCount++
		m.log.Debug("sync.noSession",
			"failCount", m.syncFailCount,
			"threshold", syncFailThreshold,
			"sessionCount", len(m.store.All()))
		if m.syncFailCount < syncFailThreshold {
			return nil
		}
		m.log.Debug("sync.noSession", "action", "markAllOrphan", "count", len(m.store.All()))
		// Crash diagnosis: log orphan marking.
		if f, err := os.OpenFile("/tmp/lazyclaude/crash.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			fmt.Fprintf(f, "[%s] MARK_ALL_ORPHAN failCount=%d sessions=%d\n", time.Now().Format(time.RFC3339), m.syncFailCount, len(m.store.All()))
			f.Sync()
			f.Close()
		}
		m.store.MarkAllStatus(StatusOrphan)
		return nil
	}
	m.syncFailCount = 0

	windows, err := m.tmux.ListWindows(ctx, tmuxSessionName)
	if err != nil {
		m.log.Warn("sync.listWindows.error", "err", err)
		return fmt.Errorf("list windows: %w", err)
	}

	panes, err := m.tmux.ListPanes(ctx, tmuxSessionName)
	if err != nil {
		m.log.Warn("sync.listPanes.error", "err", err)
		return fmt.Errorf("list panes: %w", err)
	}

	m.log.Debug("sync", "windows", len(windows), "panes", len(panes), "sessions", len(m.store.All()))
	for _, w := range windows {
		m.log.Debug("sync.window", "id", w.ID, "name", w.Name)
	}
	m.store.SyncWithTmux(windows, panes)
	for _, s := range m.store.All() {
		m.log.Debug("sync.result", "name", s.Name, "status", s.Status, "tmuxWindow", s.TmuxWindow)
	}
	return nil
}

// EnsureClaudeConfigured sets onboarding flags in ~/.claude.json so that
// Claude Code skips theme selection and workspace trust dialogs.
// Only writes if hasCompletedOnboarding is not already true.
// No subprocess calls — just JSON file I/O (< 1ms).
func (m *Manager) EnsureClaudeConfigured(dirPath string) {
	configPath := filepath.Join(os.Getenv("HOME"), ".claude.json")

	var cfg map[string]any
	if data, err := os.ReadFile(configPath); err == nil && len(data) > 0 {
		if json.Unmarshal(data, &cfg) != nil {
			cfg = make(map[string]any)
		}
	} else {
		cfg = make(map[string]any)
	}

	if completed, ok := cfg["hasCompletedOnboarding"].(bool); ok && completed {
		return
	}

	cfg["hasCompletedOnboarding"] = true
	cfg["numStartups"] = 10

	projects, _ := cfg["projects"].(map[string]any)
	if projects == nil {
		projects = make(map[string]any)
	}
	if abs, err := filepath.Abs(dirPath); err == nil {
		projects[abs] = map[string]any{"hasTrustDialogAccepted": true, "allowedTools": []any{}}
	}
	projects["/"] = map[string]any{"hasTrustDialogAccepted": true, "allowedTools": []any{}}
	cfg["projects"] = projects

	if out, err := json.Marshal(cfg); err == nil {
		os.WriteFile(configPath, out, 0o600)
	}
}

// Create creates a new session with a tmux window.
// Holds the manager mutex throughout to prevent GC from orphaning the new session.
func (m *Manager) Create(ctx context.Context, dirPath, host string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := m.store.GenerateName(dirPath, host)
	id := uuid.New().String()
	m.log.Info("create.start", "name", name, "id", id[:8], "path", dirPath)

	sess := Session{
		ID:        id,
		Name:      name,
		Path:      dirPath,
		Host:      host,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Read MCP info for environment variable injection.
	mcpPort, mcpToken, mcpErr := m.readMCPInfo()

	var claudeCmd string
	if host != "" {
		if mcpErr != nil {
			return nil, fmt.Errorf("read MCP server info for SSH session: %w", mcpErr)
		}
		var sshErr error
		claudeCmd, sshErr = buildSSHCommand(sess, mcpPort, mcpToken, nil)
		if sshErr != nil {
			return nil, fmt.Errorf("build SSH command: %w", sshErr)
		}

		// Write pending window file so the MCP server can associate
		// the remote ide_connected PID with this tmux window.
		pendingFile := filepath.Join(m.paths.RuntimeDir, "lazyclaude-pending-window")
		if writeErr := os.WriteFile(pendingFile, []byte(sess.WindowName()), 0o600); writeErr != nil {
			m.log.Warn("create.pending-window.write", "err", writeErr)
		}
	} else {
		claudeCmd = m.buildClaudeCommand(sess)
	}

	absPath, err := filepath.Abs(sess.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve path %q: %w", sess.Path, err)
	}

	env := claudeEnv(id)
	return m.launchSession(ctx, sess, claudeCmd, absPath, env)
}

// worktreeOpts configures how a worktree session is created.
type worktreeOpts struct {
	Name        string // session/branch name (validated unless SkipGitAdd)
	WtPath      string // explicit worktree path (set for ResumeWorktree; empty = derive from projectRoot)
	UserPrompt  string
	ProjectRoot string
	Host        string // SSH host; empty for local sessions
	Role        Role   // RoleNone for regular worktree, RoleWorker for worker sessions
	SkipGitAdd  bool   // true for ResumeWorktree (directory already exists)
}

// createWorktreeSession is the shared implementation for CreateWorktree,
// ResumeWorktree, and CreateWorkerSession.
func (m *Manager) createWorktreeSession(ctx context.Context, opts worktreeOpts) (*Session, error) {
	if !opts.SkipGitAdd {
		if err := ValidateWorktreeName(opts.Name); err != nil {
			return nil, fmt.Errorf("invalid worktree name: %w", err)
		}
	}

	if opts.SkipGitAdd && opts.WtPath != "" && opts.Host == "" {
		if _, err := os.Stat(opts.WtPath); err != nil {
			return nil, fmt.Errorf("worktree path does not exist: %w", err)
		}
	}

	// Read MCP info outside the lock (file I/O).
	// Used only to decide whether the Worker prompt (CLI-based) or the
	// basic worktree isolation prompt is injected.
	mcpPort, mcpToken, _ := m.readMCPInfo()

	m.mu.Lock()
	defer m.mu.Unlock()

	if existing := m.store.FindByName(opts.Name); existing != nil {
		return nil, fmt.Errorf("worktree %q already exists", opts.Name)
	}

	wtPath := opts.WtPath
	if wtPath == "" {
		wtPath = WorktreePath(opts.ProjectRoot, opts.Name)
	}

	if !opts.SkipGitAdd {
		if err := createGitWorktree(ctx, opts.ProjectRoot, wtPath, opts.Name, opts.Host); err != nil {
			return nil, fmt.Errorf("git worktree: %w", err)
		}
	}

	return m.launchWorktreeSession(ctx, opts.Name, wtPath, opts.UserPrompt, opts.ProjectRoot, opts.Host, opts.Role, mcpPort, mcpToken)
}

// CreateWorktree creates a git worktree and launches Claude Code with an initial prompt.
// The worktree is placed at {projectRoot}/.lazyclaude/worktrees/{name}/.
func (m *Manager) CreateWorktree(ctx context.Context, name, userPrompt, projectRoot, host string) (*Session, error) {
	return m.createWorktreeSession(ctx, worktreeOpts{
		Name:        name,
		UserPrompt:  userPrompt,
		ProjectRoot: projectRoot,
		Host:        host,
		Role:        RoleWorker,
	})
}

// ResumeWorktree launches Claude Code in an existing worktree directory.
// Unlike CreateWorktree, it does not run `git worktree add`.
func (m *Manager) ResumeWorktree(ctx context.Context, worktreePath, userPrompt, projectRoot, host string) (*Session, error) {
	return m.createWorktreeSession(ctx, worktreeOpts{
		Name:        filepath.Base(worktreePath),
		WtPath:      worktreePath,
		UserPrompt:  userPrompt,
		ProjectRoot: projectRoot,
		Host:        host,
		Role:        RoleWorker,
		SkipGitAdd:  true,
	})
}

// launchSession creates a tmux window for sess using claudeCmd and persists the
// session to the store. Caller must hold m.mu.
func (m *Manager) launchSession(ctx context.Context, sess Session, claudeCmd, startDir string, env map[string]string) (*Session, error) {
	windowName := sess.WindowName()

	exists, err := m.tmux.HasSession(ctx, tmuxSessionName)
	if err != nil {
		return nil, fmt.Errorf("check session: %w", err)
	}

	width, height := termSize()

	if !exists {
		err = m.tmux.NewSession(ctx, tmux.NewSessionOpts{
			Name:         tmuxSessionName,
			WindowName:   windowName,
			Command:      claudeCmd,
			StartDir:     startDir,
			Detached:     true,
			Width:        width,
			Height:       height,
			Env:          env,
			PostCommands: cleanSessionCommands(),
		})
	} else {
		err = m.tmux.NewWindow(ctx, tmux.NewWindowOpts{
			Session:  tmuxSessionName,
			Name:     windowName,
			Command:  claudeCmd,
			StartDir: startDir,
			Env:      env,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("create tmux window: %w", err)
	}

	sess.Status = StatusRunning
	m.store.Add(sess)

	if err := m.store.Save(); err != nil {
		return nil, fmt.Errorf("save store: %w", err)
	}

	return &sess, nil
}

// launchWorktreeSession is the shared logic for creating a tmux window
// running Claude Code in a worktree directory. Called by CreateWorktree,
// ResumeWorktree, and CreateWorkerSession. Caller must hold m.mu.
func (m *Manager) launchWorktreeSession(ctx context.Context, name, wtPath, userPrompt, projectRoot, host string, role Role, mcpPort int, mcpToken string) (*Session, error) {
	id := uuid.New().String()

	systemPrompt := BuildWorkerPrompt(wtPath, projectRoot, id)

	sess := Session{
		ID:        id,
		Name:      name,
		Path:      wtPath,
		Host:      host,
		Role:      role,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	var claudeCmd string
	launchSuccess := false
	if host != "" {
		// SSH worktree session: pass system/user prompts via heredoc in the remote script.
		scriptOpts := &remoteScriptOpts{
			SystemPrompt: systemPrompt,
			UserPrompt:   userPrompt,
		}
		var sshErr error
		claudeCmd, sshErr = buildSSHCommand(sess, mcpPort, mcpToken, scriptOpts)
		if sshErr != nil {
			return nil, fmt.Errorf("build SSH command: %w", sshErr)
		}

		// Write pending window file for MCP server association.
		pendingFile := filepath.Join(m.paths.RuntimeDir, "lazyclaude-pending-window")
		if writeErr := os.WriteFile(pendingFile, []byte(sess.WindowName()), 0o600); writeErr != nil {
			m.log.Warn("launchWorktree.pending-window.write", "err", writeErr)
		}
	} else {
		launcher, err := writeWorktreeLauncher(systemPrompt, userPrompt, m.paths.RuntimeDir)
		if err != nil {
			return nil, fmt.Errorf("write launcher: %w", err)
		}
		defer func() {
			if !launchSuccess {
				os.Remove(launcher)
			}
		}()

		// shell.Quote wraps the temp path in single quotes ('...').
		// OS temp paths never contain single quotes, so this is safe.
		claudeCmd = fmt.Sprintf("exec \"$SHELL\" -lic 'exec sh %s'", shell.Quote(launcher))
	}

	m.log.Info("launchWorktree", "name", name, "id", id[:8], "path", wtPath, "host", host, "role", role)

	env := claudeEnv(id)
	startDir := wtPath
	if host != "" {
		// SSH sessions: tmux starts locally, the SSH command handles the remote path.
		abs, err := filepath.Abs(".")
		if err != nil {
			abs = "."
		}
		startDir = abs
	}
	result, err := m.launchSession(ctx, sess, claudeCmd, startDir, env)
	if err == nil {
		launchSuccess = true
	}
	return result, err
}

// createGitWorktree creates a git worktree at wtPath with a new branch.
// Returns an error if projectRoot is not a git repository.
// If the branch already exists, it checks out the existing branch.
// If the worktree path already exists (reuse), this is a no-op.
// When host is non-empty, all commands are executed on the remote host via SSH.
func createGitWorktree(ctx context.Context, projectRoot, wtPath, branch, host string) error {
	if host != "" {
		return createGitWorktreeRemote(ctx, projectRoot, wtPath, branch, host)
	}

	// Verify projectRoot is a git repository.
	check := exec.CommandContext(ctx, "git", "rev-parse", "--git-dir")
	check.Dir = projectRoot
	if err := check.Run(); err != nil {
		return fmt.Errorf("not a git repository: %s", projectRoot)
	}

	// If the worktree directory already exists, assume reuse.
	if _, err := os.Stat(wtPath); err == nil {
		return nil
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	// Try creating worktree with a new branch first.
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "-b", branch, wtPath)
	cmd.Dir = projectRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		// Branch may already exist — try without -b.
		cmd2 := exec.CommandContext(ctx, "git", "worktree", "add", wtPath, branch)
		cmd2.Dir = projectRoot
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("%s\n%s", strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)))
		}
		_ = out
	}
	return nil
}

// createGitWorktreeRemote creates a git worktree on a remote host via SSH.
func createGitWorktreeRemote(ctx context.Context, projectRoot, wtPath, branch, host string) error {
	sq := posixQuote // alias for readability

	// Verify projectRoot is a git repository on remote.
	checkCmd := fmt.Sprintf("cd %s && git rev-parse --git-dir", sq(projectRoot))
	if _, err := RunSSHCommand(ctx, host, checkCmd); err != nil {
		return fmt.Errorf("not a git repository: %s", projectRoot)
	}

	// Check if worktree directory already exists on remote (reuse).
	existsCmd := fmt.Sprintf("test -d %s && echo exists", sq(wtPath))
	if out, err := RunSSHCommand(ctx, host, existsCmd); err == nil && strings.TrimSpace(string(out)) == "exists" {
		return nil
	}

	// Ensure parent directory exists on remote.
	mkdirCmd := fmt.Sprintf("mkdir -p %s", sq(filepath.Dir(wtPath)))
	if _, err := RunSSHCommand(ctx, host, mkdirCmd); err != nil {
		return fmt.Errorf("create parent dir on remote: %w", err)
	}

	// Try creating worktree with a new branch first.
	addCmd := fmt.Sprintf("cd %s && git worktree add -b %s %s 2>&1", sq(projectRoot), sq(branch), sq(wtPath))
	out, err := RunSSHCommand(ctx, host, addCmd)
	if err == nil {
		return nil
	}
	// Branch may already exist — try without -b.
	addCmd2 := fmt.Sprintf("cd %s && git worktree add %s %s 2>&1", sq(projectRoot), sq(wtPath), sq(branch))
	out2, err2 := RunSSHCommand(ctx, host, addCmd2)
	if err2 != nil {
		return fmt.Errorf("%s\n%s", strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)))
	}
	return nil
}

// posixQuote wraps a string in POSIX single quotes with proper escaping.
// Any embedded single quotes are replaced with '\'' (end single quote,
// escaped single quote, start single quote).
func posixQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// writeWorktreeLauncher writes a shell script that launches claude with
// --append-system-prompt and an optional user prompt as positional argument.
// Returns the script path. The script self-deletes after execution.
func writeWorktreeLauncher(systemPrompt, userPrompt, runtimeDir string) (string, error) {
	f, err := os.CreateTemp("", "lazyclaude-wt-*.sh")
	if err != nil {
		return "", fmt.Errorf("create temp script: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	// Self-delete the launcher script (already read by shell at this point).
	sb.WriteString("rm -f \"$0\"\n")
	sb.WriteString("exec claude")

	// Inject hooks via --settings file so ~/.claude/settings.json stays untouched.
	// Using a file avoids shell quoting issues with nested single quotes in hook commands.
	if settingsFile, err := config.WriteHooksSettingsFile(runtimeDir); err == nil {
		sb.WriteString(" --settings ")
		sb.WriteString(shell.Quote(settingsFile))
	}

	sb.WriteString(" --append-system-prompt ")
	sb.WriteString(shell.Quote(systemPrompt))
	if strings.TrimSpace(userPrompt) != "" {
		sb.WriteString(" ")
		sb.WriteString(shell.Quote(userPrompt))
	}
	sb.WriteString("\n")

	if _, err := f.WriteString(sb.String()); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("write launcher script: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("close launcher script: %w", err)
	}
	return f.Name(), nil
}

// Delete removes a session and kills its tmux window.
func (m *Manager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess := m.store.FindByID(id)
	if sess == nil {
		return fmt.Errorf("session not found: %s", id)
	}

	// Kill tmux window only for Dead sessions (process exited but pane remains).
	// Orphan sessions are skipped: the window may still be alive if HasSession
	// failed transiently (e.g. tmux server temporarily unreachable). Killing an
	// orphan's window would destroy a perfectly healthy Claude Code session.
	target := tmuxSessionName + ":" + sess.WindowName()
	m.log.Info("delete", "name", sess.Name, "id", id[:8], "target", target, "status", sess.Status)
	if sess.Status != StatusOrphan {
		_ = m.tmux.KillWindow(ctx, target)
	}

	m.store.Remove(id)

	if err := m.store.Save(); err != nil {
		return fmt.Errorf("save store: %w", err)
	}
	return nil
}

// Rename changes a session's name.
func (m *Manager) Rename(id, newName string) error {
	if !m.store.Rename(id, newName) {
		return fmt.Errorf("session not found: %s", id)
	}
	return m.store.Save()
}

// PurgeOrphans removes all orphan sessions from the store.
func (m *Manager) PurgeOrphans() (int, error) {
	sessions := m.store.All()
	count := 0
	for _, s := range sessions {
		if s.Status == StatusOrphan {
			m.store.Remove(s.ID)
			count++
		}
	}
	if count > 0 {
		if err := m.store.Save(); err != nil {
			return count, err
		}
	}
	return count, nil
}

// Sessions returns all sessions (read-only copy).
func (m *Manager) Sessions() []Session {
	return m.store.All()
}

// Projects returns all projects (read-only copy).
func (m *Manager) Projects() []Project {
	return m.store.Projects()
}

// ToggleProjectExpanded toggles a project's expanded state.
func (m *Manager) ToggleProjectExpanded(projectID string) {
	m.store.ToggleProjectExpanded(projectID)
}

// readMCPInfo reads the MCP server port and auth token from disk.
// The port file contains the port number; the lock file contains the auth token.
func (m *Manager) readMCPInfo() (int, string, error) {
	portData, err := os.ReadFile(m.paths.PortFile())
	if err != nil {
		return 0, "", fmt.Errorf("read port file %s: %w", m.paths.PortFile(), err)
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(portData)))
	if err != nil {
		return 0, "", fmt.Errorf("parse port from %s: %w", m.paths.PortFile(), err)
	}

	lockData, err := os.ReadFile(m.paths.LockFile(port))
	if err != nil {
		return 0, "", fmt.Errorf("read lock file %s: %w", m.paths.LockFile(port), err)
	}
	var lock struct {
		AuthToken string `json:"authToken"`
	}
	if err := json.Unmarshal(lockData, &lock); err != nil {
		return 0, "", fmt.Errorf("parse lock file: %w", err)
	}
	return port, lock.AuthToken, nil
}

func (m *Manager) buildClaudeCommand(sess Session) string {
	claudeArgs := "claude"

	// Inject hooks via --settings file so ~/.claude/settings.json stays untouched.
	// The path is NOT wrapped in shell.Quote because the entire claudeArgs string
	// is already inside single quotes in the final command. Nesting single quotes
	// would break the shell parsing. Runtime dir paths (/tmp, /var/folders/...)
	// never contain spaces or special characters.
	if settingsFile, err := config.WriteHooksSettingsFile(m.paths.RuntimeDir); err == nil {
		claudeArgs += " --settings " + settingsFile
	}

	for _, f := range sess.Flags {
		claudeArgs += " " + shell.Quote(f)
	}
	// exec $SHELL -lic runs in login shell so PATH (.zshrc/.profile) is loaded
	return fmt.Sprintf("exec \"$SHELL\" -lic 'exec %s'", claudeArgs)
}

// claudeEnv returns environment variables to pass to Claude Code sessions.
// Inherits auth tokens and Claude-specific vars from the parent process.
// Server port/token are NOT injected as env vars — hooks always discover the
// server via lock file scanning so they survive server restarts.
func claudeEnv(sessionID string) map[string]string {
	env := map[string]string{
		"CLAUDE_CODE_AUTO_CONNECT_IDE": "true",
	}
	if sessionID != "" {
		env["LAZYCLAUDE_SESSION_ID"] = sessionID
	}
	// Pass through Claude auth and config env vars
	passthrough := []string{
		"CLAUDE_CODE_OAUTH_TOKEN",
		"ANTHROPIC_API_KEY",
		"CLAUDE_CODE_API_KEY",
		"CLAUDE_CODE_SSE_PORT",
	}
	for _, key := range passthrough {
		if val := os.Getenv(key); val != "" {
			env[key] = val
		}
	}
	return env
}

// cleanSessionCommands returns tmux commands chained after new-session via ";".
// Configures the lazyclaude tmux server: disables status bar, prevents window
// renaming, keeps dead panes, and binds Ctrl+\ to detach-client.
func cleanSessionCommands() [][]string {
	// Use -g (global) so settings apply to all windows on the lazyclaude
	// server, not just the current session/window context.
	return [][]string{
		{"set-option", "-g", "status", "off"},
		{"set-option", "-g", "automatic-rename", "off"},
		{"set-option", "-g", "remain-on-exit", "on"},
		{"set-option", "-g", "window-size", "largest"},
		{"set-option", "-g", "exit-empty", "off"},
		{"set-hook", "-g", "pane-died", "detach-client"},
		{"bind-key", "-T", "root", "C-\\", "detach-client"},
	}
}

// termSize returns the current terminal width and height.
// Returns 0, 0 if the terminal size cannot be determined.
func termSize() (int, int) {
	w, h, err := term.GetSize(int(os.Stdin.Fd()))
	if err != nil {
		return 0, 0
	}
	return w, h
}

// CreatePMSession creates a PM (Project Manager) session for the given projectRoot.
// Returns an error if a PM session already exists for this projectRoot.
// Holds the manager mutex throughout to prevent races.
func (m *Manager) CreatePMSession(ctx context.Context, projectRoot, host string) (*Session, error) {
	// Read MCP info outside the lock (file I/O).
	mcpPort, mcpToken, mcpErr := m.readMCPInfo()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check that no PM session already exists for this projectRoot.
	if p := m.store.FindProjectByPath(projectRoot); p != nil && p.PM != nil {
		return nil, fmt.Errorf("pm session already exists for %q", projectRoot)
	}

	// Build worker list string from existing worker sessions in this project.
	var workerLines []string
	if p := m.store.FindProjectByPath(projectRoot); p != nil {
		for _, s := range p.Sessions {
			if s.Role == RoleWorker {
				workerLines = append(workerLines, fmt.Sprintf("- %s (id=%s, path=%s)", s.Name, s.ID, s.Path))
			}
		}
	}
	workerList := strings.Join(workerLines, "\n")

	id := uuid.New().String()
	systemPrompt := BuildPMPrompt(projectRoot, id, workerList)

	sess := Session{
		ID:        id,
		Name:      "pm",
		Path:      projectRoot,
		Host:      host,
		Role:      RolePM,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	var claudeCmd string
	launchSuccess := false
	if host != "" {
		if mcpErr != nil {
			return nil, fmt.Errorf("read MCP server info for SSH PM session: %w", mcpErr)
		}
		scriptOpts := &remoteScriptOpts{
			SystemPrompt: systemPrompt,
		}
		var sshErr error
		claudeCmd, sshErr = buildSSHCommand(sess, mcpPort, mcpToken, scriptOpts)
		if sshErr != nil {
			return nil, fmt.Errorf("build SSH command: %w", sshErr)
		}

		pendingFile := filepath.Join(m.paths.RuntimeDir, "lazyclaude-pending-window")
		if writeErr := os.WriteFile(pendingFile, []byte(sess.WindowName()), 0o600); writeErr != nil {
			m.log.Warn("createPMSession.pending-window.write", "err", writeErr)
		}
	} else {
		launcher, err := writeWorktreeLauncher(systemPrompt, "", m.paths.RuntimeDir)
		if err != nil {
			return nil, fmt.Errorf("write launcher: %w", err)
		}
		defer func() {
			if !launchSuccess {
				os.Remove(launcher)
			}
		}()
		claudeCmd = fmt.Sprintf("exec \"$SHELL\" -lic 'exec sh %s'", shell.Quote(launcher))
	}

	m.log.Info("createPMSession", "id", id[:8], "path", projectRoot, "host", host)

	env := claudeEnv(id)
	startDir := projectRoot
	if host != "" {
		abs, err := filepath.Abs(".")
		if err != nil {
			abs = "."
		}
		startDir = abs
	}
	result, err := m.launchSession(ctx, sess, claudeCmd, startDir, env)
	if err == nil {
		launchSuccess = true
	}
	return result, err
}

// CreateWorkerSession creates a git worktree and launches Claude Code with the
// Worker role and MCP-integrated system prompt.
func (m *Manager) CreateWorkerSession(ctx context.Context, name, userPrompt, projectRoot, host string) (*Session, error) {
	return m.createWorktreeSession(ctx, worktreeOpts{
		Name:        name,
		UserPrompt:  userPrompt,
		ProjectRoot: projectRoot,
		Host:        host,
		Role:        RoleWorker,
	})
}

