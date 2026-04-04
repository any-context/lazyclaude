package daemon

import (
	"testing"
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
