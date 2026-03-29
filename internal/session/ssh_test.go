package session

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- writeRemoteScript tests ---

func TestWriteRemoteScript_CreatesFile(t *testing.T) {
	t.Parallel()
	sess := Session{
		ID:   "test1234-abcd-5678",
		Host: "user@remote",
		Path: "/home/user/project",
	}
	path, err := writeRemoteScript(sess, 12345, "test-token")
	require.NoError(t, err)
	defer os.Remove(path)

	assert.FileExists(t, path)
	assert.Contains(t, path, "/lazyclaude/ssh-")
}

func TestWriteRemoteScript_PlainBash(t *testing.T) {
	t.Parallel()
	sess := Session{
		ID:   "test2222-abcd-5678",
		Host: "user@remote",
		Path: "/home/user/project",
	}
	path, err := writeRemoteScript(sess, 12345, "test-token")
	require.NoError(t, err)
	defer os.Remove(path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	script := string(content)

	// Must start with shebang
	assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"))

	// Must NOT contain shell.Quote patterns (no '\'' sequences)
	assert.NotContains(t, script, `'\''`)
}

func TestWriteRemoteScript_HeredocForJSON(t *testing.T) {
	t.Parallel()
	sess := Session{
		ID:   "test3333-abcd-5678",
		Host: "user@remote",
		Path: "/home/user/project",
	}
	path, err := writeRemoteScript(sess, 12345, "my-secret-token")
	require.NoError(t, err)
	defer os.Remove(path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	script := string(content)

	// Lock file JSON should use heredoc
	assert.Contains(t, script, "LOCKEOF")
	assert.Contains(t, script, `"authToken":"my-secret-token"`)
	assert.Contains(t, script, `"transport":"ws"`)
}

func TestWriteRemoteScript_CdToPath(t *testing.T) {
	t.Parallel()
	sess := Session{
		ID:   "test4444-abcd-5678",
		Host: "user@remote",
		Path: "/home/user/my project",
	}
	path, err := writeRemoteScript(sess, 12345, "tok")
	require.NoError(t, err)
	defer os.Remove(path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	script := string(content)

	// cd should use double quotes for $HOME safety and spaces
	assert.Contains(t, script, `cd "/home/user/my project"`)
}

func TestWriteRemoteScript_NoCdForDot(t *testing.T) {
	t.Parallel()
	sess := Session{
		ID:   "test5555-abcd-5678",
		Host: "user@remote",
		Path: ".",
	}
	path, err := writeRemoteScript(sess, 12345, "tok")
	require.NoError(t, err)
	defer os.Remove(path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(content), "cd ")
}

func TestWriteRemoteScript_LockFileCleanup(t *testing.T) {
	t.Parallel()
	sess := Session{
		ID:   "test6666-abcd-5678",
		Host: "user@remote",
		Path: "/home",
	}
	path, err := writeRemoteScript(sess, 9999, "tok")
	require.NoError(t, err)
	defer os.Remove(path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	script := string(content)

	assert.Contains(t, script, `trap`)
	assert.Contains(t, script, `9999.lock`)
}

func TestWriteRemoteScript_ClaudeFlags(t *testing.T) {
	t.Parallel()
	sess := Session{
		ID:    "test7777-abcd-5678",
		Host:  "user@remote",
		Path:  "/home",
		Flags: []string{"--resume", "--working-dir=/tmp"},
	}
	path, err := writeRemoteScript(sess, 5555, "tok")
	require.NoError(t, err)
	defer os.Remove(path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	script := string(content)

	assert.Contains(t, script, "--resume")
	assert.Contains(t, script, "--working-dir=/tmp")
}

// --- buildSSHCommand tests ---

func TestBuildSSHCommand_Basic(t *testing.T) {
	t.Parallel()
	sess := Session{
		ID:   "testbasic-abcd-5678",
		Host: "user@remote-server",
		Path: "/home/user/project",
	}
	cmd, err := buildSSHCommand(sess, 12345, "test-token-abc")
	require.NoError(t, err)

	assert.Contains(t, cmd, "ssh -t")
	assert.Contains(t, cmd, "user@remote-server")
	assert.Contains(t, cmd, "-R 12345:127.0.0.1:12345")
	assert.Contains(t, cmd, "base64 -d")
	assert.Contains(t, cmd, "eval")
}

func TestBuildSSHCommand_ReverseTunnel(t *testing.T) {
	t.Parallel()
	sess := Session{
		ID:   "testtunnel-abcd-5678",
		Host: "dev@10.0.1.5",
		Path: "/workspace",
	}
	cmd, err := buildSSHCommand(sess, 9876, "tok-xyz")
	require.NoError(t, err)

	assert.Contains(t, cmd, "-R 9876:127.0.0.1:9876")
}

func TestBuildSSHCommand_HostWithPort(t *testing.T) {
	t.Parallel()
	sess := Session{
		ID:   "testport-abcd-5678",
		Host: "user@host:2222",
		Path: "/home",
	}
	cmd, err := buildSSHCommand(sess, 5555, "tok")
	require.NoError(t, err)

	assert.Contains(t, cmd, "-p 2222")
	assert.Contains(t, cmd, "user@host")
	assert.NotContains(t, cmd, "host:2222")
}

func TestBuildSSHCommand_NoNestedQuotes(t *testing.T) {
	t.Parallel()
	sess := Session{
		ID:   "testnoquote-abcd-5678",
		Host: "user@host",
		Path: "/home/user/my project",
	}
	cmd, err := buildSSHCommand(sess, 5555, "tok")
	require.NoError(t, err)

	// Command should NOT have nested quote escaping
	assert.NotContains(t, cmd, `'\''`)
	assert.NotContains(t, cmd, `\"`)
	// Should use base64 eval pattern
	assert.Contains(t, cmd, "eval")
	assert.Contains(t, cmd, "base64 -d")
}

func TestBuildSSHCommand_KeepAlive(t *testing.T) {
	t.Parallel()
	sess := Session{
		ID:   "testkeepalive-abcd-5678",
		Host: "user@host",
		Path: "/home",
	}
	cmd, err := buildSSHCommand(sess, 5555, "tok")
	require.NoError(t, err)

	assert.Contains(t, cmd, "ServerAliveInterval")
	assert.Contains(t, cmd, "ServerAliveCountMax")
}

// --- splitHostPort tests (unchanged) ---

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
