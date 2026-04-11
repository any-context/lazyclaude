package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerRefresh(t *testing.T) {
	t.Parallel()

	// Setup: user config with 2 servers, project config with 1 server,
	// deny list with 1 server name.
	dir := t.TempDir()

	userCfg := filepath.Join(dir, "claude.json")
	writeJSON(t, userCfg, `{
		"mcpServers": {
			"github": { "command": "npx", "args": ["-y", "server-github"] },
			"memory": { "command": "npx", "args": ["-y", "server-memory"] }
		}
	}`)

	projDir := filepath.Join(dir, "project")
	mustMkdir(t, projDir)

	projMCP := filepath.Join(projDir, ".mcp.json")
	writeJSON(t, projMCP, `{
		"mcpServers": {
			"my-db": { "command": "node", "args": ["db.js"] }
		}
	}`)

	claudeDir := filepath.Join(projDir, ".claude")
	mustMkdir(t, claudeDir)
	settingsLocal := filepath.Join(claudeDir, "settings.local.json")
	writeJSON(t, settingsLocal, `{
		"deniedMcpServers": [{ "serverName": "memory" }]
	}`)

	mgr := NewManager(userCfg, nil)
	mgr.SetProjectDir(projDir)

	if err := mgr.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	servers := mgr.Servers()
	if len(servers) != 3 {
		t.Fatalf("got %d servers, want 3", len(servers))
	}

	byName := make(map[string]MCPServer, len(servers))
	for _, s := range servers {
		byName[s.Name] = s
	}

	if gh := byName["github"]; gh.Scope != "user" || gh.Denied {
		t.Errorf("github: scope=%q denied=%v", gh.Scope, gh.Denied)
	}
	if mem := byName["memory"]; mem.Scope != "user" || !mem.Denied {
		t.Errorf("memory: scope=%q denied=%v", mem.Scope, mem.Denied)
	}
	if db := byName["my-db"]; db.Scope != "project" || db.Denied {
		t.Errorf("my-db: scope=%q denied=%v", db.Scope, db.Denied)
	}
}

func TestManagerToggleDenied(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	userCfg := filepath.Join(dir, "claude.json")
	writeJSON(t, userCfg, `{
		"mcpServers": {
			"github": { "command": "npx", "args": ["-y", "server-github"] },
			"memory": { "command": "npx", "args": ["-y", "server-memory"] }
		}
	}`)

	projDir := filepath.Join(dir, "project")
	mustMkdir(t, projDir)
	mustMkdir(t, filepath.Join(projDir, ".claude"))

	mgr := NewManager(userCfg, nil)
	mgr.SetProjectDir(projDir)

	if err := mgr.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Initially both are enabled.
	for _, s := range mgr.Servers() {
		if s.Denied {
			t.Errorf("%s should not be denied initially", s.Name)
		}
	}

	// Toggle memory off.
	if err := mgr.ToggleDenied(context.Background(), "memory"); err != nil {
		t.Fatalf("ToggleDenied(memory) error = %v", err)
	}

	byName := serverMap(mgr.Servers())
	if !byName["memory"].Denied {
		t.Error("memory should be denied after toggle off")
	}
	if byName["github"].Denied {
		t.Error("github should not be affected")
	}

	// Toggle memory back on.
	if err := mgr.ToggleDenied(context.Background(), "memory"); err != nil {
		t.Fatalf("ToggleDenied(memory) error = %v", err)
	}

	byName = serverMap(mgr.Servers())
	if byName["memory"].Denied {
		t.Error("memory should be enabled after toggle on")
	}

	// Verify settings.local.json was cleaned up (empty deny list removes key).
	settingsPath := filepath.Join(projDir, ".claude", "settings.local.json")
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if strings.Contains(string(raw), "deniedMcpServers") {
		t.Error("deniedMcpServers should be removed when list is empty")
	}
}

func TestManagerToggleDenied_unknown_server(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	userCfg := filepath.Join(dir, "claude.json")
	writeJSON(t, userCfg, `{ "mcpServers": {} }`)

	projDir := filepath.Join(dir, "project")
	mustMkdir(t, projDir)

	mgr := NewManager(userCfg, nil)
	mgr.SetProjectDir(projDir)
	_ = mgr.Refresh(context.Background())

	err := mgr.ToggleDenied(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown server")
	}
}

func TestManagerRefresh_no_project(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	userCfg := filepath.Join(dir, "claude.json")
	writeJSON(t, userCfg, `{
		"mcpServers": {
			"github": { "command": "npx", "args": ["-y", "server-github"] }
		}
	}`)

	mgr := NewManager(userCfg, nil)
	// No project dir set — should still read user config.

	if err := mgr.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	servers := mgr.Servers()
	if len(servers) != 1 {
		t.Fatalf("got %d servers, want 1", len(servers))
	}
	if servers[0].Name != "github" || servers[0].Scope != "user" {
		t.Errorf("unexpected server: %+v", servers[0])
	}
}

// --- helpers ---

func writeJSON(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func serverMap(servers []MCPServer) map[string]MCPServer {
	m := make(map[string]MCPServer, len(servers))
	for _, s := range servers {
		m[s.Name] = s
	}
	return m
}

