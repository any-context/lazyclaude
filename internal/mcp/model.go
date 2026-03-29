package mcp

// ServerConfig represents the JSON configuration of an MCP server
// as found in ~/.claude.json or .mcp.json.
type ServerConfig struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// MCPServer is a fully resolved MCP server entry with scope and deny state.
type MCPServer struct {
	Name   string
	Config ServerConfig
	Scope  string // "user", "project"
	Denied bool   // true if in deniedMcpServers for current project
}

// EffectiveType returns the transport type, defaulting to "stdio" when unset.
func (c ServerConfig) EffectiveType() string {
	if c.Type != "" {
		return c.Type
	}
	return "stdio"
}
