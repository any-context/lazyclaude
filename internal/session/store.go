package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/google/uuid"
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
	Role      Role      `json:"role,omitempty"`

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

// stateFile is the versioned on-disk format for state.json.
const stateVersion = 2

type stateFile struct {
	Version  int       `json:"version"`
	Projects []Project `json:"projects"`
}

// Store manages session persistence to a JSON file.
// Internally organizes sessions into Projects by project root path.
type Store struct {
	mu       sync.RWMutex
	path     string
	projects []Project
}

// NewStore creates a store backed by the given file path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Load reads projects from disk. If the file is legacy format ([]Session),
// it resets to empty (no migration).
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.projects = nil
			return nil
		}
		return fmt.Errorf("read state: %w", err)
	}

	// Try versioned format first
	var sf stateFile
	if err := json.Unmarshal(data, &sf); err == nil && sf.Version == stateVersion {
		s.projects = sf.Projects
		// Expanded is not persisted (json:"-"), default to expanded on load.
		for i := range s.projects {
			s.projects[i].Expanded = true
		}
		return nil
	}

	// Legacy format or unrecognized — reset
	s.projects = nil
	return nil
}

// Save writes projects to disk atomically (write to temp, then rename).
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	sf := stateFile{
		Version:  stateVersion,
		Projects: s.projects,
	}
	data, err := json.MarshalIndent(sf, "", "  ")
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

// Projects returns a deep copy of all projects.
// The returned values do not share pointers with store internals.
func (s *Store) Projects() []Project {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Project, len(s.projects))
	for i, p := range s.projects {
		result[i] = p
		if p.PM != nil {
			pm := *p.PM
			result[i].PM = &pm
		}
		if len(p.Sessions) > 0 {
			sessions := make([]Session, len(p.Sessions))
			copy(sessions, p.Sessions)
			result[i].Sessions = sessions
		}
	}
	return result
}

// All returns a flat copy of all sessions across all projects.
// PM sessions are included.
func (s *Store) All() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Session
	for _, p := range s.projects {
		if p.PM != nil {
			result = append(result, *p.PM)
		}
		result = append(result, p.Sessions...)
	}
	return result
}

// Add inserts a session, auto-creating or finding the parent project.
// PM sessions (Role=RolePM) are stored as Project.PM.
// When projectRoot is non-empty it is used directly instead of inferring
// the project root from sess.Path. This avoids mismatches when the
// worktree path (e.g. from git worktree list on a remote) differs from
// the stored project path (e.g. relative "." or symlink-resolved).
func (s *Store) Add(sess Session, projectRoot string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	projectPath := projectRoot
	if projectPath == "" {
		projectPath = InferProjectRoot(sess.Path)
	}
	idx := s.findProjectIdxLocked(projectPath, sess.Host)

	if idx < 0 {
		// Create new project
		p := Project{
			ID:        uuid.New().String(),
			Name:      filepath.Base(projectPath),
			Path:      projectPath,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Expanded:  true,
		}
		if sess.Role == RolePM {
			p.PM = &sess
		} else {
			p.Sessions = []Session{sess}
		}
		s.projects = append(s.projects, p)
		return
	}

	if sess.Role == RolePM {
		s.projects[idx].PM = &sess
	} else {
		s.projects[idx].Sessions = append(s.projects[idx].Sessions, sess)
	}
	s.projects[idx].UpdatedAt = time.Now()
}

// Remove deletes a session by ID. Removes the project if it becomes empty.
func (s *Store) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for pi := range s.projects {
		// Check PM
		if s.projects[pi].PM != nil && s.projects[pi].PM.ID == id {
			s.projects[pi].PM = nil
			s.maybeRemoveProjectLocked(pi)
			return true
		}
		// Check sessions
		for si := range s.projects[pi].Sessions {
			if s.projects[pi].Sessions[si].ID == id {
				sessions := make([]Session, 0, len(s.projects[pi].Sessions)-1)
				sessions = append(sessions, s.projects[pi].Sessions[:si]...)
				sessions = append(sessions, s.projects[pi].Sessions[si+1:]...)
				s.projects[pi].Sessions = sessions
				s.maybeRemoveProjectLocked(pi)
				return true
			}
		}
	}
	return false
}

// FindByID returns a session by ID, searching all projects.
func (s *Store) FindByID(id string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for pi := range s.projects {
		if s.projects[pi].PM != nil && s.projects[pi].PM.ID == id {
			sess := *s.projects[pi].PM
			return &sess
		}
		for si := range s.projects[pi].Sessions {
			if s.projects[pi].Sessions[si].ID == id {
				sess := s.projects[pi].Sessions[si]
				return &sess
			}
		}
	}
	return nil
}

// FindByName returns a session by name, searching all projects.
func (s *Store) FindByName(name string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for pi := range s.projects {
		if s.projects[pi].PM != nil && s.projects[pi].PM.Name == name {
			sess := *s.projects[pi].PM
			return &sess
		}
		for si := range s.projects[pi].Sessions {
			if s.projects[pi].Sessions[si].Name == name {
				sess := s.projects[pi].Sessions[si]
				return &sess
			}
		}
	}
	return nil
}

// FindProjectForSession returns the project that owns the given session ID.
// Returns nil if the session is not found in any project.
func (s *Store) FindProjectForSession(id string) *Project {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := range s.projects {
		if s.projects[i].PM != nil && s.projects[i].PM.ID == id {
			return s.copyProjectLocked(i)
		}
		for si := range s.projects[i].Sessions {
			if s.projects[i].Sessions[si].ID == id {
				return s.copyProjectLocked(i)
			}
		}
	}
	return nil
}

// copyProjectLocked returns a deep copy of the project at index i.
// Caller must hold s.mu (at least RLock).
func (s *Store) copyProjectLocked(i int) *Project {
	p := s.projects[i]
	if p.PM != nil {
		pm := *p.PM
		p.PM = &pm
	}
	if len(p.Sessions) > 0 {
		sessions := make([]Session, len(p.Sessions))
		copy(sessions, p.Sessions)
		p.Sessions = sessions
	}
	return &p
}

// FindProjectByPath returns a project by its root path.
func (s *Store) FindProjectByPath(path string) *Project {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := range s.projects {
		if s.projects[i].Path == path {
			p := s.projects[i]
			return &p
		}
	}
	return nil
}

// Rename changes a session's name.
func (s *Store) Rename(id, newName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for pi := range s.projects {
		if s.projects[pi].PM != nil && s.projects[pi].PM.ID == id {
			s.projects[pi].PM.Name = newName
			s.projects[pi].PM.UpdatedAt = time.Now()
			return true
		}
		for si := range s.projects[pi].Sessions {
			if s.projects[pi].Sessions[si].ID == id {
				s.projects[pi].Sessions[si].Name = newName
				s.projects[pi].Sessions[si].UpdatedAt = time.Now()
				return true
			}
		}
	}
	return false
}

// MarkAllStatus sets the status of all sessions across all projects.
func (s *Store) MarkAllStatus(status Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for pi := range s.projects {
		if s.projects[pi].PM != nil {
			s.projects[pi].PM.Status = status
			s.projects[pi].PM.TmuxWindow = ""
			s.projects[pi].PM.PID = 0
		}
		for si := range s.projects[pi].Sessions {
			s.projects[pi].Sessions[si].Status = status
			s.projects[pi].Sessions[si].TmuxWindow = ""
			s.projects[pi].Sessions[si].PID = 0
		}
	}
}

// GenerateName creates a unique session name from a directory path.
func (s *Store) GenerateName(dirPath string) string {
	base := filepath.Base(dirPath)

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
	s.mutateSessionLocked(id, func(sess *Session) {
		sess.CreatedAt = sess.CreatedAt.Add(-d)
	})
}

// SetTmuxWindow sets the TmuxWindow field of a session by ID.
// Used in tests to inject runtime state without a full tmux sync.
func (s *Store) SetTmuxWindow(id, window string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mutateSessionLocked(id, func(sess *Session) {
		sess.TmuxWindow = window
	})
}

// SetStatus sets the Status field of a session by ID.
// Used in tests to inject runtime state without a full tmux sync.
func (s *Store) SetStatus(id string, status Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mutateSessionLocked(id, func(sess *Session) {
		sess.Status = status
	})
}

// ToggleProjectExpanded toggles the Expanded state of a project by ID.
func (s *Store) ToggleProjectExpanded(projectID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.projects {
		if s.projects[i].ID == projectID {
			s.projects[i].Expanded = !s.projects[i].Expanded
			return
		}
	}
}

func (s *Store) nameExistsLocked(name string) bool {
	for _, p := range s.projects {
		if p.PM != nil && p.PM.Name == name {
			return true
		}
		for _, sess := range p.Sessions {
			if sess.Name == name {
				return true
			}
		}
	}
	return false
}

// findProjectIdxLocked returns the index of the project matching both path and host.
// Host is derived from the project's sessions (PM or first worker).
// An empty host matches projects whose sessions also have empty hosts (local).
func (s *Store) findProjectIdxLocked(path, host string) int {
	for i := range s.projects {
		if s.projects[i].Path != path {
			continue
		}
		if projectHost(&s.projects[i]) == host {
			return i
		}
	}
	return -1
}

// projectHost returns the SSH host of a project by inspecting its sessions.
// Returns "" for local projects.
// Invariant: all sessions in a project share the same host. Mixed-host
// projects are not supported.
func projectHost(p *Project) string {
	if p.PM != nil && p.PM.Host != "" {
		return p.PM.Host
	}
	for _, s := range p.Sessions {
		if s.Host != "" {
			return s.Host
		}
	}
	return ""
}

func (s *Store) maybeRemoveProjectLocked(idx int) {
	p := s.projects[idx]
	if p.PM == nil && len(p.Sessions) == 0 {
		result := make([]Project, 0, len(s.projects)-1)
		result = append(result, s.projects[:idx]...)
		result = append(result, s.projects[idx+1:]...)
		s.projects = result
	}
}

// mutateSessionLocked finds a session by ID across all projects and applies fn.
// Caller must hold s.mu.
func (s *Store) mutateSessionLocked(id string, fn func(*Session)) {
	for pi := range s.projects {
		if s.projects[pi].PM != nil && s.projects[pi].PM.ID == id {
			fn(s.projects[pi].PM)
			return
		}
		for si := range s.projects[pi].Sessions {
			if s.projects[pi].Sessions[si].ID == id {
				fn(&s.projects[pi].Sessions[si])
				return
			}
		}
	}
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
		if existing, ok := paneByWindow[p.Window]; ok {
			// When multiple panes exist in a window (e.g. remain-on-exit keeps
			// dead panes), prefer the alive one to avoid false StatusDead.
			if existing.Dead && !p.Dead {
				paneByWindow[p.Window] = p
			}
			// If both are alive (or both are dead), keep the first-seen pane.
			continue
		}
		paneByWindow[p.Window] = p
	}

	syncSession := func(sess *Session) {
		wName := sess.WindowName()
		w, found := windowByName[wName]
		if !found {
			sess.Status = StatusOrphan
			sess.TmuxWindow = ""
			sess.PID = 0
			return
		}

		sess.TmuxWindow = w.ID
		p, hasPane := paneByWindow[w.ID]
		if !hasPane {
			sess.Status = StatusDetached
			sess.PID = 0
			return
		}

		if p.Dead {
			sess.Status = StatusDead
			sess.PID = 0
		} else if p.PID > 0 {
			sess.Status = StatusRunning
			sess.PID = p.PID
		} else {
			sess.Status = StatusDetached
			sess.PID = 0
		}
	}

	for pi := range s.projects {
		if s.projects[pi].PM != nil {
			syncSession(s.projects[pi].PM)
		}
		for si := range s.projects[pi].Sessions {
			syncSession(&s.projects[pi].Sessions[si])
		}
	}
}
