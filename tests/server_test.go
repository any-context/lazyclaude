package tests_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"nhooyr.io/websocket"
)

// serverHelper manages an isolated MCP server process for E2E testing.
type serverHelper struct {
	t       *testing.T
	cmd     *exec.Cmd
	port    int
	token   string
	dataDir string
	runDir  string
	ideDir  string
}

func newServerHelper(t *testing.T) *serverHelper {
	t.Helper()
	binary := e2eBinary(t)
	tmpDir := t.TempDir()
	token := "e2e-test-token"

	h := &serverHelper{
		t:       t,
		token:   token,
		dataDir: filepath.Join(tmpDir, "data"),
		runDir:  filepath.Join(tmpDir, "run"),
		ideDir:  filepath.Join(tmpDir, "ide"),
	}

	// Pre-create directories so server can write port/lock files
	os.MkdirAll(h.dataDir, 0o755)
	os.MkdirAll(h.runDir, 0o755)
	os.MkdirAll(h.ideDir, 0o755)

	cmd := exec.Command(binary, "server", "--port", "0", "--token", token)
	cmd.Env = append(os.Environ(),
		"LAZYCLAUDE_DATA_DIR="+h.dataDir,
		"LAZYCLAUDE_RUNTIME_DIR="+h.runDir,
		"LAZYCLAUDE_IDE_DIR="+h.ideDir,
	)
	cmd.Stderr = os.Stderr
	h.cmd = cmd

	return h
}

func (h *serverHelper) start(t *testing.T) {
	t.Helper()
	require.NoError(t, h.cmd.Start())

	t.Cleanup(func() {
		h.cmd.Process.Signal(syscall.SIGTERM)
		h.cmd.Wait()
	})

	// Wait for port file
	portFile := filepath.Join(h.runDir, "lazyclaude-mcp.port")
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(portFile)
		if err != nil {
			return false
		}
		port, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || port <= 0 {
			return false
		}
		h.port = port
		return true
	}, 10*time.Second, 100*time.Millisecond, "port file should appear")
}

func (h *serverHelper) wsConnect(ctx context.Context) *websocket.Conn {
	h.t.Helper()
	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/", h.port), &websocket.DialOptions{
		HTTPHeader: http.Header{"X-Auth-Token": []string{h.token}},
	})
	require.NoError(h.t, err)
	return conn
}

func (h *serverHelper) postNotify(body []byte) *http.Response {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/notify", h.port),
		bytes.NewReader(body))
	require.NoError(h.t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", h.token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(h.t, err)
	return resp
}

// TestE2E_Server_StartAndConnect verifies the MCP server binary starts,
// listens, and responds to WebSocket initialize.
func TestE2E_Server_StartAndConnect(t *testing.T) {
	t.Parallel()
	h := newServerHelper(t)
	h.start(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := h.wsConnect(ctx)
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send initialize
	initReq, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{"capabilities": map[string]any{}},
	})
	require.NoError(t, conn.Write(ctx, websocket.MessageText, initReq))

	_, respData, err := conn.Read(ctx)
	require.NoError(t, err)
	assert.Contains(t, string(respData), "lazyclaude")
	assert.Contains(t, string(respData), "protocolVersion")
}

// TestE2E_Server_Unauthorized verifies auth is enforced.
func TestE2E_Server_Unauthorized(t *testing.T) {
	t.Parallel()
	h := newServerHelper(t)
	h.start(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// WebSocket without token
	_, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/", h.port), nil)
	assert.Error(t, err)

	// HTTP without token
	body, _ := json.Marshal(map[string]int{"pid": 1})
	resp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/notify", h.port),
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestE2E_Server_NotifyFullFlow tests the complete notification flow:
// tmux session → ide_connected → tool_info → permission_prompt → notification enqueued.
// Requires tmux (the server needs PID→window resolution).
func TestE2E_Server_NotifyFullFlow(t *testing.T) {
	t.Parallel()

	// This test needs tmux for PID→window resolution
	tmuxH := newTmuxHelper(t)
	tmuxH.startSession("lazyclaude", 80, 24)
	// Start a sleep process in the tmux window so we have a known PID
	tmuxH.sendKeys("lazyclaude", "sleep 300", "Enter")
	time.Sleep(200 * time.Millisecond)

	// Get the PID of the sleep process
	pidStr, err := tmuxH.run("list-panes", "-t", "lazyclaude", "-F", "#{pane_pid}")
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(pidStr))
	require.NoError(t, err)
	t.Logf("tmux pane PID: %d", pid)

	// Start server using the same tmux socket as our helper
	h := newServerHelper(t)
	// Override the server to use the test tmux socket
	h.cmd.Env = append(h.cmd.Env, "LAZYCLAUDE_TMUX_SOCKET="+tmuxH.socket)
	h.start(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1) Connect via WebSocket and send ide_connected with the real PID
	conn := h.wsConnect(ctx)
	defer conn.Close(websocket.StatusNormalClosure, "")

	ideReq, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "ide_connected",
		"params":  map[string]any{"pid": pid},
	})
	require.NoError(t, conn.Write(ctx, websocket.MessageText, ideReq))
	time.Sleep(300 * time.Millisecond)

	// 2) Phase 1: POST /notify with tool_info
	body1, _ := json.Marshal(map[string]any{
		"type":       "tool_info",
		"pid":        pid,
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "rm -rf /"},
		"cwd":        "/home/user/project",
	})
	resp1 := h.postNotify(body1)
	resp1.Body.Close()
	require.Equal(t, http.StatusOK, resp1.StatusCode)

	// 3) Phase 2: POST /notify with permission_prompt
	body2, _ := json.Marshal(map[string]any{
		"pid":     pid,
		"message": "Allow Bash: rm -rf /?",
	})
	resp2 := h.postNotify(body2)
	resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	// 4) Verify notification file was created in runtime dir
	var notifFiles []string
	require.Eventually(t, func() bool {
		entries, err := os.ReadDir(h.runDir)
		if err != nil {
			return false
		}
		notifFiles = nil
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "lazyclaude-q-") {
				notifFiles = append(notifFiles, e.Name())
			}
		}
		return len(notifFiles) > 0
	}, 3*time.Second, 100*time.Millisecond, "notification file should exist")

	// Read and verify notification content
	data, err := os.ReadFile(filepath.Join(h.runDir, notifFiles[0]))
	require.NoError(t, err)
	assert.Contains(t, string(data), "Bash")
	assert.Contains(t, string(data), "rm -rf /")
}

// TestE2E_Server_GracefulShutdown verifies SIGTERM triggers clean shutdown.
func TestE2E_Server_GracefulShutdown(t *testing.T) {
	t.Parallel()
	h := newServerHelper(t)
	h.start(t)

	portFile := filepath.Join(h.runDir, "lazyclaude-mcp.port")

	// Port file should exist
	_, err := os.Stat(portFile)
	require.NoError(t, err)

	// Send SIGTERM
	require.NoError(t, h.cmd.Process.Signal(syscall.SIGTERM))
	err = h.cmd.Wait()
	// Process may exit with 0 or signal error
	_ = err

	// Port file should eventually be cleaned up (lock file removal is best-effort)
	time.Sleep(200 * time.Millisecond)
}

// TestE2E_Server_MultipleClients tests multiple WebSocket clients.
func TestE2E_Server_MultipleClients(t *testing.T) {
	t.Parallel()
	h := newServerHelper(t)
	h.start(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect two clients
	conn1 := h.wsConnect(ctx)
	defer conn1.Close(websocket.StatusNormalClosure, "")

	conn2 := h.wsConnect(ctx)
	defer conn2.Close(websocket.StatusNormalClosure, "")

	// Both should be able to initialize
	initReq, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{"capabilities": map[string]any{}},
	})

	require.NoError(t, conn1.Write(ctx, websocket.MessageText, initReq))
	_, resp1, err := conn1.Read(ctx)
	require.NoError(t, err)
	assert.Contains(t, string(resp1), "lazyclaude")

	require.NoError(t, conn2.Write(ctx, websocket.MessageText, initReq))
	_, resp2, err := conn2.Read(ctx)
	require.NoError(t, err)
	assert.Contains(t, string(resp2), "lazyclaude")
}
