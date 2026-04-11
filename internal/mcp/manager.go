package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/any-context/lazyclaude/internal/core/shell"
	"github.com/any-context/lazyclaude/internal/daemon"
)

// Remote user-level config path. Double-quoted so the remote shell
// expands $HOME when the command executes — the only reliable way to
// resolve the remote user's home directory without a separate probe.
const remoteUserConfigPath = `"$HOME/.claude.json"`

// Manager coordinates MCP server state between config files and the TUI.
// It reads user-level and project-level configs, tracks per-project deny
// lists, and provides thread-safe access to the merged server list.
//
// When host is empty the Manager reads and writes local files via the
// os package. When host is non-empty the Manager delegates file IO to
// the injected SSHExecutor, running commands on the remote host and
// treating projectDir as the remote absolute path.
//
// Locking order: writeMu MUST be acquired before mu when both are held.
// writeMu serialises the read-modify-write critical section of
// ToggleDenied so two concurrent calls cannot clobber each other's
// updates to the settings file. mu guards the cached fields.
type Manager struct {
	writeMu    sync.Mutex // serialises ToggleDenied's read-modify-write
	mu         sync.RWMutex
	servers    []MCPServer
	userConfig string // local ~/.claude.json (used only when host == "")
	projectDir string // project directory (local path or remote absolute path)
	host       string // "" for local, SSH host spec for remote
	ssh        daemon.SSHExecutor
}

// NewManager creates a new Manager.
// userConfig is the local path to ~/.claude.json. The SSHExecutor is
// used only after SetHost is called with a non-empty host.
func NewManager(userConfig string, ssh daemon.SSHExecutor) *Manager {
	return &Manager{
		userConfig: userConfig,
		ssh:        ssh,
	}
}

// SetProjectDir sets the project directory for subsequent operations.
// For remote managers the directory must be an absolute path valid on
// the remote host. Prefer SetRemote when callers also need to change
// the host — otherwise the two setters introduce a mixed-pair window
// that an async Refresh could observe.
func (m *Manager) SetProjectDir(dir string) {
	m.mu.Lock()
	m.projectDir = dir
	m.mu.Unlock()
}

// SetHost switches the manager between local and SSH-backed modes.
// An empty host restores the local code path. Prefer SetRemote when
// callers also need to change projectDir atomically.
func (m *Manager) SetHost(host string) {
	m.mu.Lock()
	m.host = host
	m.mu.Unlock()
}

// SetRemote atomically updates both host and projectDir under a
// single lock acquisition. This is the preferred setter for GUI code
// paths because it eliminates the mixed-pair window that SetHost
// followed by SetProjectDir would expose to a concurrent Refresh /
// ToggleDenied running on a previously-spawned goroutine.
//
// Passing host="" installs the local code path; passing host="" with
// an empty dir is equivalent to "no target, operate in user-config
// only" mode.
func (m *Manager) SetRemote(host, projectDir string) {
	m.mu.Lock()
	m.host = host
	m.projectDir = projectDir
	m.mu.Unlock()
}

// Refresh reloads server configs and deny lists from disk (local) or
// from the remote host via SSH.
func (m *Manager) Refresh(ctx context.Context) error {
	m.mu.RLock()
	projDir := m.projectDir
	host := m.host
	m.mu.RUnlock()

	var (
		userServers    map[string]ServerConfig
		projectServers map[string]ServerConfig
		denied         []string
		err            error
	)

	if host == "" {
		userServers, projectServers, denied, err = m.refreshLocal(projDir)
	} else {
		userServers, projectServers, denied, err = m.refreshRemote(ctx, host, projDir)
	}
	if err != nil {
		return err
	}

	merged := MergeServers(userServers, projectServers, denied)

	m.mu.Lock()
	m.servers = merged
	m.mu.Unlock()

	return nil
}

// refreshLocal reads the three config files from the local filesystem.
func (m *Manager) refreshLocal(projDir string) (map[string]ServerConfig, map[string]ServerConfig, []string, error) {
	userServers, err := ReadClaudeJSON(m.userConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read user config: %w", err)
	}

	var projectServers map[string]ServerConfig
	if projDir != "" {
		mcpPath := filepath.Join(projDir, ".mcp.json")
		projectServers, err = ReadClaudeJSON(mcpPath)
		if err != nil {
			// .mcp.json is optional.
			projectServers = nil
		}
	}

	var denied []string
	if projDir != "" {
		settingsPath := filepath.Join(projDir, ".claude", "settings.local.json")
		denied, _ = ReadDeniedServers(settingsPath)
	}

	return userServers, projectServers, denied, nil
}

// refreshRemote reads the three config files from the remote host via
// SSH. Project-level files (.mcp.json and settings.local.json) are
// optional: read errors fall back to empty, matching the local path.
//
// host is captured by Refresh under m.mu and passed through so the SSH
// helpers cannot be redirected mid-flight by a concurrent SetHost.
func (m *Manager) refreshRemote(ctx context.Context, host, projDir string) (map[string]ServerConfig, map[string]ServerConfig, []string, error) {
	// User-level config: mandatory. An SSH failure must propagate so
	// the caller sees a real error instead of an empty list.
	userJSON, err := m.sshReadFile(ctx, host, remoteUserConfigPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read user config: %w", err)
	}
	var userServers map[string]ServerConfig
	if userJSON != "" {
		userServers, err = parseClaudeJSON([]byte(userJSON))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("parse user config: %w", err)
		}
	}

	var projectServers map[string]ServerConfig
	var denied []string
	if projDir != "" {
		mcpJSONPath := shell.Quote(projDir + "/.mcp.json")
		if projectJSON, readErr := m.sshReadFile(ctx, host, mcpJSONPath); readErr == nil && projectJSON != "" {
			// .mcp.json is optional — ignore parse errors to match local.
			projectServers, _ = parseClaudeJSON([]byte(projectJSON))
		}

		settingsPath := shell.Quote(projDir + "/.claude/settings.local.json")
		if settingsJSON, readErr := m.sshReadFile(ctx, host, settingsPath); readErr == nil && settingsJSON != "" {
			denied, _ = parseDeniedServers([]byte(settingsJSON))
		}
	}

	return userServers, projectServers, denied, nil
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
//
// Holds writeMu for the entire read-modify-write sequence so concurrent
// callers cannot clobber each other's updates — the cached state
// queried at the top may still be stale when we write, but two
// simultaneous toggles will serialise cleanly instead of racing on the
// filesystem / remote shell.
func (m *Manager) ToggleDenied(ctx context.Context, serverName string) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	m.mu.RLock()
	projDir := m.projectDir
	host := m.host
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

	if host == "" {
		if err := m.toggleDeniedLocal(projDir, serverName, currentlyDenied); err != nil {
			return err
		}
	} else {
		if err := m.toggleDeniedRemote(ctx, host, projDir, serverName, currentlyDenied); err != nil {
			return err
		}
	}

	// Re-read to update cached state. This deliberately re-reads
	// m.host rather than reusing the captured snapshot: if the user
	// navigated away during the write, the cache should reflect the
	// now-current selection, not the host whose file we just mutated.
	return m.Refresh(ctx)
}

// toggleDeniedLocal performs the read-modify-write on the local
// settings.local.json.
func (m *Manager) toggleDeniedLocal(projDir, serverName string, currentlyDenied bool) error {
	settingsPath := filepath.Join(projDir, ".claude", "settings.local.json")

	denied, err := ReadDeniedServers(settingsPath)
	if err != nil {
		return fmt.Errorf("read denied servers: %w", err)
	}

	denied = toggleDenied(denied, serverName, currentlyDenied)

	if err := WriteDeniedServers(settingsPath, denied); err != nil {
		return fmt.Errorf("write denied servers: %w", err)
	}
	return nil
}

// toggleDeniedRemote performs the read-modify-write against the remote
// settings.local.json while preserving all unrelated top-level keys.
// host is captured by the caller so the entire RMW targets a single
// machine even if SetHost is racing from the GUI goroutine.
func (m *Manager) toggleDeniedRemote(ctx context.Context, host, projDir, serverName string, currentlyDenied bool) error {
	settingsPath := shell.Quote(projDir + "/.claude/settings.local.json")

	existingJSON, err := m.sshReadFile(ctx, host, settingsPath)
	if err != nil {
		return fmt.Errorf("read remote settings: %w", err)
	}

	currentDenied, err := parseDeniedServers([]byte(existingJSON))
	if err != nil {
		return fmt.Errorf("parse remote settings: %w", err)
	}
	updated := toggleDenied(currentDenied, serverName, currentlyDenied)

	newJSON, err := updateDeniedInJSON([]byte(existingJSON), updated)
	if err != nil {
		return fmt.Errorf("build updated settings: %w", err)
	}

	if err := m.sshWriteFile(ctx, host, settingsPath, string(newJSON)); err != nil {
		return fmt.Errorf("write remote settings: %w", err)
	}
	return nil
}

// toggleDenied returns the deny list after adding or removing serverName
// based on its current state. The returned slice is independent of the
// input.
func toggleDenied(current []string, serverName string, currentlyDenied bool) []string {
	if currentlyDenied {
		return removeFromSlice(current, serverName)
	}
	// Avoid duplicate entries if the caller's cached state disagrees.
	for _, name := range current {
		if name == serverName {
			out := make([]string, len(current))
			copy(out, current)
			return out
		}
	}
	out := make([]string, 0, len(current)+1)
	out = append(out, current...)
	out = append(out, serverName)
	return out
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
