package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/any-context/lazyclaude/internal/server"
)

func TestPrintSessionsTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := printSessionsTable(&buf, nil, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "No sessions found") {
		t.Errorf("expected 'No sessions found', got %q", buf.String())
	}
}

func TestPrintSessionsTable_Normal(t *testing.T) {
	sessions := []server.SessionInfo{
		{ID: "abc123", Name: "pm", Role: "pm", Status: "Running", Path: "/home/user/project", Window: "@1"},
		{ID: "def456", Name: "feat-a", Role: "worker", Status: "Running", Path: "/home/user/project/.lazyclaude/worktrees/feat-a", Window: "@2"},
	}

	var buf bytes.Buffer
	if err := printSessionsTable(&buf, sessions, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "ID") {
		t.Error("missing ID header")
	}
	if !strings.Contains(output, "HOST") {
		t.Error("missing HOST header")
	}
	if !strings.Contains(output, "/home/user/project") {
		t.Error("expected absolute path in output")
	}
	// WINDOW should NOT appear in non-verbose mode
	if strings.Contains(output, "WINDOW") {
		t.Error("WINDOW should not appear in non-verbose mode")
	}
}

func TestPrintSessionsTable_Verbose(t *testing.T) {
	sessions := []server.SessionInfo{
		{ID: "abc12345", Name: "pm", Role: "pm", Status: "Running", Path: "/home/user/project", Window: "@1"},
	}

	var buf bytes.Buffer
	if err := printSessionsTable(&buf, sessions, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "abc12345") {
		t.Error("expected ID in verbose output")
	}
	if !strings.Contains(output, "@1") {
		t.Error("expected WINDOW in verbose output")
	}
	if !strings.Contains(output, "HOST") {
		t.Error("expected HOST header in verbose output")
	}
}

func TestPrintSessionsTable_WithSSHHost(t *testing.T) {
	sessions := []server.SessionInfo{
		{ID: "abc123", Name: "pm", Role: "pm", Status: "Running", Path: "/home/user/project"},
		{ID: "def456", Name: "remote", Role: "worker", Status: "Running", Path: "/home/dev/work", Host: "dev@10.0.1.5"},
	}

	var buf bytes.Buffer
	if err := printSessionsTable(&buf, sessions, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "dev@10.0.1.5") {
		t.Error("expected SSH host in output")
	}
}
