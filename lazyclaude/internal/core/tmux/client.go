package tmux

import "context"

// Client abstracts tmux operations for testability.
type Client interface {
	// ListClients returns all attached tmux clients.
	ListClients(ctx context.Context) ([]ClientInfo, error)

	// FindActiveClient returns the most recently active client.
	FindActiveClient(ctx context.Context) (*ClientInfo, error)

	// HasSession checks if a tmux session exists.
	HasSession(ctx context.Context, name string) (bool, error)

	// NewSession creates a new tmux session.
	NewSession(ctx context.Context, opts NewSessionOpts) error

	// ListWindows returns all windows in a session.
	ListWindows(ctx context.Context, session string) ([]WindowInfo, error)

	// NewWindow creates a new window in a session.
	NewWindow(ctx context.Context, opts NewWindowOpts) error

	// RespawnPane respawns a dead pane with a new command.
	RespawnPane(ctx context.Context, target, cmd string) error

	// KillWindow destroys a tmux window.
	KillWindow(ctx context.Context, target string) error

	// ListPanes returns all panes (optionally filtered by session).
	ListPanes(ctx context.Context, session string) ([]PaneInfo, error)

	// CapturePaneContent captures the visible content of a pane (plain text).
	CapturePaneContent(ctx context.Context, target string) (string, error)

	// CapturePaneANSI captures pane content with ANSI escape codes.
	CapturePaneANSI(ctx context.Context, target string) (string, error)

	// SendKeys sends key sequences to a tmux target.
	SendKeys(ctx context.Context, target string, keys ...string) error

	// DisplayPopup opens a popup overlay on a client.
	DisplayPopup(ctx context.Context, opts PopupOpts) error

	// ShowMessage executes display-message with a format string and returns the result.
	ShowMessage(ctx context.Context, target, format string) (string, error)

	// GetOption returns the value of a tmux option.
	GetOption(ctx context.Context, target, option string) (string, error)

	// ResizeWindow resizes a tmux window.
	ResizeWindow(ctx context.Context, target string, width, height int) error
}