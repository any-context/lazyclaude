package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
)

// Manager coordinates MCP server state between config files and the TUI.
// It reads user-level and project-level configs, tracks per-project deny
// lists, and provides thread-safe access to the merged server list.
type Manager struct {
	mu         sync.RWMutex
	servers    []MCPServer
	userConfig string // path to ~/.claude.json
	projectDir string // current project directory
}

// NewManager creates a new Manager.
// userConfig is the path to ~/.claude.json.
func NewManager(userConfig string) *Manager {
	return &Manager{
		userConfig: userConfig,
	}
}

// SetProjectDir sets the project directory for subsequent operations.
func (m *Manager) SetProjectDir(dir string) {
	m.mu.Lock()
	m.projectDir = dir
	m.mu.Unlock()
}

// Refresh reloads server configs and deny lists from disk.
func (m *Manager) Refresh(_ context.Context) error {
	m.mu.RLock()
	projDir := m.projectDir
	m.mu.RUnlock()

	// Read user-level servers.
	userServers, err := ReadClaudeJSON(m.userConfig)
	if err != nil {
		return fmt.Errorf("read user config: %w", err)
	}

	// Read project-level servers (optional).
	var projectServers map[string]ServerConfig
	if projDir != "" {
		mcpPath := filepath.Join(projDir, ".mcp.json")
		projectServers, err = ReadClaudeJSON(mcpPath)
		if err != nil {
			// .mcp.json is optional.
			projectServers = nil
		}
	}

	// Read denied servers (optional).
	var denied []string
	if projDir != "" {
		settingsPath := filepath.Join(projDir, ".claude", "settings.local.json")
		denied, _ = ReadDeniedServers(settingsPath)
	}

	merged := MergeServers(userServers, projectServers, denied)

	m.mu.Lock()
	m.servers = merged
	m.mu.Unlock()

	return nil
}

// Servers returns a copy of the cached server list.
func (m *Manager) Servers() []MCPServer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]MCPServer, len(m.servers))
	copy(result, m.servers)
	return result
}

// ToggleDenied toggles a server's denied state for the current project.
// If the server is currently denied, it is removed from the deny list (enabled).
// If the server is currently allowed, it is added to the deny list (disabled).
func (m *Manager) ToggleDenied(_ context.Context, serverName string) error {
	m.mu.RLock()
	projDir := m.projectDir
	var found bool
	var currentlyDenied bool
	for _, s := range m.servers {
		if s.Name == serverName {
			found = true
			currentlyDenied = s.Denied
			break
		}
	}
	m.mu.RUnlock()

	if !found {
		return fmt.Errorf("server not found: %s", serverName)
	}
	if projDir == "" {
		return fmt.Errorf("no project directory set")
	}

	settingsPath := filepath.Join(projDir, ".claude", "settings.local.json")

	// Read current deny list.
	denied, err := ReadDeniedServers(settingsPath)
	if err != nil {
		return fmt.Errorf("read denied servers: %w", err)
	}

	if currentlyDenied {
		// Remove from deny list.
		denied = removeFromSlice(denied, serverName)
	} else {
		// Add to deny list.
		denied = append(denied, serverName)
	}

	if err := WriteDeniedServers(settingsPath, denied); err != nil {
		return fmt.Errorf("write denied servers: %w", err)
	}

	// Re-read to update cached state.
	return m.Refresh(context.Background())
}

func removeFromSlice(s []string, val string) []string {
	result := make([]string, 0, len(s))
	for _, v := range s {
		if v != val {
			result = append(result, v)
		}
	}
	return result
}
