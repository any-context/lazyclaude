package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/any-context/lazyclaude/internal/core/tmux"
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
	if err := waitForPort(ctx, host, localPort, done); err != nil {
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

// SocketTunnel forwards a remote Unix socket to a local Unix socket via SSH.
// This allows direct tmux operations on remote sessions through the forwarded
// socket, avoiding the daemon API for latency-sensitive operations like preview
// capture and key sending.
//
// It runs: ssh -L localSocket:remoteSocket -N -a [...] host
type SocketTunnel struct {
	host       string
	localSock  string // local Unix socket path
	remoteSock string // remote Unix socket path (detected or provided)

	mu     sync.Mutex
	cmd    *exec.Cmd
	done   chan error
	cancel context.CancelFunc // cancels the internal context for the SSH process
}

// NewSocketTunnel creates a SocketTunnel. The remote socket path is detected
// lazily via DetectRemoteSocket if remoteSock is empty.
func NewSocketTunnel(host, localSock, remoteSock string) *SocketTunnel {
	return &SocketTunnel{
		host:       host,
		localSock:  localSock,
		remoteSock: remoteSock,
	}
}

// DetectRemoteSocket queries the remote host for the lazyclaude tmux server's
// socket path using: ssh host tmux -L lazyclaude display -p '#{socket_path}'
func DetectRemoteSocket(ctx context.Context, host string) (string, error) {
	sshHost, port := splitHostPort(host)
	args := []string{"-o", "BatchMode=yes"}
	if port != "" {
		args = append(args, "-p", port)
	}
	args = append(args, sshHost, "tmux", "-L", "lazyclaude", "display", "-p", "#{socket_path}")

	out, err := exec.CommandContext(ctx, "ssh", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("detect remote tmux socket: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	sock := strings.TrimSpace(string(out))
	if sock == "" {
		return "", fmt.Errorf("remote tmux socket path is empty")
	}
	return sock, nil
}

// Start launches the SSH socket forwarding process.
// If remoteSock was not provided at construction, it is detected first.
// The caller-supplied context is used only for the initial remote socket
// detection. The SSH process itself runs under an internal context controlled
// by Stop, so it is not terminated when the caller's context is canceled.
func (st *SocketTunnel) Start(ctx context.Context) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.cmd != nil {
		return fmt.Errorf("socket tunnel already started")
	}

	if st.remoteSock == "" {
		sock, err := DetectRemoteSocket(ctx, st.host)
		if err != nil {
			return err
		}
		st.remoteSock = sock
	}

	// Remove stale local socket file if it exists.
	os.Remove(st.localSock)

	sshHost, port := splitHostPort(st.host)
	forwardSpec := fmt.Sprintf("%s:%s", st.localSock, st.remoteSock)

	args := []string{
		"-L", forwardSpec,
		"-N",
		"-a",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "BatchMode=yes",
		"-o", "StreamLocalBindUnlink=yes",
	}
	if port != "" {
		args = append(args, "-p", port)
	}
	args = append(args, sshHost)

	// Use an internal context so the long-lived SSH process is not killed
	// when the caller's context is canceled. Stop() cancels this context.
	intCtx, intCancel := context.WithCancel(context.Background())
	st.cmd = exec.CommandContext(intCtx, "ssh", args...)
	st.cancel = intCancel
	st.done = make(chan error, 1)

	if err := st.cmd.Start(); err != nil {
		intCancel()
		st.cmd = nil
		st.done = nil
		st.cancel = nil
		return fmt.Errorf("start socket tunnel: %w", err)
	}

	go func() {
		st.done <- st.cmd.Wait()
		close(st.done)
	}()

	return nil
}

// Stop terminates the SSH socket tunnel process and cleans up the local socket.
func (st *SocketTunnel) Stop() error {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.cancel != nil {
		st.cancel()
		st.cancel = nil
	}

	if err := os.Remove(st.localSock); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "warning: remove socket %s: %v\n", st.localSock, err)
	}

	return nil
}

// LocalSocket returns the local Unix socket path.
func (st *SocketTunnel) LocalSocket() string {
	return st.localSock
}

// IsAlive returns true if the tunnel process is still running.
func (st *SocketTunnel) IsAlive() bool {
	st.mu.Lock()
	done := st.done
	st.mu.Unlock()

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
// Returns nil if the tunnel has not been started or has already been cleaned up.
func (st *SocketTunnel) Wait() <-chan error {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.done
}

// TmuxClient returns a tmux.Client that connects through the forwarded socket.
// The returned client uses -S (absolute path) mode since localSock is absolute.
func (st *SocketTunnel) TmuxClient() tmux.Client {
	return tmux.NewExecClientWithSocket(st.localSock)
}

// SSHArgs returns the SSH command-line arguments for testing.
func (st *SocketTunnel) SSHArgs() []string {
	sshHost, port := splitHostPort(st.host)
	forwardSpec := fmt.Sprintf("%s:%s", st.localSock, st.remoteSock)

	args := []string{
		"-L", forwardSpec,
		"-N",
		"-a",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "BatchMode=yes",
		"-o", "StreamLocalBindUnlink=yes",
	}
	if port != "" {
		args = append(args, "-p", port)
	}
	args = append(args, sshHost)
	return args
}

// SocketTunnelLocalPath returns the conventional local socket path for a host.
// The socket lives in /tmp with default permissions. Any local user with
// filesystem access can connect to it and issue tmux commands against the
// remote server. This matches the trust model of the port-forwarding Tunnel
// (also binds to 127.0.0.1) and is acceptable for a developer tool where
// the local machine is trusted.
func SocketTunnelLocalPath(host string) string {
	safe := strings.NewReplacer("@", "-", ":", "-", "/", "-").Replace(host)
	return fmt.Sprintf("/tmp/lazyclaude-tmux-%s.sock", safe)
}
