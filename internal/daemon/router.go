package daemon

import (
	"fmt"
	"sync"
)

// Provider is a session backend that can deliver messages and look up sessions.
// Both LocalProvider and RemoteProvider implement this interface.
type Provider interface {
	// ID returns a unique identifier for this provider (e.g. "local" or hostname).
	ID() string

	// HasSession returns true if this provider manages the given session.
	HasSession(sessionID string) bool

	// DeliverMessage delivers a message to a session managed by this provider.
	DeliverMessage(from, to, msgType, body string) error
}

// MessageRouter routes messages across providers. It maintains a registry
// of all providers (local + remote) and finds the correct one for delivery.
type MessageRouter struct {
	mu        sync.RWMutex
	providers []Provider
}

// NewMessageRouter creates an empty router.
func NewMessageRouter() *MessageRouter {
	return &MessageRouter{}
}

// Register adds a provider to the registry.
func (r *MessageRouter) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = append(r.providers, p)
}

// Unregister removes a provider by ID.
func (r *MessageRouter) Unregister(providerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	filtered := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		if p.ID() != providerID {
			filtered = append(filtered, p)
		}
	}
	r.providers = filtered
}

// Send routes a message to the provider that manages the target session.
func (r *MessageRouter) Send(from, to, msgType, body string) error {
	p, err := r.FindProvider(to)
	if err != nil {
		return err
	}
	return p.DeliverMessage(from, to, msgType, body)
}

// FindProvider returns the provider that manages the given session ID.
func (r *MessageRouter) FindProvider(sessionID string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.providers {
		if p.HasSession(sessionID) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no provider found for session %q", sessionID)
}
