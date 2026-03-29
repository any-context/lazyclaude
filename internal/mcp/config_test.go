package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadClaudeJSON(t *testing.T) {
	t.Parallel()

	t.Run("parses stdio and http servers", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "claude.json")
		data := `{
			"mcpServers": {
				"github": {
					"command": "npx",
					"args": ["-y", "@modelcontextprotocol/server-github"],
					"env": { "GITHUB_TOKEN": "secret" }
				},
				"vercel": {
					"type": "http",
					"url": "https://mcp.vercel.com"
				}
			},
			"otherSetting": true
		}`
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}

		servers, err := ReadClaudeJSON(path)
		if err != nil {
			t.Fatalf("ReadClaudeJSON() error = %v", err)
		}

		if len(servers) != 2 {
			t.Fatalf("got %d servers, want 2", len(servers))
		}

		gh, ok := servers["github"]
		if !ok {
			t.Fatal("missing github server")
		}
		if gh.Command != "npx" {
			t.Errorf("github.Command = %q, want %q", gh.Command, "npx")
		}
		if len(gh.Args) != 2 || gh.Args[0] != "-y" {
			t.Errorf("github.Args = %v, want [-y @modelcontextprotocol/server-github]", gh.Args)
		}
		if gh.Env["GITHUB_TOKEN"] != "secret" {
			t.Errorf("github.Env[GITHUB_TOKEN] = %q, want %q", gh.Env["GITHUB_TOKEN"], "secret")
		}

		vc, ok := servers["vercel"]
		if !ok {
			t.Fatal("missing vercel server")
		}
		if vc.Type != "http" {
			t.Errorf("vercel.Type = %q, want %q", vc.Type, "http")
		}
		if vc.URL != "https://mcp.vercel.com" {
			t.Errorf("vercel.URL = %q, want %q", vc.URL, "https://mcp.vercel.com")
		}
	})

	t.Run("returns empty map when no mcpServers key", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "claude.json")
		if err := os.WriteFile(path, []byte(`{"foo":"bar"}`), 0o644); err != nil {
			t.Fatal(err)
		}

		servers, err := ReadClaudeJSON(path)
		if err != nil {
			t.Fatalf("ReadClaudeJSON() error = %v", err)
		}
		if len(servers) != 0 {
			t.Errorf("got %d servers, want 0", len(servers))
		}
	})

	t.Run("returns error for missing file", func(t *testing.T) {
		t.Parallel()
		_, err := ReadClaudeJSON("/nonexistent/claude.json")
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "claude.json")
		if err := os.WriteFile(path, []byte(`{invalid`), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := ReadClaudeJSON(path)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

func TestReadDeniedServers(t *testing.T) {
	t.Parallel()

	t.Run("reads serverName entries", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.local.json")
		data := `{
			"deniedMcpServers": [
				{ "serverName": "filesystem" },
				{ "serverName": "memory" }
			],
			"permissions": { "allow": [] }
		}`
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}

		denied, err := ReadDeniedServers(path)
		if err != nil {
			t.Fatalf("ReadDeniedServers() error = %v", err)
		}
		if len(denied) != 2 {
			t.Fatalf("got %d denied, want 2", len(denied))
		}
		if denied[0] != "filesystem" || denied[1] != "memory" {
			t.Errorf("denied = %v, want [filesystem memory]", denied)
		}
	})

	t.Run("returns empty for missing file", func(t *testing.T) {
		t.Parallel()
		denied, err := ReadDeniedServers("/nonexistent/settings.local.json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(denied) != 0 {
			t.Errorf("got %d denied, want 0", len(denied))
		}
	})

	t.Run("returns empty when no deniedMcpServers key", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.local.json")
		if err := os.WriteFile(path, []byte(`{"permissions":{}}`), 0o644); err != nil {
			t.Fatal(err)
		}

		denied, err := ReadDeniedServers(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(denied) != 0 {
			t.Errorf("got %d denied, want 0", len(denied))
		}
	})
}

func TestWriteDeniedServers(t *testing.T) {
	t.Parallel()

	t.Run("creates new file with deniedMcpServers", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.local.json")

		if err := WriteDeniedServers(path, []string{"filesystem", "memory"}); err != nil {
			t.Fatalf("WriteDeniedServers() error = %v", err)
		}

		denied, err := ReadDeniedServers(path)
		if err != nil {
			t.Fatalf("ReadDeniedServers() error = %v", err)
		}
		if len(denied) != 2 || denied[0] != "filesystem" || denied[1] != "memory" {
			t.Errorf("roundtrip denied = %v, want [filesystem memory]", denied)
		}
	})

	t.Run("merges with existing settings", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.local.json")
		existing := `{
			"permissions": { "allow": ["Bash(ls *)"] },
			"deniedMcpServers": [
				{ "serverName": "old" }
			]
		}`
		if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := WriteDeniedServers(path, []string{"filesystem"}); err != nil {
			t.Fatalf("WriteDeniedServers() error = %v", err)
		}

		// Verify deniedMcpServers was updated.
		denied, err := ReadDeniedServers(path)
		if err != nil {
			t.Fatalf("ReadDeniedServers() error = %v", err)
		}
		if len(denied) != 1 || denied[0] != "filesystem" {
			t.Errorf("denied = %v, want [filesystem]", denied)
		}

		// Verify other settings are preserved.
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"permissions"`) {
			t.Error("existing permissions key was lost after merge")
		}
	})

	t.Run("removes deniedMcpServers key when list is empty", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.local.json")
		existing := `{
			"permissions": {},
			"deniedMcpServers": [
				{ "serverName": "old" }
			]
		}`
		if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := WriteDeniedServers(path, nil); err != nil {
			t.Fatalf("WriteDeniedServers() error = %v", err)
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `deniedMcpServers`) {
			t.Error("deniedMcpServers should be removed when list is empty")
		}
		if !strings.Contains(string(raw), `"permissions"`) {
			t.Error("existing permissions key was lost")
		}
	})

	t.Run("creates parent directory if needed", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, ".claude", "settings.local.json")

		if err := WriteDeniedServers(path, []string{"test"}); err != nil {
			t.Fatalf("WriteDeniedServers() error = %v", err)
		}

		denied, err := ReadDeniedServers(path)
		if err != nil {
			t.Fatalf("ReadDeniedServers() error = %v", err)
		}
		if len(denied) != 1 || denied[0] != "test" {
			t.Errorf("denied = %v, want [test]", denied)
		}
	})
}

func TestMergeServers(t *testing.T) {
	t.Parallel()

	user := map[string]ServerConfig{
		"github": {Command: "npx", Args: []string{"-y", "server-github"}},
		"memory": {Command: "npx", Args: []string{"-y", "server-memory"}},
	}
	project := map[string]ServerConfig{
		"my-db": {Command: "node", Args: []string{"db-server.js"}},
	}
	denied := []string{"memory"}

	servers := MergeServers(user, project, denied)

	if len(servers) != 3 {
		t.Fatalf("got %d servers, want 3", len(servers))
	}

	byName := make(map[string]MCPServer, len(servers))
	for _, s := range servers {
		byName[s.Name] = s
	}

	gh, ok := byName["github"]
	if !ok {
		t.Fatal("missing github")
	}
	if gh.Scope != "user" || gh.Denied {
		t.Errorf("github: scope=%q denied=%v, want user/false", gh.Scope, gh.Denied)
	}

	mem, ok := byName["memory"]
	if !ok {
		t.Fatal("missing memory")
	}
	if mem.Scope != "user" || !mem.Denied {
		t.Errorf("memory: scope=%q denied=%v, want user/true", mem.Scope, mem.Denied)
	}

	db, ok := byName["my-db"]
	if !ok {
		t.Fatal("missing my-db")
	}
	if db.Scope != "project" || db.Denied {
		t.Errorf("my-db: scope=%q denied=%v, want project/false", db.Scope, db.Denied)
	}
}

