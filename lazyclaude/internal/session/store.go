package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
)

// Status represents the runtime status of a session.
type Status int

const (
	StatusUnknown  Status = iota // not yet synced with tmux
	StatusDetached               // tmux window exists, Claude not running
	StatusRunning                // Claude Code is running
	StatusDead                   // pane is dead
	StatusOrphan                 // in state but tmux window gone
)

func (s Status) String() string {
	switch s {
	case StatusUnknown:
		return "Unknown"
	case StatusDetached:
		return "Detached"
	case StatusRunning:
		return "Running"
	case StatusDead:
		return "Dead"
	case StatusOrphan:
		return "Orphan"
	default:
		return "Unknown"
	}
}

// Session represents a lazyclaude session.
type Session struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Host      string    `json:"host,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Flags     []string  `json:"flags,omitempty"`

	// Runtime state (not persisted)
	TmuxWindow string `json:"-"`
	Status     Status `json:"-"`
	PID        int    `json:"-"`
}

// WindowName returns the tmux window name for this session.
// Uses first 8 chars of ID with "lc-" prefix.
func (s *Session) WindowName() string {
	if len(s.ID) < 8 {
		return "lc-" + s.ID
	}
	return "lc-" + s.ID[:8]
}

// Store manages session persistence to a JSON file.
type Store struct {
	mu       sync.RWMutex
	path     string
	sessions []Session
}

// NewStore creates a store backed by the given file path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Load reads sessions from disk.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.sessions = nil
			return nil
		}
		return fmt.Errorf("read state: %w", err)
	}

	var sessions []Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		return fmt.Errorf("parse state: %w", err)
	}
	s.sessions = sessions
	return nil
}

// Save writes sessions to disk atomically (write to temp, then rename).
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	data, err := json.MarshalIndent(s.sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}

// All returns a copy of all sessions.
func (s *Store) All() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Session, len(s.sessions))
	copy(result, s.sessions)
	return result
}

// Add inserts a new session.
func (s *Store) Add(sess Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = append(s.sessions, sess)
}

// Remove deletes a session by ID.
func (s *Store) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, sess := range s.sessions {
		if sess.ID == id {
			result := make([]Session, 0, len(s.sessions)-1)
			result = append(result, s.sessions[:i]...)
			result = append(result, s.sessions[i+1:]...)
			s.sessions = result
			return true
		}
	}
	return false
}

// FindByID returns a session by ID.
func (s *Store) FindByID(id string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := range s.sessions {
		if s.sessions[i].ID == id {
			sess := s.sessions[i]
			return &sess
		}
	}
	return nil
}

// FindByName returns a session by name.
func (s *Store) FindByName(name string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := range s.sessions {
		if s.sessions[i].Name == name {
			sess := s.sessions[i]
			return &sess
		}
	}
	return nil
}

// Rename changes a session's name.
func (s *Store) Rename(id, newName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.sessions {
		if s.sessions[i].ID == id {
			s.sessions[i].Name = newName
			s.sessions[i].UpdatedAt = time.Now()
			return true
		}
	}
	return false
}

// MarkAllStatus sets the status of all sessions.
func (s *Store) MarkAllStatus(status Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sessions {
		s.sessions[i].Status = status
		s.sessions[i].TmuxWindow = ""
		s.sessions[i].PID = 0
	}
}

// GenerateName creates a unique session name from a directory path.
func (s *Store) GenerateName(dirPath, host string) string {
	base := filepath.Base(dirPath)
	if host != "" {
		base = host + ":" + base
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	candidate := base
	suffix := 2
	for s.nameExistsLocked(candidate) {
		candidate = fmt.Sprintf("%s-%d", base, suffix)
		suffix++
	}
	return candidate
}

// BackdateForTest moves a session's CreatedAt backwards by the given duration.
// Only for testing — not for production use.
func (s *Store) BackdateForTest(id string, d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			s.sessions[i].CreatedAt = s.sessions[i].CreatedAt.Add(-d)
			return
		}
	}
}

func (s *Store) nameExistsLocked(name string) bool {
	for _, sess := range s.sessions {
		if sess.Name == name {
			return true
		}
	}
	return false
}

// SyncWithTmux updates runtime state by comparing with tmux windows.
func (s *Store) SyncWithTmux(windows []tmux.WindowInfo, panes []tmux.PaneInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	windowByName := make(map[string]tmux.WindowInfo, len(windows))
	for _, w := range windows {
		windowByName[w.Name] = w
	}

	paneByWindow := make(map[string]tmux.PaneInfo, len(panes))
	for _, p := range panes {
		paneByWindow[p.Window] = p
	}

	for i := range s.sessions {
		wName := s.sessions[i].WindowName()
		w, found := windowByName[wName]
		if !found {
			s.sessions[i].Status = StatusOrphan
			s.sessions[i].TmuxWindow = ""
			s.sessions[i].PID = 0
			continue
		}

		s.sessions[i].TmuxWindow = w.ID
		p, hasPane := paneByWindow[w.ID]
		if !hasPane {
			s.sessions[i].Status = StatusDetached
			s.sessions[i].PID = 0
			continue
		}

		if p.Dead {
			s.sessions[i].Status = StatusDead
			s.sessions[i].PID = 0
		} else if p.PID > 0 {
			s.sessions[i].Status = StatusRunning
			s.sessions[i].PID = p.PID
		} else {
			s.sessions[i].Status = StatusDetached
			s.sessions[i].PID = 0
		}
	}
}
