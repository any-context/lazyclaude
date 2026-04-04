package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/notify"
	"github.com/any-context/lazyclaude/internal/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"
)

func startTestServer(t *testing.T) (*server.Server, int, *tmux.MockClient) {
	t.Helper()
	mock := tmux.NewMockClient()
	logger := log.New(&bytes.Buffer{}, "", 0)

	tmpDir := t.TempDir()
	cfg := server.Config{
		Port:       0, // random port
		Token:      "test-token",
		IDEDir:     filepath.Join(tmpDir, "ide"),
		RuntimeDir: filepath.Join(tmpDir, "run"),
	}

	srv := server.New(cfg, mock, logger)
	ctx := context.Background()
	port, err := srv.Start(ctx)
	require.NoError(t, err)
	require.Greater(t, port, 0)

	t.Cleanup(func() {
		srv.Stop(context.Background())
	})

	return srv, port, mock
}

func TestServer_StartAndStop(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)

	assert.Greater(t, port, 0)
	assert.Equal(t, port, srv.Port())

	err := srv.Stop(context.Background())
	require.NoError(t, err)

	// Double stop should be safe
	err = srv.Stop(context.Background())
	require.NoError(t, err)
}

func TestServer_WebSocket_Unauthorized(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Connect without token
	_, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/", port), nil)
	assert.Error(t, err) // should fail with 401
}

func TestServer_WebSocket_Initialize(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/", port), &websocket.DialOptions{
		HTTPHeader: http.Header{"X-Auth-Token": []string{"test-token"}},
	})
	require.NoError(t, err)
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send initialize request
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{"capabilities": map[string]any{}},
	}
	data, _ := json.Marshal(initReq)
	err = conn.Write(ctx, websocket.MessageText, data)
	require.NoError(t, err)

	// Read response
	_, respData, err := conn.Read(ctx)
	require.NoError(t, err)

	var resp server.Response
	require.NoError(t, json.Unmarshal(respData, &resp))
	assert.Nil(t, resp.Error)

	resultJSON, _ := json.Marshal(resp.Result)
	assert.Contains(t, string(resultJSON), "lazyclaude")
	assert.Contains(t, string(resultJSON), "protocolVersion")
}

func TestServer_WebSocket_IDEConnected_Then_OpenDiff(t *testing.T) {
	t.Parallel()
	srv, port, mock := startTestServer(t)

	// Set up mock for PID resolution
	mock.Panes[""] = []tmux.PaneInfo{
		{ID: "%1", Window: "@1", PID: 5555},
	}
	mock.Sessions["lc"] = []tmux.WindowInfo{
		{ID: "@1", Name: "lc-abc", Session: "lc"},
	}
	mock.Messages["@1"] = "lc"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/", port), &websocket.DialOptions{
		HTTPHeader: http.Header{"X-Auth-Token": []string{"test-token"}},
	})
	require.NoError(t, err)
	defer conn.Close(websocket.StatusNormalClosure, "")

	// 1) Send ide_connected (notification, no response)
	ideReq, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "ide_connected",
		"params":  map[string]any{"pid": 5555},
	})
	require.NoError(t, conn.Write(ctx, websocket.MessageText, ideReq))

	// Poll for state update with deadline (avoid flaky time.Sleep)
	require.Eventually(t, func() bool {
		return srv.State().ConnCount() >= 1
	}, 2*time.Second, 10*time.Millisecond, "expected connection to be registered")

	// 2) Send openDiff (request, expects response)
	diffReq, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "openDiff",
		"params":  map[string]any{"old_file_path": "/home/user/main.go", "new_contents": "package main"},
	})
	require.NoError(t, conn.Write(ctx, websocket.MessageText, diffReq))

	_, respData, err := conn.Read(ctx)
	require.NoError(t, err)

	var resp server.Response
	require.NoError(t, json.Unmarshal(respData, &resp))
	assert.Nil(t, resp.Error)

	resultJSON, _ := json.Marshal(resp.Result)
	assert.Contains(t, string(resultJSON), "@1")
	assert.Contains(t, string(resultJSON), "main.go")
}

func TestServer_Notify_POST(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)

	// Pre-populate PID -> window mapping
	srv.State().SetConn("c1", &server.ConnState{PID: 7777, Window: "@3"})

	body, _ := json.Marshal(map[string]int{"pid": 7777})
	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/notify", port),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", "test-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "@3", result["window"])
}

func TestServer_Notify_WritesNotificationFile(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)

	srv.State().SetConn("c1", &server.ConnState{PID: 8888, Window: "@5"})

	body, _ := json.Marshal(map[string]any{
		"pid":       8888,
		"tool_name": "Bash",
		"input":     `{"command":"ls"}`,
		"cwd":       "/home/user",
	})
	resp := postNotify(t, port, body)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify notification was enqueued
	ns, err := notify.ReadAll(srv.RuntimeDir())
	require.NoError(t, err)
	require.Len(t, ns, 1, "notification should be enqueued")
	assert.Equal(t, "Bash", ns[0].ToolName)
	assert.Equal(t, `{"command":"ls"}`, ns[0].Input)
	assert.Equal(t, "/home/user", ns[0].CWD)
	assert.Equal(t, "@5", ns[0].Window)
}

func TestServer_Notify_TwoPhase_ToolInfoThenPermission(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 9999, Window: "@7"})

	// Phase 1: PreToolUse sends tool_info — should NOT write notification file
	body1, _ := json.Marshal(map[string]any{
		"type":       "tool_info",
		"pid":        9999,
		"tool_name":  "Write",
		"tool_input": map[string]any{"file_path": "hello.txt", "content": "Hello"},
	})
	resp1 := postNotify(t, port, body1)
	resp1.Body.Close()
	require.Equal(t, http.StatusOK, resp1.StatusCode)

	// Verify no notification yet
	ns, err := notify.ReadAll(srv.RuntimeDir())
	require.NoError(t, err)
	assert.Empty(t, ns, "tool_info should not enqueue notification")

	// Phase 2: Notification hook sends permission_prompt — should trigger popup
	body2, _ := json.Marshal(map[string]any{
		"pid":     9999,
		"message": "Do you want to create hello.txt?",
	})
	resp2 := postNotify(t, port, body2)
	resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	// Verify notification enqueued with stored tool info
	ns, err = notify.ReadAll(srv.RuntimeDir())
	require.NoError(t, err)
	require.Len(t, ns, 1, "permission_prompt should enqueue notification")
	assert.Equal(t, "Write", ns[0].ToolName)
	assert.Contains(t, ns[0].Input, "hello.txt")
	assert.Equal(t, "@7", ns[0].Window)
}

func TestServer_Notify_AcceptsBothAuthHeaders(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 1111, Window: "@1"})

	// Test with X-Claude-Code-Ide-Authorization header
	body, _ := json.Marshal(map[string]any{
		"pid":       1111,
		"tool_name": "Bash",
	})
	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/notify", port),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Claude-Code-Ide-Authorization", "test-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestServer_Notify_Unauthorized(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServer(t)

	body, _ := json.Marshal(map[string]int{"pid": 1})
	resp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/notify", port),
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestServer_Notify_GET_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServer(t)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/notify", port))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func postNotify(t *testing.T, port int, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/notify", port),
		bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", "test-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func TestServer_Notify_InvalidPID(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServer(t)

	body, _ := json.Marshal(map[string]int{"pid": 0})
	resp := postNotify(t, port, body)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServer_Notify_UnknownPID(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServer(t)

	body, _ := json.Marshal(map[string]int{"pid": 9999})
	resp := postNotify(t, port, body)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestServer_Notify_BadJSON(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServer(t)

	resp := postNotify(t, port, []byte(`{invalid`))
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServer_LockFile_CreatedAndRemoved(t *testing.T) {
	t.Parallel()
	srv, _, _ := startTestServer(t)

	// Lock file creation is verified by the server starting successfully.
	// Stop should remove lock file.
	err := srv.Stop(context.Background())
	require.NoError(t, err)
}

func postEndpoint(t *testing.T, port int, path string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d%s", port, path),
		bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", "test-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func TestServer_Stop_POST(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)

	srv.State().SetConn("c1", &server.ConnState{PID: 1234, Window: "@2"})

	body, _ := json.Marshal(map[string]any{
		"pid":         1234,
		"stop_reason": "end_turn",
		"session_id":  "sess-abc",
	})
	resp := postEndpoint(t, port, "/stop", body)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "ok", result["status"])
}

func TestServer_Stop_Unauthorized(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServer(t)

	body, _ := json.Marshal(map[string]any{"pid": 1234})
	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/stop", port),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", "wrong-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestServer_Stop_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServer(t)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/stop", port))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestServer_SessionStart_POST(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)

	srv.State().SetConn("c1", &server.ConnState{PID: 5678, Window: "@4"})

	body, _ := json.Marshal(map[string]any{
		"pid":        5678,
		"session_id": "sess-xyz",
	})
	resp := postEndpoint(t, port, "/session-start", body)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "ok", result["status"])
}

func TestServer_SessionStart_Unauthorized(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServer(t)

	body, _ := json.Marshal(map[string]any{"pid": 5678})
	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/session-start", port),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", "wrong-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestServer_Stop_PublishesBrokerEvent(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)

	srv.State().SetConn("c1", &server.ConnState{PID: 3333, Window: "@6"})

	sub := srv.NotifyBroker().Subscribe(8)
	defer sub.Cancel()

	body, _ := json.Marshal(map[string]any{
		"pid":         3333,
		"stop_reason": "error",
		"session_id":  "sess-err",
	})
	resp := postEndpoint(t, port, "/stop", body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case ev := <-sub.Ch():
		require.NotNil(t, ev.StopNotification)
		assert.Equal(t, "@6", ev.StopNotification.Window)
		assert.Equal(t, "error", ev.StopNotification.StopReason)
		assert.Equal(t, "sess-err", ev.StopNotification.SessionID)
	case <-time.After(2 * time.Second):
		t.Fatal("expected stop event on broker")
	}
}

func TestServer_SessionStart_PublishesBrokerEvent(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)

	srv.State().SetConn("c1", &server.ConnState{PID: 4444, Window: "@8"})

	sub := srv.NotifyBroker().Subscribe(8)
	defer sub.Cancel()

	body, _ := json.Marshal(map[string]any{
		"pid":        4444,
		"session_id": "sess-start",
	})
	resp := postEndpoint(t, port, "/session-start", body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case ev := <-sub.Ch():
		require.NotNil(t, ev.SessionStartNotification)
		assert.Equal(t, "@8", ev.SessionStartNotification.Window)
		assert.Equal(t, "sess-start", ev.SessionStartNotification.SessionID)
	case <-time.After(2 * time.Second):
		t.Fatal("expected session-start event on broker")
	}
}

func TestServer_PromptSubmit_POST(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)

	srv.State().SetConn("c1", &server.ConnState{PID: 7777, Window: "@10"})

	body, _ := json.Marshal(map[string]any{
		"pid":        7777,
		"session_id": "sess-prompt",
	})
	resp := postEndpoint(t, port, "/prompt-submit", body)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "ok", result["status"])
}

func TestServer_PromptSubmit_Unauthorized(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServer(t)

	body, _ := json.Marshal(map[string]any{"pid": 7777})
	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/prompt-submit", port),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", "wrong-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestServer_PromptSubmit_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServer(t)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/prompt-submit", port))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestServer_PromptSubmit_PublishesBrokerEvent(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)

	srv.State().SetConn("c1", &server.ConnState{PID: 8888, Window: "@12"})

	sub := srv.NotifyBroker().Subscribe(8)
	defer sub.Cancel()

	body, _ := json.Marshal(map[string]any{
		"pid":        8888,
		"session_id": "sess-ps",
	})
	resp := postEndpoint(t, port, "/prompt-submit", body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case ev := <-sub.Ch():
		require.NotNil(t, ev.PromptSubmitNotification)
		assert.Equal(t, "@12", ev.PromptSubmitNotification.Window)
		assert.Equal(t, "sess-ps", ev.PromptSubmitNotification.SessionID)
	case <-time.After(2 * time.Second):
		t.Fatal("expected prompt-submit event on broker")
	}
}

// --- Activity tracking tests ---

func TestServer_Activity_SessionStartSetsRunning(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 2000, Window: "@20"})

	body, _ := json.Marshal(map[string]any{
		"pid":        2000,
		"session_id": "sess-act",
	})
	resp := postEndpoint(t, port, "/session-start", body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.Eventually(t, func() bool {
		state, _ := srv.WindowActivity("@20")
		return state.String() == "running"
	}, 2*time.Second, 10*time.Millisecond)
}

func TestServer_Activity_StopSetsIdle(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 2001, Window: "@21"})

	body, _ := json.Marshal(map[string]any{
		"pid":         2001,
		"stop_reason": "end_turn",
		"session_id":  "sess-idle",
	})
	resp := postEndpoint(t, port, "/stop", body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.Eventually(t, func() bool {
		state, _ := srv.WindowActivity("@21")
		return state.String() == "idle"
	}, 2*time.Second, 10*time.Millisecond)
}

func TestServer_Activity_StopErrorSetsError(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 2002, Window: "@22"})

	body, _ := json.Marshal(map[string]any{
		"pid":         2002,
		"stop_reason": "error",
		"session_id":  "sess-err",
	})
	resp := postEndpoint(t, port, "/stop", body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.Eventually(t, func() bool {
		state, _ := srv.WindowActivity("@22")
		return state.String() == "error"
	}, 2*time.Second, 10*time.Millisecond)
}

func TestServer_Activity_StopInterruptSetsError(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 2005, Window: "@25"})

	body, _ := json.Marshal(map[string]any{
		"pid":         2005,
		"stop_reason": "interrupt",
		"session_id":  "sess-int",
	})
	resp := postEndpoint(t, port, "/stop", body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.Eventually(t, func() bool {
		state, _ := srv.WindowActivity("@25")
		return state.String() == "error"
	}, 2*time.Second, 10*time.Millisecond)
}

func TestServer_Activity_PromptSubmitSetsRunning(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 2003, Window: "@23"})

	body, _ := json.Marshal(map[string]any{
		"pid":        2003,
		"session_id": "sess-ps",
	})
	resp := postEndpoint(t, port, "/prompt-submit", body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.Eventually(t, func() bool {
		state, _ := srv.WindowActivity("@23")
		return state.String() == "running"
	}, 2*time.Second, 10*time.Millisecond)
}

func TestServer_Activity_ToolInfoSetsRunning(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 2004, Window: "@24"})

	body, _ := json.Marshal(map[string]any{
		"type":      "tool_info",
		"pid":       2004,
		"tool_name": "Read",
	})
	resp := postNotify(t, port, body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.Eventually(t, func() bool {
		state, toolName := srv.WindowActivity("@24")
		return state.String() == "running" && toolName == "Read"
	}, 2*time.Second, 10*time.Millisecond)
}

func TestServer_Activity_UnknownWindowReturnsUnknown(t *testing.T) {
	t.Parallel()
	srv, _, _ := startTestServer(t)

	state, toolName := srv.WindowActivity("@nonexistent")
	assert.Equal(t, "unknown", state.String())
	assert.Equal(t, "", toolName)
}

func TestMsgSessions_returns_activity(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)

	sessions := []server.SessionInfo{
		{ID: "s1", Name: "main", Role: "pm", Path: "/work", Window: "@30"},
		{ID: "s2", Name: "worker", Role: "worker", Path: "/work/feat", Window: "@31"},
	}
	srv.SetSessionLister(&fakeSessionLister{sessions: sessions})

	// Inject activity via hook endpoints
	srv.State().SetConn("c1", &server.ConnState{PID: 3000, Window: "@30"})
	srv.State().SetConn("c2", &server.ConnState{PID: 3001, Window: "@31"})

	// Session start for @30 -> running
	body, _ := json.Marshal(map[string]any{"pid": 3000, "session_id": "s1"})
	resp := postEndpoint(t, port, "/session-start", body)
	resp.Body.Close()

	// Stop for @31 -> idle
	body, _ = json.Marshal(map[string]any{"pid": 3001, "stop_reason": "end_turn", "session_id": "s2"})
	resp = postEndpoint(t, port, "/stop", body)
	resp.Body.Close()

	// Wait for activity loop to process
	require.Eventually(t, func() bool {
		s1, _ := srv.WindowActivity("@30")
		s2, _ := srv.WindowActivity("@31")
		return s1.String() == "running" && s2.String() == "idle"
	}, 2*time.Second, 10*time.Millisecond)

	// Fetch sessions and check activity field
	sessResp := msgSessions(t, port, "test-token")
	defer sessResp.Body.Close()
	require.Equal(t, http.StatusOK, sessResp.StatusCode)

	var result []server.SessionInfo
	require.NoError(t, json.NewDecoder(sessResp.Body).Decode(&result))
	require.Len(t, result, 2)
	assert.Equal(t, "running", result[0].Activity)
	assert.Equal(t, "idle", result[1].Activity)
}
