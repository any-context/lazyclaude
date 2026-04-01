package server

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestDiscoverServer_FindsAliveServer(t *testing.T) {
	// Start a minimal HTTP server to simulate a live lazyclaude instance.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})) //nolint:errcheck

	port := ln.Addr().(*net.TCPAddr).Port

	// Create a temp IDE dir with a lock file.
	ideDir := t.TempDir()
	lock := LockFile{
		PID:       os.Getpid(),
		AuthToken: "test-token-abc",
		Transport: "ws",
		App:       lockApp,
	}
	data, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	lockPath := filepath.Join(ideDir, strconv.Itoa(port)+".lock")
	if err := os.WriteFile(lockPath, data, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	result, err := DiscoverServer(ideDir)
	if err != nil {
		t.Fatalf("DiscoverServer: %v", err)
	}
	if result.Port != port {
		t.Errorf("port = %d, want %d", result.Port, port)
	}
	if result.Token != "test-token-abc" {
		t.Errorf("token = %q, want %q", result.Token, "test-token-abc")
	}
}

func TestDiscoverServer_SkipsDeadServer(t *testing.T) {
	ideDir := t.TempDir()
	lock := LockFile{
		PID:       99999999, // unlikely to be alive
		AuthToken: "dead-token",
		Transport: "ws",
		App:       lockApp,
	}
	data, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	lockPath := filepath.Join(ideDir, "99999.lock")
	if err := os.WriteFile(lockPath, data, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	_, discErr := DiscoverServer(ideDir)
	if discErr == nil {
		t.Fatal("expected error for dead server, got nil")
	}
}

func TestDiscoverServer_SkipsNonLazyclaudeLock(t *testing.T) {
	// Start a live server.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})) //nolint:errcheck

	port := ln.Addr().(*net.TCPAddr).Port
	ideDir := t.TempDir()

	// Write a lock with App="vscode" — should be skipped.
	lock := LockFile{
		PID:       os.Getpid(),
		AuthToken: "vscode-token",
		Transport: "ws",
		App:       "vscode",
	}
	data, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ideDir, strconv.Itoa(port)+".lock"), data, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	_, err = DiscoverServer(ideDir)
	if err == nil {
		t.Fatal("expected error when only non-lazyclaude locks exist")
	}
}

func TestDiscoverServer_PicksHighestPort(t *testing.T) {
	// Start two servers.
	ln1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen1: %v", err)
	}
	defer ln1.Close()
	go http.Serve(ln1, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})) //nolint:errcheck

	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen2: %v", err)
	}
	defer ln2.Close()
	go http.Serve(ln2, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})) //nolint:errcheck

	port1 := ln1.Addr().(*net.TCPAddr).Port
	port2 := ln2.Addr().(*net.TCPAddr).Port

	ideDir := t.TempDir()
	for _, tc := range []struct {
		port  int
		token string
	}{
		{port1, "token-1"},
		{port2, "token-2"},
	} {
		lock := LockFile{
			PID:       os.Getpid(),
			AuthToken: tc.token,
			Transport: "ws",
			App:       lockApp,
		}
		data, err := json.Marshal(lock)
		if err != nil {
			t.Fatalf("marshal lock: %v", err)
		}
		if err := os.WriteFile(filepath.Join(ideDir, strconv.Itoa(tc.port)+".lock"), data, 0o600); err != nil {
			t.Fatalf("write lock: %v", err)
		}
	}

	result, err := DiscoverServer(ideDir)
	if err != nil {
		t.Fatalf("DiscoverServer: %v", err)
	}

	highPort := port1
	expectedToken := "token-1"
	if port2 > port1 {
		highPort = port2
		expectedToken = "token-2"
	}
	if result.Port != highPort {
		t.Errorf("port = %d, want %d (highest)", result.Port, highPort)
	}
	if result.Token != expectedToken {
		t.Errorf("token = %q, want %q", result.Token, expectedToken)
	}
}

func TestDiscoverServer_EmptyDir(t *testing.T) {
	ideDir := t.TempDir()
	_, err := DiscoverServer(ideDir)
	if err == nil {
		t.Fatal("expected error for empty dir, got nil")
	}
}
