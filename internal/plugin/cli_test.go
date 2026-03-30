package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// mockRunner records commands and returns pre-set output.
type mockRunner struct {
	output string
	err    error
	called [][]string
}

func (m *mockRunner) Run(_ context.Context, _ string, args ...string) (string, error) {
	m.called = append(m.called, args)
	return m.output, m.err
}

func TestExecCLI_ListInstalled(t *testing.T) {
	installed := []InstalledPlugin{
		{
			ID:      "lua-lsp@claude-plugins-official",
			Version: "1.0.0",
			Scope:   "user",
			Enabled: true,
		},
	}
	data, _ := json.Marshal(installed)

	runner := &mockRunner{output: string(data)}
	cli := NewExecCLI(WithRunner(runner))

	result, err := cli.ListInstalled(context.Background())
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d, want 1", len(result))
	}
	if result[0].ID != "lua-lsp@claude-plugins-official" {
		t.Errorf("ID = %q", result[0].ID)
	}

	if len(runner.called) != 1 {
		t.Fatalf("called %d times, want 1", len(runner.called))
	}
	args := runner.called[0]
	assertContains(t, args, "plugins")
	assertContains(t, args, "list")
	assertContains(t, args, "--json")
}

func TestExecCLI_ListAll(t *testing.T) {
	lr := ListResult{
		Installed: []InstalledPlugin{{ID: "a@b", Enabled: true}},
		Available: []AvailablePlugin{{PluginID: "c@b", Name: "c", InstallCount: 42}},
	}
	data, _ := json.Marshal(lr)

	runner := &mockRunner{output: string(data)}
	cli := NewExecCLI(WithRunner(runner))

	result, err := cli.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(result.Installed) != 1 || len(result.Available) != 1 {
		t.Fatalf("Installed=%d Available=%d", len(result.Installed), len(result.Available))
	}

	args := runner.called[0]
	assertContains(t, args, "--available")
	assertContains(t, args, "--json")
}

func TestExecCLI_ListMarketplaces(t *testing.T) {
	markets := []MarketplaceInfo{{Name: "official", Source: "github", Repo: "anthropics/claude-plugins-official"}}
	data, _ := json.Marshal(markets)

	runner := &mockRunner{output: string(data)}
	cli := NewExecCLI(WithRunner(runner))

	result, err := cli.ListMarketplaces(context.Background())
	if err != nil {
		t.Fatalf("ListMarketplaces: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d, want 1", len(result))
	}
	if result[0].Repo != "anthropics/claude-plugins-official" {
		t.Errorf("Repo = %q", result[0].Repo)
	}

	args := runner.called[0]
	assertContains(t, args, "marketplace")
	assertContains(t, args, "list")
	assertContains(t, args, "--json")
}

func TestExecCLI_Install(t *testing.T) {
	runner := &mockRunner{output: ""}
	cli := NewExecCLI(WithRunner(runner))

	err := cli.Install(context.Background(), "code-review@claude-plugins-official", "user")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	args := runner.called[0]
	assertContains(t, args, "install")
	assertContains(t, args, "code-review@claude-plugins-official")
	assertContains(t, args, "--scope")
	assertContains(t, args, "user")
}

func TestExecCLI_Uninstall(t *testing.T) {
	runner := &mockRunner{output: ""}
	cli := NewExecCLI(WithRunner(runner))

	err := cli.Uninstall(context.Background(), "lua-lsp@claude-plugins-official", "project")
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	args := runner.called[0]
	assertContains(t, args, "uninstall")
	assertContains(t, args, "lua-lsp@claude-plugins-official")
	assertContains(t, args, "--scope")
	assertContains(t, args, "project")
}

func TestExecCLI_Enable(t *testing.T) {
	runner := &mockRunner{output: ""}
	cli := NewExecCLI(WithRunner(runner))

	err := cli.Enable(context.Background(), "lua-lsp@claude-plugins-official", "project")
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}

	args := runner.called[0]
	assertContains(t, args, "enable")
	assertContains(t, args, "lua-lsp@claude-plugins-official")
	assertContains(t, args, "--scope")
	assertContains(t, args, "project")
}

func TestExecCLI_Disable(t *testing.T) {
	runner := &mockRunner{output: ""}
	cli := NewExecCLI(WithRunner(runner))

	err := cli.Disable(context.Background(), "lua-lsp@claude-plugins-official", "user")
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}

	args := runner.called[0]
	assertContains(t, args, "disable")
	assertContains(t, args, "lua-lsp@claude-plugins-official")
	assertContains(t, args, "--scope")
	assertContains(t, args, "user")
}

func TestExecCLI_RunnerError(t *testing.T) {
	runner := &mockRunner{err: fmt.Errorf("command failed")}
	cli := NewExecCLI(WithRunner(runner))

	_, err := cli.ListInstalled(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecCLI_InvalidJSON(t *testing.T) {
	runner := &mockRunner{output: "not json"}
	cli := NewExecCLI(WithRunner(runner))

	_, err := cli.ListInstalled(context.Background())
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestExecCLI_Update(t *testing.T) {
	runner := &mockRunner{output: ""}
	cli := NewExecCLI(WithRunner(runner))

	err := cli.Update(context.Background(), "lua-lsp@claude-plugins-official")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	args := runner.called[0]
	assertContains(t, args, "update")
	assertContains(t, args, "lua-lsp@claude-plugins-official")
}

func TestExecCLI_MarketplaceAdd(t *testing.T) {
	runner := &mockRunner{output: ""}
	cli := NewExecCLI(WithRunner(runner))

	err := cli.MarketplaceAdd(context.Background(), "anthropics/claude-plugins-official")
	if err != nil {
		t.Fatalf("MarketplaceAdd: %v", err)
	}

	args := runner.called[0]
	assertContains(t, args, "marketplace")
	assertContains(t, args, "add")
	assertContains(t, args, "anthropics/claude-plugins-official")
}

func TestExecCLI_MarketplaceRemove(t *testing.T) {
	runner := &mockRunner{output: ""}
	cli := NewExecCLI(WithRunner(runner))

	err := cli.MarketplaceRemove(context.Background(), "official")
	if err != nil {
		t.Fatalf("MarketplaceRemove: %v", err)
	}

	args := runner.called[0]
	assertContains(t, args, "marketplace")
	assertContains(t, args, "remove")
	assertContains(t, args, "official")
}

func TestExecCLI_MarketplaceUpdate(t *testing.T) {
	runner := &mockRunner{output: ""}
	cli := NewExecCLI(WithRunner(runner))

	err := cli.MarketplaceUpdate(context.Background(), "official")
	if err != nil {
		t.Fatalf("MarketplaceUpdate: %v", err)
	}

	args := runner.called[0]
	assertContains(t, args, "marketplace")
	assertContains(t, args, "update")
	assertContains(t, args, "official")
}

func TestExecCLI_MarketplaceUpdateAll(t *testing.T) {
	runner := &mockRunner{output: ""}
	cli := NewExecCLI(WithRunner(runner))

	err := cli.MarketplaceUpdate(context.Background(), "")
	if err != nil {
		t.Fatalf("MarketplaceUpdate all: %v", err)
	}

	args := runner.called[0]
	assertContains(t, args, "marketplace")
	assertContains(t, args, "update")
	// Should NOT contain empty string arg
	if len(args) != 3 {
		t.Errorf("expected 3 args, got %d: %v", len(args), args)
	}
}

func TestExecCLI_WithClaudePath(t *testing.T) {
	cli := NewExecCLI(WithClaudePath("/usr/local/bin/claude"))
	er, ok := cli.runner.(*execRunner)
	if !ok {
		t.Fatal("expected *execRunner")
	}
	if er.claudePath != "/usr/local/bin/claude" {
		t.Errorf("claudePath = %q", er.claudePath)
	}
}

func assertContains(t *testing.T, args []string, want string) {
	t.Helper()
	for _, a := range args {
		if a == want {
			return
		}
	}
	t.Errorf("args %v does not contain %q", args, want)
}
