package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/any-context/lazyclaude/internal/core/event"
	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/notify"
	"github.com/any-context/lazyclaude/internal/server"
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

// TestServer_NotifyBroker_ToolInfo_PublishesActivityRunning verifies that tool_info
// publishes an ActivityNotification with Running state (for sidebar status update).
func TestServer_NotifyBroker_ToolInfo_PublishesActivityRunning(t *testing.T) {
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

	// Should receive an ActivityNotification with Running state.
	select {
	case ev := <-sub.Ch():
		require.NotNil(t, ev.ActivityNotification, "tool_info should publish ActivityNotification")
		assert.Equal(t, model.ActivityRunning, ev.ActivityNotification.State)
		assert.Equal(t, "Write", ev.ActivityNotification.ToolName)
		assert.Equal(t, "@8", ev.ActivityNotification.Window)
		// ToolNotification should NOT be set.
		assert.Nil(t, ev.Notification, "tool_info should not publish ToolNotification")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for activity event on tool_info")
	}
}

// TestServer_NotifyBroker_TwoPhase_PublishesAfterPermission verifies the two-phase flow:
// tool_info publishes ActivityNotification (running), then permission_prompt publishes
// ToolNotification with the stored tool data.
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

	// Drain the ActivityNotification from tool_info.
	select {
	case ev := <-sub.Ch():
		require.NotNil(t, ev.ActivityNotification, "tool_info should publish ActivityNotification")
		assert.Equal(t, model.ActivityRunning, ev.ActivityNotification.State)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for activity event from tool_info")
	}

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

// TestServer_WithBroker_UsesInjectedBroker verifies that WithBroker injects
// an externally-owned broker and the server publishes events to it.
func TestServer_WithBroker_UsesInjectedBroker(t *testing.T) {
	t.Parallel()

	externalBroker := event.NewBroker[model.Event]()
	defer externalBroker.Close()

	mock := tmux.NewMockClient()
	logger := log.New(&bytes.Buffer{}, "", 0)
	tmpDir := t.TempDir()
	cfg := server.Config{
		Port:       0,
		Token:      "test-token",
		IDEDir:     filepath.Join(tmpDir, "ide"),
		RuntimeDir: filepath.Join(tmpDir, "run"),
	}

	srv := server.New(cfg, mock, logger, server.WithBroker(externalBroker))
	port, err := srv.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { srv.Stop(context.Background()) })

	// The server's broker should be the injected one.
	assert.Same(t, externalBroker, srv.NotifyBroker())

	// Subscribe and verify events arrive on the external broker.
	sub := externalBroker.Subscribe(4)
	defer sub.Cancel()

	srv.State().SetConn("c1", &server.ConnState{PID: 1111, Window: "@1"})
	body, _ := json.Marshal(map[string]any{
		"pid":       1111,
		"tool_name": "Bash",
		"input":     `{"command":"test"}`,
	})
	resp := postNotify(t, port, body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case ev := <-sub.Ch():
		require.NotNil(t, ev.Notification)
		assert.Equal(t, "Bash", ev.Notification.ToolName)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event on injected broker")
	}
}

// TestServer_WithBroker_StopDoesNotCloseBroker verifies that stopping a server
// with an injected broker does NOT close the broker, allowing GUI subscriptions
// to survive server restarts.
func TestServer_WithBroker_StopDoesNotCloseBroker(t *testing.T) {
	t.Parallel()

	externalBroker := event.NewBroker[model.Event]()
	defer externalBroker.Close()

	mock := tmux.NewMockClient()
	logger := log.New(&bytes.Buffer{}, "", 0)
	tmpDir := t.TempDir()
	cfg := server.Config{
		Port:       0,
		Token:      "test-token",
		IDEDir:     filepath.Join(tmpDir, "ide"),
		RuntimeDir: filepath.Join(tmpDir, "run"),
	}

	srv := server.New(cfg, mock, logger, server.WithBroker(externalBroker))
	_, err := srv.Start(context.Background())
	require.NoError(t, err)

	// Subscribe before stopping the server.
	sub := externalBroker.Subscribe(4)
	defer sub.Cancel()

	// Stop the server — broker must remain open.
	require.NoError(t, srv.Stop(context.Background()))

	// Verify the broker is still open by publishing an event.
	// If Close() had been called, Publish() would silently drop it.
	externalBroker.Publish(model.Event{StopNotification: &model.StopNotification{
		Window:     "@1",
		StopReason: "end_turn",
	}})

	select {
	case ev := <-sub.Ch():
		require.NotNil(t, ev.StopNotification, "broker must still deliver events after server stop")
	case <-time.After(2 * time.Second):
		t.Fatal("broker was closed by server.Stop — events not delivered")
	}
}

// TestServer_DefaultBroker_StopClosesBroker verifies that a server without
// WithBroker closes its own broker on Stop (backwards compatibility).
func TestServer_DefaultBroker_StopClosesBroker(t *testing.T) {
	t.Parallel()
	srv, _, _ := startTestServer(t)

	broker := srv.NotifyBroker()
	sub := broker.Subscribe(4)
	defer sub.Cancel()

	require.NoError(t, srv.Stop(context.Background()))

	// Channel must be closed.
	select {
	case _, ok := <-sub.Ch():
		assert.False(t, ok, "default broker channel should be closed after Stop")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out — broker channel was not closed after Stop")
	}
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

// TestServer_NotifyBroker_WritePopulatesDiffFields verifies that Write tool
// notifications extract file_path and content from input JSON into
// OldFilePath/NewContents, enabling DiffPopup routing.
func TestServer_NotifyBroker_WritePopulatesDiffFields(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 1001, Window: "@1"})

	broker := srv.NotifyBroker()
	sub := broker.Subscribe(4)
	defer sub.Cancel()

	body, _ := json.Marshal(map[string]any{
		"pid":       1001,
		"tool_name": "Write",
		"input":     `{"file_path":"/home/user/main.go","content":"package main\nfunc main() {}\n"}`,
		"cwd":       "/home/user",
	})
	resp := postNotify(t, port, body)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case ev := <-sub.Ch():
		require.NotNil(t, ev.Notification)
		assert.Equal(t, "Write", ev.Notification.ToolName)
		assert.Equal(t, "/home/user/main.go", ev.Notification.OldFilePath)
		assert.Equal(t, "package main\nfunc main() {}\n", ev.Notification.NewContents)
		assert.True(t, ev.Notification.IsDiff(), "Write notification should be a diff")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for broker event")
	}
}

// TestServer_NotifyBroker_WriteNonDiffWhenNoFilePath verifies that Write without
// file_path in input does not populate diff fields (falls back to ToolPopup).
func TestServer_NotifyBroker_WriteNonDiffWhenNoFilePath(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 1002, Window: "@2"})

	broker := srv.NotifyBroker()
	sub := broker.Subscribe(4)
	defer sub.Cancel()

	body, _ := json.Marshal(map[string]any{
		"pid":       1002,
		"tool_name": "Write",
		"input":     `{"some_other_field":"value"}`,
	})
	resp := postNotify(t, port, body)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case ev := <-sub.Ch():
		require.NotNil(t, ev.Notification)
		assert.Equal(t, "Write", ev.Notification.ToolName)
		assert.Empty(t, ev.Notification.OldFilePath, "no file_path => OldFilePath should be empty")
		assert.False(t, ev.Notification.IsDiff(), "Write without file_path should not be a diff")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for broker event")
	}
}

// TestServer_NotifyBroker_NonWriteToolNoDiffFields verifies that non-Write tools
// do not populate diff fields even if input contains file_path.
func TestServer_NotifyBroker_NonWriteToolNoDiffFields(t *testing.T) {
	t.Parallel()
	srv, port, _ := startTestServer(t)
	srv.State().SetConn("c1", &server.ConnState{PID: 1003, Window: "@3"})

	broker := srv.NotifyBroker()
	sub := broker.Subscribe(4)
	defer sub.Cancel()

	body, _ := json.Marshal(map[string]any{
		"pid":       1003,
		"tool_name": "Read",
		"input":     `{"file_path":"/home/user/main.go"}`,
	})
	resp := postNotify(t, port, body)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case ev := <-sub.Ch():
		require.NotNil(t, ev.Notification)
		assert.Equal(t, "Read", ev.Notification.ToolName)
		assert.Empty(t, ev.Notification.OldFilePath, "non-Write tool should not populate OldFilePath")
		assert.False(t, ev.Notification.IsDiff())
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for broker event")
	}
}
