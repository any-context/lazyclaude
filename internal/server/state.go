package server

import (
	"sync"
	"time"
)

// ConnState holds per-connection state.
type ConnState struct {
	PID    int
	Window string
}

// PendingTool stores tool info from PreToolUse hooks with a TTL.
type PendingTool struct {
	ToolName string
	Input    string
	CWD      string
	Expiry   time.Time
}

// diffChoice stores a diff popup choice with TTL.
type diffChoice struct {
	Key    string
	Expiry time.Time
}

// State manages shared server state across connections.
type State struct {
	mu          sync.RWMutex
	connections map[string]*ConnState    // connID -> state
	pidToWindow map[int]string           // pid -> window ID
	pending     map[string][]PendingTool // window -> FIFO queue of pending tool info
	diffChoices map[string]diffChoice    // window -> diff choice (consumed on read)
}

// NewState creates an empty State.
func NewState() *State {
	return &State{
		connections: make(map[string]*ConnState),
		pidToWindow: make(map[int]string),
		pending:     make(map[string][]PendingTool),
		diffChoices: make(map[string]diffChoice),
	}
}

// SetConn registers or updates a connection's state.
func (s *State) SetConn(connID string, cs *ConnState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connections[connID] = cs
	if cs.PID > 0 && cs.Window != "" {
		s.pidToWindow[cs.PID] = cs.Window
	}
}

// GetConn returns a connection's state.
func (s *State) GetConn(connID string) *ConnState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connections[connID]
}

// RemoveConn removes a connection's state.
func (s *State) RemoveConn(connID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cs, ok := s.connections[connID]; ok {
		delete(s.pidToWindow, cs.PID)
	}
	delete(s.connections, connID)
}

// WindowForPID returns the cached window for a PID.
func (s *State) WindowForPID(pid int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pidToWindow[pid]
}

const pendingTTL = 15 * time.Second

// SetPending appends pending tool info to the FIFO queue for a window.
// Multiple tools can be pending concurrently for the same window.
func (s *State) SetPending(window string, tool PendingTool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tool.Expiry = time.Now().Add(pendingTTL)
	s.pending[window] = append(s.pending[window], tool)
}

// SetPendingWithExpiry appends pending tool info with an explicit expiry (for testing).
func (s *State) SetPendingWithExpiry(window string, tool PendingTool, expiry time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tool.Expiry = expiry
	s.pending[window] = append(s.pending[window], tool)
}

// GetPending pops the oldest non-expired pending tool info from the FIFO queue.
func (s *State) GetPending(window string) (PendingTool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	queue := s.pending[window]
	now := time.Now()
	for len(queue) > 0 {
		tool := queue[0]
		queue = queue[1:]
		if now.After(tool.Expiry) {
			continue // skip expired entries
		}
		if len(queue) == 0 {
			delete(s.pending, window)
		} else {
			s.pending[window] = queue
		}
		return tool, true
	}
	delete(s.pending, window)
	return PendingTool{}, false
}

// SetDiffChoice stores a diff popup choice for a window with default TTL.
func (s *State) SetDiffChoice(window, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.diffChoices[window] = diffChoice{
		Key:    key,
		Expiry: time.Now().Add(pendingTTL),
	}
}

// SetDiffChoiceWithExpiry stores a diff choice with explicit expiry (for testing).
func (s *State) SetDiffChoiceWithExpiry(window, key string, expiry time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.diffChoices[window] = diffChoice{Key: key, Expiry: expiry}
}

// GetDiffChoice retrieves and removes a diff choice (if not expired).
func (s *State) GetDiffChoice(window string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dc, ok := s.diffChoices[window]
	if !ok {
		return "", false
	}
	delete(s.diffChoices, window)
	if time.Now().After(dc.Expiry) {
		return "", false
	}
	return dc.Key, true
}

// LastPendingWindow returns the window of the most recently stored pending tool info.
// Used as fallback when a permission_prompt arrives from a different PID than the
// earlier PreToolUse hook, since each hook invocation spawns a new node process.
func (s *State) LastPendingWindow() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest string
	var latestExpiry time.Time
	now := time.Now()
	for w, queue := range s.pending {
		for _, p := range queue {
			if now.After(p.Expiry) {
				continue
			}
			if p.Expiry.After(latestExpiry) {
				latest = w
				latestExpiry = p.Expiry
			}
		}
	}
	return latest
}

// ConnCount returns the number of active connections.
func (s *State) ConnCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.connections)
}