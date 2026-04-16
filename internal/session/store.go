package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	Profile   string    `json:"profile,omitempty"`
	ParentID  string    `json:"parent_id,omitempty"`

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

// MirrorWindowName returns the tmux window name for a remote session's
// local mirror. Uses "rm-" prefix to distinguish from local "lc-" windows.
func MirrorWindowName(sessionID string) string {
	if len(sessionID) > 8 {
		sessionID = sessionID[:8]
	}
	return "rm-" + sessionID
}

// TmuxTarget returns the tmux target string for runtime operations
// (attach-session, capture-pane, send-keys, kill-window).
//
// Encapsulates the local/remote distinction in ONE place so that callers
// do not need to branch on sess.Host. Returns a fully-qualified target of
// the form "lazyclaude:<window>" suitable for tmux -L lazyclaude commands
// that require a session:window target (e.g. attach-session).
//
// Resolution order:
//  1. If TmuxWindow is non-empty, use it (may be a tmux window ID "@42"
//     for local, or a mirror window name "rm-xxxx" for remote).
//  2. Otherwise fall back to the canonical window name:
//     - Remote (Host != ""): MirrorWindowName(ID) -> "rm-xxxx"
//     - Local  (Host == ""): WindowName()         -> "lc-xxxx"
//  3. If the resulting target does not contain ':', prefix with
//     "lazyclaude:" so tmux parses it as a session:window target.
func (s *Session) TmuxTarget() string {
	target := s.TmuxWindow
	if target == "" {
		if s.Host != "" {
			target = MirrorWindowName(s.ID)
		} else {
			target = s.WindowName()
		}
	}
	if !strings.Contains(target, ":") {
		target = tmuxSessionName + ":" + target
	}
	return target
}

// stateFile is the versioned on-disk format for state.json.
const stateVersion = 3

type stateFile struct {
	Version  int       `json:"version"`
	Projects []Project `json:"projects"`
}

// stateFileV2 is the v2 on-disk format used before PM sessions were unified
// into the Sessions slice. Used only for migration.
type stateFileV2 struct {
	Version  int         `json:"version"`
	Projects []projectV2 `json:"projects"`
}

// projectV2 is the v2 project layout with a separate PM field.
type projectV2 struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	PM        *Session  `json:"pm,omitempty"`
	Sessions  []Session `json:"sessions,omitempty"`
}

// migrateV2ToV3 converts v2 projects (separate PM field) to v3 format
// (PM merged into Sessions slice). Pure function — does not write to disk.
func migrateV2ToV3(data []byte) ([]Project, error) {
	var sf stateFileV2
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("unmarshal v2 state: %w", err)
	}
	projects := make([]Project, len(sf.Projects))
	for i, p := range sf.Projects {
		var sessions []Session
		if p.PM != nil {
			// Ensure the migrated PM session has RolePM set, even if the
			// v2 file omitted the role field (older writers).
			pm := *p.PM
			if pm.Role == RoleNone {
				pm.Role = RolePM
			}
			sessions = append(sessions, pm)
		}
		sessions = append(sessions, p.Sessions...)
		projects[i] = Project{
			ID:        p.ID,
			Name:      p.Name,
			Path:      p.Path,
			CreatedAt: p.CreatedAt,
			UpdatedAt: p.UpdatedAt,
			Sessions:  sessions,
		}
	}
	return projects, nil
}

// Store manages session persistence to a JSON file.
// Internally organizes sessions into Projects by project root path.
type Store struct {
	mu       sync.RWMutex
	path     string
	projects []Project
	// deleted tracks session IDs explicitly removed by this process.
	// mergeFromDiskLocked skips these so that GC deletes are not undone by the
	// merge, while sessions added by other processes are still preserved.
	deleted map[string]struct{}
}

// NewStore creates a store backed by the given file path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Load reads projects from disk. Migrates v2 format (separate PM field) to
// v3 (PM in Sessions). Legacy v1 format (flat []Session) resets to empty.
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

	// Try current format (v3) first.
	var sf stateFile
	if err := json.Unmarshal(data, &sf); err == nil && sf.Version == stateVersion {
		s.projects = sf.Projects
		for i := range s.projects {
			s.projects[i].Expanded = true
		}
		return nil
	}

	// Try v2 format (separate PM field) and migrate.
	var peek struct{ Version int }
	if err := json.Unmarshal(data, &peek); err == nil && peek.Version == 2 {
		projects, migErr := migrateV2ToV3(data)
		if migErr != nil {
			return fmt.Errorf("migrate v2→v3 state: %w", migErr)
		}
		s.projects = projects
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
// Before writing, merges any sessions present on disk but absent from memory.
// This prevents a stale-snapshot overwrite when multiple processes share the
// same state.json: sessions added by another process are preserved.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.mergeFromDiskLocked()

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

// mergeFromDiskLocked reads the on-disk state and adds any sessions that exist
// on disk but are absent from the in-memory store. Sessions known to memory
// (whether alive or deleted) are left unchanged. Disk-only sessions are added
// so that concurrent processes do not overwrite each other's writes.
//
// Caller must hold s.mu (write lock).
func (s *Store) mergeFromDiskLocked() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return // file missing or unreadable — nothing to merge
	}

	var diskProjects []Project

	var sf stateFile
	if jsonErr := json.Unmarshal(data, &sf); jsonErr == nil && sf.Version == stateVersion {
		diskProjects = sf.Projects
	} else {
		var peek struct{ Version int }
		if jsonErr := json.Unmarshal(data, &peek); jsonErr == nil && peek.Version == 2 {
			if projects, migErr := migrateV2ToV3(data); migErr == nil {
				diskProjects = projects
			}
		}
	}

	if len(diskProjects) == 0 {
		return
	}

	// Build set of session IDs currently in memory.
	memIDs := make(map[string]struct{})
	for _, p := range s.projects {
		for _, sess := range p.Sessions {
			memIDs[sess.ID] = struct{}{}
		}
	}

	// Add disk-only sessions to memory, skipping sessions we explicitly deleted.
	for _, diskProj := range diskProjects {
		for _, diskSess := range diskProj.Sessions {
			if _, known := memIDs[diskSess.ID]; known {
				continue
			}
			if _, tombstoned := s.deleted[diskSess.ID]; tombstoned {
				continue
			}
			s.addSessionLocked(diskSess, diskProj.Path)
		}
	}
}

// addSessionLocked inserts a session into the store without acquiring the lock.
// Caller must hold s.mu (write lock).
func (s *Store) addSessionLocked(sess Session, projectRoot string) {
	projectPath := projectRoot
	if projectPath == "" {
		projectPath = InferProjectRoot(sess.Path)
	}
	idx := s.findProjectIdxLocked(projectPath, sess.Host)

	if idx < 0 {
		p := Project{
			ID:        uuid.New().String(),
			Name:      filepath.Base(projectPath),
			Path:      projectPath,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Expanded:  true,
			Sessions:  []Session{sess},
		}
		s.projects = append(s.projects, p)
		return
	}

	s.projects[idx].Sessions = append(s.projects[idx].Sessions, sess)
	s.projects[idx].UpdatedAt = time.Now()
}

// Projects returns a deep copy of all projects.
// The returned values do not share pointers with store internals.
func (s *Store) Projects() []Project {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Project, len(s.projects))
	for i, p := range s.projects {
		result[i] = p
		if len(p.Sessions) > 0 {
			sessions := make([]Session, len(p.Sessions))
			copy(sessions, p.Sessions)
			result[i].Sessions = sessions
		}
	}
	return result
}

// All returns a flat copy of all sessions across all projects.
// PM sessions are included (they live in Sessions with Role==RolePM).
func (s *Store) All() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Session
	for _, p := range s.projects {
		result = append(result, p.Sessions...)
	}
	return result
}

// Add inserts a session, auto-creating or finding the parent project.
// All sessions (including PM) are stored in Project.Sessions.
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
			Sessions:  []Session{sess},
		}
		s.projects = append(s.projects, p)
		return
	}

	s.projects[idx].Sessions = append(s.projects[idx].Sessions, sess)
	s.projects[idx].UpdatedAt = time.Now()
}

// Remove deletes a session by ID. Removes the project if it becomes empty.
// Records the ID in the deleted set so that mergeFromDiskLocked does not
// re-add the session from disk on the next Save().
func (s *Store) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for pi := range s.projects {
		for si := range s.projects[pi].Sessions {
			if s.projects[pi].Sessions[si].ID == id {
				sessions := make([]Session, 0, len(s.projects[pi].Sessions)-1)
				sessions = append(sessions, s.projects[pi].Sessions[:si]...)
				sessions = append(sessions, s.projects[pi].Sessions[si+1:]...)
				s.projects[pi].Sessions = sessions
				s.maybeRemoveProjectLocked(pi)
				if s.deleted == nil {
					s.deleted = make(map[string]struct{})
				}
				s.deleted[id] = struct{}{}
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
		for si := range s.projects[pi].Sessions {
			if s.projects[pi].Sessions[si].ID == id {
				sess := s.projects[pi].Sessions[si]
				return &sess
			}
		}
	}
	return nil
}

// ChildrenOf returns all sessions whose ParentID matches the given ID.
// Returns copies; modifications do not affect the store.
func (s *Store) ChildrenOf(parentID string) []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Session
	for _, p := range s.projects {
		for _, sess := range p.Sessions {
			if sess.ParentID == parentID {
				result = append(result, sess)
			}
		}
	}
	return result
}

// FindByName returns a session by name, searching all projects.
func (s *Store) FindByName(name string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for pi := range s.projects {
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
	if len(p.Sessions) > 0 {
		sessions := make([]Session, len(p.Sessions))
		copy(sessions, p.Sessions)
		p.Sessions = sessions
	}
	return &p
}

// FindProjectByPath returns a deep copy of the project matching the given root
// path. Uses filepath.Clean for comparison to handle trailing slashes and
// redundant path components. The returned value does not share pointers with
// store internals.
func (s *Store) FindProjectByPath(path string) *Project {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cleanPath := filepath.Clean(path)
	for i := range s.projects {
		if filepath.Clean(s.projects[i].Path) == cleanPath {
			return s.copyProjectLocked(i)
		}
	}
	return nil
}

// Rename changes a session's name.
func (s *Store) Rename(id, newName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for pi := range s.projects {
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
	for _, s := range p.Sessions {
		if s.Host != "" {
			return s.Host
		}
	}
	return ""
}

func (s *Store) maybeRemoveProjectLocked(idx int) {
	if len(s.projects[idx].Sessions) == 0 {
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
		for si := range s.projects[pi].Sessions {
			if s.projects[pi].Sessions[si].ID == id {
				fn(&s.projects[pi].Sessions[si])
				return
			}
		}
	}
}

// UpdateSession applies fn to the session identified by id.
// Thread-safe; acquires the write lock.
func (s *Store) UpdateSession(id string, fn func(*Session)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mutateSessionLocked(id, fn)
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
		// For remote sessions (Host != ""), also check mirror window name (rm-).
		if !found && sess.Host != "" {
			mirrorName := MirrorWindowName(sess.ID)
			w, found = windowByName[mirrorName]
		}
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
		for si := range s.projects[pi].Sessions {
			syncSession(&s.projects[pi].Sessions[si])
		}
	}
}
