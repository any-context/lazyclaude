package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSessionLister implements server.SessionLister for tests.
type fakeSessionLister struct {
	sessions []server.SessionInfo
}

func (f *fakeSessionLister) Sessions() []server.SessionInfo {
	return f.sessions
}

// startTestServerWithSessions starts a test server with a fake SessionLister.
func startTestServerWithSessions(t *testing.T, sessions []server.SessionInfo) (*server.Server, int) {
	t.Helper()
	srv, port, _ := startTestServer(t)
	srv.SetSessionLister(&fakeSessionLister{sessions: sessions})
	return srv, port
}

// msgSend sends a POST /msg/send request to the server.
func msgSend(t *testing.T, port int, token string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/msg/send", port),
		bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Auth-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// msgSessions sends a GET /msg/sessions request.
func msgSessions(t *testing.T, port int, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/msg/sessions", port), nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("X-Auth-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// --- fakeSessionCreator for /msg/create tests ---

type fakeSessionCreator struct {
	projectPath string // returned by FindProjectForSession
	result      *server.SessionCreateResult
	err         error
}

func (f *fakeSessionCreator) FindProjectForSession(id string) *server.SessionProjectInfo {
	if f.projectPath == "" {
		return nil
	}
	return &server.SessionProjectInfo{Path: f.projectPath}
}

func (f *fakeSessionCreator) CreateWorkerSession(ctx context.Context, name, prompt, projectRoot string) (*server.SessionCreateResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func (f *fakeSessionCreator) CreateLocalSession(ctx context.Context, name, projectPath string) (*server.SessionCreateResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func (f *fakeSessionCreator) ResumeSession(ctx context.Context, id, prompt, name string) (*server.SessionCreateResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// startTestServerWithCreator starts a test server with both SessionLister and SessionCreator.
func startTestServerWithCreator(t *testing.T, sessions []server.SessionInfo, creator server.SessionCreator) (*server.Server, int, *tmux.MockClient) {
	t.Helper()
	srv, port, mock := startTestServer(t)
	srv.SetSessionLister(&fakeSessionLister{sessions: sessions})
	srv.SetSessionCreator(creator)
	return srv, port, mock
}

// msgCreate sends a POST /msg/create request to the server.
func msgCreate(t *testing.T, port int, token string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/msg/create", port),
		bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Auth-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// --- POST /msg/create ---

func TestMsgCreate_missing_auth(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServerWithCreator(t, nil, &fakeSessionCreator{})

	resp := msgCreate(t, port, "", map[string]string{
		"from": "a", "name": "test", "type": "worker",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMsgCreate_wrong_method(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServerWithCreator(t, nil, &fakeSessionCreator{})

	req, err := http.NewRequest(http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/msg/create", port), nil)
	require.NoError(t, err)
	req.Header.Set("X-Auth-Token", "test-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestMsgCreate_invalid_json(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServerWithCreator(t, nil, &fakeSessionCreator{})

	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/msg/create", port),
		bytes.NewReader([]byte(`{invalid`)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", "test-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMsgCreate_missing_from(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServerWithCreator(t, nil, &fakeSessionCreator{})

	resp := msgCreate(t, port, "test-token", map[string]string{
		"name": "test", "type": "worker",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMsgCreate_missing_name(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServerWithCreator(t, nil, &fakeSessionCreator{})

	resp := msgCreate(t, port, "test-token", map[string]string{
		"from": "abc", "type": "worker",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMsgCreate_invalid_type(t *testing.T) {
	t.Parallel()
	_, port, _ := startTestServerWithCreator(t, nil, &fakeSessionCreator{})

	resp := msgCreate(t, port, "test-token", map[string]string{
		"from": "abc", "name": "test", "type": "unknown",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMsgCreate_from_not_found(t *testing.T) {
	t.Parallel()
	creator := &fakeSessionCreator{projectPath: ""} // not found
	_, port, _ := startTestServerWithCreator(t, nil, creator)

	resp := msgCreate(t, port, "test-token", map[string]string{
		"from": "nonexistent", "name": "test", "type": "worker",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestMsgCreate_no_creator_set(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.SetSessionLister(&fakeSessionLister{})
	// No creator set

	resp := msgCreate(t, port, "test-token", map[string]string{
		"from": "abc", "name": "test", "type": "worker",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestMsgCreate_worker_success(t *testing.T) {
	t.Parallel()
	creator := &fakeSessionCreator{
		projectPath: "/project",
		result: &server.SessionCreateResult{
			ID:     "new-session-id",
			Name:   "feat-auth",
			Role:   "worker",
			Path:   "/project/.lazyclaude/worktrees/feat-auth",
			Window: "@5",
		},
	}
	_, port, _ := startTestServerWithCreator(t, nil, creator)

	resp := msgCreate(t, port, "test-token", map[string]string{
		"from": "pm-id", "name": "feat-auth", "type": "worker", "prompt": "Implement auth",
	})
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result server.MsgCreateResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "created", result.Status)
	require.NotNil(t, result.Session)
	assert.Equal(t, "new-session-id", result.Session.ID)
	assert.Equal(t, "feat-auth", result.Session.Name)
	assert.Equal(t, "worker", result.Session.Role)
	assert.Equal(t, "/project/.lazyclaude/worktrees/feat-auth", result.Session.Path)
	assert.Equal(t, "@5", result.Session.Window)
}

func TestMsgCreate_local_success(t *testing.T) {
	t.Parallel()
	creator := &fakeSessionCreator{
		projectPath: "/project",
		result: &server.SessionCreateResult{
			ID:     "local-session-id",
			Name:   "debug-task",
			Role:   "",
			Path:   "/project",
			Window: "@6",
		},
	}
	_, port, _ := startTestServerWithCreator(t, nil, creator)

	resp := msgCreate(t, port, "test-token", map[string]string{
		"from": "w1-id", "name": "debug-task", "type": "local",
	})
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result server.MsgCreateResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "created", result.Status)
	assert.Equal(t, "local-session-id", result.Session.ID)
}

func TestMsgCreate_local_with_prompt(t *testing.T) {
	t.Parallel()
	creator := &fakeSessionCreator{
		projectPath: "/project",
		result: &server.SessionCreateResult{
			ID:     "local-prompt-id",
			Name:   "task",
			Role:   "",
			Path:   "/project",
			Window: "@7",
		},
	}
	_, port, mock := startTestServerWithCreator(t, nil, creator)

	resp := msgCreate(t, port, "test-token", map[string]string{
		"from": "w1-id", "name": "task", "type": "local", "prompt": "Fix the bug",
	})
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify SendKeysLiteral was called with the prompt
	target := "lazyclaude:@7"
	sent := mock.SentKeys[target]
	require.GreaterOrEqual(t, len(sent), 1, "prompt should be sent via SendKeysLiteral")
	assert.Contains(t, sent[0], "Fix the bug")
}

func TestMsgCreate_creation_error(t *testing.T) {
	t.Parallel()
	creator := &fakeSessionCreator{
		projectPath: "/project",
		err:         fmt.Errorf("git worktree add failed"),
	}
	_, port, _ := startTestServerWithCreator(t, nil, creator)

	resp := msgCreate(t, port, "test-token", map[string]string{
		"from": "pm-id", "name": "feat-x", "type": "worker",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// --- POST /msg/send ---

// startTestServerWithMock starts a test server and returns the MockClient for assertions.
func startTestServerWithMock(t *testing.T, sessions []server.SessionInfo) (*server.Server, int, *tmux.MockClient) {
	t.Helper()
	srv, port, mock := startTestServer(t)
	srv.SetSessionLister(&fakeSessionLister{sessions: sessions})
	return srv, port, mock
}

func TestMsgSend_missing_auth(t *testing.T) {
	t.Parallel()
	_, port := startTestServerWithSessions(t, nil)

	resp := msgSend(t, port, "", map[string]string{"from": "a", "to": "b", "type": "status", "body": "x"})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMsgSend_wrong_token(t *testing.T) {
	t.Parallel()
	_, port := startTestServerWithSessions(t, nil)

	resp := msgSend(t, port, "wrong-token", map[string]string{"from": "a", "to": "b", "type": "status", "body": "x"})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMsgSend_invalid_json(t *testing.T) {
	t.Parallel()
	_, port := startTestServerWithSessions(t, nil)

	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/msg/send", port),
		bytes.NewReader([]byte(`{invalid`)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", "test-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMsgSend_wrong_method(t *testing.T) {
	t.Parallel()
	_, port := startTestServerWithSessions(t, nil)

	req, err := http.NewRequest(http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/msg/send", port), nil)
	require.NoError(t, err)
	req.Header.Set("X-Auth-Token", "test-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestHandleMsgSend_EmptyFrom(t *testing.T) {
	t.Parallel()
	_, port := startTestServerWithSessions(t, nil)

	resp := msgSend(t, port, "test-token", map[string]string{
		"from": "",
		"to":   "target",
		"type": "status",
		"body": "hello",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleMsgSend_EmptyTo(t *testing.T) {
	t.Parallel()
	_, port := startTestServerWithSessions(t, nil)

	resp := msgSend(t, port, "test-token", map[string]string{
		"from": "source",
		"to":   "",
		"type": "status",
		"body": "hello",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMsgSend_FromEqualsTo(t *testing.T) {
	t.Parallel()
	sessions := []server.SessionInfo{
		{ID: "self-id", Name: "pm", Role: "pm", Window: "lc-aabbccdd", Status: "Running"},
	}
	_, port, _ := startTestServerWithMock(t, sessions)

	resp := msgSend(t, port, "test-token", map[string]string{
		"from": "self-id",
		"to":   "self-id",
		"type": "status",
		"body": "hello myself",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "cannot send a message to yourself")
}

// --- Push-based delivery tests ---

// TestMsgSend_PushDelivery_Success verifies that a message is pasted to the recipient's tmux pane.
func TestMsgSend_PushDelivery_Success(t *testing.T) {
	t.Parallel()

	sessions := []server.SessionInfo{
		{ID: "pm-session-id", Name: "pm", Role: "pm", Window: "lc-aabbccdd", Status: "Running"},
		{ID: "w1-session-id", Name: "worker1", Role: "worker", Window: "lc-11223344", Status: "Running"},
	}
	_, port, mock := startTestServerWithMock(t, sessions)

	payload := map[string]string{
		"from": "w1-session-id",
		"to":   "pm-session-id",
		"type": "review_request",
		"body": "Please review my PR",
	}
	resp := msgSend(t, port, "test-token", payload)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "delivered", result["status"])

	// Verify SendKeysLiteral + SendKeys("Enter") were called.
	target := "lazyclaude:lc-aabbccdd"
	sent := mock.SentKeys[target]
	require.GreaterOrEqual(t, len(sent), 2, "SendKeysLiteral + SendKeys(Enter) expected")

	// First entry is the message text (from SendKeysLiteral).
	msg := sent[0]
	assert.Contains(t, msg, "worker1")   // sender name
	assert.Contains(t, msg, "w1-session-id") // sender ID
	assert.Contains(t, msg, "review_request") // type
	assert.Contains(t, msg, "Please review my PR") // body
}

// TestMsgSend_PushDelivery_RecipientNotFound returns 404 when recipient session is unknown.
func TestMsgSend_PushDelivery_RecipientNotFound(t *testing.T) {
	t.Parallel()

	sessions := []server.SessionInfo{
		{ID: "pm-id", Name: "pm", Role: "pm", Window: "lc-aabbccdd", Status: "Running"},
	}
	_, port, _ := startTestServerWithMock(t, sessions)

	resp := msgSend(t, port, "test-token", map[string]string{
		"from": "pm-id",
		"to":   "nonexistent-session",
		"type": "status",
		"body": "hello",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestMsgSend_PushDelivery_DeadSessionStillAttempts verifies that /msg/send
// does not reject based on status alone — it tries tmux delivery regardless.
// If the pane is truly dead, tmux send-keys will fail and return 502.
func TestMsgSend_PushDelivery_DeadSessionStillAttempts(t *testing.T) {
	t.Parallel()

	sessions := []server.SessionInfo{
		{ID: "pm-id", Name: "pm", Role: "pm", Window: "lc-aabbccdd", Status: "Running"},
		{ID: "w1-id", Name: "worker1", Role: "worker", Window: "lc-11223344", Status: "Dead"},
	}
	_, port, _ := startTestServerWithMock(t, sessions)

	resp := msgSend(t, port, "test-token", map[string]string{
		"from": "pm-id",
		"to":   "w1-id",
		"type": "review_response",
		"body": "LGTM",
	})
	defer resp.Body.Close()
	// MockClient.SendKeysLiteral succeeds → delivery succeeds even with status="Dead".
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestMsgSend_PushDelivery_PasteError returns 502 when PasteToPane fails.
func TestMsgSend_PushDelivery_PasteError(t *testing.T) {
	t.Parallel()

	sessions := []server.SessionInfo{
		{ID: "pm-id", Name: "pm", Role: "pm", Window: "lc-aabbccdd", Status: "Running"},
		{ID: "w1-id", Name: "worker1", Role: "worker", Window: "lc-11223344", Status: "Running"},
	}
	_, port, mock := startTestServerWithMock(t, sessions)
	mock.ErrSendKeys = fmt.Errorf("tmux paste failed")

	resp := msgSend(t, port, "test-token", map[string]string{
		"from": "pm-id",
		"to":   "w1-id",
		"type": "review_response",
		"body": "LGTM",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
}

// TestMsgSend_PushDelivery_MessageFormat verifies the message text format.
func TestMsgSend_PushDelivery_MessageFormat(t *testing.T) {
	t.Parallel()

	sessions := []server.SessionInfo{
		{ID: "sender-abc", Name: "my-worker", Role: "worker", Window: "lc-senderwin", Status: "Running"},
		{ID: "recv-xyz", Name: "my-pm", Role: "pm", Window: "lc-recvwin", Status: "Running"},
	}
	_, port, mock := startTestServerWithMock(t, sessions)

	resp := msgSend(t, port, "test-token", map[string]string{
		"from": "sender-abc",
		"to":   "recv-xyz",
		"type": "done",
		"body": "All tasks complete",
	})
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	sent := mock.SentKeys["lazyclaude:lc-recvwin"]
	require.GreaterOrEqual(t, len(sent), 2)
	msg := sent[0]

	// Must contain section header with sender info.
	assert.Contains(t, msg, "MESSAGE")
	assert.Contains(t, msg, "my-worker")
	assert.Contains(t, msg, "sender-abc")
	// Must contain type and body.
	assert.Contains(t, msg, "done")
	assert.Contains(t, msg, "All tasks complete")
}

// TestMsgSend_PushDelivery_NoWindowForRecipient returns 502 when recipient has no Window.
func TestMsgSend_PushDelivery_NoWindowForRecipient(t *testing.T) {
	t.Parallel()

	sessions := []server.SessionInfo{
		{ID: "sender-id", Name: "sender", Role: "worker", Window: "lc-senderwin", Status: "Running"},
		{ID: "recv-id", Name: "recv", Role: "pm", Window: "", Status: "Running"}, // no window
	}
	_, port, _ := startTestServerWithMock(t, sessions)

	resp := msgSend(t, port, "test-token", map[string]string{
		"from": "sender-id",
		"to":   "recv-id",
		"type": "status",
		"body": "hello",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
}

// --- GET /msg/sessions ---

func TestMsgSessions_returns_list(t *testing.T) {
	t.Parallel()
	sessions := []server.SessionInfo{
		{ID: "s1", Name: "main", Role: "pm", Path: "/work/main"},
		{ID: "s2", Name: "feature", Role: "worker", Path: "/work/feature"},
	}
	_, port := startTestServerWithSessions(t, sessions)

	resp := msgSessions(t, port, "test-token")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result []server.SessionInfo
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.Len(t, result, 2)
	assert.Equal(t, "s1", result[0].ID)
	assert.Equal(t, "pm", result[0].Role)
	assert.Equal(t, "s2", result[1].ID)
	assert.Equal(t, "worker", result[1].Role)
}

func TestMsgSessions_missing_auth(t *testing.T) {
	t.Parallel()
	_, port := startTestServerWithSessions(t, nil)

	resp := msgSessions(t, port, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMsgSessions_no_lister_returns_empty(t *testing.T) {
	// Not parallel: uses t.Setenv to isolate HOME so readSessionsFromState
	// cannot read the real ~/.local/share/lazyclaude/state.json.
	t.Setenv("HOME", t.TempDir())
	// Start without setting a SessionLister
	_, port, _ := startTestServer(t)

	resp := msgSessions(t, port, "test-token")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result []server.SessionInfo
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotNil(t, result)
	assert.Empty(t, result)
}
