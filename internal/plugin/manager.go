package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Manager coordinates plugin operations between the TUI and CLI.
type Manager struct {
	cli *ExecCLI
	log *slog.Logger
	mu  sync.RWMutex

	installed []InstalledPlugin
	available []AvailablePlugin
	markets   []MarketplaceInfo
}

// NewManager creates a new Manager with the given CLI and logger.
func NewManager(cli *ExecCLI, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		cli: cli,
		log: log,
	}
}

// SetProjectDir changes the project context for all CLI operations.
// Subsequent Refresh/Install/Enable/Disable calls will operate on this project.
func (m *Manager) SetProjectDir(dir string) {
	m.cli.SetProjectDir(dir)
}

// Refresh reloads all plugin data from the CLI.
// If ListAll (which includes marketplace available list) fails, it falls
// back to ListInstalled so that installed plugins are always displayed.
// ListMarketplaces failure is non-fatal and only logged.
func (m *Manager) Refresh(ctx context.Context) error {
	var installed []InstalledPlugin
	var available []AvailablePlugin

	result, err := m.cli.ListAll(ctx)
	if err != nil {
		// Fallback: at minimum, show installed plugins.
		inst, instErr := m.cli.ListInstalled(ctx)
		if instErr != nil {
			return fmt.Errorf("refresh plugins: ListAll: %w; ListInstalled: %w", err, instErr)
		}
		installed = inst
	} else {
		installed = result.Installed
		available = result.Available
	}

	markets, mErr := m.cli.ListMarketplaces(ctx)
	if mErr != nil {
		// Non-fatal: marketplace data is optional for displaying installed plugins.
		markets = nil
	}

	m.mu.Lock()
	m.installed = installed
	m.available = available
	m.markets = markets
	m.mu.Unlock()

	return nil
}

// Installed returns a copy of the cached installed plugins.
func (m *Manager) Installed() []InstalledPlugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]InstalledPlugin, len(m.installed))
	copy(result, m.installed)
	return result
}

// Available returns a copy of the cached available plugins.
func (m *Manager) Available() []AvailablePlugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]AvailablePlugin, len(m.available))
	copy(result, m.available)
	return result
}

// Marketplaces returns a copy of the cached marketplace list.
func (m *Manager) Marketplaces() []MarketplaceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]MarketplaceInfo, len(m.markets))
	copy(result, m.markets)
	return result
}

// Install installs a plugin and refreshes the cache.
func (m *Manager) Install(ctx context.Context, pluginID string, scope string) error {
	if err := m.cli.Install(ctx, pluginID, scope); err != nil {
		return err
	}
	return m.Refresh(ctx)
}

// Uninstall removes a plugin and refreshes the cache.
func (m *Manager) Uninstall(ctx context.Context, pluginID string) error {
	if err := m.cli.Uninstall(ctx, pluginID); err != nil {
		return err
	}
	return m.Refresh(ctx)
}

// ToggleEnabled enables a disabled plugin or disables an enabled one.
func (m *Manager) ToggleEnabled(ctx context.Context, pluginID string) error {
	m.mu.RLock()
	var found *InstalledPlugin
	for i := range m.installed {
		if m.installed[i].ID == pluginID {
			p := m.installed[i]
			found = &p
			break
		}
	}
	m.mu.RUnlock()

	if found == nil {
		return fmt.Errorf("plugin not found: %s", pluginID)
	}

	if found.Enabled {
		if err := m.cli.Disable(ctx, pluginID); err != nil {
			return err
		}
	} else {
		if err := m.cli.Enable(ctx, pluginID); err != nil {
			return err
		}
	}

	return m.Refresh(ctx)
}

// Update updates a plugin and refreshes the cache.
func (m *Manager) Update(ctx context.Context, pluginID string) error {
	if err := m.cli.Update(ctx, pluginID); err != nil {
		return err
	}
	return m.Refresh(ctx)
}
