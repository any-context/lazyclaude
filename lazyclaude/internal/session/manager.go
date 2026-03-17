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

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
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

	// Ensure tmux session exists
	exists, err := m.tmux.HasSession(ctx, tmuxSessionName)
	if err != nil {
		return nil, fmt.Errorf("check session: %w", err)
	}

	claudeCmd := m.buildClaudeCommand(sess)
	windowName := sess.WindowName()
	absPath, err := filepath.Abs(sess.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve path %q: %w", sess.Path, err)
	}
	m.log.Debug("create.tmux", "exists", exists, "window", windowName, "cmd", claudeCmd, "dir", absPath)

	env := claudeEnv()
	width, height := termSize()
	m.log.Debug("create.termSize", "width", width, "height", height)

	if !exists {
		err = m.tmux.NewSession(ctx, tmux.NewSessionOpts{
			Name:         tmuxSessionName,
			WindowName:   windowName,
			Command:      claudeCmd,
			StartDir:     absPath,
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
			StartDir: absPath,
			Env:      env,
		})
	}
	if err != nil {
		m.log.Error("create.tmux.error", "err", err, "name", name)
		return nil, fmt.Errorf("create tmux window: %w", err)
	}

	m.log.Info("create.done", "name", name, "window", windowName)
	sess.Status = StatusRunning
	m.store.Add(sess)

	if err := m.store.Save(); err != nil {
		return nil, fmt.Errorf("save store: %w", err)
	}

	return &sess, nil
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

func (m *Manager) buildClaudeCommand(sess Session) string {
	cmd := "exec claude"
	for _, f := range sess.Flags {
		cmd += " " + shellQuote(f)
	}
	return cmd
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

// cleanSessionCommands returns tmux commands to disable status bar and all keybindings.
// These are chained after new-session via ";".
// IMPORTANT: automatic-rename and remain-on-exit are set WITHOUT -w flag
// so they become session-level defaults that apply to ALL windows (not just the first).
// With -w they only apply to the current window, causing new windows created via
// NewWindow to get tmux's defaults (automatic-rename=on), which renames windows
// and breaks SyncWithTmux's name-based matching.
func cleanSessionCommands() [][]string {
	return [][]string{
		{"set-option", "status", "off"},
		{"set-option", "prefix", "None"},
		{"set-option", "automatic-rename", "off"},
		{"set-option", "remain-on-exit", "on"},
		{"unbind-key", "-a", "-T", "prefix"},
		{"unbind-key", "-a", "-T", "root"},
		{"unbind-key", "-a", "-T", "copy-mode"},
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

// shellQuote wraps a string in single quotes for safe shell interpolation.
func shellQuote(s string) string {
	// Replace single quotes with '\'' (end quote, escaped quote, start quote)
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return "'" + escaped + "'"
}
