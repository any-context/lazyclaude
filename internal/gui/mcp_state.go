package gui

import "context"

// MCPItem is a read-only view of an MCP server for display.
type MCPItem struct {
	Name    string
	Type    string // "stdio", "http", "sse"
	Scope   string // "user", "project"
	Denied  bool   // true = off for this project
	Command string
	Args    []string
	URL     string
}

// MCPProvider abstracts MCP operations for the GUI layer.
type MCPProvider interface {
	SetProjectDir(dir string)
	Refresh(ctx context.Context) error
	Servers() []MCPItem
	ToggleDenied(ctx context.Context, name string) error
}

// MCPState holds the UI state for the MCP tab.
type MCPState struct {
	cursor  int
	loading bool
	errMsg  string
}

// NewMCPState creates a new MCPState.
func NewMCPState() *MCPState {
	return &MCPState{}
}
