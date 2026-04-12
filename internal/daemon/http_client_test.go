package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServer creates an httptest.Server with the given handler map.
// Each key is "METHOD /path", value is the handler function.
func newClientTestServer(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if h, ok := handlers[key]; ok {
			h(w, r)
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "not found", http.StatusNotFound)
	}))
}

func testWriteJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func TestHTTPClient_CreateSession(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /session/create": func(w http.ResponseWriter, r *http.Request) {
			var req SessionCreateRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.Path != "/home/user/project" {
				t.Errorf("unexpected path: %s", req.Path)
			}
			if req.SessionType != "plain" {
				t.Errorf("unexpected type: %s", req.SessionType)
			}
			testWriteJSON(w, SessionCreateResponse{ID: "abc123", Name: "project"})
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "test-token")
	resp, err := c.CreateSession(context.Background(), SessionCreateRequest{
		Path:        "/home/user/project",
		SessionType: "plain",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != "abc123" {
		t.Errorf("got ID=%q, want abc123", resp.ID)
	}
}

func TestHTTPClient_DeleteSession(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"DELETE /session/abc123": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	if err := c.DeleteSession(context.Background(), "abc123"); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPClient_RenameSession(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /session/abc123/rename": func(w http.ResponseWriter, r *http.Request) {
			var req SessionRenameRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.NewName != "new-name" {
				t.Errorf("got name=%q, want new-name", req.NewName)
			}
			w.WriteHeader(http.StatusOK)
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	if err := c.RenameSession(context.Background(), "abc123", "new-name"); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPClient_Sessions(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
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

	c := NewHTTPClient(srv.URL, "")
	sessions, err := c.Sessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
	if sessions[0].ID != "s1" {
		t.Errorf("got ID=%q, want s1", sessions[0].ID)
	}
}

func TestHTTPClient_PurgeOrphans(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /sessions/purge": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, map[string]int{"purged": 3})
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	n, err := c.PurgeOrphans(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("got %d, want 3", n)
	}
}

func TestHTTPClient_Shutdown(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /shutdown": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	if err := c.Shutdown(context.Background(), ShutdownRequest{}); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPClient_MsgSend(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /msg/send": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, MsgSendResponse{Delivered: true})
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	resp, err := c.MsgSend(context.Background(), MsgSendRequest{From: "a", To: "b", Body: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Delivered {
		t.Error("expected delivered=true")
	}
}

func TestHTTPClient_ResumeWorktree(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /worktree/resume": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, WorktreeResumeResponse{SessionID: "wt-resume"})
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	resp, err := c.ResumeWorktree(context.Background(), WorktreeResumeRequest{
		WorktreePath: "/tmp/wt",
		ProjectRoot:  "/project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SessionID != "wt-resume" {
		t.Errorf("got session=%q", resp.SessionID)
	}
}

func TestHTTPClient_CreateWorktree(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /worktree/create": func(w http.ResponseWriter, r *http.Request) {
			var req WorktreeCreateRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.Name != "feature-x" {
				t.Errorf("got name=%q", req.Name)
			}
			testWriteJSON(w, WorktreeCreateResponse{SessionID: "wt1", Path: "/tmp/wt", Branch: "feature-x"})
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	resp, err := c.CreateWorktree(context.Background(), WorktreeCreateRequest{
		Name:        "feature-x",
		ProjectRoot: "/home/user/project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SessionID != "wt1" {
		t.Errorf("got session=%q", resp.SessionID)
	}
}

func TestHTTPClient_ListWorktrees(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"GET /worktrees": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("project_root") != "/project" {
				t.Errorf("unexpected project_root: %s", r.URL.Query().Get("project_root"))
			}
			testWriteJSON(w, WorktreeListResponse{
				Worktrees: []WorktreeInfo{{Name: "wt1", Path: "/tmp/wt1", Branch: "main"}},
			})
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	wts, err := c.ListWorktrees(context.Background(), "/project")
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 1 || wts[0].Name != "wt1" {
		t.Errorf("unexpected worktrees: %v", wts)
	}
}

func TestHTTPClient_Health(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"GET /health": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, HealthResponse{
				APIVersion:    1,
				BinaryVersion: "0.1.0",
				UptimeSeconds: 60,
				SessionCount:  2,
			})
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	resp, err := c.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp.APIVersion != 1 {
		t.Errorf("got version=%d", resp.APIVersion)
	}
}

func TestHTTPClient_AuthHeader(t *testing.T) {
	var gotHeader string
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"GET /health": func(w http.ResponseWriter, r *http.Request) {
			gotHeader = r.Header.Get(AuthHeader)
			testWriteJSON(w, HealthResponse{APIVersion: 1})
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "secret-token")
	_, _ = c.Health(context.Background())
	if gotHeader != "secret-token" {
		t.Errorf("got auth=%q, want secret-token", gotHeader)
	}
}

func TestHTTPClient_ErrorResponse(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"GET /sessions": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "internal error", http.StatusInternalServerError)
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	_, err := c.Sessions(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected HTTP 500 in error, got: %s", err)
	}
}

func TestHTTPClient_SubscribeNotifications(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/notifications" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}

		// Send two events.
		ev1, _ := json.Marshal(NotificationEvent{
			Type:      EventActivity,
			SessionID: "s1",
			Activity:  2,
		})
		fmt.Fprintf(w, "event:activity\ndata:%s\n\n", ev1)
		flusher.Flush()

		ev2, _ := json.Marshal(NotificationEvent{
			Type: EventFullSync,
			Sessions: []SessionInfo{
				{ID: "s1", Name: "test"},
			},
		})
		fmt.Fprintf(w, "event:full_sync\ndata:%s\n\n", ev2)
		flusher.Flush()
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := c.SubscribeNotifications(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var events []NotificationEvent
	for ev := range ch {
		events = append(events, ev)
		if len(events) >= 2 {
			cancel()
		}
	}

	if len(events) < 2 {
		t.Fatalf("got %d events, want >= 2", len(events))
	}
	if events[0].Type != EventActivity {
		t.Errorf("event[0] type=%q, want activity", events[0].Type)
	}
	if events[1].Type != EventFullSync {
		t.Errorf("event[1] type=%q, want full_sync", events[1].Type)
	}
}

func TestHTTPClient_PendingNotifications(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"GET /notifications/pending": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, map[string]interface{}{
				"notifications": []ToolNotificationInfo{
					{SessionID: "s1", ToolName: "Read", Window: "@1"},
				},
			})
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	notifs, err := c.PendingNotifications(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(notifs) != 1 || notifs[0].ToolName != "Read" {
		t.Errorf("unexpected notifications: %v", notifs)
	}
}
