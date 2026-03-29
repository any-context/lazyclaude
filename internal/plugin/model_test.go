package plugin

import (
	"encoding/json"
	"testing"
)

func TestMarketplaceName(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"lua-lsp@claude-plugins-official", "claude-plugins-official"},
		{"code-review@custom", "custom"},
		{"no-marketplace", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := MarketplaceName(tt.id); got != tt.want {
				t.Errorf("MarketplaceName(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestInstalledPlugin_JSONParse(t *testing.T) {
	// Actual output from: claude plugins list --json
	raw := `[
		{
			"id": "lua-lsp@claude-plugins-official",
			"version": "1.0.0",
			"scope": "user",
			"enabled": true,
			"installPath": "/Users/test/.claude/plugins/cache/claude-plugins-official/lua-lsp/1.0.0",
			"installedAt": "2026-03-04T16:26:07.583Z",
			"lastUpdated": "2026-03-04T16:26:07.583Z"
		},
		{
			"id": "pyright-lsp@claude-plugins-official",
			"version": "1.0.0",
			"scope": "user",
			"enabled": false,
			"installPath": "/Users/test/.claude/plugins/cache/claude-plugins-official/pyright-lsp/1.0.0",
			"installedAt": "2026-03-14T19:51:54.467Z",
			"lastUpdated": "2026-03-14T19:51:54.467Z"
		}
	]`

	var plugins []InstalledPlugin
	if err := json.Unmarshal([]byte(raw), &plugins); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(plugins) != 2 {
		t.Fatalf("got %d plugins, want 2", len(plugins))
	}

	p := plugins[0]
	if p.ID != "lua-lsp@claude-plugins-official" {
		t.Errorf("ID = %q", p.ID)
	}
	if p.Version != "1.0.0" {
		t.Errorf("Version = %q", p.Version)
	}
	if p.Scope != "user" {
		t.Errorf("Scope = %q", p.Scope)
	}
	if !p.Enabled {
		t.Error("Enabled = false, want true")
	}

	p2 := plugins[1]
	if p2.Enabled {
		t.Error("second plugin: Enabled = true, want false")
	}
}

func TestListResult_JSONParse(t *testing.T) {
	// Actual output from: claude plugins list --available --json
	// Includes both struct source and string source (local path)
	raw := `{
		"installed": [
			{
				"id": "lua-lsp@claude-plugins-official",
				"version": "1.0.0",
				"scope": "user",
				"enabled": true,
				"installPath": "/Users/test/.claude/plugins/cache/claude-plugins-official/lua-lsp/1.0.0",
				"installedAt": "2026-03-04T16:26:07.583Z",
				"lastUpdated": "2026-03-04T16:26:07.583Z"
			}
		],
		"available": [
			{
				"pluginId": "code-review@claude-plugins-official",
				"name": "code-review",
				"description": "Cross-platform ad management",
				"marketplaceName": "claude-plugins-official",
				"source": {
					"source": "url",
					"url": "https://github.com/example/plugin.git",
					"sha": "aa70dbdbbbb843e94a794c10c2b13f5dd66b5e40"
				},
				"installCount": 899
			},
			{
				"pluginId": "agent-sdk-dev@claude-plugins-official",
				"name": "agent-sdk-dev",
				"description": "Agent SDK development tools",
				"marketplaceName": "claude-plugins-official",
				"source": "./plugins/agent-sdk-dev",
				"installCount": 50
			}
		]
	}`

	var result ListResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(result.Installed) != 1 {
		t.Fatalf("Installed: got %d, want 1", len(result.Installed))
	}
	if len(result.Available) != 2 {
		t.Fatalf("Available: got %d, want 2", len(result.Available))
	}

	avail := result.Available[0]
	if avail.PluginID != "code-review@claude-plugins-official" {
		t.Errorf("PluginID = %q", avail.PluginID)
	}
	if avail.Name != "code-review" {
		t.Errorf("Name = %q", avail.Name)
	}
	if avail.InstallCount != 899 {
		t.Errorf("InstallCount = %d", avail.InstallCount)
	}
	if avail.Source.Source != "url" {
		t.Errorf("Source.Source = %q", avail.Source.Source)
	}
	if avail.Source.SHA != "aa70dbdbbbb843e94a794c10c2b13f5dd66b5e40" {
		t.Errorf("Source.SHA = %q", avail.Source.SHA)
	}

	// Second plugin: string source (local path)
	avail2 := result.Available[1]
	if avail2.Source.Source != "path" {
		t.Errorf("avail2 Source.Source = %q, want \"path\"", avail2.Source.Source)
	}
	if avail2.Source.Raw != "./plugins/agent-sdk-dev" {
		t.Errorf("avail2 Source.Raw = %q", avail2.Source.Raw)
	}
}

func TestMarketplaceInfo_JSONParse(t *testing.T) {
	// Actual output from: claude plugins marketplace list --json
	raw := `[
		{
			"name": "claude-plugins-official",
			"source": "github",
			"repo": "anthropics/claude-plugins-official",
			"installLocation": "/Users/test/.claude/plugins/marketplaces/claude-plugins-official"
		}
	]`

	var markets []MarketplaceInfo
	if err := json.Unmarshal([]byte(raw), &markets); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(markets) != 1 {
		t.Fatalf("got %d, want 1", len(markets))
	}

	m := markets[0]
	if m.Name != "claude-plugins-official" {
		t.Errorf("Name = %q", m.Name)
	}
	if m.Source != "github" {
		t.Errorf("Source = %q", m.Source)
	}
	if m.Repo != "anthropics/claude-plugins-official" {
		t.Errorf("Repo = %q", m.Repo)
	}
}
