package daemon

import (
	"fmt"
	"sync"
)

// --- Small interfaces composing SessionProvider ---

// SessionLister provides read-only access to sessions.
type SessionLister interface {
	HasSession(sessionID string) bool
	Host() string
	Sessions() ([]SessionInfo, error)
}

// SessionMutator provides session create/delete/rename operations.
type SessionMutator interface {
	Create(path string) error
	Delete(id string) error
	Rename(id, newName string) error
	PurgeOrphans() (int, error)
}

// PreviewProvider captures pane content and scrollback.
type PreviewProvider interface {
	CapturePreview(id string, width, height int) (*PreviewResponse, error)
	CaptureScrollback(id string, width, startLine, endLine int) (*ScrollbackResponse, error)
	HistorySize(id string) (int, error)
}

// SessionActioner handles interactive session operations.
type SessionActioner interface {
	SendChoice(window string, choice int) error
	AttachSession(id string) error
	LaunchLazygit(path string) error
}

// WorktreeProvider manages git worktrees.
type WorktreeProvider interface {
	CreateWorktree(name, prompt, projectRoot string) error
	ResumeWorktree(worktreePath, prompt, projectRoot string) error
	ListWorktrees(projectRoot string) ([]WorktreeInfo, error)
}

// RoleSessionProvider creates PM and worker sessions.
type RoleSessionProvider interface {
	CreatePMSession(projectRoot string) error
	CreateWorkerSession(name, prompt, projectRoot string) error
}

// ConnectionAware exposes the daemon connection state.
type ConnectionAware interface {
	ConnectionState() ConnectionState
}

// SessionProvider is the full interface for a session backend.
// Composed of smaller interfaces for testability.
type SessionProvider interface {
	SessionLister
	SessionMutator
	PreviewProvider
	SessionActioner
	WorktreeProvider
	RoleSessionProvider
	ConnectionAware
}

// MessageSender routes messages across providers.
type MessageSender interface {
	Send(from, to, msgType, body string) error
}

// CompositeProvider merges local and remote session providers.
// The TUI interacts with this provider transparently; routing to the correct
// backend is handled internally based on host or session ID.
//
// Concurrency model:
// - c.local is set at construction and never replaced; safe to read without mutex.
// - c.remotes and c.staleCache are protected by c.mu.
type CompositeProvider struct {
	mu      sync.RWMutex
	local   SessionProvider
	remotes map[string]SessionProvider // host -> provider
	router  MessageSender

	// staleCache holds the last known sessions from disconnected remotes.
	staleCache map[string][]SessionInfo
}

// NewCompositeProvider creates a CompositeProvider with the given local backend.
func NewCompositeProvider(local SessionProvider, router MessageSender) *CompositeProvider {
	return &CompositeProvider{
		local:      local,
		remotes:    make(map[string]SessionProvider),
		router:     router,
		staleCache: make(map[string][]SessionInfo),
	}
}

// AddRemote registers a remote provider.
func (c *CompositeProvider) AddRemote(host string, rp SessionProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remotes[host] = rp
}

// RemoveRemote unregisters a remote provider.
func (c *CompositeProvider) RemoveRemote(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.remotes, host)
	delete(c.staleCache, host)
}

// Sessions returns all sessions from local and remote providers merged.
// Disconnected remotes return stale cached data.
func (c *CompositeProvider) Sessions() ([]SessionInfo, error) {
	local, err := c.local.Sessions()
	if err != nil {
		return nil, fmt.Errorf("local sessions: %w", err)
	}
	items := make([]SessionInfo, len(local))
	copy(items, local)

	// Collect remote sessions under read lock.
	c.mu.RLock()
	type cacheUpdate struct {
		host     string
		sessions []SessionInfo
	}
	var updates []cacheUpdate
	for host, rp := range c.remotes {
		if rp.ConnectionState() == Connected {
			remote, rerr := rp.Sessions()
			if rerr == nil {
				items = append(items, remote...)
				updates = append(updates, cacheUpdate{host: host, sessions: remote})
			} else {
				items = append(items, c.staleCache[host]...)
			}
		} else {
			items = append(items, c.staleCache[host]...)
		}
	}
	c.mu.RUnlock()

	// Apply cache updates under write lock.
	if len(updates) > 0 {
		c.mu.Lock()
		for _, u := range updates {
			cached := make([]SessionInfo, len(u.sessions))
			copy(cached, u.sessions)
			c.staleCache[u.host] = cached
		}
		c.mu.Unlock()
	}

	return items, nil
}

// Create creates a session, routing to the provider for the given host.
func (c *CompositeProvider) Create(path, host string) error {
	p, err := c.providerForHost(host)
	if err != nil {
		return err
	}
	return p.Create(path)
}

// Delete deletes a session, routing to the correct provider.
func (c *CompositeProvider) Delete(id string) error {
	p := c.providerForSession(id)
	if p == nil {
		return fmt.Errorf("no provider found for session %q", id)
	}
	return p.Delete(id)
}

// Rename renames a session, routing to the correct provider.
func (c *CompositeProvider) Rename(id, newName string) error {
	p := c.providerForSession(id)
	if p == nil {
		return fmt.Errorf("no provider found for session %q", id)
	}
	return p.Rename(id, newName)
}

// PurgeOrphans purges orphaned sessions from the local provider.
func (c *CompositeProvider) PurgeOrphans() (int, error) {
	return c.local.PurgeOrphans()
}

// CapturePreview captures pane content, routing to the correct provider.
func (c *CompositeProvider) CapturePreview(id string, width, height int) (*PreviewResponse, error) {
	p := c.providerForSession(id)
	if p == nil {
		return nil, fmt.Errorf("no provider found for session %q", id)
	}
	return p.CapturePreview(id, width, height)
}

// CaptureScrollback captures scrollback, routing to the correct provider.
func (c *CompositeProvider) CaptureScrollback(id string, width, startLine, endLine int) (*ScrollbackResponse, error) {
	p := c.providerForSession(id)
	if p == nil {
		return nil, fmt.Errorf("no provider found for session %q", id)
	}
	return p.CaptureScrollback(id, width, startLine, endLine)
}

// HistorySize returns scrollback size, routing to the correct provider.
func (c *CompositeProvider) HistorySize(id string) (int, error) {
	p := c.providerForSession(id)
	if p == nil {
		return 0, fmt.Errorf("no provider found for session %q", id)
	}
	return p.HistorySize(id)
}

// SendChoice sends a permission choice. Tries local first, then remotes.
func (c *CompositeProvider) SendChoice(window string, choice int) error {
	// c.local is immutable after construction; safe without mutex.
	err := c.local.SendChoice(window, choice)
	if err == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, rp := range c.remotes {
		if rp.ConnectionState() == Connected {
			if rerr := rp.SendChoice(window, choice); rerr == nil {
				return nil
			}
		}
	}
	return err
}

// AttachSession attaches to a session, routing to the correct provider.
func (c *CompositeProvider) AttachSession(id string) error {
	p := c.providerForSession(id)
	if p == nil {
		return fmt.Errorf("no provider found for session %q", id)
	}
	return p.AttachSession(id)
}

// LaunchLazygit launches lazygit, routing by host.
func (c *CompositeProvider) LaunchLazygit(path, host string) error {
	p, err := c.providerForHost(host)
	if err != nil {
		return err
	}
	return p.LaunchLazygit(path)
}

// CreateWorktree creates a worktree, routing by host.
func (c *CompositeProvider) CreateWorktree(name, prompt, projectRoot, host string) error {
	p, err := c.providerForHost(host)
	if err != nil {
		return err
	}
	return p.CreateWorktree(name, prompt, projectRoot)
}

// ResumeWorktree resumes a worktree, routing by host.
func (c *CompositeProvider) ResumeWorktree(worktreePath, prompt, projectRoot, host string) error {
	p, err := c.providerForHost(host)
	if err != nil {
		return err
	}
	return p.ResumeWorktree(worktreePath, prompt, projectRoot)
}

// ListWorktrees lists worktrees, routing by host.
func (c *CompositeProvider) ListWorktrees(projectRoot, host string) ([]WorktreeInfo, error) {
	p, err := c.providerForHost(host)
	if err != nil {
		return nil, err
	}
	return p.ListWorktrees(projectRoot)
}

// CreatePMSession creates a PM session, routing by host.
func (c *CompositeProvider) CreatePMSession(projectRoot, host string) error {
	p, err := c.providerForHost(host)
	if err != nil {
		return err
	}
	return p.CreatePMSession(projectRoot)
}

// CreateWorkerSession creates a worker session, routing by host.
func (c *CompositeProvider) CreateWorkerSession(name, prompt, projectRoot, host string) error {
	p, err := c.providerForHost(host)
	if err != nil {
		return err
	}
	return p.CreateWorkerSession(name, prompt, projectRoot)
}

// providerForHost returns the provider for the given host.
// c.local is immutable after construction; safe without mutex for host=="".
func (c *CompositeProvider) providerForHost(host string) (SessionProvider, error) {
	if host == "" {
		return c.local, nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	rp, ok := c.remotes[host]
	if !ok {
		return nil, fmt.Errorf("no remote provider for host %q", host)
	}
	return rp, nil
}

// providerForSession returns the provider that manages the given session.
// c.local is immutable after construction; safe to call HasSession without mutex.
func (c *CompositeProvider) providerForSession(sessionID string) SessionProvider {
	if c.local.HasSession(sessionID) {
		return c.local
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, rp := range c.remotes {
		if rp.HasSession(sessionID) {
			return rp
		}
	}
	return nil
}
