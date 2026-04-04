package session

import "context"

// Service defines the session management interface.
// Implementations must be safe for concurrent use.
type Service interface {
	// Load reads sessions from persistent storage and syncs with tmux.
	Load(ctx context.Context) error

	// Sync updates runtime state by comparing store with tmux windows.
	Sync(ctx context.Context) error

	// Create creates a new session with a tmux window.
	Create(ctx context.Context, dirPath string) (*Session, error)

	// Delete removes a session and kills its tmux window.
	Delete(ctx context.Context, id string) error

	// Rename changes a session's display name.
	Rename(id, newName string) error

	// PurgeOrphans removes all orphan sessions from the store.
	PurgeOrphans() (int, error)

	// Sessions returns all sessions (read-only copy).
	Sessions() []Session
}

// Compile-time check: Manager implements Service.
var _ Service = (*Manager)(nil)
