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
//
// SetRemote atomically installs the (host, projectDir) target. Using
// a single combined setter eliminates the mixed-pair window that
// separate SetHost + SetProjectDir calls would expose to a racing
// async Refresh / ToggleDenied: an in-flight goroutine spawned from a
// previous selection could otherwise observe (host=new, projectDir=old)
// and mutate the wrong remote file.
type MCPProvider interface {
	SetRemote(host, projectDir string)
	Refresh(ctx context.Context) error
	Servers() []MCPItem
	ToggleDenied(ctx context.Context, name string) error
}

// MCPState holds the UI state for the MCP tab.
type MCPState struct {
	cursor         int
	loading        bool
	remoteDisabled bool   // true when MCP editing is not supported for the selection
	remoteKey      string // cached "host|projectDir" for the last remote sync (dedupe)
}

// NewMCPState creates a new MCPState.
func NewMCPState() *MCPState {
	return &MCPState{}
}
