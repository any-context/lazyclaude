package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildSSHCommand_Basic(t *testing.T) {
	t.Parallel()
	sess := Session{
		Host: "user@remote-server",
		Path: "/home/user/project",
	}
	cmd := buildSSHCommand(sess, 12345, "test-token-abc")
	assert.Contains(t, cmd, "ssh")
	assert.Contains(t, cmd, "-t")
	assert.Contains(t, cmd, "user@remote-server")
	assert.Contains(t, cmd, "-R")
	assert.Contains(t, cmd, "12345")
	assert.Contains(t, cmd, "claude")
}

func TestBuildSSHCommand_ReverseTunnel(t *testing.T) {
	t.Parallel()
	sess := Session{
		Host: "dev@10.0.1.5",
		Path: "/workspace",
	}
	cmd := buildSSHCommand(sess, 9876, "tok-xyz")
	// Should include reverse tunnel mapping
	assert.Contains(t, cmd, "-R 9876:127.0.0.1:9876")
}

func TestBuildSSHCommand_WritesLockFile(t *testing.T) {
	t.Parallel()
	sess := Session{
		Host: "user@host",
		Path: "/home",
	}
	cmd := buildSSHCommand(sess, 5555, "my-token")
	// The remote command should create lock file for Claude Code auto-connect
	assert.Contains(t, cmd, ".claude/ide")
	assert.Contains(t, cmd, "my-token")
	assert.Contains(t, cmd, "5555")
}

func TestBuildSSHCommand_WithFlags(t *testing.T) {
	t.Parallel()
	sess := Session{
		Host:  "user@host",
		Path:  "/home",
		Flags: []string{"--working-dir=/tmp"},
	}
	cmd := buildSSHCommand(sess, 5555, "tok")
	assert.Contains(t, cmd, "--working-dir=/tmp")
}

func TestBuildSSHCommand_SetsAutoConnect(t *testing.T) {
	t.Parallel()
	sess := Session{
		Host: "user@host",
		Path: "/home",
	}
	cmd := buildSSHCommand(sess, 5555, "tok")
	assert.Contains(t, cmd, "CLAUDE_CODE_AUTO_CONNECT_IDE=true")
}

func TestBuildSSHCommand_KeepAlive(t *testing.T) {
	t.Parallel()
	sess := Session{
		Host: "user@host",
		Path: "/home",
	}
	cmd := buildSSHCommand(sess, 5555, "tok")
	assert.Contains(t, cmd, "ServerAliveInterval")
}

func TestBuildSSHCommand_HostWithPort(t *testing.T) {
	t.Parallel()
	sess := Session{
		Host: "user@host:2222",
		Path: "/home",
	}
	cmd := buildSSHCommand(sess, 5555, "tok")
	assert.Contains(t, cmd, "-p 2222")
	assert.Contains(t, cmd, "user@host")
	assert.NotContains(t, cmd, "host:2222")
}

func TestSplitHostPort(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		wantHost string
		wantPort string
	}{
		{"host", "host", ""},
		{"host:22", "host", "22"},
		{"user@host", "user@host", ""},
		{"user@host:2222", "user@host", "2222"},
		{"10.0.0.1:22", "10.0.0.1", "22"},
		{"user@10.0.0.1:22", "user@10.0.0.1", "22"},
		{"[::1]", "[::1]", ""},
		{"host:", "host:", ""},
		{"user@host:", "user@host:", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			host, port := splitHostPort(tt.input)
			assert.Equal(t, tt.wantHost, host)
			assert.Equal(t, tt.wantPort, port)
		})
	}
}
