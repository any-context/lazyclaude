package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/core/event"
	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/session"
)

const testToken = "test-token-1234"

// newTestServer creates a DaemonServer backed by a mock tmux client and
// an in-memory session store. The returned httptest.Server handles auth.
func newTestServer(t *testing.T) (*DaemonServer, *httptest.Server, *tmux.MockClient) {
	t.Helper()

	mock := tmux.NewMockClient()
	// Pre-populate tmux session so manager.Create doesn't fail
	mock.Sessions["lazyclaude"] = []tmux.WindowInfo{{ID: "@0", Name: "init"}}

	dir := t.TempDir()
	paths := config.Paths{
		DataDir:    dir,
		RuntimeDir: dir,
		IDEDir:     dir,
	}
	store := session.NewStore(paths.StateFile())
	logger := log.New(io.Discard, "", 0)
	mgr := session.NewManager(store, mock, paths, nil)

	broker := event.NewBroker[model.Event]()
	t.Cleanup(broker.Close)

	cfg := DaemonConfig{
		Port:       0,
		Token:      testToken,
		RuntimeDir: dir,
	}

	srv := NewDaemonServer(cfg, mgr, broker, mock, logger, WithVersion("test-1.0"))

	ts := httptest.NewServer(srv.httpSrv.Handler)
	t.Cleanup(ts.Close)

	return srv, ts, mock
}

func authReq(method, url string, body string) *http.Request {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, url, bodyReader)
	req.Header.Set(AuthHeader, testToken)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func TestHealth_NoAuth(t *testing.T) {
	_, ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}

	if health.APIVersion != APIVersion {
		t.Errorf("api_version: want %d, got %d", APIVersion, health.APIVersion)
	}
	if health.BinaryVersion != "test-1.0" {
		t.Errorf("binary_version: want test-1.0, got %s", health.BinaryVersion)
	}
}

func TestAuth_Unauthorized(t *testing.T) {
	_, ts, _ := newTestServer(t)

	// No token
	resp, err := http.Get(ts.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}

	// Wrong token
	req, _ := http.NewRequest("GET", ts.URL+"/sessions", nil)
	req.Header.Set(AuthHeader, "wrong-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

func TestSessionList_Empty(t *testing.T) {
	_, ts, _ := newTestServer(t)

	req := authReq("GET", ts.URL+"/sessions", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var list SessionListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Sessions) != 0 {
		t.Errorf("want 0 sessions, got %d", len(list.Sessions))
	}
}

func TestSessionDelete_NotFound(t *testing.T) {
	_, ts, _ := newTestServer(t)

	req := authReq("DELETE", ts.URL+"/session/nonexistent", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func TestSessionRename_NotFound(t *testing.T) {
	_, ts, _ := newTestServer(t)

	req := authReq("POST", ts.URL+"/session/nonexistent/rename", `{"new_name":"foo"}`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}


func TestMsgSend_Validation(t *testing.T) {
	_, ts, _ := newTestServer(t)

	tests := []struct {
		name string
		body string
		want int
	}{
		{"missing fields", `{}`, http.StatusBadRequest},
		{"self send", `{"from":"a","to":"a","type":"status","body":"x"}`, http.StatusBadRequest},
		{"recipient not found", `{"from":"a","to":"b","type":"status","body":"x"}`, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := authReq("POST", ts.URL+"/msg/send", tt.body)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Fatalf("want %d, got %d", tt.want, resp.StatusCode)
			}
		})
	}
}

func TestMsgCreate_MissingFields(t *testing.T) {
	_, ts, _ := newTestServer(t)

	req := authReq("POST", ts.URL+"/msg/create", `{"from":"","name":""}`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestMsgSessions_Empty(t *testing.T) {
	_, ts, _ := newTestServer(t)

	req := authReq("GET", ts.URL+"/msg/sessions", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var result MsgSessionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Sessions) != 0 {
		t.Errorf("want 0 sessions, got %d", len(result.Sessions))
	}
}

func TestWorktreeList_MissingProjectRoot(t *testing.T) {
	_, ts, _ := newTestServer(t)

	req := authReq("GET", ts.URL+"/worktrees", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestSessionCreate_InvalidType(t *testing.T) {
	_, ts, _ := newTestServer(t)

	req := authReq("POST", ts.URL+"/session/create", `{"path":"/tmp","session_type":"invalid"}`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestMsgCreate_CallerNotFound(t *testing.T) {
	_, ts, _ := newTestServer(t)

	// from="x" doesn't exist, so FindProjectForSession returns nil -> 404
	req := authReq("POST", ts.URL+"/msg/create", `{"from":"x","name":"y","type":"worker"}`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func TestShutdown(t *testing.T) {
	srv, ts, _ := newTestServer(t)

	req := authReq("POST", ts.URL+"/shutdown", `{}`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	// ShutdownCh should be closed
	select {
	case <-srv.ShutdownCh():
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown channel not closed")
	}
}

func TestGenerateDaemonToken(t *testing.T) {
	token, err := GenerateDaemonToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 32 { // 16 bytes = 32 hex chars
		t.Errorf("want 32 chars, got %d", len(token))
	}
}

// TestAPIVersion_Constant verifies that the health endpoint reports the
// current APIVersion constant. This acts as a regression guard: if APIVersion
// is bumped without updating the health handler, this test will catch it.
func TestAPIVersion_Constant(t *testing.T) {
	_, ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health.APIVersion != APIVersion {
		t.Errorf("health.api_version=%d, want constant APIVersion=%d", health.APIVersion, APIVersion)
	}
	if APIVersion != 4 {
		t.Errorf("APIVersion constant=%d, want 4 (Phase 2b bump)", APIVersion)
	}
}

// TestProfilesEndpoint_NoConfig verifies that GET /profiles returns an empty
// profile list (with the builtin default) when the user has no config.json.
func TestProfilesEndpoint_NoConfig(t *testing.T) {
	_, ts, _ := newTestServer(t)

	req := authReq("GET", ts.URL+"/profiles", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var result ProfileListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	// The daemon loads from the actual home directory; since config.json is
	// almost certainly absent in a test environment, we expect either an empty
	// list or the builtin default only. No parse error should be present.
	if result.Error != "" {
		t.Errorf("unexpected error: %q", result.Error)
	}
	// Profiles slice must not be nil (absent config returns the builtin default
	// or empty list, never nil).
	if result.Profiles == nil {
		t.Error("Profiles should not be nil when config is absent")
	}
}

// TestProfilesEndpoint_Unauthorized verifies that GET /profiles requires auth.
func TestProfilesEndpoint_Unauthorized(t *testing.T) {
	_, ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/profiles")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}
