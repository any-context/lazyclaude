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

func TestRemoteProvider_Create_DoesNotCallPostCreateHook(t *testing.T) {
	hookCalled := false
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /session/create": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, SessionCreateResponse{ID: "new1"})
		},
	})
	defer srv.Close()

	client := NewHTTPClient(srv.URL, "test-token")
	conn := &mockConnManager{state: Connected, client: client}
	rp := NewRemoteProvider("host", conn, WithPostCreate(func(_, _ string, _ *SessionCreateResponse) error {
		hookCalled = true
		return nil
	}))

	if err := rp.Create("/project"); err != nil {
		t.Fatal(err)
	}
	if hookCalled {
		t.Fatal("PostCreateHook must NOT be called from Create (mirror setup is caller's responsibility)")
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

func TestRemoteProvider_PostCreateHook_CalledOnCreateWorktree(t *testing.T) {
	var hookCalled bool
	var gotHost, gotPath string
	var gotResp *SessionCreateResponse

	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /worktree/create": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, WorktreeCreateResponse{SessionID: "wt1", Path: "/project/.lazyclaude/worktrees/feat", TmuxWindow: "lc-wt1", Role: "worker"})
		},
	})
	defer srv.Close()

	client := NewHTTPClient(srv.URL, "test-token")
	conn := &mockConnManager{state: Connected, client: client}
	rp := NewRemoteProvider("remote-host", conn, WithPostCreate(func(host, path string, resp *SessionCreateResponse) error {
		hookCalled = true
		gotHost = host
		gotPath = path
		gotResp = resp
		return nil
	}))

	if err := rp.CreateWorktree("feat", "do stuff", "/project"); err != nil {
		t.Fatal(err)
	}
	if !hookCalled {
		t.Fatal("PostCreateHook was not called")
	}
	if gotHost != "remote-host" {
		t.Errorf("hook host=%q, want remote-host", gotHost)
	}
	if gotPath != "/project" {
		t.Errorf("hook path=%q, want /project (projectRoot, not resp.Path)", gotPath)
	}
	if gotResp == nil || gotResp.ID != "wt1" {
		t.Errorf("hook resp=%v, want ID=wt1", gotResp)
	}
	if gotResp != nil && gotResp.Role != "worker" {
		t.Errorf("hook resp.Role=%q, want worker", gotResp.Role)
	}
}

func TestRemoteProvider_PostCreateHook_CalledOnCreatePMSession(t *testing.T) {
	var hookCalled bool
	var gotPath string
	var gotResp *SessionCreateResponse
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /session/create": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, SessionCreateResponse{ID: "pm1", Path: "/project", TmuxWindow: "lc-pm1", Role: "pm"})
		},
	})
	defer srv.Close()

	client := NewHTTPClient(srv.URL, "test-token")
	conn := &mockConnManager{state: Connected, client: client}
	rp := NewRemoteProvider("host", conn, WithPostCreate(func(_, path string, resp *SessionCreateResponse) error {
		hookCalled = true
		gotPath = path
		gotResp = resp
		return nil
	}))

	if err := rp.CreatePMSession("/project"); err != nil {
		t.Fatal(err)
	}
	if !hookCalled {
		t.Fatal("PostCreateHook was not called for CreatePMSession")
	}
	if gotPath != "/project" {
		t.Errorf("hook path=%q, want /project", gotPath)
	}
	if gotResp != nil && gotResp.Role != "pm" {
		t.Errorf("hook resp.Role=%q, want pm", gotResp.Role)
	}
}

func TestRemoteProvider_PostCreateHook_CalledOnResumeWorktree(t *testing.T) {
	var hookCalled bool
	var gotHost, gotPath string
	var gotResp *SessionCreateResponse
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /worktree/resume": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, WorktreeResumeResponse{SessionID: "wt-resume", Name: "feat", Path: "/tmp/wt", TmuxWindow: "lc-resume", Role: "worker"})
		},
	})
	defer srv.Close()

	client := NewHTTPClient(srv.URL, "test-token")
	conn := &mockConnManager{state: Connected, client: client}
	rp := NewRemoteProvider("remote-host", conn, WithPostCreate(func(host, path string, resp *SessionCreateResponse) error {
		hookCalled = true
		gotHost = host
		gotPath = path
		gotResp = resp
		return nil
	}))

	if err := rp.ResumeWorktree("/tmp/wt", "continue", "/project"); err != nil {
		t.Fatal(err)
	}
	if !hookCalled {
		t.Fatal("PostCreateHook was not called for ResumeWorktree")
	}
	if gotHost != "remote-host" {
		t.Errorf("hook host=%q, want remote-host", gotHost)
	}
	if gotPath != "/project" {
		t.Errorf("hook path=%q, want /project (projectRoot, not resp.Path)", gotPath)
	}
	if gotResp != nil && gotResp.Role != "worker" {
		t.Errorf("hook resp.Role=%q, want worker", gotResp.Role)
	}
}

func TestRemoteProvider_PostCreateHook_CalledOnCreateWorkerSession(t *testing.T) {
	var hookCalled bool
	var gotPath string
	var gotResp *SessionCreateResponse
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /session/create": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, SessionCreateResponse{ID: "w1", Path: "/project", TmuxWindow: "lc-w1", Role: "worker"})
		},
	})
	defer srv.Close()

	client := NewHTTPClient(srv.URL, "test-token")
	conn := &mockConnManager{state: Connected, client: client}
	rp := NewRemoteProvider("host", conn, WithPostCreate(func(_, path string, resp *SessionCreateResponse) error {
		hookCalled = true
		gotPath = path
		gotResp = resp
		return nil
	}))

	if err := rp.CreateWorkerSession("w1", "task", "/project"); err != nil {
		t.Fatal(err)
	}
	if !hookCalled {
		t.Fatal("PostCreateHook was not called for CreateWorkerSession")
	}
	if gotPath != "/project" {
		t.Errorf("hook path=%q, want /project", gotPath)
	}
	if gotResp != nil && gotResp.Role != "worker" {
		t.Errorf("hook resp.Role=%q, want worker", gotResp.Role)
	}
}

func TestRemoteProvider_PostCreateHook_ErrorPropagated(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /session/create": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, SessionCreateResponse{ID: "w1"})
		},
	})
	defer srv.Close()

	client := NewHTTPClient(srv.URL, "test-token")
	conn := &mockConnManager{state: Connected, client: client}
	rp := NewRemoteProvider("host", conn, WithPostCreate(func(_, _ string, _ *SessionCreateResponse) error {
		return fmt.Errorf("mirror setup failed")
	}))

	err := rp.CreateWorkerSession("w1", "task", "/project")
	if err == nil {
		t.Fatal("expected error from PostCreateHook")
	}
	if !strings.Contains(err.Error(), "mirror setup failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRemoteProvider_NoHook_NoError(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /worktree/resume": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, WorktreeResumeResponse{SessionID: "wt-resume"})
		},
	})
	defer srv.Close()

	client := NewHTTPClient(srv.URL, "test-token")
	conn := &mockConnManager{state: Connected, client: client}
	// No WithPostCreate — hook is nil.
	rp := NewRemoteProvider("host", conn)

	if err := rp.ResumeWorktree("/tmp/wt", "continue", "/project"); err != nil {
		t.Fatalf("unexpected error without hook: %v", err)
	}
}

func TestRemoteProvider_CaptureScrollback(t *testing.T) {
	var gotReq ScrollbackRequest
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"POST /session/s1/scrollback": func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
				t.Fatalf("decode: %v", err)
			}
			testWriteJSON(w, ScrollbackResponse{Content: "remote-body"})
		},
	})
	defer srv.Close()

	resp, err := rp.CaptureScrollback("s1", 120, 10, 50)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "remote-body" {
		t.Errorf("content=%q, want remote-body", resp.Content)
	}
	if gotReq.ID != "s1" || gotReq.Width != 120 || gotReq.StartLine != 10 || gotReq.EndLine != 50 {
		t.Errorf("got request=%+v", gotReq)
	}
}

func TestRemoteProvider_CaptureScrollback_NoClient(t *testing.T) {
	conn := &mockConnManager{state: Disconnected, err: fmt.Errorf("not connected")}
	rp := NewRemoteProvider("host", conn)
	if _, err := rp.CaptureScrollback("s1", 80, 0, 10); err == nil {
		t.Fatal("expected error when client unavailable")
	}
}

func TestRemoteProvider_HistorySize(t *testing.T) {
	rp, srv := newRemoteTestSetup(t, map[string]http.HandlerFunc{
		"GET /session/s1/history-size": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, HistorySizeResponse{Lines: 987})
		},
	})
	defer srv.Close()

	n, err := rp.HistorySize("s1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 987 {
		t.Errorf("lines=%d, want 987", n)
	}
}

func TestRemoteProvider_HistorySize_NoClient(t *testing.T) {
	conn := &mockConnManager{state: Disconnected, err: fmt.Errorf("not connected")}
	rp := NewRemoteProvider("host", conn)
	if _, err := rp.HistorySize("s1"); err == nil {
		t.Fatal("expected error when client unavailable")
	}
}

func TestRemoteProvider_LocalSessionHost(t *testing.T) {
	conn := &mockConnManager{state: Connected}
	rp := NewRemoteProvider("host-A", conn)

	// Unknown id → (, false).
	if host, ok := rp.LocalSessionHost("nope"); ok || host != "" {
		t.Errorf("nope: host=%q, ok=%v", host, ok)
	}

	// Seed the cache with a session.
	rp.mu.Lock()
	rp.sessions = []SessionInfo{{ID: "s1", Name: "cached"}}
	rp.mu.Unlock()

	host, ok := rp.LocalSessionHost("s1")
	if !ok {
		t.Fatal("s1: want ok=true")
	}
	if host != "host-A" {
		t.Errorf("host=%q, want host-A", host)
	}
}

// CapturePreview is intentionally still a stub; the mirror window path
// handles remote previews. If this test starts failing it means someone
// re-routed preview capture without updating the plan.
func TestRemoteProvider_CapturePreview_StillStubbed(t *testing.T) {
	conn := &mockConnManager{state: Connected}
	rp := NewRemoteProvider("host", conn)
	if _, err := rp.CapturePreview("s1", 80, 24); err == nil {
		t.Fatal("CapturePreview should still return an error (mirror path)")
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

func TestRemoteProvider_SSEActivity_PassesSessionID(t *testing.T) {
	// handleSSEEvent for EventActivity must call the callback with the
	// session ID so that root.go's activityFwd can resolve the local
	// mirror session's current tmux window ID.
	//
	// Production path: the daemon carries an 8-char session hint in
	// NotificationEvent.SessionID (see windowToSessionHint), and
	// handleSSEEvent matches it against the cached full UUID using the
	// HasPrefix branch. We simulate that asymmetry here to ensure the
	// callback receives the *expanded* full ID (which root.go needs to
	// look up the local mirror via store.FindByID).
	var gotSessionID string
	var gotEvent model.Event
	var cbCalls int

	conn := &mockConnManager{state: Connected}
	rp := NewRemoteProvider("remote-host", conn, WithSSEActivity(func(ev model.Event, sessionID string) {
		cbCalls++
		gotEvent = ev
		gotSessionID = sessionID
	}))

	const fullID = "sess1234-abcd-ef01-2345-6789abcdef01"
	const hintID = "sess1234" // 8-char prefix hint as the daemon emits
	rp.mu.Lock()
	rp.sessions = []SessionInfo{
		{ID: fullID, Host: "remote-host", TmuxWindow: "lc-sess1234"},
	}
	rp.mu.Unlock()

	rp.handleSSEEvent(NotificationEvent{
		Type:      EventActivity,
		SessionID: hintID,
		Activity:  model.ActivityRunning,
		ToolName:  "Bash",
	})

	if cbCalls != 1 {
		t.Fatalf("callback invocations = %d, want 1", cbCalls)
	}
	// The callback must receive the cached full UUID, not the incoming
	// 8-char hint, so that root.go's store.FindByID can resolve the
	// local mirror session.
	if gotSessionID != fullID {
		t.Errorf("gotSessionID = %q, want %q (full cached ID, not hint)", gotSessionID, fullID)
	}
	if gotEvent.ActivityNotification == nil {
		t.Fatal("ActivityNotification is nil")
	}
	if gotEvent.ActivityNotification.State != model.ActivityRunning {
		t.Errorf("State = %v, want Running", gotEvent.ActivityNotification.State)
	}
	if gotEvent.ActivityNotification.ToolName != "Bash" {
		t.Errorf("ToolName = %q, want Bash", gotEvent.ActivityNotification.ToolName)
	}
	// Best-effort Window is the remapped mirror name ("rm-" prefix).
	// root.go's callback overrides this with the local tmux window ID.
	if gotEvent.ActivityNotification.Window != "rm-sess1234" {
		t.Errorf("Window = %q, want rm-sess1234", gotEvent.ActivityNotification.Window)
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

func TestRemoteProvider_SSEToolInfo_Callback_Rewrites(t *testing.T) {
	// handleSSEEvent for EventToolInfo must invoke the registered
	// SSEToolInfoCallback with the authoritative SessionID before
	// buffering the notification, so root.go can rewrite Window to
	// the local mirror tmux window ID. The Window rewrite must be
	// reflected in the notification that is ultimately returned from
	// PendingNotifications (mutation in place is intentional — SSE
	// pushes a fresh instance per event).
	var (
		gotSessionID string
		gotNotif     *model.ToolNotification
		cbCalls      int
	)

	conn := &mockConnManager{state: Connected}
	rp := NewRemoteProvider("remote-host", conn, WithSSEToolInfo(func(n *model.ToolNotification, sessionID string) {
		cbCalls++
		gotSessionID = sessionID
		gotNotif = n
		// Simulate root.go's lookup rewrite.
		n.Window = "@42"
	}))

	rp.handleSSEEvent(NotificationEvent{
		Type:      EventToolInfo,
		SessionID: "sess-123",
		ToolNotification: &model.ToolNotification{
			ToolName: "Edit",
			Window:   "@22", // remote tmux window ID
		},
	})

	if cbCalls != 1 {
		t.Fatalf("callback invocations = %d, want 1", cbCalls)
	}
	if gotSessionID != "sess-123" {
		t.Errorf("gotSessionID = %q, want sess-123", gotSessionID)
	}
	if gotNotif == nil {
		t.Fatal("callback received nil notification")
	}

	notifs := rp.PendingNotifications()
	if len(notifs) != 1 {
		t.Fatalf("got %d buffered notifications, want 1", len(notifs))
	}
	if notifs[0].Window != "@42" {
		t.Errorf("buffered Window = %q, want @42 (callback rewrite)", notifs[0].Window)
	}
	if notifs[0].ToolName != "Edit" {
		t.Errorf("buffered ToolName = %q, want Edit", notifs[0].ToolName)
	}
}

func TestRemoteProvider_SSEToolInfo_EmptySessionID_NoRewrite(t *testing.T) {
	// With an empty SessionID (old daemon without Phase B wire
	// format), the callback is still invoked so root.go can decide
	// what to do. The production callback is a no-op in that case,
	// which we simulate here. The notification must be buffered with
	// its original Window untouched so behavior degrades to the
	// pre-fix (popup visible, action not routed).
	var cbCalls int
	conn := &mockConnManager{state: Connected}
	rp := NewRemoteProvider("remote-host", conn, WithSSEToolInfo(func(n *model.ToolNotification, sessionID string) {
		cbCalls++
		// Production guard: bail out on empty sessionID or nil n.
		if sessionID == "" || n == nil {
			return
		}
		n.Window = "SHOULD NOT HAPPEN"
	}))

	rp.handleSSEEvent(NotificationEvent{
		Type:      EventToolInfo,
		SessionID: "",
		ToolNotification: &model.ToolNotification{
			ToolName: "Read",
			Window:   "@22",
		},
	})

	if cbCalls != 1 {
		t.Fatalf("callback invocations = %d, want 1", cbCalls)
	}
	notifs := rp.PendingNotifications()
	if len(notifs) != 1 {
		t.Fatalf("got %d notifications, want 1", len(notifs))
	}
	if notifs[0].Window != "@22" {
		t.Errorf("Window = %q, want @22 (untouched)", notifs[0].Window)
	}
}

func TestRemoteProvider_SSEToolInfo_NoCallback_Passthrough(t *testing.T) {
	// With no callback registered, handleSSEEvent must still buffer
	// the ToolNotification so the legacy path (file polling fallback
	// or local mirror setups) is not regressed.
	conn := &mockConnManager{state: Connected}
	rp := NewRemoteProvider("remote-host", conn)

	rp.handleSSEEvent(NotificationEvent{
		Type:      EventToolInfo,
		SessionID: "sess-999",
		ToolNotification: &model.ToolNotification{
			ToolName: "Write",
			Window:   "@7",
		},
	})

	notifs := rp.PendingNotifications()
	if len(notifs) != 1 {
		t.Fatalf("got %d notifications, want 1", len(notifs))
	}
	if notifs[0].Window != "@7" {
		t.Errorf("Window = %q, want @7 (passthrough)", notifs[0].Window)
	}
	if notifs[0].ToolName != "Write" {
		t.Errorf("ToolName = %q, want Write", notifs[0].ToolName)
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
	t.Run("session:window format", func(t *testing.T) {
		cmd := buildTmuxAttachCommand("lazyclaude:lc-abcd1234")
		if !strings.Contains(cmd, "new-session -t lazyclaude") {
			t.Errorf("missing grouped new-session in: %s", cmd)
		}
		if !strings.Contains(cmd, "select-window") {
			t.Errorf("missing select-window in: %s", cmd)
		}
		if !strings.Contains(cmd, "lc-abcd1234") {
			t.Errorf("missing window target in: %s", cmd)
		}
		if !strings.Contains(cmd, "destroy-unattached") {
			t.Errorf("missing destroy-unattached in: %s", cmd)
		}
	})

	t.Run("bare window name without colon", func(t *testing.T) {
		cmd := buildTmuxAttachCommand("lc-bare")
		if !strings.Contains(cmd, "new-session -t lazyclaude") {
			t.Errorf("missing grouped new-session in: %s", cmd)
		}
		if !strings.Contains(cmd, "select-window") {
			t.Errorf("missing select-window in: %s", cmd)
		}
		if !strings.Contains(cmd, "'lc-bare'") {
			t.Errorf("missing bare window target in: %s", cmd)
		}
	})
}

// --- Helpers ---

type nopReadCloser struct {
	*strings.Reader
}

func (nopReadCloser) Close() error { return nil }
