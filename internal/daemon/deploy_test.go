package daemon

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// mockSSH records calls and returns configured responses.
type mockSSH struct {
	runResults map[string]mockResult // command -> result
	copyErr    error
	runCalls   []string
	copyCalls  []copyCall
}

type mockResult struct {
	output []byte
	err    error
}

type copyCall struct {
	host, local, remote string
}

func newMockSSH() *mockSSH {
	return &mockSSH{
		runResults: make(map[string]mockResult),
	}
}

func (m *mockSSH) onRun(cmd string, output string, err error) {
	m.runResults[cmd] = mockResult{output: []byte(output), err: err}
}

func (m *mockSSH) Run(_ context.Context, _, command string) ([]byte, error) {
	m.runCalls = append(m.runCalls, command)
	if r, ok := m.runResults[command]; ok {
		return r.output, r.err
	}
	return nil, fmt.Errorf("unexpected command: %s", command)
}

func (m *mockSSH) Copy(_ context.Context, host, localPath, remotePath string) error {
	m.copyCalls = append(m.copyCalls, copyCall{host, localPath, remotePath})
	return m.copyErr
}

func TestMapArch(t *testing.T) {
	tests := []struct {
		uname   string
		want    string
		wantErr bool
	}{
		{"x86_64", "amd64", false},
		{"aarch64", "arm64", false},
		{"arm64", "arm64", false},
		{"armv7l", "arm", false},
		{"i686", "386", false},
		{"i386", "386", false},
		{"sparc64", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.uname, func(t *testing.T) {
			got, err := mapArch(tt.uname)
			if (err != nil) != tt.wantErr {
				t.Errorf("mapArch(%q) error = %v, wantErr %v", tt.uname, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("mapArch(%q) = %q, want %q", tt.uname, got, tt.want)
			}
		})
	}
}

func TestSplitHostPort(t *testing.T) {
	tests := []struct {
		input    string
		wantHost string
		wantPort string
	}{
		{"host", "host", ""},
		{"host:22", "host", "22"},
		{"user@host", "user@host", ""},
		{"user@host:22", "user@host", "22"},
		{"user@host:2222", "user@host", "2222"},
		{"user@host:", "user@host:", ""},
		{"user@host:abc", "user@host:abc", ""},
		{"[::1]", "[::1]", ""},
		{"[::1]:22", "[::1]", "22"},
		{"[::1]:2222", "[::1]", "2222"},
		{"[::1]:", "[::1]:", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotHost, gotPort := splitHostPort(tt.input)
			if gotHost != tt.wantHost || gotPort != tt.wantPort {
				t.Errorf("splitHostPort(%q) = (%q, %q), want (%q, %q)",
					tt.input, gotHost, gotPort, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestPathContains(t *testing.T) {
	tests := []struct {
		path string
		dir  string
		want bool
	}{
		{"/usr/bin:/usr/local/bin", "/usr/bin", true},
		{"/usr/bin:/usr/local/bin", "/usr/local/bin", true},
		{"/usr/bin:/usr/local/bin", "/home/user/.local/bin", false},
		{"", "/usr/bin", false},
		{"/usr/bin", "/usr/bin", true},
	}
	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			if got := pathContains(tt.path, tt.dir); got != tt.want {
				t.Errorf("pathContains(%q, %q) = %v, want %v", tt.path, tt.dir, got, tt.want)
			}
		})
	}
}

func TestPosixQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "'simple'"},
		{"/home/user/.local/bin", "'/home/user/.local/bin'"},
		{"it's", "'it'\"'\"'s'"},
		{"a b", "'a b'"},
		{"$(cmd)", "'$(cmd)'"},
		{"; rm -rf /", "'; rm -rf /'"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := posixQuote(tt.input); got != tt.want {
				t.Errorf("posixQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"22", true},
		{"2222", true},
		{"", false},
		{"abc", false},
		{"22a", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isNumeric(tt.input); got != tt.want {
				t.Errorf("isNumeric(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDeploy_ArchDetectionFailure(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("uname -m", "", fmt.Errorf("connection refused"))

	_, err := Deploy(context.Background(), DeployConfig{Host: "user@host"}, ssh)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to detect architecture on user@host") {
		t.Errorf("error = %q, want to contain architecture detection message", err)
	}
}

func TestDeploy_UnsupportedArch(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("uname -m", "sparc64\n", nil)

	_, err := Deploy(context.Background(), DeployConfig{Host: "user@host"}, ssh)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unsupported architecture") {
		t.Errorf("error = %q, want to contain unsupported architecture message", err)
	}
}

func TestDeploy_NoTmux(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("uname -m", "x86_64\n", nil)
	ssh.onRun("which tmux", "", fmt.Errorf("not found"))

	_, err := Deploy(context.Background(), DeployConfig{
		Host:       "user@host",
		BinaryPath: "/dev/null", // skip build
	}, ssh)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tmux is not installed on user@host") {
		t.Errorf("error = %q, want to contain tmux not installed message", err)
	}
}

func TestDeploy_MkdirFailure(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("uname -m", "x86_64\n", nil)
	ssh.onRun("which tmux", "/usr/bin/tmux\n", nil)
	ssh.onRun("mkdir -p '~/.local/bin' && test -w '~/.local/bin'", "", fmt.Errorf("permission denied"))

	_, err := Deploy(context.Background(), DeployConfig{
		Host:       "user@host",
		BinaryPath: "/dev/null",
	}, ssh)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cannot write to ~/.local/bin on user@host") {
		t.Errorf("error = %q, want to contain write permission message", err)
	}
}

func TestDeploy_VerifyFailure(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("uname -m", "x86_64\n", nil)
	ssh.onRun("which tmux", "/usr/bin/tmux\n", nil)
	ssh.onRun("mkdir -p '~/.local/bin' && test -w '~/.local/bin'", "", nil)
	ssh.onRun("chmod +x '~/.local/bin/lazyclaude'", "", nil)
	ssh.onRun("'~/.local/bin/lazyclaude' --version", "", fmt.Errorf("exec format error"))

	_, err := Deploy(context.Background(), DeployConfig{
		Host:       "user@host",
		BinaryPath: "/dev/null",
	}, ssh)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "deployed binary failed to start on user@host") {
		t.Errorf("error = %q, want to contain binary start failure message", err)
	}
}

func TestDeploy_Success(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("uname -m", "x86_64\n", nil)
	ssh.onRun("which tmux", "/usr/bin/tmux\n", nil)
	ssh.onRun("mkdir -p '~/.local/bin' && test -w '~/.local/bin'", "", nil)
	ssh.onRun("chmod +x '~/.local/bin/lazyclaude'", "", nil)
	ssh.onRun("'~/.local/bin/lazyclaude' --version", "lazyclaude 0.1.0 (abc1234)\n", nil)
	ssh.onRun("echo $PATH", "/usr/bin:/home/user/.local/bin\n", nil)
	ssh.onRun("echo $HOME", "/home/user\n", nil)

	result, err := Deploy(context.Background(), DeployConfig{
		Host:       "user@host",
		BinaryPath: "/dev/null",
	}, ssh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Arch != "amd64" {
		t.Errorf("Arch = %q, want %q", result.Arch, "amd64")
	}
	if result.RemotePath != "~/.local/bin/lazyclaude" {
		t.Errorf("RemotePath = %q, want %q", result.RemotePath, "~/.local/bin/lazyclaude")
	}
	if result.Version != "lazyclaude 0.1.0 (abc1234)" {
		t.Errorf("Version = %q, want %q", result.Version, "lazyclaude 0.1.0 (abc1234)")
	}
	if result.PathWarning != "" {
		t.Errorf("PathWarning = %q, want empty (PATH includes ~/.local/bin)", result.PathWarning)
	}

	// Verify scp was called.
	if len(ssh.copyCalls) != 1 {
		t.Fatalf("expected 1 copy call, got %d", len(ssh.copyCalls))
	}
	if ssh.copyCalls[0].remote != "~/.local/bin/lazyclaude" {
		t.Errorf("copy remote = %q, want %q", ssh.copyCalls[0].remote, "~/.local/bin/lazyclaude")
	}
}

func TestDeploy_SuccessWithPathWarning(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("uname -m", "x86_64\n", nil)
	ssh.onRun("which tmux", "/usr/bin/tmux\n", nil)
	ssh.onRun("mkdir -p '~/.local/bin' && test -w '~/.local/bin'", "", nil)
	ssh.onRun("chmod +x '~/.local/bin/lazyclaude'", "", nil)
	ssh.onRun("'~/.local/bin/lazyclaude' --version", "lazyclaude 0.1.0 (abc1234)\n", nil)
	ssh.onRun("echo $PATH", "/usr/bin:/usr/local/bin\n", nil)
	ssh.onRun("echo $HOME", "/home/user\n", nil)

	result, err := Deploy(context.Background(), DeployConfig{
		Host:       "user@host",
		BinaryPath: "/dev/null",
	}, ssh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PathWarning == "" {
		t.Error("PathWarning should be non-empty when PATH does not include install dir")
	}
	if !strings.Contains(result.PathWarning, "not in PATH") {
		t.Errorf("PathWarning = %q, want to mention 'not in PATH'", result.PathWarning)
	}
}

func TestDeploy_CustomRemoteDir(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("uname -m", "aarch64\n", nil)
	ssh.onRun("which tmux", "/usr/bin/tmux\n", nil)
	ssh.onRun("mkdir -p '/opt/bin' && test -w '/opt/bin'", "", nil)
	ssh.onRun("chmod +x '/opt/bin/lazyclaude'", "", nil)
	ssh.onRun("'/opt/bin/lazyclaude' --version", "lazyclaude 0.2.0 (def5678)\n", nil)
	ssh.onRun("echo $PATH", "/usr/bin:/opt/bin\n", nil)

	result, err := Deploy(context.Background(), DeployConfig{
		Host:       "user@host",
		BinaryPath: "/dev/null",
		RemoteDir:  "/opt/bin",
	}, ssh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Arch != "arm64" {
		t.Errorf("Arch = %q, want %q", result.Arch, "arm64")
	}
	if result.RemotePath != "/opt/bin/lazyclaude" {
		t.Errorf("RemotePath = %q, want %q", result.RemotePath, "/opt/bin/lazyclaude")
	}
}

func TestDeploy_EmptyHost(t *testing.T) {
	ssh := newMockSSH()
	_, err := Deploy(context.Background(), DeployConfig{}, ssh)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "host is required") {
		t.Errorf("error = %q, want to contain host required message", err)
	}
}

func TestDeploy_BinaryNotFound(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("uname -m", "x86_64\n", nil)

	_, err := Deploy(context.Background(), DeployConfig{
		Host:       "user@host",
		BinaryPath: "/nonexistent/lazyclaude",
	}, ssh)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "binary not found") {
		t.Errorf("error = %q, want to contain binary not found message", err)
	}
}

func TestDeploy_CopyFailure(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("uname -m", "x86_64\n", nil)
	ssh.onRun("which tmux", "/usr/bin/tmux\n", nil)
	ssh.onRun("mkdir -p '~/.local/bin' && test -w '~/.local/bin'", "", nil)
	ssh.copyErr = fmt.Errorf("scp: connection lost")

	_, err := Deploy(context.Background(), DeployConfig{
		Host:       "user@host",
		BinaryPath: "/dev/null",
	}, ssh)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to transfer binary") {
		t.Errorf("error = %q, want to contain transfer failure message", err)
	}
}
