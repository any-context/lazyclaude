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

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/core/shell"
	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/google/uuid"
)

const tmuxSessionName = "lazyclaude"

// Manager handles session lifecycle (CRUD + tmux synchronization).
type Manager struct {
	store *Store
	tmux  tmux.Client
	paths config.Paths
	log   *slog.Logger
	mu    sync.Mutex // guards Create/Delete/Sync against concurrent GC
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
		m.log.Debug("sync.noSession", "action", "markAllOrphan", "count", len(m.store.All()))
		m.store.MarkAllStatus(StatusOrphan)
		return nil
	}

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

	var claudeCmd string
	if host != "" {
		mcpPort, token, mcpErr := m.readMCPInfo()
		if mcpErr != nil {
			return nil, fmt.Errorf("read MCP server info for SSH session: %w", mcpErr)
		}
		var sshErr error
		claudeCmd, sshErr = buildSSHCommand(sess, mcpPort, token)
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

	return m.launchSession(ctx, sess, claudeCmd, absPath)
}

// worktreeOpts configures how a worktree session is created.
type worktreeOpts struct {
	Name        string // session/branch name (validated unless SkipGitAdd)
	WtPath      string // explicit worktree path (set for ResumeWorktree; empty = derive from projectRoot)
	UserPrompt  string
	ProjectRoot string
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

	if opts.SkipGitAdd && opts.WtPath != "" {
		if _, err := os.Stat(opts.WtPath); err != nil {
			return nil, fmt.Errorf("worktree path does not exist: %w", err)
		}
	}

	// Read MCP info outside the lock (file I/O).
	// Worker role requires MCP info; regular worktrees gracefully degrade.
	mcpPort, mcpToken, mcpErr := m.readMCPInfo()
	if mcpErr != nil && opts.Role == RoleWorker {
		return nil, fmt.Errorf("read MCP info for worker session: %w", mcpErr)
	}

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
		if err := createGitWorktree(ctx, opts.ProjectRoot, wtPath, opts.Name); err != nil {
			return nil, fmt.Errorf("git worktree: %w", err)
		}
	}

	return m.launchWorktreeSession(ctx, opts.Name, wtPath, opts.UserPrompt, opts.ProjectRoot, opts.Role, mcpPort, mcpToken)
}

// CreateWorktree creates a git worktree and launches Claude Code with an initial prompt.
// The worktree is placed at {projectRoot}/.claude/worktrees/{name}/.
func (m *Manager) CreateWorktree(ctx context.Context, name, userPrompt, projectRoot string) (*Session, error) {
	return m.createWorktreeSession(ctx, worktreeOpts{
		Name:        name,
		UserPrompt:  userPrompt,
		ProjectRoot: projectRoot,
		Role:        RoleNone,
	})
}

// ResumeWorktree launches Claude Code in an existing worktree directory.
// Unlike CreateWorktree, it does not run `git worktree add`.
func (m *Manager) ResumeWorktree(ctx context.Context, worktreePath, userPrompt, projectRoot string) (*Session, error) {
	return m.createWorktreeSession(ctx, worktreeOpts{
		Name:        filepath.Base(worktreePath),
		WtPath:      worktreePath,
		UserPrompt:  userPrompt,
		ProjectRoot: projectRoot,
		Role:        RoleNone,
		SkipGitAdd:  true,
	})
}

// launchSession creates a tmux window for sess using claudeCmd and persists the
// session to the store. Caller must hold m.mu.
func (m *Manager) launchSession(ctx context.Context, sess Session, claudeCmd, startDir string) (*Session, error) {
	windowName := sess.WindowName()

	exists, err := m.tmux.HasSession(ctx, tmuxSessionName)
	if err != nil {
		return nil, fmt.Errorf("check session: %w", err)
	}

	env := claudeEnv()
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
//
// mcpPort/mcpToken are pre-resolved by the caller (outside the lock).
// When mcpPort <= 0, the basic worktree isolation prompt is used instead.
func (m *Manager) launchWorktreeSession(ctx context.Context, name, wtPath, userPrompt, projectRoot string, role Role, mcpPort int, mcpToken string) (*Session, error) {
	id := uuid.New().String()

	var systemPrompt string
	if mcpPort > 0 {
		systemPrompt = BuildWorkerPrompt(wtPath, projectRoot, id, mcpPort, mcpToken, m.paths.PortFile(), m.paths.IDEDir)
	} else {
		systemPrompt = BuildWorktreePrompt(wtPath, projectRoot)
	}

	launcher, err := writeWorktreeLauncher(systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("write launcher: %w", err)
	}
	cleanupLauncher := true
	defer func() {
		if cleanupLauncher {
			os.Remove(launcher)
		}
	}()

	sess := Session{
		ID:        id,
		Name:      name,
		Path:      wtPath,
		Role:      role,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// shell.Quote wraps the temp path in single quotes ('...').
	// OS temp paths never contain single quotes, so this is safe.
	claudeCmd := fmt.Sprintf("exec \"$SHELL\" -lic 'exec sh %s'", shell.Quote(launcher))
	m.log.Info("launchWorktree", "name", name, "id", id[:8], "path", wtPath, "role", role)

	result, err := m.launchSession(ctx, sess, claudeCmd, wtPath)
	if err != nil {
		return nil, err
	}
	cleanupLauncher = false

	return result, nil
}

// createGitWorktree creates a git worktree at wtPath with a new branch.
// Returns an error if projectRoot is not a git repository.
// If the branch already exists, it checks out the existing branch.
// If the worktree path already exists (reuse), this is a no-op.
func createGitWorktree(ctx context.Context, projectRoot, wtPath, branch string) error {
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

// writeWorktreeLauncher writes a shell script that launches claude with
// --append-system-prompt and an optional user prompt as positional argument.
// Returns the script path. The script self-deletes after execution.
func writeWorktreeLauncher(systemPrompt, userPrompt string) (string, error) {
	f, err := os.CreateTemp("", "lazyclaude-wt-*.sh")
	if err != nil {
		return "", fmt.Errorf("create temp script: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	// Self-delete the launcher script (already read by shell at this point).
	sb.WriteString("rm -f \"$0\"\n")
	sb.WriteString("exec claude")
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

	// Kill tmux window by name (best-effort, window may already be gone).
	target := tmuxSessionName + ":" + sess.WindowName()
	m.log.Info("delete", "name", sess.Name, "id", id[:8], "target", target, "status", sess.Status)
	_ = m.tmux.KillWindow(ctx, target)

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
	for _, f := range sess.Flags {
		claudeArgs += " " + shell.Quote(f)
	}
	// exec $SHELL -lic runs in login shell so PATH (.zshrc/.profile) is loaded
	return fmt.Sprintf("exec \"$SHELL\" -lic 'exec %s'", claudeArgs)
}

// claudeEnv returns environment variables to pass to Claude Code sessions.
// Inherits auth tokens and Claude-specific vars from the parent process.
func claudeEnv() map[string]string {
	env := map[string]string{
		"CLAUDE_CODE_AUTO_CONNECT_IDE": "true",
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
func (m *Manager) CreatePMSession(ctx context.Context, projectRoot string) (*Session, error) {
	// Read MCP info outside the lock (file I/O).
	mcpPort, token, err := m.readMCPInfo()
	if err != nil {
		return nil, fmt.Errorf("read MCP info for pm session: %w", err)
	}

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
	systemPrompt := BuildPMPrompt(id, mcpPort, token, workerList, m.paths.PortFile(), m.paths.IDEDir)

	launcher, err := writeWorktreeLauncher(systemPrompt, "")
	if err != nil {
		return nil, fmt.Errorf("write launcher: %w", err)
	}
	cleanupLauncher := true
	defer func() {
		if cleanupLauncher {
			os.Remove(launcher)
		}
	}()

	sess := Session{
		ID:        id,
		Name:      "pm",
		Path:      projectRoot,
		Role:      RolePM,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	claudeCmd := fmt.Sprintf("exec \"$SHELL\" -lic 'exec sh %s'", shell.Quote(launcher))
	m.log.Info("createPMSession", "id", id[:8], "path", projectRoot)

	result, err := m.launchSession(ctx, sess, claudeCmd, projectRoot)
	if err != nil {
		return nil, err
	}
	cleanupLauncher = false

	return result, nil
}

// CreateWorkerSession creates a git worktree and launches Claude Code with the
// Worker role and MCP-integrated system prompt.
func (m *Manager) CreateWorkerSession(ctx context.Context, name, userPrompt, projectRoot string) (*Session, error) {
	return m.createWorktreeSession(ctx, worktreeOpts{
		Name:        name,
		UserPrompt:  userPrompt,
		ProjectRoot: projectRoot,
		Role:        RoleWorker,
	})
}

