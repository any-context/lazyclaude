package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/any-context/lazyclaude/internal/session"
)

// seedSession inserts a bare session into the DaemonServer's store so that
// the capture handlers can look it up by ID. Returns the seeded session
// for assertions.
func seedSession(t *testing.T, srv *DaemonServer, id string) session.Session {
	t.Helper()
	sess := session.Session{
		ID:   id,
		Name: "test-" + id,
		Path: "/home/user/project",
	}
	srv.mgr.Store().Add(sess, "/home/user/project")
	return sess
}

// --- handleScrollback ---

func TestServer_HandleScrollback_Success(t *testing.T) {
	srv, ts, mock := newTestServer(t)
	sess := seedSession(t, srv, "abcd1234")
	target := sess.TmuxTarget()

	// Seed a range capture for (target, 100, 150) so CapturePaneANSIRange
	// returns the expected content. Any other target produces an empty
	// string which would make the assertion fail.
	mock.RangeCaptures[fmt.Sprintf("%s:%d:%d", target, 100, 150)] = "scrollback-body"

	body := `{"id":"abcd1234","width":80,"start_line":100,"end_line":150}`
	req := authReq("POST", ts.URL+"/session/abcd1234/scrollback", body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var got ScrollbackResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Content != "scrollback-body" {
		t.Errorf("content=%q, want %q", got.Content, "scrollback-body")
	}
}

func TestServer_HandleScrollback_SessionNotFound(t *testing.T) {
	_, ts, _ := newTestServer(t)

	body := `{"id":"nope","start_line":0,"end_line":10}`
	req := authReq("POST", ts.URL+"/session/nope/scrollback", body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func TestServer_HandleScrollback_BadJSON(t *testing.T) {
	srv, ts, _ := newTestServer(t)
	seedSession(t, srv, "abcd1234")

	req := authReq("POST", ts.URL+"/session/abcd1234/scrollback", `{not-json`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestServer_HandleScrollback_TmuxError(t *testing.T) {
	srv, ts, mock := newTestServer(t)
	seedSession(t, srv, "abcd1234")
	mock.ErrCapture = fmt.Errorf("capture-pane failed")

	body := `{"id":"abcd1234","start_line":0,"end_line":10}`
	req := authReq("POST", ts.URL+"/session/abcd1234/scrollback", body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", resp.StatusCode)
	}
}

func TestServer_HandleScrollback_Unauthorized(t *testing.T) {
	_, ts, _ := newTestServer(t)
	body := `{"id":"abcd1234","start_line":0,"end_line":10}`
	req, _ := http.NewRequest("POST", ts.URL+"/session/abcd1234/scrollback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

// --- handleHistorySize ---

func TestServer_HandleHistorySize_Success(t *testing.T) {
	srv, ts, mock := newTestServer(t)
	sess := seedSession(t, srv, "abcd1234")
	mock.Messages[sess.TmuxTarget()] = "42"

	req := authReq("GET", ts.URL+"/session/abcd1234/history-size", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var got HistorySizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Lines != 42 {
		t.Errorf("lines=%d, want 42", got.Lines)
	}
}

func TestServer_HandleHistorySize_Whitespace(t *testing.T) {
	srv, ts, mock := newTestServer(t)
	sess := seedSession(t, srv, "abcd1234")
	// Real tmux show-message output is newline-terminated; handler must
	// trim before parsing.
	mock.Messages[sess.TmuxTarget()] = "  17\n"

	req := authReq("GET", ts.URL+"/session/abcd1234/history-size", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got HistorySizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Lines != 17 {
		t.Errorf("lines=%d, want 17", got.Lines)
	}
}

func TestServer_HandleHistorySize_SessionNotFound(t *testing.T) {
	_, ts, _ := newTestServer(t)
	req := authReq("GET", ts.URL+"/session/nope/history-size", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func TestServer_HandleHistorySize_UnparseableOutput(t *testing.T) {
	srv, ts, mock := newTestServer(t)
	sess := seedSession(t, srv, "abcd1234")
	// Simulate a pane in a bad state where show-message returns the
	// unexpanded format string. Without guard logic this would be
	// silently coerced to 0 and look like an empty history.
	mock.Messages[sess.TmuxTarget()] = "#{history_size}"

	req := authReq("GET", ts.URL+"/session/abcd1234/history-size", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", resp.StatusCode)
	}
}

func TestServer_HandleHistorySize_TmuxError(t *testing.T) {
	srv, ts, mock := newTestServer(t)
	seedSession(t, srv, "abcd1234")
	mock.ErrShowMessage = fmt.Errorf("show-message failed")

	req := authReq("GET", ts.URL+"/session/abcd1234/history-size", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", resp.StatusCode)
	}
}

func TestServer_HandleHistorySize_Unauthorized(t *testing.T) {
	_, ts, _ := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/session/abcd1234/history-size", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}
