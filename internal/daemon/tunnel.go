package daemon

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"sync"
)

// Tunnel manages an SSH local port forwarding process.
//
// It runs: ssh -L localPort:127.0.0.1:remotePort -N -a -o ServerAliveInterval=15 -o ServerAliveCountMax=3 host
type Tunnel struct {
	host       string
	remotePort int
	localPort  int

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

// Start launches the SSH tunnel process. It picks a free local port, starts
// the SSH process, and returns once the process has been launched.
// The context controls the lifetime of the SSH process.
func (t *Tunnel) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cmd != nil {
		return fmt.Errorf("tunnel already started")
	}

	localPort, err := pickFreePort()
	if err != nil {
		return fmt.Errorf("failed to pick free port: %w", err)
	}
	t.localPort = localPort

	sshHost, port := splitHostPort(t.host)
	forwardSpec := fmt.Sprintf("%d:127.0.0.1:%d", localPort, t.remotePort)

	args := []string{
		"-L", forwardSpec,
		"-N", // no remote command
		"-a", // disable agent forwarding
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "BatchMode=yes",
	}
	if port != "" {
		args = append(args, "-p", port)
	}
	args = append(args, sshHost)

	t.cmd = exec.CommandContext(ctx, "ssh", args...)
	t.done = make(chan error, 1)

	if err := t.cmd.Start(); err != nil {
		t.cmd = nil
		t.done = nil
		return fmt.Errorf("failed to start SSH tunnel: %w", err)
	}

	go func() {
		t.done <- t.cmd.Wait()
		close(t.done)
	}()

	return nil
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
	sshHost, port := splitHostPort(t.host)
	forwardSpec := fmt.Sprintf("%d:127.0.0.1:%d", t.localPort, t.remotePort)

	args := []string{
		"-L", forwardSpec,
		"-N",
		"-a",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "BatchMode=yes",
	}
	if port != "" {
		args = append(args, "-p", port)
	}
	args = append(args, sshHost)
	return args
}
