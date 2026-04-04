package daemon

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

// ExponentialBackoff calculates wait durations with exponential growth.
// Not safe for concurrent use; callers must synchronise access.
type ExponentialBackoff struct {
	initial    time.Duration
	max        time.Duration
	factor     float64
	maxRetries int // 0 = unlimited
	attempts   int
}

// NewExponentialBackoff creates a backoff starting at initial, growing by factor
// up to max.
func NewExponentialBackoff(initial, max time.Duration, factor float64) *ExponentialBackoff {
	return &ExponentialBackoff{
		initial: initial,
		max:     max,
		factor:  factor,
	}
}

// WithMaxRetries sets the maximum number of retry attempts.
// Zero means unlimited (the default).
func (b *ExponentialBackoff) WithMaxRetries(n int) *ExponentialBackoff {
	b.maxRetries = n
	return b
}

// Next returns the next backoff duration and increments the attempt counter.
func (b *ExponentialBackoff) Next() time.Duration {
	d := time.Duration(float64(b.initial) * math.Pow(b.factor, float64(b.attempts)))
	if d > b.max {
		d = b.max
	}
	b.attempts++
	return d
}

// Reset resets the attempt counter.
func (b *ExponentialBackoff) Reset() {
	b.attempts = 0
}

// Attempts returns the current attempt count.
func (b *ExponentialBackoff) Attempts() int {
	return b.attempts
}

// Exhausted returns true if maxRetries is set and attempts have reached it.
func (b *ExponentialBackoff) Exhausted() bool {
	return b.maxRetries > 0 && b.attempts >= b.maxRetries
}

// ClientFactory creates a ClientAPI connected to the given address and token.
type ClientFactory func(addr string, token string) ClientAPI

// DefaultMaxRetries is the default number of reconnection attempts before
// transitioning to ConnectionError.
const DefaultMaxRetries = 5

// RemoteConnection implements ConnectionManager for a remote daemon.
type RemoteConnection struct {
	host          string
	lifecycle     *LifecycleManager
	clientFactory ClientFactory

	// connMu serialises Connect/Disconnect calls.
	connMu sync.Mutex

	// mu protects mutable state read by State/Client/OnStateChange.
	mu              sync.RWMutex
	tunnel          *Tunnel
	client          ClientAPI
	state           ConnectionState
	remoteVersion   string // binary version reported by the remote daemon
	callbacks       []func(ConnectionState)
	reconnectHooks  []func() // called after successful reconnection
	backoff         *ExponentialBackoff

	cancel context.CancelFunc // cancels the monitor/reconnect goroutine
}

// NewRemoteConnection creates a RemoteConnection for the given host.
func NewRemoteConnection(host string, lifecycle *LifecycleManager, factory ClientFactory) *RemoteConnection {
	return &RemoteConnection{
		host:          host,
		lifecycle:     lifecycle,
		clientFactory: factory,
		state:         Disconnected,
		backoff:       NewExponentialBackoff(1*time.Second, 30*time.Second, 2).WithMaxRetries(DefaultMaxRetries),
	}
}

// Host returns the remote hostname.
func (rc *RemoteConnection) Host() string {
	return rc.host
}

// OnReconnect registers a callback invoked after a successful reconnection
// (not after the initial Connect). Used to re-subscribe SSE, re-establish
// socket tunnels, etc.
func (rc *RemoteConnection) OnReconnect(fn func()) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.reconnectHooks = append(rc.reconnectHooks, fn)
}

// Connect establishes a connection: discovers or starts the remote daemon,
// opens an SSH tunnel, and verifies the health endpoint.
func (rc *RemoteConnection) Connect(ctx context.Context) error {
	rc.connMu.Lock()
	defer rc.connMu.Unlock()
	return rc.connectLocked(ctx)
}

// connectLocked performs the actual connection. Caller must hold connMu.
func (rc *RemoteConnection) connectLocked(ctx context.Context) error {
	// Cancel any previous monitor goroutine.
	rc.mu.Lock()
	if rc.cancel != nil {
		rc.cancel()
		rc.cancel = nil
	}
	rc.mu.Unlock()

	rc.setState(Connecting)

	info, err := rc.lifecycle.DiscoverRemoteDaemon(ctx, rc.host)
	if err != nil {
		info, err = rc.lifecycle.StartRemoteDaemon(ctx, rc.host)
		if err != nil {
			rc.setState(ConnectionError)
			return fmt.Errorf("connect to %s: %w", rc.host, err)
		}
	}

	if info.Token == "" {
		rc.setState(ConnectionError)
		return fmt.Errorf("daemon on %s returned empty auth token", rc.host)
	}

	tunnel := NewTunnel(rc.host, info.Port)
	if err := tunnel.Start(ctx); err != nil {
		rc.setState(ConnectionError)
		return fmt.Errorf("start tunnel to %s: %w", rc.host, err)
	}

	addr := fmt.Sprintf("http://127.0.0.1:%d", tunnel.LocalPort())
	client := rc.clientFactory(addr, info.Token)

	health, err := client.Health(ctx)
	if err != nil {
		tunnel.Stop()
		rc.setState(ConnectionError)
		return fmt.Errorf("health check on %s: %w", rc.host, err)
	}
	if health.APIVersion != APIVersion {
		tunnel.Stop()
		rc.setState(ConnectionError)
		return fmt.Errorf("API version mismatch on %s: local=%d remote=%d (run lazyclaude deploy)",
			rc.host, APIVersion, health.APIVersion)
	}

	// Derive monitor context from caller's ctx so shutdown propagates.
	monCtx, cancel := context.WithCancel(ctx)

	rc.mu.Lock()
	rc.tunnel = tunnel
	rc.client = client
	rc.remoteVersion = health.BinaryVersion
	rc.backoff.Reset()
	rc.cancel = cancel
	rc.mu.Unlock()

	rc.setState(Connected)

	go rc.monitorTunnel(monCtx)

	return nil
}

// Disconnect tears down the tunnel and releases resources.
func (rc *RemoteConnection) Disconnect() error {
	rc.connMu.Lock()
	defer rc.connMu.Unlock()

	rc.mu.Lock()
	cancel := rc.cancel
	tunnel := rc.tunnel
	rc.cancel = nil
	rc.tunnel = nil
	rc.client = nil
	rc.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if tunnel != nil {
		if err := tunnel.Stop(); err != nil {
			rc.setState(Disconnected)
			return fmt.Errorf("failed to stop tunnel: %w", err)
		}
	}
	rc.setState(Disconnected)
	return nil
}

// State returns the current connection state.
func (rc *RemoteConnection) State() ConnectionState {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.state
}

// RemoteVersion returns the binary version reported by the remote daemon.
// Returns "" if not connected or version was not reported.
func (rc *RemoteConnection) RemoteVersion() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.remoteVersion
}

// Client returns the daemon client. Returns an error if not connected.
func (rc *RemoteConnection) Client() (ClientAPI, error) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	if rc.state != Connected {
		return nil, fmt.Errorf("not connected to %s (state: %s)", rc.host, rc.state)
	}
	return rc.client, nil
}

// OnStateChange registers a callback for state transitions.
func (rc *RemoteConnection) OnStateChange(fn func(ConnectionState)) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.callbacks = append(rc.callbacks, fn)
}

// setState updates the state and invokes callbacks.
func (rc *RemoteConnection) setState(s ConnectionState) {
	rc.mu.Lock()
	if rc.state == s {
		rc.mu.Unlock()
		return
	}
	rc.state = s
	cbs := make([]func(ConnectionState), len(rc.callbacks))
	copy(cbs, rc.callbacks)
	rc.mu.Unlock()

	for _, cb := range cbs {
		cb(s)
	}
}

// monitorTunnel watches for tunnel death and triggers reconnection.
func (rc *RemoteConnection) monitorTunnel(ctx context.Context) {
	rc.mu.RLock()
	tunnel := rc.tunnel
	rc.mu.RUnlock()

	if tunnel == nil {
		return
	}

	waitCh := tunnel.Wait()
	if waitCh == nil {
		return
	}

	select {
	case <-ctx.Done():
		return
	case <-waitCh:
		rc.reconnect(ctx)
	}
}

// reconnect attempts to re-establish the connection with exponential backoff.
// It acquires connMu for each attempt to avoid racing with external Connect/Disconnect.
// When max retries are exhausted, it transitions to ConnectionError.
func (rc *RemoteConnection) reconnect(ctx context.Context) {
	rc.setState(Reconnecting)

	for {
		rc.mu.Lock()
		if rc.backoff.Exhausted() {
			rc.mu.Unlock()
			rc.setState(ConnectionError)
			return
		}
		delay := rc.backoff.Next()
		rc.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		rc.connMu.Lock()
		err := rc.connectLocked(ctx)
		rc.connMu.Unlock()
		if err == nil {
			rc.invokeReconnectHooks()
			return
		}

		rc.mu.RLock()
		state := rc.state
		rc.mu.RUnlock()
		if state != Reconnecting && state != Connecting {
			return
		}
	}
}

// invokeReconnectHooks calls all registered reconnect hooks.
func (rc *RemoteConnection) invokeReconnectHooks() {
	rc.mu.RLock()
	hooks := make([]func(), len(rc.reconnectHooks))
	copy(hooks, rc.reconnectHooks)
	rc.mu.RUnlock()

	for _, fn := range hooks {
		fn()
	}
}

// Compile-time check: RemoteConnection implements ConnectionManager.
var _ ConnectionManager = (*RemoteConnection)(nil)
