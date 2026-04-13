package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
func (m *Manager) Create(ctx context.Context, dirPath string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := m.store.GenerateName(dirPath)
	id := uuid.New().String()
	m.log.Info("create.start", "name", name, "id", id[:8], "path", dirPath)

	sess := Session{
		ID:        id,
		Name:      name,
		Path:      dirPath,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	claudeCmd := m.buildClaudeCommand(sess)

	absPath, err := filepath.Abs(sess.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve path %q: %w", sess.Path, err)
	}

	env := claudeEnv(id)
	return m.launchSession(ctx, sess, claudeCmd, absPath, "", env)
}

// worktreeOpts configures how a worktree session is created.
type worktreeOpts struct {
	Name        string // session/branch name (validated unless SkipGitAdd)
	WtPath      string // explicit worktree path (set for ResumeWorktree; empty = derive from projectRoot)
	UserPrompt  string
	ProjectRoot string
	Role        Role // RoleNone for regular worktree, RoleWorker for worker sessions
	SkipGitAdd  bool // true for ResumeWorktree (directory already exists)
}

// createWorktreeSession is the shared implementation for CreateWorktree,
// ResumeWorktree, and CreateWorkerSession.
func (m *Manager) createWorktreeSession(ctx context.Context, opts worktreeOpts) (*Session, error) {
	if !opts.SkipGitAdd {
		if err := ValidateWorktreeName(opts.Name); err != nil {
			return nil, fmt.Errorf("invalid worktree name: %w", err)
		}
	}

	runner := &LocalRunner{}

	if opts.SkipGitAdd && opts.WtPath != "" {
		exists, err := runner.Exists(ctx, opts.WtPath)
		if err != nil {
			return nil, fmt.Errorf("check worktree path: %w", err)
		}
		if !exists {
			return nil, fmt.Errorf("worktree path does not exist: %s", opts.WtPath)
		}
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
		if err := CreateWorktreeWithRunner(ctx, runner, opts.ProjectRoot, wtPath, opts.Name); err != nil {
			return nil, fmt.Errorf("git worktree: %w", err)
		}
	}

	return m.launchWorktreeSession(ctx, opts.Name, wtPath, opts.UserPrompt, opts.ProjectRoot, opts.Role, "", false)
}

// CreateWorktree creates a git worktree and launches Claude Code with an initial prompt.
// The worktree is placed at {projectRoot}/.lazyclaude/worktrees/{name}/.
func (m *Manager) CreateWorktree(ctx context.Context, name, userPrompt, projectRoot string) (*Session, error) {
	return m.createWorktreeSession(ctx, worktreeOpts{
		Name:        name,
		UserPrompt:  userPrompt,
		ProjectRoot: projectRoot,
		Role:        RoleWorker,
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
		Role:        RoleWorker,
		SkipGitAdd:  true,
	})
}

// launchSession creates a tmux window for sess using claudeCmd and persists the
// session to the store. Caller must hold m.mu.
// projectRoot, when non-empty, is passed to store.Add so that the project is
// matched by the caller-supplied root rather than inferred from sess.Path.
func (m *Manager) launchSession(ctx context.Context, sess Session, claudeCmd, startDir, projectRoot string, env map[string]string) (*Session, error) {
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
	m.store.Add(sess, projectRoot)

	if err := m.store.Save(); err != nil {
		return nil, fmt.Errorf("save store: %w", err)
	}

	return &sess, nil
}

// launchWorktreeSession is the shared logic for creating a tmux window
// running Claude Code in a worktree directory. Called by CreateWorktree,
// ResumeWorktree, ResumeSession, and CreateWorkerSession. Caller must hold m.mu.
// When sessionID is non-empty it is reused (resume of a GC'd session);
// otherwise a fresh UUID is generated. When resume is true, the launcher
// script includes --resume so Claude Code resumes an existing conversation.
func (m *Manager) launchWorktreeSession(ctx context.Context, name, wtPath, userPrompt, projectRoot string, role Role, sessionID string, resume bool) (*Session, error) {
	id := sessionID
	if id == "" {
		id = uuid.New().String()
	}
	systemPrompt := BuildWorkerPrompt(ctx, wtPath, projectRoot, id)

	sess := Session{
		ID:        id,
		Name:      name,
		Path:      wtPath,
		Role:      role,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	claudeCmd, startDir, cleanupFn, err := m.buildLaunchCommand(sess, systemPrompt, userPrompt, resume)
	if err != nil {
		return m.launchErrorSession(ctx, sess, err)
	}
	if cleanupFn != nil {
		launchSuccess := false
		defer func() {
			if !launchSuccess {
				cleanupFn()
			}
		}()
		result, launchErr := m.launchSession(ctx, sess, claudeCmd, startDir, projectRoot, claudeEnv(id))
		if launchErr == nil {
			launchSuccess = true
		}
		return result, launchErr
	}
	return m.launchSession(ctx, sess, claudeCmd, startDir, projectRoot, claudeEnv(id))
}

// launchErrorSession creates a tmux window that displays an error message.
// This makes build-time errors visible in the TUI's main pane instead of
// being silently swallowed.
func (m *Manager) launchErrorSession(ctx context.Context, sess Session, buildErr error) (*Session, error) {
	errMsg := fmt.Sprintf("echo 'lazyclaude: session launch failed'; echo; echo '%s'; echo; echo 'Press Enter to close'; read",
		strings.ReplaceAll(buildErr.Error(), "'", "'\\''"))
	abs, _ := filepath.Abs(".")
	result, launchErr := m.launchSession(ctx, sess, errMsg, abs, "", claudeEnv(sess.ID))
	if launchErr != nil {
		return nil, fmt.Errorf("%w (additionally, tmux window creation failed: %v)", buildErr, launchErr)
	}
	return result, nil
}

// buildLaunchCommand builds the tmux command for launching Claude Code
// in a worktree session. Writes a temp launcher script and returns
// the command, start directory, optional cleanup function, and error.
// When resume is true, the script includes --resume to continue an
// existing Claude Code conversation.
func (m *Manager) buildLaunchCommand(sess Session, systemPrompt, userPrompt string, resume bool) (claudeCmd string, startDir string, cleanup func(), err error) {
	launcher, launcherErr := writeWorktreeLauncher(systemPrompt, userPrompt, m.paths.RuntimeDir, sess.ID, resume)
	if launcherErr != nil {
		return "", "", nil, fmt.Errorf("write launcher: %w", launcherErr)
	}
	claudeCmd = fmt.Sprintf("exec \"$SHELL\" -lic 'exec bash %s'", shell.Quote(launcher))
	return claudeCmd, sess.Path, func() { os.Remove(launcher) }, nil
}

// writeWorktreeLauncher writes a shell script that launches claude with
// --append-system-prompt and an optional user prompt as positional argument.
// When resume is true, the script includes --resume so Claude Code resumes
// an existing conversation instead of failing with "Session ID already in use".
// Returns the script path. The script self-deletes after execution.
func writeWorktreeLauncher(systemPrompt, userPrompt, runtimeDir, sessionID string, resume bool) (string, error) {
	f, err := os.CreateTemp("", "lazyclaude-wt-*.sh")
	if err != nil {
		return "", fmt.Errorf("create temp script: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	// Self-delete the launcher script (already read by shell at this point).
	sb.WriteString("rm -f \"$0\"\n")
	sb.WriteString("exec claude")
	if resume {
		sb.WriteString(" --resume")
	}
	sb.WriteString(" --session-id ")
	sb.WriteString(shell.Quote(sessionID))

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
	//
	// TmuxTarget() encapsulates the local (lc-) vs remote mirror (rm-) window
	// distinction so Delete does not need to branch on sess.Host.
	target := sess.TmuxTarget()
	m.log.Info("delete", "name", sess.Name, "id", id[:8], "target", target, "status", sess.Status)
	if sess.Status != StatusOrphan {
		if err := m.tmux.KillWindow(ctx, target); err != nil {
			m.log.Warn("delete.kill_window", "target", target, "err", err)
		}
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

// hasSessionFlag returns true if flags already contain --resume or --session-id,
// indicating that the caller manages session identity explicitly.
func hasSessionFlag(flags []string) bool {
	for _, f := range flags {
		// Exact match for "--session-id" (two-token form) and prefix match for
		// "--session-id=" (single-token --session-id=value form).
		if f == "--resume" || f == "--session-id" || strings.HasPrefix(f, "--session-id=") {
			return true
		}
	}
	return false
}

func (m *Manager) buildClaudeCommand(sess Session) string {
	claudeArgs := "claude"

	// sess.ID is always uuid.New().String() (hex + hyphens only), safe to embed
	// inside the outer 'exec ...' single-quoted context without shell.Quote.
	if !hasSessionFlag(sess.Flags) {
		claudeArgs += " --session-id " + sess.ID
	}

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
		{"set-option", "-g", "allow-rename", "off"},
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
func (m *Manager) CreatePMSession(ctx context.Context, projectRoot string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p := m.store.FindProjectByPath(projectRoot); p != nil && p.PM != nil {
		return nil, fmt.Errorf("pm session already exists for %q", projectRoot)
	}

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
	systemPrompt := BuildPMPrompt(ctx, projectRoot, id, workerList)

	sess := Session{
		ID:        id,
		Name:      "pm",
		Path:      projectRoot,
		Role:      RolePM,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	claudeCmd, startDir, cleanupFn, buildErr := m.buildLaunchCommand(sess, systemPrompt, "", false)
	if buildErr != nil {
		return m.launchErrorSession(ctx, sess, buildErr)
	}

	m.log.Info("createPMSession", "id", id[:8], "path", projectRoot)

	if cleanupFn != nil {
		launchSuccess := false
		defer func() {
			if !launchSuccess {
				cleanupFn()
			}
		}()
		result, launchErr := m.launchSession(ctx, sess, claudeCmd, startDir, projectRoot, claudeEnv(id))
		if launchErr == nil {
			launchSuccess = true
		}
		return result, launchErr
	}
	return m.launchSession(ctx, sess, claudeCmd, startDir, projectRoot, claudeEnv(id))
}

// ResumeSession resumes a session by ID. If the session is still in state.json,
// it cleans up the old entry and re-launches at the same worktree path. If the
// session has been GC'd, it uses the provided worktree name to reconstruct the
// path and launches a new session with the original ID preserved.
//
// Only local worktree/worker sessions can be resumed. Remote sessions and PM
// sessions are rejected because they require different launch semantics.
func (m *Manager) ResumeSession(ctx context.Context, id, prompt, name string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	old := m.store.FindByID(id)
	if old != nil {
		// Reject remote mirror sessions — these must be resumed on the remote host.
		if old.Host != "" {
			return nil, fmt.Errorf("cannot resume remote session %s locally (host=%s)", id, old.Host)
		}
		// Reject PM sessions — PM has different launch semantics.
		if old.Role == RolePM {
			return nil, fmt.Errorf("cannot resume PM session %s via sessions resume (use PM launch instead)", id)
		}

		// Session still in state.json — derive projectRoot and re-launch.
		project := m.store.FindProjectForSession(id)
		projectRoot := ""
		if project != nil {
			projectRoot = project.Path
		}

		// Kill the old tmux window so the new session can reuse the worktree.
		// Orphan windows are skipped: the tmux window may not exist.
		// Running/Detached/Dead sessions all get their window killed because
		// we are about to launch a replacement in the same worktree directory.
		target := old.TmuxTarget()
		if old.Status != StatusOrphan {
			_ = m.tmux.KillWindow(ctx, target)
		}

		// Remove old entry before launching the replacement. If the launch
		// fails we restore the old record so state.json is not left corrupt.
		savedOld := *old
		m.store.Remove(id)

		result, launchErr := m.launchWorktreeSession(ctx, old.Name, old.Path, prompt, projectRoot, old.Role, id, true)
		if launchErr != nil {
			// Restore old record since launch failed.
			m.store.Add(savedOld, projectRoot)
			if err := m.store.Save(); err != nil {
				m.log.Warn("resumeSession.restoreSave", "err", err)
			}
			return nil, launchErr
		}
		return result, nil
	}

	// Fallback: session GC'd but worktree directory may still exist on disk.
	if name == "" {
		return nil, fmt.Errorf("session not found: %s (specify --name for GC'd sessions)", id)
	}

	// Validate worktree name to prevent path traversal.
	if err := ValidateWorktreeName(name); err != nil {
		return nil, fmt.Errorf("invalid worktree name: %w", err)
	}

	// Find the project that owns this worktree by checking which project's
	// worktree directory contains a matching subdirectory.
	projectRoot, err := m.findProjectRootForWorktree(name)
	if err != nil {
		return nil, err
	}

	wtPath := filepath.Join(projectRoot, ".lazyclaude", "worktrees", name)

	// Defense-in-depth: verify the resolved path is inside the expected directory.
	expectedPrefix := filepath.Join(projectRoot, ".lazyclaude", "worktrees") + string(filepath.Separator)
	if !strings.HasPrefix(wtPath, expectedPrefix) {
		return nil, fmt.Errorf("resolved worktree path escapes worktrees directory: %s", wtPath)
	}

	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("worktree directory not found: %s", wtPath)
	}

	return m.launchWorktreeSession(ctx, name, wtPath, prompt, projectRoot, RoleWorker, id, true)
}

// findProjectRootForWorktree searches registered projects for one whose
// worktree directory contains a subdirectory matching name. Returns the
// project root path or an error if no match is found.
func (m *Manager) findProjectRootForWorktree(name string) (string, error) {
	projects := m.store.Projects()
	if len(projects) == 0 {
		return "", fmt.Errorf("no projects registered")
	}
	for _, p := range projects {
		candidate := filepath.Join(p.Path, ".lazyclaude", "worktrees", name)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return p.Path, nil
		}
	}
	return "", fmt.Errorf("no project found containing worktree %q", name)
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

