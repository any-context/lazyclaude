package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/any-context/lazyclaude/internal/core/model"
)

// --- Mock ConnectionManager ---

type mockConnManager struct {
	mu     sync.Mutex
	state  ConnectionState
	client ClientAPI
	err    error
}

func (m *mockConnManager) Connect(_ context.Context) error { return nil }
func (m *mockConnManager) Disconnect() error               { return nil }
func (m *mockConnManager) Host() string                    { return "mock-host" }

func (m *mockConnManager) State() ConnectionState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *mockConnManager) Client() (ClientAPI, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return m.client, nil
}

func (m *mockConnManager) OnStateChange(_ func(ConnectionState)) {}

func (m *mockConnManager) setState(s ConnectionState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s
}

// --- Interface compliance ---

func TestRemoteProvider_ImplementsSessionProvider(t *testing.T) {
	// Compile-time check via var _ in remote_provider.go.
	// This test verifies the same at runtime.
	var p SessionProvider = &RemoteProvider{}
	if p == nil {
		t.Fatal("nil provider")
	}
}

// --- Unit tests with mock daemon server ---

func newRemoteTestSetup(t *testing.T, handlers map[string]http.HandlerFunc) (*RemoteProvider, *httptest.Server) {
	t.Helper()
	srv := newClientTestServer(t, handlers)
	client := NewHTTPClient(srv.URL, "test-token")
	conn := &mockConnManager{state: Connected, client: client}
	rp := NewRemoteProvider("remote-host", conn)
	return rp, srv
}

func TestRemoteProvider_Host(t *testing.T) {
	conn := &mockConnManager{state: Connected}
	rp := NewRemoteProvider("my-host:22", conn)
	if rp.Host() != "my-host:22" {
		t.Errorf("got host=%q", rp.Host())
	}
}

func TestRemoteProvider_ConnectionState(t *testing.T) {
	conn := &mockConnManager{state: Disconnected}
	rp := NewRemoteProvider("host", conn)
	if rp.ConnectionState() != Disconnected {
		t.Errorf("got state=%v", rp.ConnectionState())
	}
	conn.setState(Connected)
	if rp.ConnectionState() != Connected {
		t.Errorf("got state=%v", rp.ConnectionState())
	}
}

func TestRemoteProvider_Sessions(t *testing.T) {
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"GET /sessions": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, SessionListResponse{
				Sessions: []SessionInfo{
					{ID: "s1", Name: "session-1"},
					{ID: "s2", Name: "session-2"},
				},
			})
		},
	})
	defer srv.Close()

	sessions, err := rp.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions", len(sessions))
	}
	// Sessions should be tagged with the remote host.
	for _, s := range sessions {
		if s.Host != "remote-host" {
			t.Errorf("session %s host=%q, want remote-host", s.ID, s.Host)
		}
	}
}

func TestRemoteProvider_HasSession(t *testing.T) {
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"GET /sessions": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, SessionListResponse{
				Sessions: []SessionInfo{{ID: "s1", Name: "test"}},
			})
		},
	})
	defer srv.Close()

	// Before fetching, cache is empty.
	if rp.HasSession("s1") {
		t.Error("should not have session before fetching")
	}

	// Fetch sessions to populate cache.
	_, _ = rp.Sessions()

	if !rp.HasSession("s1") {
		t.Error("should have session s1 after fetch")
	}
	if rp.HasSession("s2") {
		t.Error("should not have session s2")
	}
}

func TestRemoteProvider_Create(t *testing.T) {
	var gotReq SessionCreateRequest
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"POST /session/create": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&gotReq)
			testWriteJSON(w, SessionCreateResponse{ID: "new1"})
		},
	})
	defer srv.Close()

	if err := rp.Create("/home/user/project"); err != nil {
		t.Fatal(err)
	}
	if gotReq.Path != "/home/user/project" {
		t.Errorf("got path=%q", gotReq.Path)
	}
	if gotReq.SessionType != "plain" {
		t.Errorf("got type=%q", gotReq.SessionType)
	}
}

func TestRemoteProvider_Delete(t *testing.T) {
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"DELETE /session/s1": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})
	defer srv.Close()

	if err := rp.Delete("s1"); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteProvider_Rename(t *testing.T) {
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"POST /session/s1/rename": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})
	defer srv.Close()

	if err := rp.Rename("s1", "new-name"); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteProvider_PurgeOrphans(t *testing.T) {
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"POST /sessions/purge": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, map[string]int{"purged": 2})
		},
	})
	defer srv.Close()

	n, err := rp.PurgeOrphans()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("got %d, want 2", n)
	}
}

func TestRemoteProvider_CapturePreview(t *testing.T) {
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"GET /session/s1/preview": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, PreviewResponse{Content: "preview-content", CursorX: 10})
		},
	})
	defer srv.Close()

	resp, err := rp.CapturePreview("s1", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "preview-content" {
		t.Errorf("got content=%q", resp.Content)
	}
}

func TestRemoteProvider_HistorySize(t *testing.T) {
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"GET /session/s1/history-size": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, HistorySizeResponse{Lines: 1000})
		},
	})
	defer srv.Close()

	lines, err := rp.HistorySize("s1")
	if err != nil {
		t.Fatal(err)
	}
	if lines != 1000 {
		t.Errorf("got %d, want 1000", lines)
	}
}

func TestRemoteProvider_SendChoice(t *testing.T) {
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"POST /session/choice": func(w http.ResponseWriter, r *http.Request) {
			var req SendChoiceRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.Window != "@1" {
				t.Errorf("got window=%q, want @1", req.Window)
			}
			w.WriteHeader(http.StatusOK)
		},
	})
	defer srv.Close()

	if err := rp.SendChoice("@1", 1); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteProvider_CaptureScrollback(t *testing.T) {
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"GET /session/s1/scrollback": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, ScrollbackResponse{Content: "scroll", CursorX: 0, CursorY: 10})
		},
	})
	defer srv.Close()

	resp, err := rp.CaptureScrollback("s1", 80, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "scroll" {
		t.Errorf("got content=%q", resp.Content)
	}
}

func TestRemoteProvider_ResumeWorktree(t *testing.T) {
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"POST /worktree/resume": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, WorktreeResumeResponse{SessionID: "wt-resume"})
		},
	})
	defer srv.Close()

	if err := rp.ResumeWorktree("/tmp/wt", "continue", "/project"); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteProvider_CreateWorktree(t *testing.T) {
	var gotReq WorktreeCreateRequest
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"POST /worktree/create": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&gotReq)
			testWriteJSON(w, WorktreeCreateResponse{SessionID: "wt1"})
		},
	})
	defer srv.Close()

	if err := rp.CreateWorktree("feature-x", "do stuff", "/project"); err != nil {
		t.Fatal(err)
	}
	if gotReq.Name != "feature-x" {
		t.Errorf("got name=%q", gotReq.Name)
	}
}

func TestRemoteProvider_ListWorktrees(t *testing.T) {
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"GET /worktrees": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, WorktreeListResponse{
				Worktrees: []WorktreeInfo{{Name: "wt1", Path: "/tmp/wt1"}},
			})
		},
	})
	defer srv.Close()

	wts, err := rp.ListWorktrees("/project")
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 1 || wts[0].Name != "wt1" {
		t.Errorf("unexpected: %v", wts)
	}
}

func TestRemoteProvider_CreatePMSession(t *testing.T) {
	var gotReq SessionCreateRequest
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"POST /session/create": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&gotReq)
			testWriteJSON(w, SessionCreateResponse{ID: "pm1"})
		},
	})
	defer srv.Close()

	if err := rp.CreatePMSession("/project"); err != nil {
		t.Fatal(err)
	}
	if gotReq.SessionType != "pm" {
		t.Errorf("got type=%q, want pm", gotReq.SessionType)
	}
}

func TestRemoteProvider_CreateWorkerSession(t *testing.T) {
	var gotReq SessionCreateRequest
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"POST /session/create": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&gotReq)
			testWriteJSON(w, SessionCreateResponse{ID: "w1"})
		},
	})
	defer srv.Close()

	if err := rp.CreateWorkerSession("worker-1", "do task", "/project"); err != nil {
		t.Fatal(err)
	}
	if gotReq.SessionType != "worker" {
		t.Errorf("got type=%q, want worker", gotReq.SessionType)
	}
	if gotReq.Name != "worker-1" {
		t.Errorf("got name=%q", gotReq.Name)
	}
}

func TestRemoteProvider_ConnectionError(t *testing.T) {
	conn := &mockConnManager{
		state: Disconnected,
		err:   fmt.Errorf("not connected"),
	}
	rp := NewRemoteProvider("host", conn)

	_, err := rp.Sessions()
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- SSE / Notification tests ---

func TestRemoteProvider_HandleSSEEvent_Activity(t *testing.T) {
	conn := &mockConnManager{state: Connected}
	rp := NewRemoteProvider("host", conn)

	// Pre-populate session cache.
	rp.mu.Lock()
	rp.sessions = []SessionInfo{
		{ID: "s1", Name: "test", Activity: 0},
	}
	rp.mu.Unlock()

	rp.handleSSEEvent(NotificationEvent{
		Type:      EventActivity,
		SessionID: "s1",
		Activity:  3, // e.g. Idle
		ToolName:  "Read",
	})

	rp.mu.Lock()
	defer rp.mu.Unlock()
	if rp.sessions[0].Activity != 3 {
		t.Errorf("got activity=%d, want 3", rp.sessions[0].Activity)
	}
	if rp.sessions[0].ToolName != "Read" {
		t.Errorf("got tool=%q, want Read", rp.sessions[0].ToolName)
	}
}

func TestRemoteProvider_HandleSSEEvent_ToolInfo(t *testing.T) {
	conn := &mockConnManager{state: Connected}
	rp := NewRemoteProvider("host", conn)

	rp.handleSSEEvent(NotificationEvent{
		Type: EventToolInfo,
		ToolNotification: &model.ToolNotification{
			ToolName: "Edit",
			Window:   "@1",
		},
	})

	notifs := rp.PendingNotifications()
	if len(notifs) != 1 {
		t.Fatalf("got %d notifications, want 1", len(notifs))
	}
	if notifs[0].ToolName != "Edit" {
		t.Errorf("got tool=%q", notifs[0].ToolName)
	}

	// Second call should be empty (cleared).
	notifs = rp.PendingNotifications()
	if len(notifs) != 0 {
		t.Errorf("got %d notifications after clear, want 0", len(notifs))
	}
}

func TestRemoteProvider_HandleSSEEvent_FullSync(t *testing.T) {
	conn := &mockConnManager{state: Connected}
	rp := NewRemoteProvider("remote", conn)

	rp.handleSSEEvent(NotificationEvent{
		Type: EventFullSync,
		Sessions: []SessionInfo{
			{ID: "s1", Name: "synced-1"},
			{ID: "s2", Name: "synced-2"},
		},
	})

	rp.mu.Lock()
	defer rp.mu.Unlock()
	if len(rp.sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(rp.sessions))
	}
	// Should be tagged with host.
	for _, s := range rp.sessions {
		if s.Host != "remote" {
			t.Errorf("session %s host=%q, want remote", s.ID, s.Host)
		}
	}
}

func TestRemoteProvider_PendingNotifications_Empty(t *testing.T) {
	conn := &mockConnManager{state: Connected}
	rp := NewRemoteProvider("host", conn)
	notifs := rp.PendingNotifications()
	if notifs != nil {
		t.Errorf("expected nil, got %v", notifs)
	}
}

func TestRemoteProvider_StartSSE(t *testing.T) {
	// Create an SSE server that sends one event then closes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/notifications":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, ok := w.(http.Flusher)
			if !ok {
				return
			}
			ev, _ := json.Marshal(NotificationEvent{
				Type: EventToolInfo,
				ToolNotification: &model.ToolNotification{
					ToolName: "Write",
					Window:   "@2",
				},
			})
			fmt.Fprintf(w, "event:tool_info\ndata:%s\n\n", ev)
			flusher.Flush()
			// Keep connection open briefly so consumer can read.
			time.Sleep(100 * time.Millisecond)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewHTTPClient(srv.URL, "")
	conn := &mockConnManager{state: Connected, client: client}
	rp := NewRemoteProvider("host", conn)

	if err := rp.StartSSE(); err != nil {
		t.Fatal(err)
	}
	defer rp.StopSSE()

	// Poll for the notification to arrive instead of sleeping.
	deadline := time.After(2 * time.Second)
	var notifs []*model.ToolNotification
	for {
		notifs = rp.PendingNotifications()
		if len(notifs) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for SSE notification")
		case <-time.After(10 * time.Millisecond):
		}
	}

	if len(notifs) != 1 {
		t.Fatalf("got %d notifications, want 1", len(notifs))
	}
	if notifs[0].ToolName != "Write" {
		t.Errorf("got tool=%q", notifs[0].ToolName)
	}
}

// --- SSE parser tests ---

func TestParseSSEStream_MultipleEvents(t *testing.T) {
	sseData := ""
	ev1, _ := json.Marshal(NotificationEvent{Type: EventActivity, SessionID: "s1", Activity: 1})
	sseData += fmt.Sprintf("event:activity\ndata:%s\n\n", ev1)

	ev2, _ := json.Marshal(NotificationEvent{Type: EventToolInfo})
	sseData += fmt.Sprintf("data:%s\n\n", ev2)

	r := nopReadCloser{strings.NewReader(sseData)}
	ch := make(chan NotificationEvent, 10)
	ctx := context.Background()

	parseSSEStream(ctx, r, ch)

	var events []NotificationEvent
	for ev := range ch {
		events = append(events, ev)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Type != EventActivity {
		t.Errorf("event[0] type=%q", events[0].Type)
	}
}

func TestParseSSEStream_IgnoresComments(t *testing.T) {
	ev, _ := json.Marshal(NotificationEvent{Type: EventActivity, SessionID: "s1"})
	sseData := fmt.Sprintf(": this is a comment\nevent:activity\ndata:%s\n\n", ev)

	r := nopReadCloser{strings.NewReader(sseData)}
	ch := make(chan NotificationEvent, 10)
	parseSSEStream(context.Background(), r, ch)

	var events []NotificationEvent
	for ev := range ch {
		events = append(events, ev)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}

func TestParseSSEStream_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	// Provide data that would block if context not checked.
	r := nopReadCloser{strings.NewReader("event:activity\ndata:{}\n\n")}
	ch := make(chan NotificationEvent, 10)
	parseSSEStream(ctx, r, ch)

	// Channel should be closed without delivering events.
	count := 0
	for range ch {
		count++
	}
	// May deliver 0 or 1 depending on timing; just ensure it terminates.
	if count > 1 {
		t.Errorf("got %d events after cancel, want <= 1", count)
	}
}

func TestBuildTmuxAttachCommand(t *testing.T) {
	cmd := buildTmuxAttachCommand("lazyclaude:lc-abcd1234")
	if !strings.Contains(cmd, "attach-session") {
		t.Errorf("missing attach-session in: %s", cmd)
	}
	if !strings.Contains(cmd, "lazyclaude:lc-abcd1234") {
		t.Errorf("missing target in: %s", cmd)
	}
}

// --- Helpers ---

type nopReadCloser struct {
	*strings.Reader
}

func (nopReadCloser) Close() error { return nil }
