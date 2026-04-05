package daemon

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestNewTunnel(t *testing.T) {
	tun := NewTunnel("user@host:22", 8080)
	if tun.host != "user@host:22" {
		t.Errorf("host = %q, want %q", tun.host, "user@host:22")
	}
	if tun.remotePort != 8080 {
		t.Errorf("remotePort = %d, want %d", tun.remotePort, 8080)
	}
}

func TestTunnel_SSHArgs_Basic(t *testing.T) {
	tun := NewTunnelWithPort("user@host", 8080, 9090)
	args := tun.SSHArgs()

	want := []string{
		"-L", "9090:127.0.0.1:8080",
		"-N",
		"-a",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "BatchMode=yes",
		"user@host",
	}

	if len(args) != len(want) {
		t.Fatalf("SSHArgs() len = %d, want %d\nargs: %v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("SSHArgs()[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestTunnel_SSHArgs_WithPort(t *testing.T) {
	tun := NewTunnelWithPort("user@host:2222", 8080, 9090)
	args := tun.SSHArgs()

	want := []string{
		"-L", "9090:127.0.0.1:8080",
		"-N",
		"-a",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "BatchMode=yes",
		"-p", "2222",
		"user@host",
	}

	if len(args) != len(want) {
		t.Fatalf("SSHArgs() len = %d, want %d\nargs: %v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("SSHArgs()[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestTunnel_SSHArgs_IPv6(t *testing.T) {
	tun := NewTunnelWithPort("[::1]:22", 3000, 4000)
	args := tun.SSHArgs()

	// Should have -p 22 and [::1] as the host.
	foundPort := false
	lastArg := args[len(args)-1]
	for i, a := range args {
		if a == "-p" && i+1 < len(args) && args[i+1] == "22" {
			foundPort = true
		}
	}
	if !foundPort {
		t.Errorf("expected -p 22 in args: %v", args)
	}
	if lastArg != "[::1]" {
		t.Errorf("last arg = %q, want %q", lastArg, "[::1]")
	}
}

func TestTunnel_IsAlive_NotStarted(t *testing.T) {
	tun := NewTunnel("user@host", 8080)
	if tun.IsAlive() {
		t.Error("IsAlive() = true for tunnel that was never started")
	}
}

func TestTunnel_Wait_NotStarted(t *testing.T) {
	tun := NewTunnel("user@host", 8080)
	if tun.Wait() != nil {
		t.Error("Wait() should return nil for tunnel that was never started")
	}
}

func TestTunnel_LocalPort(t *testing.T) {
	tun := NewTunnelWithPort("user@host", 8080, 12345)
	if got := tun.LocalPort(); got != 12345 {
		t.Errorf("LocalPort() = %d, want %d", got, 12345)
	}
}

func TestTunnel_Stop_NotStarted(t *testing.T) {
	tun := NewTunnel("user@host", 8080)
	if err := tun.Stop(); err != nil {
		t.Errorf("Stop() on not-started tunnel returned error: %v", err)
	}
}

func TestPickFreePort(t *testing.T) {
	port, err := pickFreePort()
	if err != nil {
		t.Fatalf("pickFreePort() error: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Errorf("pickFreePort() = %d, want valid port number", port)
	}
}

func TestSocketTunnelLocalPath(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"user@host", "/tmp/lazyclaude-tmux-user-host.sock"},
		{"user@host:2222", "/tmp/lazyclaude-tmux-user-host-2222.sock"},
		{"deploy@192.168.1.1", "/tmp/lazyclaude-tmux-deploy-192.168.1.1.sock"},
	}
	for _, tt := range tests {
		got := SocketTunnelLocalPath(tt.host)
		if got != tt.want {
			t.Errorf("SocketTunnelLocalPath(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestSocketTunnel_SSHArgs_Basic(t *testing.T) {
	st := NewSocketTunnel("user@host", "/tmp/local.sock", "/tmp/remote.sock")
	args := st.SSHArgs()

	want := []string{
		"-L", "/tmp/local.sock:/tmp/remote.sock",
		"-N",
		"-a",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "BatchMode=yes",
		"-o", "StreamLocalBindUnlink=yes",
		"user@host",
	}

	if len(args) != len(want) {
		t.Fatalf("SSHArgs() len = %d, want %d\nargs: %v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("SSHArgs()[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestSocketTunnel_SSHArgs_WithPort(t *testing.T) {
	st := NewSocketTunnel("user@host:2222", "/tmp/local.sock", "/tmp/remote.sock")
	args := st.SSHArgs()

	want := []string{
		"-L", "/tmp/local.sock:/tmp/remote.sock",
		"-N",
		"-a",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "BatchMode=yes",
		"-o", "StreamLocalBindUnlink=yes",
		"-p", "2222",
		"user@host",
	}

	if len(args) != len(want) {
		t.Fatalf("SSHArgs() len = %d, want %d\nargs: %v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("SSHArgs()[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestSocketTunnel_IsAlive_NotStarted(t *testing.T) {
	st := NewSocketTunnel("user@host", "/tmp/local.sock", "/tmp/remote.sock")
	if st.IsAlive() {
		t.Error("IsAlive() = true for socket tunnel that was never started")
	}
}

func TestSocketTunnel_Wait_NotStarted(t *testing.T) {
	st := NewSocketTunnel("user@host", "/tmp/local.sock", "/tmp/remote.sock")
	if st.Wait() != nil {
		t.Error("Wait() should return nil for socket tunnel that was never started")
	}
}

func TestSocketTunnel_Stop_NotStarted(t *testing.T) {
	st := NewSocketTunnel("user@host", "/tmp/local.sock", "/tmp/remote.sock")
	if err := st.Stop(); err != nil {
		t.Errorf("Stop() on not-started socket tunnel returned error: %v", err)
	}
}

func TestSocketTunnel_LocalSocket(t *testing.T) {
	st := NewSocketTunnel("user@host", "/tmp/test.sock", "/tmp/remote.sock")
	if got := st.LocalSocket(); got != "/tmp/test.sock" {
		t.Errorf("LocalSocket() = %q, want %q", got, "/tmp/test.sock")
	}
}

func TestSocketTunnel_TmuxClient(t *testing.T) {
	st := NewSocketTunnel("user@host", "/tmp/test.sock", "/tmp/remote.sock")
	client := st.TmuxClient()
	if client == nil {
		t.Fatal("TmuxClient() returned nil")
	}
}

func TestWaitForPort_ImmediateSuccess(t *testing.T) {
	// Start a TCP listener before calling waitForPort.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	tun := NewTunnelWithPort("user@host", 8080, port)
	done := make(chan error, 1)

	if err := tun.waitForPort(port, done); err != nil {
		t.Errorf("waitForPort() returned error: %v", err)
	}
}

func TestWaitForPort_DelayedSuccess(t *testing.T) {
	// Pick a free port but don't listen yet.
	port, err := pickFreePort()
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}

	tun := NewTunnelWithPort("user@host", 8080, port)
	done := make(chan error, 1)

	// Start listener after a short delay.
	go func() {
		time.Sleep(300 * time.Millisecond)
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", itoa(port)))
		if err != nil {
			return
		}
		defer ln.Close()
		// Keep listener alive until test completes.
		time.Sleep(5 * time.Second)
	}()

	if err := tun.waitForPort(port, done); err != nil {
		t.Errorf("waitForPort() returned error: %v", err)
	}
}

func TestWaitForPort_ProcessExit(t *testing.T) {
	port, err := pickFreePort()
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}

	tun := NewTunnelWithPort("user@host", 8080, port)
	done := make(chan error, 1)

	// Simulate SSH process exiting immediately.
	done <- fmt.Errorf("process exited")

	err = tun.waitForPort(port, done)
	if err == nil {
		t.Fatal("waitForPort() should have returned error when process exits")
	}
	if !strings.Contains(err.Error(), "exited before becoming ready") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// itoa converts an int to a string without importing strconv.
func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
