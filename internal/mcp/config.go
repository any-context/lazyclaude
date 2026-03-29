package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// claudeJSON is the top-level structure of ~/.claude.json (partial).
type claudeJSON struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// settingsLocal is the structure of {project}/.claude/settings.local.json (partial).
type settingsLocal struct {
	DeniedMcpServers []deniedEntry `json:"deniedMcpServers,omitempty"`
}

type deniedEntry struct {
	ServerName string `json:"serverName"`
}

// ReadClaudeJSON reads MCP server definitions from a claude.json file.
// The file may be ~/.claude.json or {project}/.mcp.json.
func ReadClaudeJSON(path string) (map[string]ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cj claudeJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if cj.MCPServers == nil {
		return map[string]ServerConfig{}, nil
	}
	return cj.MCPServers, nil
}

// ReadDeniedServers reads the deniedMcpServers list from a settings.local.json file.
// Returns an empty slice (no error) if the file does not exist.
func ReadDeniedServers(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var sl settingsLocal
	if err := json.Unmarshal(data, &sl); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	names := make([]string, 0, len(sl.DeniedMcpServers))
	for _, e := range sl.DeniedMcpServers {
		if e.ServerName != "" {
			names = append(names, e.ServerName)
		}
	}
	return names, nil
}

// WriteDeniedServers updates the deniedMcpServers list in a settings.local.json file.
// Other keys in the file are preserved. If denied is empty, the key is removed.
// Creates the file and parent directory if they do not exist.
func WriteDeniedServers(path string, denied []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	// Read existing file into a generic map to preserve other keys.
	existing := make(map[string]json.RawMessage)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	}

	if len(denied) == 0 {
		delete(existing, "deniedMcpServers")
	} else {
		entries := make([]deniedEntry, len(denied))
		for i, name := range denied {
			entries[i] = deniedEntry{ServerName: name}
		}
		raw, err := json.Marshal(entries)
		if err != nil {
			return fmt.Errorf("marshal denied: %w", err)
		}
		existing["deniedMcpServers"] = raw
	}

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	out = append(out, '\n')

	return atomicWriteFile(path, out, 0o644)
}

// MergeServers combines user-scope and project-scope server configs with a
// denied list into a sorted slice of MCPServer.
func MergeServers(user, project map[string]ServerConfig, denied []string) []MCPServer {
	deniedSet := make(map[string]struct{}, len(denied))
	for _, name := range denied {
		deniedSet[name] = struct{}{}
	}

	servers := make([]MCPServer, 0, len(user)+len(project))

	for name, cfg := range user {
		_, isDenied := deniedSet[name]
		servers = append(servers, MCPServer{
			Name:   name,
			Config: cfg,
			Scope:  "user",
			Denied: isDenied,
		})
	}

	for name, cfg := range project {
		_, isDenied := deniedSet[name]
		servers = append(servers, MCPServer{
			Name:   name,
			Config: cfg,
			Scope:  "project",
			Denied: isDenied,
		})
	}

	sort.Slice(servers, func(i, j int) bool {
		return servers[i].Name < servers[j].Name
	})

	return servers
}

// atomicWriteFile writes data to a file atomically by writing to a temp file
// first and then renaming it to the target path.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename temp to %s: %w", path, err)
	}
	return nil
}
