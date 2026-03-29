package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// fakeRunner dispatches by first arg pattern to return different outputs.
type fakeRunner struct {
	handlers map[string]func(args []string) (string, error)
}

func (f *fakeRunner) Run(_ context.Context, _ string, args ...string) (string, error) {
	key := ""
	for _, a := range args {
		if key != "" {
			key += " "
		}
		key += a
	}
	// Try longest match first, then progressively shorter.
	for i := len(args); i > 0; i-- {
		k := ""
		for j := 0; j < i; j++ {
			if j > 0 {
				k += " "
			}
			k += args[j]
		}
		if h, ok := f.handlers[k]; ok {
			return h(args)
		}
	}
	return "", fmt.Errorf("no handler for: %v", args)
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{handlers: make(map[string]func([]string) (string, error))}
}

func TestManager_Refresh(t *testing.T) {
	runner := newFakeRunner()

	lr := ListResult{
		Installed: []InstalledPlugin{
			{ID: "lua-lsp@official", Version: "1.0.0", Scope: "user", Enabled: true},
			{ID: "pyright@official", Version: "1.0.0", Scope: "user", Enabled: false},
		},
		Available: []AvailablePlugin{
			{PluginID: "code-review@official", Name: "code-review", InstallCount: 100},
		},
	}
	lrData, _ := json.Marshal(lr)
	runner.handlers["plugins list"] = func(_ []string) (string, error) {
		return string(lrData), nil
	}

	markets := []MarketplaceInfo{
		{Name: "official", Source: "github", Repo: "anthropics/claude-plugins-official"},
	}
	mkData, _ := json.Marshal(markets)
	runner.handlers["plugins marketplace"] = func(_ []string) (string, error) {
		return string(mkData), nil
	}

	mgr := NewManager(NewExecCLI(WithRunner(runner)), nil)

	ctx := context.Background()
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	installed := mgr.Installed()
	if len(installed) != 2 {
		t.Fatalf("Installed: got %d, want 2", len(installed))
	}

	available := mgr.Available()
	if len(available) != 1 {
		t.Fatalf("Available: got %d, want 1", len(available))
	}

	mks := mgr.Marketplaces()
	if len(mks) != 1 {
		t.Fatalf("Marketplaces: got %d, want 1", len(mks))
	}
}

func TestManager_Install(t *testing.T) {
	runner := newFakeRunner()
	installCalled := false

	runner.handlers["plugins install"] = func(args []string) (string, error) {
		installCalled = true
		return "", nil
	}

	// After install, Refresh is called
	lr := ListResult{
		Installed: []InstalledPlugin{{ID: "code-review@official", Version: "1.0.0", Scope: "user", Enabled: true}},
	}
	lrData, _ := json.Marshal(lr)
	runner.handlers["plugins list"] = func(_ []string) (string, error) {
		return string(lrData), nil
	}
	mkData, _ := json.Marshal([]MarketplaceInfo{})
	runner.handlers["plugins marketplace"] = func(_ []string) (string, error) {
		return string(mkData), nil
	}

	mgr := NewManager(NewExecCLI(WithRunner(runner)), nil)

	err := mgr.Install(context.Background(), "code-review@official", "user")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !installCalled {
		t.Error("install command not called")
	}

	// Cache should be refreshed
	if len(mgr.Installed()) != 1 {
		t.Errorf("Installed after install: got %d, want 1", len(mgr.Installed()))
	}
}

func TestManager_Uninstall(t *testing.T) {
	runner := newFakeRunner()
	uninstallCalled := false

	runner.handlers["plugins uninstall"] = func(_ []string) (string, error) {
		uninstallCalled = true
		return "", nil
	}

	lr := ListResult{}
	lrData, _ := json.Marshal(lr)
	runner.handlers["plugins list"] = func(_ []string) (string, error) {
		return string(lrData), nil
	}
	mkData, _ := json.Marshal([]MarketplaceInfo{})
	runner.handlers["plugins marketplace"] = func(_ []string) (string, error) {
		return string(mkData), nil
	}

	mgr := NewManager(NewExecCLI(WithRunner(runner)), nil)

	err := mgr.Uninstall(context.Background(), "lua-lsp@official")
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !uninstallCalled {
		t.Error("uninstall command not called")
	}
}

func TestManager_ToggleEnabled(t *testing.T) {
	runner := newFakeRunner()
	var lastCmd string

	runner.handlers["plugins disable"] = func(_ []string) (string, error) {
		lastCmd = "disable"
		return "", nil
	}
	runner.handlers["plugins enable"] = func(_ []string) (string, error) {
		lastCmd = "enable"
		return "", nil
	}

	lr := ListResult{
		Installed: []InstalledPlugin{
			{ID: "lua-lsp@official", Version: "1.0.0", Scope: "user", Enabled: true},
			{ID: "pyright@official", Version: "1.0.0", Scope: "user", Enabled: false},
		},
	}
	lrData, _ := json.Marshal(lr)
	runner.handlers["plugins list"] = func(_ []string) (string, error) {
		return string(lrData), nil
	}
	mkData, _ := json.Marshal([]MarketplaceInfo{})
	runner.handlers["plugins marketplace"] = func(_ []string) (string, error) {
		return string(mkData), nil
	}

	mgr := NewManager(NewExecCLI(WithRunner(runner)), nil)
	ctx := context.Background()

	// First refresh to populate cache
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Toggle enabled plugin -> should disable
	if err := mgr.ToggleEnabled(ctx, "lua-lsp@official"); err != nil {
		t.Fatalf("ToggleEnabled (disable): %v", err)
	}
	if lastCmd != "disable" {
		t.Errorf("expected disable, got %q", lastCmd)
	}

	// Toggle disabled plugin -> should enable
	if err := mgr.ToggleEnabled(ctx, "pyright@official"); err != nil {
		t.Fatalf("ToggleEnabled (enable): %v", err)
	}
	if lastCmd != "enable" {
		t.Errorf("expected enable, got %q", lastCmd)
	}
}

func TestManager_ToggleEnabled_NotFound(t *testing.T) {
	runner := newFakeRunner()

	lr := ListResult{}
	lrData, _ := json.Marshal(lr)
	runner.handlers["plugins list"] = func(_ []string) (string, error) {
		return string(lrData), nil
	}
	mkData, _ := json.Marshal([]MarketplaceInfo{})
	runner.handlers["plugins marketplace"] = func(_ []string) (string, error) {
		return string(mkData), nil
	}

	mgr := NewManager(NewExecCLI(WithRunner(runner)), nil)
	ctx := context.Background()
	_ = mgr.Refresh(ctx)

	err := mgr.ToggleEnabled(ctx, "nonexistent@official")
	if err == nil {
		t.Fatal("expected error for nonexistent plugin")
	}
}

func TestManager_Refresh_FallbackToListInstalled(t *testing.T) {
	runner := newFakeRunner()

	runner.handlers["plugins list --available"] = func(_ []string) (string, error) {
		return "", fmt.Errorf("network error")
	}

	installed := []InstalledPlugin{
		{ID: "lua-lsp@official", Version: "1.0.0", Scope: "user", Enabled: true},
	}
	instData, _ := json.Marshal(installed)
	runner.handlers["plugins list --json"] = func(_ []string) (string, error) {
		return string(instData), nil
	}

	mkData, _ := json.Marshal([]MarketplaceInfo{})
	runner.handlers["plugins marketplace"] = func(_ []string) (string, error) {
		return string(mkData), nil
	}

	mgr := NewManager(NewExecCLI(WithRunner(runner)), nil)

	if err := mgr.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh should succeed with fallback: %v", err)
	}
	if got := len(mgr.Installed()); got != 1 {
		t.Errorf("Installed: got %d, want 1", got)
	}
	if got := len(mgr.Available()); got != 0 {
		t.Errorf("Available: got %d, want 0", got)
	}
}

func TestManager_Refresh_MarketplaceFailureNonFatal(t *testing.T) {
	runner := newFakeRunner()

	lr := ListResult{
		Installed: []InstalledPlugin{
			{ID: "lua-lsp@official", Version: "1.0.0", Scope: "user", Enabled: true},
		},
	}
	lrData, _ := json.Marshal(lr)
	runner.handlers["plugins list"] = func(_ []string) (string, error) {
		return string(lrData), nil
	}

	runner.handlers["plugins marketplace"] = func(_ []string) (string, error) {
		return "", fmt.Errorf("marketplace unavailable")
	}

	mgr := NewManager(NewExecCLI(WithRunner(runner)), nil)

	if err := mgr.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh should succeed despite marketplace failure: %v", err)
	}
	if got := len(mgr.Installed()); got != 1 {
		t.Errorf("Installed: got %d, want 1", got)
	}
}

func TestManager_ConcurrentRefresh(t *testing.T) {
	runner := newFakeRunner()

	lr := ListResult{
		Installed: []InstalledPlugin{{ID: "a@b", Enabled: true}},
	}
	lrData, _ := json.Marshal(lr)
	runner.handlers["plugins list"] = func(_ []string) (string, error) {
		return string(lrData), nil
	}
	mkData, _ := json.Marshal([]MarketplaceInfo{})
	runner.handlers["plugins marketplace"] = func(_ []string) (string, error) {
		return string(mkData), nil
	}

	mgr := NewManager(NewExecCLI(WithRunner(runner)), nil)
	ctx := context.Background()

	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			errs <- mgr.Refresh(ctx)
		}()
	}
	for i := 0; i < 10; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Refresh: %v", err)
		}
	}
}
