package session

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/any-context/lazyclaude/internal/profile"
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

	profilesMu sync.RWMutex
	profiles   []profile.ProfileDef
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

// SetProfiles installs the effective profile list (as returned by
// profile.Load). Passing a nil or empty slice makes ResolveProfile fall back
// to the built-in default for any resolution request.
func (m *Manager) SetProfiles(profs []profile.ProfileDef) {
	m.profilesMu.Lock()
	defer m.profilesMu.Unlock()
	if len(profs) == 0 {
		m.profiles = nil
		return
	}
	m.profiles = append([]profile.ProfileDef(nil), profs...)
}

// Profiles returns a copy of the currently installed profile list.
func (m *Manager) Profiles() []profile.ProfileDef {
	m.profilesMu.RLock()
	defer m.profilesMu.RUnlock()
	if len(m.profiles) == 0 {
		return nil
	}
	out := make([]profile.ProfileDef, len(m.profiles))
	copy(out, m.profiles)
	return out
}

// ResolveProfile returns the profile to use for a launch request.
//
// An empty name resolves to the effective default: the first profile with
// Default=true, a user-defined profile literally named "default", or the
// built-in default. A non-empty name is looked up by exact match; if the
// name is absent from the installed profile set and is not the reserved
// built-in name, an error is returned so callers surface a user-actionable
// diagnostic.
func (m *Manager) ResolveProfile(name string) (profile.ProfileDef, error) {
	m.profilesMu.RLock()
	defer m.profilesMu.RUnlock()

	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		if len(m.profiles) == 0 {
			return profile.BuiltinDefault(), nil
		}
		def, _ := profile.ResolveDefault(m.profiles)
		return def, nil
	}
	for _, p := range m.profiles {
		if p.Name == trimmed {
			return p, nil
		}
	}
	if trimmed == profile.BuiltinDefaultName {
		return profile.BuiltinDefault(), nil
	}
	return profile.ProfileDef{}, fmt.Errorf("profile %q not defined in %s", trimmed, profileConfigHint())
}

// profileConfigHint returns a human-readable path to config.json for use in
// error messages. Falls back to the literal path string when the home
// directory cannot be resolved.
func profileConfigHint() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".lazyclaude", "config.json")
	}
	return "$HOME/.lazyclaude/config.json"
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
		// Do not mark sessions as Orphan based solely on HasSession returning
		// false. Under high load (e.g. go test -race), HasSession can transiently
		// return false even while windows are alive. Marking all sessions Orphan
		// here causes GC to delete live sessions and wipes state.json.
		// Individual windows are detected as Orphan by SyncWithTmux when
		// HasSession does return true.
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

// Create creates a new plain session with a tmux window running Claude Code.
// Holds the manager mutex throughout to prevent GC from orphaning the new session.
func (m *Manager) Create(ctx context.Context, dirPath string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createLocked(ctx, dirPath, "", nil)
}

// CreateOpts creates a plain session with profile/options support.
func (m *Manager) CreateOpts(ctx context.Context, dirPath, profileName, options string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createLocked(ctx, dirPath, profileName, splitOptions(options))
}

func (m *Manager) createLocked(ctx context.Context, dirPath, profileName string, extraFlags []string) (*Session, error) {
	prof, err := m.ResolveProfile(profileName)
	if err != nil {
		return nil, err
	}
	if err := profile.Validate(prof); err != nil {
		return nil, fmt.Errorf("profile %q: %w", prof.Name, err)
	}
	spec, err := NewLaunchSpec(prof)
	if err != nil {
		return nil, err
	}

	name := m.store.GenerateName(dirPath)
	id := uuid.New().String()
	m.log.Info("create.start", "name", name, "id", id[:8], "path", dirPath, "profile", prof.Name)

	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path %q: %w", dirPath, err)
	}

	sess := Session{
		ID:        id,
		Name:      name,
		Path:      dirPath,
		Profile:   profileNameForPersist(prof),
		Flags:     append([]string(nil), extraFlags...),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	claudeCmd, cleanupFn, err := m.buildClaudeCommand(sess, spec)
	if err != nil {
		return m.launchErrorSession(ctx, sess, err)
	}
	launchSuccess := false
	defer func() {
		if !launchSuccess {
			cleanupFn()
		}
	}()

	env := claudeEnv(id, spec)
	result, launchErr := m.launchSession(ctx, sess, claudeCmd, absPath, "", env)
	if launchErr == nil {
		launchSuccess = true
	}
	return result, launchErr
}

// profileNameForPersist returns the profile name to persist in state.json.
// Built-in defaults are stored as the empty string so that resume uses
// whatever the user's current default is, rather than pinning to "default".
func profileNameForPersist(p profile.ProfileDef) string {
	if p.Builtin {
		return ""
	}
	return p.Name
}

// worktreeOpts configures how a worktree session is created.
type worktreeOpts struct {
	Name        string // session/branch name (validated unless SkipGitAdd)
	WtPath      string // explicit worktree path (set for ResumeWorktree; empty = derive from projectRoot)
	UserPrompt  string
	ProjectRoot string
	Role        Role // RoleNone for regular worktree, RoleWorker for worker sessions
	SkipGitAdd  bool // true for ResumeWorktree (directory already exists)
	Profile     string
	ExtraFlags  []string
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

	return m.launchWorktreeSession(ctx, launchWorktreeArgs{
		Name:        opts.Name,
		WtPath:      wtPath,
		UserPrompt:  opts.UserPrompt,
		ProjectRoot: opts.ProjectRoot,
		Role:        opts.Role,
		Profile:     opts.Profile,
		ExtraFlags:  opts.ExtraFlags,
	})
}

// WorktreeOpts holds arguments for Manager.CreateWorktreeOpts.
type WorktreeOpts struct {
	Name        string
	Prompt      string
	ProjectRoot string
	// Profile selects a profile by name. Empty string resolves to the
	// effective default profile.
	Profile string
	// Options is a space-separated list of extra arguments appended to the
	// claude invocation. Quoted arguments with internal spaces are NOT
	// supported; use profile.Args for that.
	Options string
}

// WorkerOpts holds arguments for Manager.CreateWorkerSessionOpts.
type WorkerOpts struct {
	Name        string
	Prompt      string
	ProjectRoot string
	Profile     string
	// Options is a space-separated list of extra arguments appended to the
	// claude invocation. Quoted arguments with internal spaces are NOT
	// supported; use profile.Args for that.
	Options string
}

// PMOpts holds arguments for Manager.CreatePMSessionOpts.
type PMOpts struct {
	ProjectRoot string
	Profile     string
	// Options is a space-separated list of extra arguments appended to the
	// claude invocation. Quoted arguments with internal spaces are NOT
	// supported; use profile.Args for that.
	Options string
}

// ResumeOpts holds arguments for Manager.ResumeWorktreeOpts.
type ResumeOpts struct {
	WorktreePath string
	Prompt       string
	ProjectRoot  string
	Profile      string
	// Options is a space-separated list of extra arguments appended to the
	// claude invocation. Quoted arguments with internal spaces are NOT
	// supported; use profile.Args for that.
	Options string
}

// CreateWorktreeOpts creates a git worktree and launches Claude Code with an
// initial prompt, using the supplied profile and extra options.
func (m *Manager) CreateWorktreeOpts(ctx context.Context, opts WorktreeOpts) (*Session, error) {
	return m.createWorktreeSession(ctx, worktreeOpts{
		Name:        opts.Name,
		UserPrompt:  opts.Prompt,
		ProjectRoot: opts.ProjectRoot,
		Role:        RoleWorker,
		Profile:     opts.Profile,
		ExtraFlags:  splitOptions(opts.Options),
	})
}

// CreateWorktree creates a git worktree and launches Claude Code with an initial prompt.
// The worktree is placed at {projectRoot}/.lazyclaude/worktrees/{name}/.
//
// Deprecated: Use CreateWorktreeOpts.
func (m *Manager) CreateWorktree(ctx context.Context, name, userPrompt, projectRoot string) (*Session, error) {
	return m.CreateWorktreeOpts(ctx, WorktreeOpts{
		Name:        name,
		Prompt:      userPrompt,
		ProjectRoot: projectRoot,
	})
}

// ResumeWorktreeOpts launches Claude Code in an existing worktree directory,
// using the supplied profile and extra options. Unlike CreateWorktreeOpts, it
// does not run `git worktree add`.
func (m *Manager) ResumeWorktreeOpts(ctx context.Context, opts ResumeOpts) (*Session, error) {
	return m.createWorktreeSession(ctx, worktreeOpts{
		Name:        filepath.Base(opts.WorktreePath),
		WtPath:      opts.WorktreePath,
		UserPrompt:  opts.Prompt,
		ProjectRoot: opts.ProjectRoot,
		Role:        RoleWorker,
		SkipGitAdd:  true,
		Profile:     opts.Profile,
		ExtraFlags:  splitOptions(opts.Options),
	})
}

// ResumeWorktree launches Claude Code in an existing worktree directory.
// Unlike CreateWorktree, it does not run `git worktree add`.
//
// Deprecated: Use ResumeWorktreeOpts.
func (m *Manager) ResumeWorktree(ctx context.Context, worktreePath, userPrompt, projectRoot string) (*Session, error) {
	return m.ResumeWorktreeOpts(ctx, ResumeOpts{
		WorktreePath: worktreePath,
		Prompt:       userPrompt,
		ProjectRoot:  projectRoot,
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

// launchWorktreeArgs bundles the inputs to launchWorktreeSession.
type launchWorktreeArgs struct {
	Name        string
	WtPath      string
	UserPrompt  string
	ProjectRoot string
	Role        Role
	SessionID   string // reused when non-empty (resume of a GC'd session)
	Resume      bool   // emit --resume <id> instead of --session-id <id>
	Profile     string
	ExtraFlags  []string
}

// launchWorktreeSession is the shared logic for creating a tmux window
// running Claude Code in a worktree directory. Called by createWorktreeSession
// and ResumeSession. Caller must hold m.mu. When args.SessionID is non-empty
// it is reused; otherwise a fresh UUID is generated.
func (m *Manager) launchWorktreeSession(ctx context.Context, args launchWorktreeArgs) (*Session, error) {
	id := args.SessionID
	if id == "" {
		id = uuid.New().String()
	}

	prof, err := m.ResolveProfile(args.Profile)
	if err != nil {
		return m.launchErrorSession(ctx, Session{
			ID:        id,
			Name:      args.Name,
			Path:      args.WtPath,
			Role:      args.Role,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, err)
	}
	if err := profile.Validate(prof); err != nil {
		return m.launchErrorSession(ctx, Session{
			ID:        id,
			Name:      args.Name,
			Path:      args.WtPath,
			Role:      args.Role,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, fmt.Errorf("profile %q: %w", prof.Name, err))
	}
	spec, err := NewLaunchSpec(prof)
	if err != nil {
		return m.launchErrorSession(ctx, Session{
			ID:        id,
			Name:      args.Name,
			Path:      args.WtPath,
			Role:      args.Role,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, err)
	}

	systemPrompt := BuildWorkerPrompt(ctx, args.WtPath, args.ProjectRoot, id)

	sess := Session{
		ID:        id,
		Name:      args.Name,
		Path:      args.WtPath,
		Role:      args.Role,
		Profile:   profileNameForPersist(prof),
		Flags:     append([]string(nil), args.ExtraFlags...),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	claudeCmd, startDir, cleanupFn, err := m.buildLaunchCommand(sess, spec, systemPrompt, args.UserPrompt, args.Resume)
	if err != nil {
		return m.launchErrorSession(ctx, sess, err)
	}
	launchSuccess := false
	defer func() {
		if !launchSuccess {
			cleanupFn()
		}
	}()
	result, launchErr := m.launchSession(ctx, sess, claudeCmd, startDir, args.ProjectRoot, claudeEnv(id, spec))
	if launchErr == nil {
		launchSuccess = true
	}
	return result, launchErr
}

// launchErrorSession creates a tmux window that displays an error message.
// This makes build-time errors visible in the TUI's main pane instead of
// being silently swallowed.
func (m *Manager) launchErrorSession(ctx context.Context, sess Session, buildErr error) (*Session, error) {
	errMsg := fmt.Sprintf("echo 'lazyclaude: session launch failed'; echo; echo '%s'; echo; echo 'Press Enter to close'; read",
		strings.ReplaceAll(buildErr.Error(), "'", "'\\''"))
	abs, _ := filepath.Abs(".")
	result, launchErr := m.launchSession(ctx, sess, errMsg, abs, "", claudeEnv(sess.ID, LaunchSpec{}))
	if launchErr != nil {
		return nil, fmt.Errorf("%w (additionally, tmux window creation failed: %v)", buildErr, launchErr)
	}
	return result, nil
}

// buildClaudeCommand builds the tmux command for launching a plain Claude
// Code session via a self-deleting launcher script. Returns the shell command,
// a cleanup function (called on launch failure), and an error.
func (m *Manager) buildClaudeCommand(sess Session, spec LaunchSpec) (string, func(), error) {
	launcher, err := writeLauncher(launcherOpts{
		Spec:       spec,
		SessionID:  sess.ID,
		Resume:     false,
		RuntimeDir: m.paths.RuntimeDir,
		ExtraFlags: sess.Flags,
	})
	if err != nil {
		return "", nil, fmt.Errorf("write launcher: %w", err)
	}
	cmd := fmt.Sprintf("exec \"$SHELL\" -lic 'exec bash %s'", shell.Quote(launcher))
	return cmd, func() { _ = os.Remove(launcher) }, nil
}

// buildLaunchCommand builds the tmux command for launching Claude Code in a
// worktree session. Writes a temp launcher script and returns the command,
// start directory, optional cleanup function, and error.
func (m *Manager) buildLaunchCommand(sess Session, spec LaunchSpec, systemPrompt, userPrompt string, resume bool) (claudeCmd string, startDir string, cleanup func(), err error) {
	launcher, werr := writeLauncher(launcherOpts{
		Spec:         spec,
		SessionID:    sess.ID,
		Resume:       resume,
		RuntimeDir:   m.paths.RuntimeDir,
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		ExtraFlags:   sess.Flags,
	})
	if werr != nil {
		return "", "", nil, fmt.Errorf("write launcher: %w", werr)
	}
	claudeCmd = fmt.Sprintf("exec \"$SHELL\" -lic 'exec bash %s'", shell.Quote(launcher))
	return claudeCmd, sess.Path, func() { _ = os.Remove(launcher) }, nil
}

// launcherOpts drives writeLauncher. SystemPrompt and UserPrompt are
// optional; when empty, the corresponding flags are omitted.
type launcherOpts struct {
	Spec         LaunchSpec
	SessionID    string
	Resume       bool
	RuntimeDir   string
	SystemPrompt string
	UserPrompt   string
	ExtraFlags   []string
}

// writeLauncher writes a self-deleting shell script that exec's the Claude
// Code CLI with the resolved profile command, profile args, session-identity
// flags, optional system/user prompts, and any extra flags. Returns the
// script path. The caller should remove the file on launch failure; on
// success the script rm's itself at the top.
//
// The script lives in /tmp with a `lazyclaude-wt-*.sh` basename (no spaces,
// no quotes) so wrapping the path in the outer `exec "$SHELL" -lic 'exec bash
// <path>'` is safe despite the enclosing single quotes.
func writeLauncher(opts launcherOpts) (string, error) {
	if opts.Spec.Command == "" {
		return "", errors.New("launcher: empty command")
	}
	f, err := os.CreateTemp("", "lazyclaude-wt-*.sh")
	if err != nil {
		return "", fmt.Errorf("create temp script: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	// Self-delete the launcher script (already read by shell at this point).
	sb.WriteString("rm -f \"$0\"\n")
	sb.WriteString("exec ")
	sb.WriteString(shell.Quote(opts.Spec.Command))
	for _, a := range opts.Spec.Args {
		sb.WriteString(" ")
		sb.WriteString(shell.Quote(a))
	}

	// Session identity: inject --session-id or --resume unless the profile
	// or caller has already supplied one. Composite check prevents Claude
	// Code from receiving the flag twice.
	if !hasSessionFlag(opts.Spec.Args, opts.ExtraFlags) {
		if opts.Resume {
			sb.WriteString(" --resume ")
		} else {
			sb.WriteString(" --session-id ")
		}
		sb.WriteString(shell.Quote(opts.SessionID))
	}

	// Inject hooks via --settings file so ~/.claude/settings.json stays
	// untouched. Writing to a file avoids shell quoting issues with nested
	// single quotes in hook commands.
	if settingsFile, werr := config.WriteHooksSettingsFile(opts.RuntimeDir); werr == nil {
		sb.WriteString(" --settings ")
		sb.WriteString(shell.Quote(settingsFile))
	}

	if strings.TrimSpace(opts.SystemPrompt) != "" {
		sb.WriteString(" --append-system-prompt ")
		sb.WriteString(shell.Quote(opts.SystemPrompt))
	}

	for _, fl := range opts.ExtraFlags {
		sb.WriteString(" ")
		sb.WriteString(shell.Quote(fl))
	}

	if strings.TrimSpace(opts.UserPrompt) != "" {
		sb.WriteString(" ")
		sb.WriteString(shell.Quote(opts.UserPrompt))
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

// hasSessionFlag returns true when any of the supplied argument slices
// contains a flag that manages Claude Code session identity (--session-id or
// --resume, both bare and = forms). Callers pass the composite of
// profile.Args and sess.Flags so neither source is allowed to collide with
// lazyclaude's own identity flags.
func hasSessionFlag(argSets ...[]string) bool {
	for _, args := range argSets {
		for _, f := range args {
			if f == "--resume" || f == "--session-id" {
				return true
			}
			if strings.HasPrefix(f, "--session-id=") || strings.HasPrefix(f, "--resume=") {
				return true
			}
		}
	}
	return false
}

// splitOptions converts a space-separated options string into a token slice.
// Quoted arguments are not supported; this is a documented limitation that
// matches the rest of the options-input surface. Empty input returns nil.
func splitOptions(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

// claudeEnv returns environment variables to pass to Claude Code sessions.
// Inherits auth tokens and Claude-specific vars from the parent process.
// Server port/token are NOT injected as env vars — hooks always discover the
// server via lock file scanning so they survive server restarts.
//
// When spec.Env is non-empty, those values are merged last and so override
// any passthrough keys that happen to collide (profile wins). Values in
// spec.Env are already $VAR-expanded by NewLaunchSpec.
func claudeEnv(sessionID string, spec LaunchSpec) map[string]string {
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
	for k, v := range spec.Env {
		env[k] = v
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

// CreatePMSessionOpts creates a PM (Project Manager) session with profile
// and extra-options support.
func (m *Manager) CreatePMSessionOpts(ctx context.Context, opts PMOpts) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p := m.store.FindProjectByPath(opts.ProjectRoot); p != nil && p.PM != nil {
		return nil, fmt.Errorf("pm session already exists for %q", opts.ProjectRoot)
	}

	prof, err := m.ResolveProfile(opts.Profile)
	if err != nil {
		return nil, err
	}
	if err := profile.Validate(prof); err != nil {
		return nil, fmt.Errorf("profile %q: %w", prof.Name, err)
	}
	spec, err := NewLaunchSpec(prof)
	if err != nil {
		return nil, err
	}

	var workerLines []string
	if p := m.store.FindProjectByPath(opts.ProjectRoot); p != nil {
		for _, s := range p.Sessions {
			if s.Role == RoleWorker {
				workerLines = append(workerLines, fmt.Sprintf("- %s (id=%s, path=%s)", s.Name, s.ID, s.Path))
			}
		}
	}
	workerList := strings.Join(workerLines, "\n")

	id := uuid.New().String()
	systemPrompt := BuildPMPrompt(ctx, opts.ProjectRoot, id, workerList)

	sess := Session{
		ID:        id,
		Name:      "pm",
		Path:      opts.ProjectRoot,
		Role:      RolePM,
		Profile:   profileNameForPersist(prof),
		Flags:     splitOptions(opts.Options),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	claudeCmd, startDir, cleanupFn, buildErr := m.buildLaunchCommand(sess, spec, systemPrompt, "", false)
	if buildErr != nil {
		return m.launchErrorSession(ctx, sess, buildErr)
	}

	m.log.Info("createPMSession", "id", id[:8], "path", opts.ProjectRoot, "profile", prof.Name)

	launchSuccess := false
	defer func() {
		if !launchSuccess {
			cleanupFn()
		}
	}()
	result, launchErr := m.launchSession(ctx, sess, claudeCmd, startDir, opts.ProjectRoot, claudeEnv(id, spec))
	if launchErr == nil {
		launchSuccess = true
	}
	return result, launchErr
}

// CreatePMSession creates a PM (Project Manager) session for the given projectRoot.
// Returns an error if a PM session already exists for this projectRoot.
//
// Deprecated: Use CreatePMSessionOpts.
func (m *Manager) CreatePMSession(ctx context.Context, projectRoot string) (*Session, error) {
	return m.CreatePMSessionOpts(ctx, PMOpts{ProjectRoot: projectRoot})
}

// ResumeSession resumes a session by ID. If the session is still in state.json,
// it cleans up the old entry and re-launches at the same worktree path. If the
// session has been GC'd, it uses the provided worktree name to reconstruct the
// path and launches a new session with the original ID preserved.
//
// The session's persisted Profile (state.json) is re-resolved through
// ResolveProfile. If the profile name no longer exists, a clear error is
// returned so the user can restore or rename the profile.
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

		result, launchErr := m.launchWorktreeSession(ctx, launchWorktreeArgs{
			Name:        old.Name,
			WtPath:      old.Path,
			UserPrompt:  prompt,
			ProjectRoot: projectRoot,
			Role:        old.Role,
			SessionID:   id,
			Resume:      true,
			Profile:     old.Profile,
			ExtraFlags:  old.Flags,
		})
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

	return m.launchWorktreeSession(ctx, launchWorktreeArgs{
		Name:        name,
		WtPath:      wtPath,
		UserPrompt:  prompt,
		ProjectRoot: projectRoot,
		Role:        RoleWorker,
		SessionID:   id,
		Resume:      true,
	})
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

// CreateWorkerSessionOpts creates a git worktree and launches Claude Code
// with the Worker role, MCP-integrated system prompt, and the supplied
// profile/options.
func (m *Manager) CreateWorkerSessionOpts(ctx context.Context, opts WorkerOpts) (*Session, error) {
	return m.createWorktreeSession(ctx, worktreeOpts{
		Name:        opts.Name,
		UserPrompt:  opts.Prompt,
		ProjectRoot: opts.ProjectRoot,
		Role:        RoleWorker,
		Profile:     opts.Profile,
		ExtraFlags:  splitOptions(opts.Options),
	})
}

// CreateWorkerSession creates a git worktree and launches Claude Code with the
// Worker role and MCP-integrated system prompt.
//
// Deprecated: Use CreateWorkerSessionOpts.
func (m *Manager) CreateWorkerSession(ctx context.Context, name, userPrompt, projectRoot string) (*Session, error) {
	return m.CreateWorkerSessionOpts(ctx, WorkerOpts{
		Name:        name,
		Prompt:      userPrompt,
		ProjectRoot: projectRoot,
	})
}
