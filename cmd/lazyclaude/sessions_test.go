package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/any-context/lazyclaude/internal/server"
)

func TestDetectProjectRoot(t *testing.T) {
	tests := []struct {
		name     string
		sessions []server.SessionInfo
		want     string
	}{
		{
			name:     "empty",
			sessions: nil,
			want:     "",
		},
		{
			name: "single session",
			sessions: []server.SessionInfo{
				{Path: "/home/user/project"},
			},
			want: "/home/user/project",
		},
		{
			name: "common parent",
			sessions: []server.SessionInfo{
				{Path: "/home/user/project"},
				{Path: "/home/user/project/.claude/worktrees/feat-a"},
				{Path: "/home/user/project/.claude/worktrees/feat-b"},
			},
			want: "/home/user/project",
		},
		{
			name: "different roots",
			sessions: []server.SessionInfo{
				{Path: "/home/alice/project"},
				{Path: "/home/bob/project"},
			},
			want: "/home",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectProjectRoot(tt.sessions)
			if got != tt.want {
				t.Errorf("detectProjectRoot() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRelativePath(t *testing.T) {
	tests := []struct {
		path string
		root string
		want string
	}{
		{"/home/user/project", "/home/user/project", "."},
		{"/home/user/project/.claude/worktrees/feat-a", "/home/user/project", ".claude/worktrees/feat-a"},
		{"/other/path", "/home/user/project", "/other/path"},
		{"/home/user/project/sub", "", "/home/user/project/sub"},
		{"/home/user/project-x/file", "/home/user/project", "/home/user/project-x/file"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := relativePath(tt.path, tt.root)
			if got != tt.want {
				t.Errorf("relativePath(%q, %q) = %q, want %q", tt.path, tt.root, got, tt.want)
			}
		})
	}
}

func TestCommonPrefix(t *testing.T) {
	tests := []struct {
		a, b string
		want string
	}{
		{"/home/user/project", "/home/user/project", "/home/user/project"},
		{"/home/user/project/a", "/home/user/project/b", "/home/user/project"},
		{"/a/b/c", "/x/y/z", ""},
		{"/home/user", "/home/user/project", "/home/user"},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			got := commonPrefix(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("commonPrefix(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

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
		{ID: "def456", Name: "feat-a", Role: "worker", Status: "Running", Path: "/home/user/project/.claude/worktrees/feat-a", Window: "@2"},
	}

	var buf bytes.Buffer
	if err := printSessionsTable(&buf, sessions, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "ID") {
		t.Error("missing ID header")
	}
	if !strings.Contains(output, "NAME") {
		t.Error("missing NAME header")
	}
	if !strings.Contains(output, "ROLE") {
		t.Error("missing ROLE header")
	}
	// Should show relative paths
	if !strings.Contains(output, ".claude/worktrees/feat-a") {
		t.Errorf("expected relative worktree path, got:\n%s", output)
	}
	// ID should appear in default mode
	if !strings.Contains(output, "abc123") {
		t.Error("expected ID in default output")
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
	if !strings.Contains(output, "/home/user/project") {
		t.Error("expected absolute path in verbose output")
	}
}
