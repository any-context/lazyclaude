package daemon

import "context"

// clientAPI compile-time check is deferred to Phase 1 where the concrete
// implementation is defined.

// CWDQuerier is implemented by providers that can report their daemon's
// working directory. Used by the composite adapter to resolve remote paths
// without coupling to concrete provider types.
type CWDQuerier interface {
	QueryCWD(ctx context.Context) (string, error)
}

// ClientAPI defines all operations available on a daemon.
// Implementations wrap HTTP calls to the daemon REST API.
type ClientAPI interface {
	// --- Session CRUD ---

	// CreateSession creates a new Claude Code session.
	CreateSession(ctx context.Context, req SessionCreateRequest) (*SessionCreateResponse, error)

	// DeleteSession deletes a session by ID.
	DeleteSession(ctx context.Context, id string) error

	// RenameSession renames a session.
	RenameSession(ctx context.Context, id, newName string) error

	// Sessions returns all sessions managed by the daemon.
	Sessions(ctx context.Context) ([]SessionInfo, error)

	// PurgeOrphans removes sessions whose tmux windows no longer exist.
	PurgeOrphans(ctx context.Context) (int, error)

	// --- Preview / Scrollback ---

	// CapturePreview captures the visible pane content for a session.
	CapturePreview(ctx context.Context, id string, width, height int) (*PreviewResponse, error)

	// CaptureScrollback captures a range of scrollback lines.
	CaptureScrollback(ctx context.Context, id string, width, startLine, endLine int) (*ScrollbackResponse, error)

	// HistorySize returns the total scrollback line count for a session.
	HistorySize(ctx context.Context, id string) (*HistorySizeResponse, error)

	// --- Input ---

	// SendKeys sends raw keys to a session's tmux pane.
	SendKeys(ctx context.Context, id, keys string) error

	// SendChoice sends a permission choice to a session.
	SendChoice(ctx context.Context, id, window string, choice int) error

	// --- Attach ---

	// AttachSession returns the tmux target string for interactive attach.
	AttachSession(ctx context.Context, id string) (*AttachResponse, error)

	// --- Worktree ---

	// CreateWorktree creates a new git worktree and associated session.
	CreateWorktree(ctx context.Context, req WorktreeCreateRequest) (*WorktreeCreateResponse, error)

	// ResumeWorktree resumes an existing worktree session.
	ResumeWorktree(ctx context.Context, req WorktreeResumeRequest) (*WorktreeResumeResponse, error)

	// ListWorktrees lists all worktrees for a project root.
	ListWorktrees(ctx context.Context, projectRoot string) ([]WorktreeInfo, error)

	// --- Messaging ---

	// MsgSend sends a message to an existing session.
	MsgSend(ctx context.Context, req MsgSendRequest) (*MsgSendResponse, error)

	// MsgCreate creates a new session and sends an initial message.
	MsgCreate(ctx context.Context, req MsgCreateRequest) (*MsgCreateResponse, error)

	// MsgSessions lists sessions available for messaging.
	MsgSessions(ctx context.Context) (*MsgSessionsResponse, error)

	// --- System Info ---

	// CWD returns the daemon process's working directory.
	CWD(ctx context.Context) (string, error)

	// --- Health / Lifecycle ---

	// Health returns the daemon's health status.
	Health(ctx context.Context) (*HealthResponse, error)

	// Shutdown requests a graceful daemon shutdown.
	Shutdown(ctx context.Context, req ShutdownRequest) error

	// --- Notifications ---

	// SubscribeNotifications opens an SSE stream for real-time events.
	// The returned channel emits events until the context is canceled or
	// the connection drops. The caller must drain the channel.
	SubscribeNotifications(ctx context.Context) (<-chan NotificationEvent, error)

	// PendingNotifications returns buffered tool notifications since the
	// last call. Maps to the existing SessionProvider.PendingNotifications
	// contract for compatibility with the notification badge system.
	PendingNotifications(ctx context.Context) ([]*ToolNotificationInfo, error)
}

// ToolNotificationInfo is the API representation of a tool notification.
// Compatible with model.ToolNotification but includes the session ID.
type ToolNotificationInfo struct {
	SessionID   string `json:"session_id"`
	ToolName    string `json:"tool_name"`
	Input       string `json:"input"`
	CWD         string `json:"cwd,omitempty"`
	Window      string `json:"window"`
	OldFilePath string `json:"old_file_path,omitempty"`
	NewContents string `json:"new_contents,omitempty"`
	MaxOption   int    `json:"max_option,omitempty"`
}
