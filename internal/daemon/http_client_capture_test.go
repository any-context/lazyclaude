package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClient_CaptureScrollback(t *testing.T) {
	var gotReq ScrollbackRequest
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /session/abcd1234/scrollback": func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			testWriteJSON(w, ScrollbackResponse{Content: "body"})
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	resp, err := c.CaptureScrollback(context.Background(), ScrollbackRequest{
		ID:        "abcd1234",
		Width:     80,
		StartLine: 100,
		EndLine:   200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "body" {
		t.Errorf("content=%q, want %q", resp.Content, "body")
	}
	if gotReq.StartLine != 100 || gotReq.EndLine != 200 || gotReq.Width != 80 {
		t.Errorf("request fields = %+v", gotReq)
	}
	if gotReq.ID != "abcd1234" {
		t.Errorf("request ID=%q, want abcd1234", gotReq.ID)
	}
}

func TestHTTPClient_CaptureScrollback_Error(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"POST /session/abcd1234/scrollback": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	_, err := c.CaptureScrollback(context.Background(), ScrollbackRequest{ID: "abcd1234"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHTTPClient_CaptureScrollback_EscapedPath(t *testing.T) {
	// The daemon API path escapes session IDs via sessionPath+url.PathEscape.
	// Use an ID that contains a '/' to verify the slash is percent-encoded
	// on the wire. We check r.URL.EscapedPath() because r.URL.Path is the
	// decoded form and would lose the escaping we want to observe.
	const rawID = "weird/id"
	const wantEscaped = "/session/weird%2Fid/scrollback"
	var sawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.EscapedPath()
		testWriteJSON(w, ScrollbackResponse{Content: "ok"})
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	if _, err := c.CaptureScrollback(context.Background(), ScrollbackRequest{ID: rawID}); err != nil {
		t.Fatal(err)
	}
	if sawPath != wantEscaped {
		t.Errorf("escaped path=%q, want %q", sawPath, wantEscaped)
	}
}

func TestHTTPClient_HistorySize(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"GET /session/abcd1234/history-size": func(w http.ResponseWriter, _ *http.Request) {
			testWriteJSON(w, HistorySizeResponse{Lines: 4321})
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	n, err := c.HistorySize(context.Background(), "abcd1234")
	if err != nil {
		t.Fatal(err)
	}
	if n != 4321 {
		t.Errorf("lines=%d, want 4321", n)
	}
}

func TestHTTPClient_HistorySize_Error(t *testing.T) {
	srv := newClientTestServer(t, map[string]http.HandlerFunc{
		"GET /session/abcd1234/history-size": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusBadGateway)
		},
	})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "")
	if _, err := c.HistorySize(context.Background(), "abcd1234"); err == nil {
		t.Fatal("expected error")
	}
}
