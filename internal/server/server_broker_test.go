package server_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/event"
	"github.com/KEMSHlM/lazyclaude/internal/core/model"
	"github.com/KEMSHlM/lazyclaude/internal/notify"
	"github.com/KEMSHlM/lazyclaude/internal/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServer_NotifyBroker_Getter verifies that NotifyBroker() returns a non-nil broker.
func TestServer_NotifyBroker_Getter(t *testing.T) {
	t.Parallel()
	srv, _, _ := startTestServer(t)

	broker := srv.NotifyBroker()
	assert.NotNil(t, broker, "NotifyBroker() must return a non-nil broker")
}

// TestServer_NotifyBroker_PublishesOnPermissionPrompt verifies that handleNotify publishes
// a model.Event to the broker when a permission_prompt (default type) is received
// and a tool_name is present.
func TestServer_NotifyBroker_PublishesOnPermissionPrompt(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)

	// Pre-populate window mapping so handleNotify can resolve the window.
	srv.State().SetConn("c1", &server.ConnState{PID: 4321, Window: "@9"})

	// Subscribe before triggering the notification.
	broker := srv.NotifyBroker()
	sub := broker.Subscribe(4)
	defer sub.Cancel()

	body, _ := json.Marshal(map[string]any{
		"pid":       4321,
		"tool_name": "Bash",
		"input":     `{"command":"echo hello"}`,
		"cwd":       "/home/user",
	})
	resp := postNotify(t, port, body)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Expect exactly one event on the broker.
	select {
	case ev := <-sub.Ch():
		require.NotNil(t, ev.Notification)
		assert.Equal(t, "Bash", ev.Notification.ToolName)
		assert.Equal(t, `{"command":"echo hello"}`, ev.Notification.Input)
		assert.Equal(t, "/home/user", ev.Notification.CWD)
		assert.Equal(t, "@9", ev.Notification.Window)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for broker event")
	}
}

// TestServer_NotifyBroker_NoPublishOnToolInfo verifies that the broker does NOT publish
// an event when the request type is "tool_info" (phase 1 — pre-tool-use only).
func TestServer_NotifyBroker_NoPublishOnToolInfo(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 5555, Window: "@8"})

	broker := srv.NotifyBroker()
	sub := broker.Subscribe(4)
	defer sub.Cancel()

	body, _ := json.Marshal(map[string]any{
		"type":       "tool_info",
		"pid":        5555,
		"tool_name":  "Write",
		"tool_input": map[string]any{"file_path": "x.go"},
	})
	resp := postNotify(t, port, body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// The broker must NOT receive an event.
	select {
	case ev := <-sub.Ch():
		t.Fatalf("unexpected event on tool_info: %+v", ev)
	case <-time.After(100 * time.Millisecond):
		// pass — no event expected
	}
}

// TestServer_NotifyBroker_TwoPhase_PublishesAfterPermission verifies the two-phase flow:
// tool_info then permission_prompt produces exactly one event with the stored tool data.
func TestServer_NotifyBroker_TwoPhase_PublishesAfterPermission(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 6666, Window: "@6"})

	broker := srv.NotifyBroker()
	sub := broker.Subscribe(4)
	defer sub.Cancel()

	// Phase 1: tool_info
	body1, _ := json.Marshal(map[string]any{
		"type":       "tool_info",
		"pid":        6666,
		"tool_name":  "Edit",
		"tool_input": map[string]any{"file_path": "main.go"},
		"cwd":        "/src",
	})
	resp1 := postNotify(t, port, body1)
	resp1.Body.Close()
	require.Equal(t, http.StatusOK, resp1.StatusCode)

	// Phase 2: permission_prompt
	body2, _ := json.Marshal(map[string]any{
		"pid":     6666,
		"message": "Allow Edit on main.go?",
	})
	resp2 := postNotify(t, port, body2)
	resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	select {
	case ev := <-sub.Ch():
		require.NotNil(t, ev.Notification)
		assert.Equal(t, "Edit", ev.Notification.ToolName)
		assert.Contains(t, ev.Notification.Input, "main.go")
		assert.Equal(t, "@6", ev.Notification.Window)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for broker event after two-phase notify")
	}
}

// TestServer_NotifyBroker_MultipleSubscribers verifies that all subscribers receive
// the event (fan-out), exercising the broker's fan-out semantics.
func TestServer_NotifyBroker_MultipleSubscribers(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 7654, Window: "@7"})

	broker := srv.NotifyBroker()
	sub1 := broker.Subscribe(4)
	defer sub1.Cancel()
	sub2 := broker.Subscribe(4)
	defer sub2.Cancel()

	body, _ := json.Marshal(map[string]any{
		"pid":       7654,
		"tool_name": "Bash",
		"input":     `{"command":"pwd"}`,
	})
	resp := postNotify(t, port, body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Both subscribers must receive the event.
	for i, sub := range []*event.Subscription[model.Event]{sub1, sub2} {
		select {
		case ev := <-sub.Ch():
			require.NotNil(t, ev.Notification, "subscriber %d notification must not be nil", i+1)
			assert.Equal(t, "Bash", ev.Notification.ToolName)
		case <-time.After(2 * time.Second):
			t.Fatalf("subscriber %d timed out waiting for event", i+1)
		}
	}
}

// TestServer_NotifyBroker_FileQueueSkippedWhenSubscribed verifies that
// when a broker subscriber exists, the file queue is NOT written
// (single-path dispatch: broker handles delivery).
func TestServer_NotifyBroker_FileQueueSkippedWhenSubscribed(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 8765, Window: "@5"})

	broker := srv.NotifyBroker()
	sub := broker.Subscribe(4)
	defer sub.Cancel()

	body, _ := json.Marshal(map[string]any{
		"pid":       8765,
		"tool_name": "Write",
		"input":     `{"file_path":"out.txt"}`,
	})
	resp := postNotify(t, port, body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Broker event arrives.
	select {
	case ev := <-sub.Ch():
		assert.Equal(t, "Write", ev.Notification.ToolName)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for broker event")
	}

	// File queue must NOT be written when broker has subscribers.
	ns, err := notify.ReadAll(srv.RuntimeDir())
	require.NoError(t, err)
	assert.Empty(t, ns, "file queue should be empty when broker has subscribers")
}

// TestServer_NotifyBroker_FileQueueWrittenWhenNoSubscriber verifies that
// when no broker subscriber exists, the file queue IS written (daemon mode).
func TestServer_NotifyBroker_FileQueueWrittenWhenNoSubscriber(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 8766, Window: "@6"})

	// No subscriber — simulates daemon mode.
	body, _ := json.Marshal(map[string]any{
		"pid":       8766,
		"tool_name": "Bash",
		"input":     `{"command":"ls"}`,
	})
	resp := postNotify(t, port, body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// File queue must be written when no subscriber exists.
	ns, err := notify.ReadAll(srv.RuntimeDir())
	require.NoError(t, err)
	require.Len(t, ns, 1, "file queue must be written when no subscriber")
	assert.Equal(t, "Bash", ns[0].ToolName)
}
