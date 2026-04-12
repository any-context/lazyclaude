package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/session"
)

func TestSSE_FullSyncOnConnect(t *testing.T) {
	_, ts, _ := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/notifications", nil)
	req.Header.Set(AuthHeader, testToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("want text/event-stream, got %s", ct)
	}

	// Read first SSE event (full_sync)
	scanner := bufio.NewScanner(resp.Body)
	var eventType, data string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
		if line == "" && eventType != "" {
			break
		}
	}

	if eventType != string(EventFullSync) {
		t.Fatalf("want event type full_sync, got %s", eventType)
	}

	var evt NotificationEvent
	if err := json.Unmarshal([]byte(data), &evt); err != nil {
		t.Fatal(err)
	}
	if evt.Type != EventFullSync {
		t.Errorf("want full_sync, got %s", evt.Type)
	}
}

func TestSSE_ActivityEvent(t *testing.T) {
	srv, ts, _ := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/notifications", nil)
	req.Header.Set(AuthHeader, testToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Publish an activity event after a small delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		srv.broker.Publish(model.Event{
			ActivityNotification: &model.ActivityNotification{
				Window:    "lc-abc12345",
				State:     model.ActivityRunning,
				ToolName:  "Bash",
				Timestamp: time.Now(),
			},
		})
	}()

	// Read events until we get the activity event
	scanner := bufio.NewScanner(resp.Body)
	foundActivity := false
	eventCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: activity") {
			foundActivity = true
			break
		}
		eventCount++
		if eventCount > 20 {
			break
		}
	}

	if !foundActivity {
		t.Error("did not receive activity event")
	}
}

func TestSSE_Unauthorized(t *testing.T) {
	_, ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/notifications")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

func TestBrokerEventToNotification(t *testing.T) {
	srv, _, _ := newTestServer(t)
	now := time.Now()

	tests := []struct {
		name     string
		event    model.Event
		wantType NotificationEventType
		wantNil  bool
	}{
		{
			name: "activity",
			event: model.Event{ActivityNotification: &model.ActivityNotification{
				Window: "lc-abc", State: model.ActivityRunning, Timestamp: now,
			}},
			wantType: EventActivity,
		},
		{
			name: "tool_info",
			event: model.Event{Notification: &model.ToolNotification{
				ToolName: "Bash", Window: "lc-abc", Timestamp: now,
			}},
			wantType: EventToolInfo,
		},
		{
			name: "stop",
			event: model.Event{StopNotification: &model.StopNotification{
				Window: "lc-abc", StopReason: "end_turn", Timestamp: now,
			}},
			wantType: EventActivity,
		},
		{
			name: "session_start",
			event: model.Event{SessionStartNotification: &model.SessionStartNotification{
				Window: "lc-abc", Timestamp: now,
			}},
			wantType: EventActivity,
		},
		{
			name: "prompt_submit",
			event: model.Event{PromptSubmitNotification: &model.PromptSubmitNotification{
				Window: "lc-abc", Timestamp: now,
			}},
			wantType: EventActivity,
		},
		{
			name:    "empty event",
			event:   model.Event{},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := srv.brokerEventToNotification(tt.event)
			if tt.wantNil {
				if result != nil {
					t.Fatal("expected nil")
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil")
			}
			if result.Type != tt.wantType {
				t.Errorf("want type %s, got %s", tt.wantType, result.Type)
			}
		})
	}
}

// TestBrokerEventToNotification_ToolInfo_SetsSessionID verifies that
// the daemon's SSE handler tags outgoing EventToolInfo events with the
// authoritative session UUID resolved from the tmux window, so that
// local RemoteProvider callbacks can rewrite ToolNotification.Window
// to the local mirror's tmux window ID (Bug 5 Phase B action routing
// fix). Without this tag, the popup would appear on the local mirror
// but Accept/Reject would target a non-existent pane.
func TestBrokerEventToNotification_ToolInfo_SetsSessionID(t *testing.T) {
	srv, _, _ := newTestServer(t)

	const sessID = "abcd1234-5678-90ef-1122-334455667788"
	srv.mgr.Store().Add(session.Session{
		ID:         sessID,
		Name:       "s1",
		Path:       "/proj",
		TmuxWindow: "@42",
		Status:     session.StatusRunning,
	}, "/proj")

	evt := model.Event{Notification: &model.ToolNotification{
		ToolName:  "Edit",
		Window:    "@42",
		Timestamp: time.Now(),
	}}

	got := srv.brokerEventToNotification(evt)
	if got == nil {
		t.Fatal("brokerEventToNotification returned nil")
	}
	if got.Type != EventToolInfo {
		t.Errorf("Type = %q, want %q", got.Type, EventToolInfo)
	}
	if got.SessionID != sessID {
		t.Errorf("SessionID = %q, want %q", got.SessionID, sessID)
	}
	if got.ToolNotification == nil {
		t.Fatal("ToolNotification is nil")
	}
	if got.ToolNotification.Window != "@42" {
		t.Errorf("ToolNotification.Window = %q, want @42 (unchanged)", got.ToolNotification.Window)
	}
}

// TestBrokerEventToNotification_ToolInfo_EmptySessionID_OnMiss verifies
// that when the tmux window does not match any local session (e.g. the
// daemon has not yet observed the session), SessionID is emitted as an
// empty string rather than a stray value. Clients must be able to
// detect the absence and degrade gracefully.
func TestBrokerEventToNotification_ToolInfo_EmptySessionID_OnMiss(t *testing.T) {
	srv, _, _ := newTestServer(t)

	evt := model.Event{Notification: &model.ToolNotification{
		ToolName:  "Bash",
		Window:    "@99", // no matching session
		Timestamp: time.Now(),
	}}

	got := srv.brokerEventToNotification(evt)
	if got == nil {
		t.Fatal("brokerEventToNotification returned nil")
	}
	if got.SessionID != "" {
		t.Errorf("SessionID = %q, want empty on miss", got.SessionID)
	}
}

// TestBrokerEventToNotification_ToolInfo_PreservesDiffFields verifies that
// the SSE handler copies OldFilePath and NewContents from the broker event,
// which is required for remote DiffPopup rendering of Write tool notifications.
func TestBrokerEventToNotification_ToolInfo_PreservesDiffFields(t *testing.T) {
	srv, _, _ := newTestServer(t)

	evt := model.Event{Notification: &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@1",
		Input:       `{"file_path":"/home/user/main.go","content":"package main\n"}`,
		Timestamp:   time.Now(),
		OldFilePath: "/home/user/main.go",
		NewContents: "package main\n",
	}}

	got := srv.brokerEventToNotification(evt)
	if got == nil {
		t.Fatal("brokerEventToNotification returned nil")
	}
	if got.ToolNotification == nil {
		t.Fatal("ToolNotification is nil")
	}
	if got.ToolNotification.OldFilePath != "/home/user/main.go" {
		t.Errorf("OldFilePath = %q, want /home/user/main.go", got.ToolNotification.OldFilePath)
	}
	if got.ToolNotification.NewContents != "package main\n" {
		t.Errorf("NewContents = %q, want %q", got.ToolNotification.NewContents, "package main\n")
	}
	if !got.ToolNotification.IsDiff() {
		t.Error("IsDiff() should be true for Write with OldFilePath")
	}
}

func TestSessionIDForWindow(t *testing.T) {
	// Helper builds a server with a minimal session store.
	setup := func(t *testing.T) *DaemonServer {
		t.Helper()
		tmp := t.TempDir()
		paths := config.TestPaths(tmp)
		store := session.NewStore(paths.StateFile())
		mgr := session.NewManager(store, tmux.NewMockClient(), paths, nil)
		srv := &DaemonServer{
			mgr: mgr,
			log: log.New(io.Discard, "", 0),
		}
		return srv
	}

	t.Run("matches by tmux window ID", func(t *testing.T) {
		srv := setup(t)
		srv.mgr.Store().Add(session.Session{
			ID:         "abcd1234-5678-90ef-1122-334455667788",
			Name:       "s1",
			Path:       "/proj",
			TmuxWindow: "@42",
			Status:     session.StatusRunning,
		}, "/proj")
		got := srv.sessionIDForWindow("@42")
		if got != "abcd1234-5678-90ef-1122-334455667788" {
			t.Errorf("want full UUID, got %q", got)
		}
	})

	t.Run("matches by canonical window name", func(t *testing.T) {
		srv := setup(t)
		srv.mgr.Store().Add(session.Session{
			ID:     "0123456789abcdef-1111-2222-3333-444455556666",
			Name:   "s2",
			Path:   "/proj",
			Status: session.StatusRunning,
		}, "/proj")
		got := srv.sessionIDForWindow("lc-01234567")
		if got != "0123456789abcdef-1111-2222-3333-444455556666" {
			t.Errorf("want full UUID for canonical name, got %q", got)
		}
	})

	t.Run("empty window returns empty", func(t *testing.T) {
		srv := setup(t)
		if got := srv.sessionIDForWindow(""); got != "" {
			t.Errorf("want empty, got %q", got)
		}
	})

	t.Run("miss returns empty", func(t *testing.T) {
		srv := setup(t)
		if got := srv.sessionIDForWindow("@99"); got != "" {
			t.Errorf("want empty on miss, got %q", got)
		}
	})
}
