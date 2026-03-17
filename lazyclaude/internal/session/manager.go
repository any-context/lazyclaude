package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

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
}

// NewManager creates a session manager.
func NewManager(store *Store, tmuxClient tmux.Client, paths config.Paths) *Manager {
	return &Manager{
		store: store,
		tmux:  tmuxClient,
		paths: paths,
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
func (m *Manager) Sync(ctx context.Context) error {
	exists, err := m.tmux.HasSession(ctx, tmuxSessionName)
	if err != nil {
		return fmt.Errorf("check session: %w", err)
	}
	if !exists {
		// No tmux session — all sessions are orphans
		sessions := m.store.All()
		for i := range sessions {
			sessions[i].Status = StatusOrphan
		}
		return nil
	}

	windows, err := m.tmux.ListWindows(ctx, tmuxSessionName)
	if err != nil {
		return fmt.Errorf("list windows: %w", err)
	}

	panes, err := m.tmux.ListPanes(ctx, tmuxSessionName)
	if err != nil {
		return fmt.Errorf("list panes: %w", err)
	}

	m.store.SyncWithTmux(windows, panes)
	return nil
}

// Create creates a new session with a tmux window.
func (m *Manager) Create(ctx context.Context, dirPath, host string) (*Session, error) {
	name := m.store.GenerateName(dirPath, host)
	id := uuid.New().String()

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

	if !exists {
		err = m.tmux.NewSession(ctx, tmux.NewSessionOpts{
			Name:       tmuxSessionName,
			WindowName: windowName,
			Command:    claudeCmd,
			Detached:   true,
			Env: map[string]string{
				"CLAUDE_CODE_AUTO_CONNECT_IDE": "true",
			},
			PostCommands: cleanSessionCommands(),
		})
	} else {
		err = m.tmux.NewWindow(ctx, tmux.NewWindowOpts{
			Session: tmuxSessionName,
			Name:    windowName,
			Command: claudeCmd,
			Env: map[string]string{
				"CLAUDE_CODE_AUTO_CONNECT_IDE": "true",
			},
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

// Delete removes a session and kills its tmux window.
func (m *Manager) Delete(ctx context.Context, id string) error {
	sess := m.store.FindByID(id)
	if sess == nil {
		return fmt.Errorf("session not found: %s", id)
	}

	// Kill tmux window if it exists (best-effort, window may already be gone)
	if sess.TmuxWindow != "" {
		_ = m.tmux.KillWindow(ctx, sess.TmuxWindow)
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

func (m *Manager) buildClaudeCommand(sess Session) string {
	cmd := "claude"
	for _, f := range sess.Flags {
		cmd += " " + shellQuote(f)
	}
	absPath, err := filepath.Abs(sess.Path)
	if err == nil {
		cmd = fmt.Sprintf("cd %s && %s", shellQuote(absPath), cmd)
	}
	return cmd
}

// cleanSessionCommands returns tmux commands to disable status bar and all keybindings.
// These are chained after new-session via ";".
func cleanSessionCommands() [][]string {
	return [][]string{
		{"set-option", "status", "off"},
		{"set-option", "prefix", "None"},
		{"unbind-key", "-a", "-T", "prefix"},
		{"unbind-key", "-a", "-T", "root"},
		{"unbind-key", "-a", "-T", "copy-mode"},
	}
}

// shellQuote wraps a string in single quotes for safe shell interpolation.
func shellQuote(s string) string {
	// Replace single quotes with '\'' (end quote, escaped quote, start quote)
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return "'" + escaped + "'"
}
