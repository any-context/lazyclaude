package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Tunnel manages an SSH local port forwarding process.
//
// It runs: ssh -L localPort:127.0.0.1:remotePort -N -a -o ServerAliveInterval=15 -o ServerAliveCountMax=3 host
type Tunnel struct {
	host       string
	remotePort int
	localPort  int

	askpassEnv []string // SSH_ASKPASS environment variables

	mu   sync.Mutex
	cmd  *exec.Cmd
	done chan error // closed when the SSH process exits
}

// NewTunnel creates a Tunnel configuration. The tunnel is not started until Start is called.
func NewTunnel(host string, remotePort int) *Tunnel {
	return &Tunnel{
		host:       host,
		remotePort: remotePort,
	}
}

// SetAskpassEnv sets the SSH_ASKPASS environment variables for the tunnel.
// Must be called before Start.
func (t *Tunnel) SetAskpassEnv(env []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.askpassEnv = env
}

// Start launches the SSH tunnel process. It picks a free local port, starts
// the SSH process, and waits until the tunnel is connectable before returning.
// The context controls the lifetime of the SSH process.
func (t *Tunnel) Start(ctx context.Context) error {
	t.mu.Lock()

	if t.cmd != nil {
		t.mu.Unlock()
		return fmt.Errorf("tunnel already started")
	}

	localPort, err := pickFreePort()
	if err != nil {
		t.mu.Unlock()
		return fmt.Errorf("failed to pick free port: %w", err)
	}
	t.localPort = localPort

	forwardSpec := fmt.Sprintf("%d:127.0.0.1:%d", localPort, t.remotePort)
	batchMode := len(t.askpassEnv) == 0
	args := append([]string{"-L", forwardSpec}, baseSSHArgs(t.host, batchMode)...)

	debugLog("Tunnel.Start: host=%q remotePort=%d localPort=%d", t.host, t.remotePort, localPort)
	debugLog("Tunnel.Start: ssh args=%v", args)

	t.cmd = exec.CommandContext(ctx, "ssh", args...)
	if len(t.askpassEnv) > 0 {
		t.cmd.Env = append(os.Environ(), t.askpassEnv...)
	}
	t.done = make(chan error, 1)

	if err := t.cmd.Start(); err != nil {
		t.cmd = nil
		t.done = nil
		t.mu.Unlock()
		return fmt.Errorf("failed to start SSH tunnel: %w", err)
	}

	go func() {
		t.done <- t.cmd.Wait()
		close(t.done)
	}()

	// Capture fields before releasing the lock.
	host := t.host
	done := t.done
	cmd := t.cmd
	t.mu.Unlock()

	// Wait for the tunnel to become connectable (lock-free).
	debugLog("Tunnel.Start: waitForPort localPort=%d", localPort)
	if err := waitForPort(ctx, host, localPort, done); err != nil {
		debugLog("Tunnel.Start: waitForPort failed: %v", err)
		_ = cmd.Process.Kill()
		// Mark the tunnel as failed so it can be retried.
		t.mu.Lock()
		t.cmd = nil
		t.mu.Unlock()
		return err
	}

	return nil
}

// tunnelTimeout is the maximum time to wait for a tunnel to become connectable.
const tunnelTimeout = 10 * time.Second

// tunnelPollInterval is how often to poll the local port during tunnel startup.
const tunnelPollInterval = 100 * time.Millisecond

// waitForPort polls the given local port until a TCP connection succeeds,
// the context is canceled, the deadline expires, or the SSH process exits.
func waitForPort(ctx context.Context, host string, port int, done <-chan error) error {
	ctx, cancel := context.WithTimeout(ctx, tunnelTimeout)
	defer cancel()

	ticker := time.NewTicker(tunnelPollInterval)
	defer ticker.Stop()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("SSH tunnel to %s timed out: %w", host, ctx.Err())
		case err := <-done:
			return fmt.Errorf("SSH tunnel to %s exited before becoming ready: %w", host, err)
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", addr, tunnelPollInterval)
			if err == nil {
				conn.Close()
				debugLog("waitForPort: connected port=%d", port)
				return nil
			}
		}
	}
}

// Stop terminates the SSH tunnel process.
func (t *Tunnel) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}

	if err := t.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("failed to kill tunnel process: %w", err)
	}
	return nil
}

// LocalPort returns the local port the tunnel is forwarding on.
func (t *Tunnel) LocalPort() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.localPort
}

// IsAlive returns true if the tunnel process is still running.
func (t *Tunnel) IsAlive() bool {
	t.mu.Lock()
	done := t.done
	t.mu.Unlock()

	if done == nil {
		return false
	}

	select {
	case <-done:
		return false
	default:
		return true
	}
}

// Wait returns a channel that receives the tunnel process exit error.
// The channel is closed after the error is sent. Returns nil if the
// tunnel has not been started.
func (t *Tunnel) Wait() <-chan error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.done
}

// pickFreePort asks the OS for a free TCP port.
// Note: there is an inherent TOCTOU race between releasing the port here
// and the SSH process binding to it. ExitOnForwardFailure=yes ensures the
// SSH process fails fast if the port is taken.
func pickFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close() // port already captured; close error is not actionable
	return port, nil
}

// NewTunnelWithPort creates a Tunnel with a fixed local port.
// Used for testing to avoid port allocation.
func NewTunnelWithPort(host string, remotePort, localPort int) *Tunnel {
	return &Tunnel{
		host:       host,
		remotePort: remotePort,
		localPort:  localPort,
	}
}

// SSHArgs returns the SSH command-line arguments the tunnel would use.
// Useful for testing command construction without starting a process.
func (t *Tunnel) SSHArgs() []string {
	batchMode := len(t.askpassEnv) == 0
	forwardSpec := fmt.Sprintf("%d:127.0.0.1:%d", t.localPort, t.remotePort)
	return append([]string{"-L", forwardSpec}, baseSSHArgs(t.host, batchMode)...)
}

// baseSSHArgs returns the common SSH arguments shared by tunnel and
// other SSH-based operations. The returned slice includes -N, keepalive
// options, security options, and the resolved host/port.
// When batchMode is true, BatchMode=yes is included (no interactive auth).
func baseSSHArgs(host string, batchMode bool) []string {
	sshHost, port := SplitHostPort(host)
	args := []string{
		"-N",
		"-a",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ControlMaster=no",
		"-o", "ControlPath=none",
	}
	if batchMode {
		args = append(args, "-o", "BatchMode=yes")
	}
	if port != "" {
		args = append(args, "-p", port)
	}
	return append(args, sshHost)
}
