package daemon

import "context"

// ConnectionState represents the current state of a connection to a remote daemon.
type ConnectionState int

const (
	// Disconnected is the initial state. No connection exists.
	Disconnected ConnectionState = iota
	// Connecting means a connection attempt is in progress.
	Connecting
	// Connected means the daemon is reachable and the API version matches.
	Connected
	// Reconnecting means the connection was lost and recovery is in progress.
	Reconnecting
	// ConnectionError means all retry attempts have been exhausted.
	ConnectionError
)

// String returns a human-readable label for the connection state.
func (s ConnectionState) String() string {
	switch s {
	case Disconnected:
		return "disconnected"
	case Connecting:
		return "connecting"
	case Connected:
		return "connected"
	case Reconnecting:
		return "reconnecting"
	case ConnectionError:
		return "error"
	default:
		return "unknown"
	}
}

// ConnectionManager manages the lifecycle of a connection to a remote daemon.
// Implementations handle SSH tunnel setup, health checking, and reconnection.
type ConnectionManager interface {
	// Connect establishes a connection to the remote daemon.
	// Returns an error if the connection cannot be established or the API
	// version does not match.
	Connect(ctx context.Context) error

	// Disconnect tears down the connection and releases resources.
	Disconnect() error

	// State returns the current connection state.
	State() ConnectionState

	// Host returns the remote hostname.
	Host() string

	// Client returns the daemon client for API calls.
	// Returns an error if the connection is not in the Connected state.
	Client() (ClientAPI, error)

	// OnStateChange registers a callback invoked when the connection state
	// transitions. The callback receives the new state. Multiple callbacks
	// may be registered; they are invoked in registration order.
	OnStateChange(fn func(ConnectionState))
}
