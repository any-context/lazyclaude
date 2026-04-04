package tests_test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func remoteHost(t *testing.T) string {
	t.Helper()
	host := os.Getenv("REMOTE_HOST")
	if host == "" {
		t.Skip("REMOTE_HOST not set, skipping SSH tests")
	}
	return host
}

// sshTunnel starts an SSH reverse tunnel in the background.
func sshTunnel(t *testing.T, host string, remotePort, localPort int) {
	t.Helper()
	cmd := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5",
		"-N",
		"-R", fmt.Sprintf("%d:127.0.0.1:%d", remotePort, localPort),
		host)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		cmd.Process.Signal(syscall.SIGTERM)
		cmd.Wait()
	})
	time.Sleep(1 * time.Second)
}

// writeRemoteLockFile creates the IDE lock file on the remote host via SSH.
// This is what buildSSHCommand/buildRemoteCommand does in production.
func writeRemoteLockFile(t *testing.T, host string, port int, token string) {
	t.Helper()
	lockJSON, _ := json.Marshal(map[string]any{
		"pid":       0,
		"authToken": token,
		"transport": "ws",
	})
	cmd := fmt.Sprintf(
		"mkdir -p ~/.claude/ide && echo '%s' > ~/.claude/ide/%d.lock",
		string(lockJSON), port)
	out, err := exec.Command("ssh", "-o", "BatchMode=yes", host, cmd).CombinedOutput()
	require.NoError(t, err, "write lock file failed: %s", string(out))
	t.Cleanup(func() {
		exec.Command("ssh", "-o", "BatchMode=yes", host,
			fmt.Sprintf("rm -f ~/.claude/ide/%d.lock", port)).Run()
	})
}

// TestE2E_SSH_Connection verifies basic SSH connectivity between containers.
func TestE2E_SSH_Connection(t *testing.T) {
	host := remoteHost(t)

	out, err := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5",
		host, "echo", "hello-from-remote").CombinedOutput()
	require.NoError(t, err, "SSH connection failed: %s", string(out))
	assert.Contains(t, strings.TrimSpace(string(out)), "hello-from-remote")
}

// TestE2E_SSH_ReverseTunnel verifies SSH reverse tunnel works.
func TestE2E_SSH_ReverseTunnel(t *testing.T) {
	host := remoteHost(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("tunnel-ok"))
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	localPort := ln.Addr().(*net.TCPAddr).Port
	go http.Serve(ln, mux)

	tunnelPort := 18900

	sshCmd := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5",
		"-R", fmt.Sprintf("%d:127.0.0.1:%d", tunnelPort, localPort),
		host,
		"curl", "-s", "--connect-timeout", "3",
		fmt.Sprintf("http://127.0.0.1:%d/health", tunnelPort))
	out, err := sshCmd.CombinedOutput()
	require.NoError(t, err, "SSH reverse tunnel failed: %s", string(out))
	assert.Equal(t, "tunnel-ok", strings.TrimSpace(string(out)))
}

// TestE2E_SSH_RemoteClaudeConnect is the key test: it simulates the full
// remote Claude Code connection flow:
//
//	local: MCP server :PORT
//	         ^
//	         | SSH reverse tunnel (-R PORT:127.0.0.1:PORT)
//	         v
//	remote: mock-claude-client reads ~/.claude/ide/PORT.lock
//	        -> connects to ws://127.0.0.1:PORT via tunnel
//	        -> sends initialize + ide_connected
//	        -> MCP server registers the connection
func TestE2E_SSH_RemoteClaudeConnect(t *testing.T) {
	host := remoteHost(t)
	tunnelPort := 18901

	// 1) Start MCP server locally
	h := newServerHelper(t)
	h.start(t)

	// 2) SSH reverse tunnel: remote:tunnelPort -> local:mcpPort
	sshTunnel(t, host, tunnelPort, h.port)

	// 3) Write lock file on remote (same as buildRemoteCommand does)
	writeRemoteLockFile(t, host, tunnelPort, h.token)

	// 4) Run mock-claude-client on remote via SSH
	sshCmd := exec.Command("ssh", "-o", "BatchMode=yes",
		"-R", fmt.Sprintf("%d:127.0.0.1:%d", tunnelPort, h.port),
		host, "mock-claude-client")
	out, err := sshCmd.CombinedOutput()
	t.Logf("mock-claude-client output:\n%s", string(out))
	require.NoError(t, err, "mock-claude-client failed: %s", string(out))

	// 5) Verify mock client connected successfully
	assert.Contains(t, string(out), "MOCK_CLAUDE_CONNECTED",
		"mock client should report successful connection")
	assert.Contains(t, string(out), fmt.Sprintf("port=%d", tunnelPort),
		"mock client should connect to the tunnel port")
}

// TestE2E_SSH_RemoteNotifyViaTunnel tests POST /notify from remote.
func TestE2E_SSH_RemoteNotifyViaTunnel(t *testing.T) {
	host := remoteHost(t)
	tunnelPort := 18902

	h := newServerHelper(t)
	h.start(t)

	sshTunnel(t, host, tunnelPort, h.port)

	curlCmd := fmt.Sprintf(
		`curl -s -w '\n%%{http_code}' `+
			`-X POST `+
			`-H 'Content-Type: application/json' `+
			`-H 'X-Auth-Token: %s' `+
			`-d '{"pid":55555,"tool_name":"Bash","tool_input":{"command":"echo hello"},"cwd":"/home/user"}' `+
			`http://127.0.0.1:%d/notify`,
		h.token, tunnelPort)
	sshCurl := exec.Command("ssh", "-o", "BatchMode=yes", host, curlCmd)
	out, err := sshCurl.CombinedOutput()
	require.NoError(t, err, "SSH notify curl failed: %s", string(out))

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	require.True(t, len(lines) >= 1)
	lastLine := lines[len(lines)-1]
	assert.True(t, lastLine == "200" || lastLine == "404",
		"expected HTTP 200 or 404, got %s", lastLine)
}

// TestE2E_SSH_RemotePopupFullFlow is the most comprehensive E2E test.
// It verifies the entire production flow:
//
//  1. lazyclaude TUI starts locally (auto-starts MCP server)
//  2. SSH reverse tunnel connects remote to local MCP server
//  3. Remote mock-claude-client connects via WebSocket + sends tool notification
//  4. TUI displays popup with tool name and action buttons
//
// This proves that a remote Claude Code session can trigger popups in the local TUI.
func TestE2E_SSH_RemotePopupFullFlow(t *testing.T) {
	host := remoteHost(t)
	cleanLazyClaudeState(t)

	// 1) Start lazyclaude TUI in tmux (auto-starts MCP server)
	bin := e2eBinary(t)
	tmuxH := newTmuxHelper(t)
	tmuxH.startSession("popup-flow", 120, 40)
	tmuxH.sendKeys("popup-flow", fmt.Sprintf("%s; sleep 999", bin), "Enter")

	found := tmuxH.waitForText("popup-flow", "no sessions", 10*time.Second)
	require.True(t, found, "TUI should start and show sessions panel")

	// 2) Wait for MCP server to be ready (port file appears)
	portFile := filepath.Join(os.TempDir(), "lazyclaude-mcp.port")
	var mcpPort int
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(portFile)
		if err != nil {
			return false
		}
		p, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || p <= 0 {
			return false
		}
		mcpPort = p
		return true
	}, 10*time.Second, 200*time.Millisecond, "MCP server port file should appear")
	t.Logf("MCP server on port %d", mcpPort)

	// 3) Read auth token from lock file
	home, _ := os.UserHomeDir()
	lockPath := filepath.Join(home, ".claude", "ide", fmt.Sprintf("%d.lock", mcpPort))
	var token string
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(lockPath)
		if err != nil {
			return false
		}
		var lock struct {
			AuthToken string `json:"authToken"`
		}
		if json.Unmarshal(data, &lock) != nil || lock.AuthToken == "" {
			return false
		}
		token = lock.AuthToken
		return true
	}, 5*time.Second, 200*time.Millisecond, "lock file with token should appear")

	// 4) Write lock file on remote + run mock client with --notify
	tunnelPort := mcpPort
	writeRemoteLockFile(t, host, tunnelPort, token)

	// 6) Run mock-claude-client on remote: connect + send tool notification
	sshCmd := exec.Command("ssh", "-o", "BatchMode=yes",
		"-R", fmt.Sprintf("%d:127.0.0.1:%d", tunnelPort, mcpPort),
		host, "mock-claude-client", "--notify", "--tool", "RemoteWrite")
	out, err := sshCmd.CombinedOutput()
	t.Logf("mock-claude-client output:\n%s", string(out))
	require.NoError(t, err, "mock-claude-client failed: %s", string(out))
	assert.Contains(t, string(out), "MOCK_CLAUDE_CONNECTED")
	assert.Contains(t, string(out), "MOCK_NOTIFY_SENT")

	// 7) Verify popup appears in TUI with tool name
	found = tmuxH.waitForText("popup-flow", "RemoteWrite", 5*time.Second)
	if !found {
		content := tmuxH.capturePane("popup-flow")
		t.Logf("TUI content after notify:\n%s", content)
	}
	assert.True(t, found, "popup should appear with tool name 'RemoteWrite'")

	// 8) Verify popup action buttons are visible
	content := tmuxH.capturePane("popup-flow")
	assert.Contains(t, content, "y/a/n", "popup action bar should be visible")
}

// TestE2E_SSH_SessionCreate tests SSH commands inside tmux.
func TestE2E_SSH_SessionCreate(t *testing.T) {
	host := remoteHost(t)

	out, err := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5",
		host, "whoami").CombinedOutput()
	require.NoError(t, err, "SSH failed: %s", string(out))
	t.Logf("remote user: %s", strings.TrimSpace(string(out)))

	tmux := newTmuxHelper(t)
	tmux.startSession("test-ssh", 120, 40)

	sshCmd := fmt.Sprintf("ssh -o BatchMode=yes -o ConnectTimeout=5 %s echo ssh-in-tmux", host)
	tmux.sendKeys("test-ssh", sshCmd, "Enter")

	found := tmux.waitForText("test-ssh", "ssh-in-tmux", 10*time.Second)
	assert.True(t, found, "SSH command should succeed inside tmux")
}
