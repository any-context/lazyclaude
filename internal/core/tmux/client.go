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

	// CapturePaneANSIRange captures a range of pane content with ANSI escape codes.
	// start and end are line offsets passed as -S and -E flags to capture-pane.
	// Negative values count from the end of the scrollback buffer.
	CapturePaneANSIRange(ctx context.Context, target string, start, end int) (string, error)

	// SendKeys sends key sequences to a tmux target.
	// Keys are interpreted as tmux key names (e.g., "Enter", "Space").
	SendKeys(ctx context.Context, target string, keys ...string) error

	// SendKeysLiteral sends text literally to a tmux target (send-keys -l).
	// The text is NOT interpreted as key names — useful for rune characters.
	SendKeysLiteral(ctx context.Context, target string, text string) error

	// PasteToPane loads text into a tmux buffer and pastes it to the target.
	// Uses paste-buffer -p to send bracketed paste sequences.
	PasteToPane(ctx context.Context, target string, text string) error

	// ShowMessage executes display-message with a format string and returns the result.
	ShowMessage(ctx context.Context, target, format string) (string, error)

	// GetOption returns the value of a tmux option.
	GetOption(ctx context.Context, target, option string) (string, error)

	// ResizeWindow resizes a tmux window.
	ResizeWindow(ctx context.Context, target string, width, height int) error
}