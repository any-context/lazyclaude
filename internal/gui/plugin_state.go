package gui

import (
	"context"

	"github.com/KEMSHlM/lazyclaude/internal/gui/keymap"
)

// PluginItem is a read-only view of an installed plugin for display.
type PluginItem struct {
	ID          string
	Version     string
	Scope       string // "project"
	Enabled     bool
	InstalledAt string // ISO 8601
}

// AvailablePluginItem is a read-only view of a marketplace plugin for display.
type AvailablePluginItem struct {
	PluginID        string
	Name            string
	Description     string
	MarketplaceName string
	InstallCount    int
}

// PluginProvider abstracts plugin operations for the GUI layer.
type PluginProvider interface {
	// SetProjectDir switches the project context. Subsequent operations
	// (Refresh, Install, Enable, Disable) apply to this project.
	SetProjectDir(dir string)
	Refresh(ctx context.Context) error
	Installed() []PluginItem
	Available() []AvailablePluginItem
	Install(ctx context.Context, pluginID string) error
	Uninstall(ctx context.Context, pluginID string, scope string) error
	ToggleEnabled(ctx context.Context, pluginID string, scope string) error
	Update(ctx context.Context, pluginID string) error
}

// PluginState holds the UI state for the plugins panel.
type PluginState struct {
	tabIdx          int // one of keymap.PluginTab* constants
	installedCursor int
	marketCursor    int
	loading         bool
	errMsg          string
	projectDir      string // current project context (from Sessions cursor)
}

// NewPluginState creates a new PluginState.
func NewPluginState() *PluginState {
	return &PluginState{}
}

// Cursor returns the cursor for the active tab.
func (ps *PluginState) Cursor() int {
	if ps.tabIdx == keymap.PluginTabMarketplace {
		return ps.marketCursor
	}
	return ps.installedCursor
}

// SetCursor sets the cursor for the active tab.
func (ps *PluginState) SetCursor(n int) {
	if ps.tabIdx == keymap.PluginTabMarketplace {
		ps.marketCursor = n
	} else {
		ps.installedCursor = n
	}
}
