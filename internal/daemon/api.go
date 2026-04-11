// Package daemon defines the API contract for the lazyclaude daemon.
// Types and interfaces only — implementation is in subsequent phases.
package daemon

import (
	"time"

	"github.com/any-context/lazyclaude/internal/core/model"
)

// APIVersion is the current daemon API version. Checked at connection time
// via /health. A mismatch indicates the remote binary needs updating.
//
// Version history:
//   - 1: initial daemon API (session CRUD, worktree, messaging, SSE)
//   - 2: adds POST /session/{id}/scrollback and GET /session/{id}/history-size
//        so that remote fullscreen copy mode can read the remote tmux server's
//        scrollback directly (the local mirror window's tmux buffer does not
//        contain the remote tmux's historical scrollback).
const APIVersion = 2

// --- Session CRUD ---

// SessionCreateRequest creates a new Claude Code session.
// SessionType determines the flavor (plain, worktree, PM, worker).
type SessionCreateRequest struct {
	Path        string `json:"path"`
	SessionType string `json:"session_type"` // "plain", "worktree", "pm", "worker"
	Name        string `json:"name,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
	ProjectRoot string `json:"project_root,omitempty"`
}

// SessionCreateResponse is returned after a session is created.
type SessionCreateResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path,omitempty"`
	TmuxWindow string `json:"tmux_window"`
	Role       string `json:"role,omitempty"`
}

// SessionDeleteRequest deletes a session by ID.
type SessionDeleteRequest struct {
	ID string `json:"id"`
}

// SessionRenameRequest renames a session.
type SessionRenameRequest struct {
	ID      string `json:"id"`
	NewName string `json:"new_name"`
}

// SessionInfo is the API representation of a session.
// Compatible with gui.SessionItem.
type SessionInfo struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Path       string              `json:"path"`
	Host       string              `json:"host"`
	Status     string              `json:"status"`
	Flags      []string            `json:"flags,omitempty"`
	TmuxWindow string              `json:"tmux_window"`
	Activity   model.ActivityState `json:"activity"`
	ToolName   string              `json:"tool_name,omitempty"`
	Role       string              `json:"role,omitempty"`
}

// SessionListResponse returns all sessions.
type SessionListResponse struct {
	Sessions []SessionInfo `json:"sessions"`
}

// --- Preview / Scrollback ---

// PreviewRequest captures the visible pane content.
type PreviewRequest struct {
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// PreviewResponse holds captured pane content and cursor position.
// Compatible with gui.PreviewResult.
type PreviewResponse struct {
	Content string `json:"content"`
	CursorX int    `json:"cursor_x"`
	CursorY int    `json:"cursor_y"`
}

// ScrollbackRequest captures a range of scrollback lines.
type ScrollbackRequest struct {
	ID        string `json:"id"`
	Width     int    `json:"width"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

// ScrollbackResponse holds captured scrollback content.
type ScrollbackResponse struct {
	Content string `json:"content"`
	CursorX int    `json:"cursor_x"`
	CursorY int    `json:"cursor_y"`
}

// HistorySizeResponse returns the total scrollback line count.
type HistorySizeResponse struct {
	Lines int `json:"lines"`
}

// --- Input ---

// SendKeysRequest sends raw keys to a session's tmux pane.
type SendKeysRequest struct {
	ID   string `json:"id"`
	Keys string `json:"keys"`
}

// SendChoiceRequest sends a permission choice (accept/allow/reject/cancel).
type SendChoiceRequest struct {
	ID     string `json:"id"`
	Window string `json:"window"`
	Choice int    `json:"choice"` // maps to choice.Choice values
}

// --- Attach ---

// AttachResponse provides the tmux target for interactive attach.
type AttachResponse struct {
	TmuxTarget string `json:"tmux_target"` // e.g. "lazyclaude:lc-abcd1234"
}

// --- Worktree ---

// WorktreeCreateRequest creates a new git worktree and session.
type WorktreeCreateRequest struct {
	Name        string `json:"name"`
	Prompt      string `json:"prompt,omitempty"`
	ProjectRoot string `json:"project_root"`
}

// WorktreeCreateResponse is returned after a worktree is created.
type WorktreeCreateResponse struct {
	SessionID  string `json:"session_id"`
	Path       string `json:"path"`
	Branch     string `json:"branch"`
	TmuxWindow string `json:"tmux_window"`
	Role       string `json:"role,omitempty"`
}

// WorktreeResumeRequest resumes an existing worktree session.
type WorktreeResumeRequest struct {
	WorktreePath string `json:"worktree_path"`
	Prompt       string `json:"prompt,omitempty"`
	ProjectRoot  string `json:"project_root"`
}

// WorktreeResumeResponse is returned after a worktree session is resumed.
type WorktreeResumeResponse struct {
	SessionID  string `json:"session_id"`
	Name       string `json:"name"`
	Path       string `json:"path,omitempty"`
	TmuxWindow string `json:"tmux_window"`
	Role       string `json:"role,omitempty"`
}

// WorktreeInfo describes an existing worktree.
// Compatible with gui.WorktreeInfo.
type WorktreeInfo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

// WorktreeListRequest requests worktrees for a project.
type WorktreeListRequest struct {
	ProjectRoot string `json:"project_root"`
}

// WorktreeListResponse returns all worktrees for a project.
type WorktreeListResponse struct {
	Worktrees []WorktreeInfo `json:"worktrees"`
}

// --- Messaging ---

// MsgSendRequest sends a message to an existing session.
type MsgSendRequest struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Type    string `json:"type"` // e.g. "review_request", "review_response"
	Body    string `json:"body"`
}

// MsgSendResponse is returned after a message is sent.
type MsgSendResponse struct {
	Delivered bool   `json:"delivered"`
	Error     string `json:"error,omitempty"`
}

// MsgCreateRequest creates a new worker session and sends an initial message.
type MsgCreateRequest struct {
	From   string `json:"from"`
	Name   string `json:"name"`
	Type   string `json:"type"` // "worker", "pm"
	Prompt string `json:"prompt"`
}

// MsgCreateResponse is returned after a new session is created via messaging.
type MsgCreateResponse struct {
	SessionID string `json:"session_id"`
}

// MsgSessionsResponse lists sessions relevant for messaging.
type MsgSessionsResponse struct {
	Sessions []MsgSessionInfo `json:"sessions"`
}

// MsgSessionInfo is a session summary for messaging context.
type MsgSessionInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
	Host string `json:"host,omitempty"`
}

// --- Health ---

// HealthResponse is returned by the /health endpoint.
type HealthResponse struct {
	APIVersion    int    `json:"api_version"`
	BinaryVersion string `json:"binary_version"`
	UptimeSeconds int64  `json:"uptime_s"`
	SessionCount  int    `json:"sessions"`
}

// --- Shutdown ---

// ShutdownRequest requests a graceful daemon shutdown.
type ShutdownRequest struct {
	Force bool `json:"force,omitempty"`
}

// --- Notifications (SSE) ---

// NotificationEventType identifies the kind of SSE event.
type NotificationEventType string

const (
	// EventActivity is emitted when a session's activity state changes.
	EventActivity NotificationEventType = "activity"
	// EventToolInfo is emitted on PreToolUse for tool notification display.
	EventToolInfo NotificationEventType = "tool_info"
	// EventFullSync is emitted on reconnect to restore all session state.
	EventFullSync NotificationEventType = "full_sync"
)

// NotificationEvent is a single SSE event from the daemon.
type NotificationEvent struct {
	ID   string                `json:"id"`
	Type NotificationEventType `json:"type"`
	Time time.Time             `json:"time"`

	// Activity fields (type == "activity")
	SessionID string              `json:"session_id,omitempty"`
	Activity  model.ActivityState `json:"activity,omitempty"`
	ToolName  string              `json:"tool_name,omitempty"`

	// ToolInfo fields (type == "tool_info")
	ToolNotification *model.ToolNotification `json:"tool_notification,omitempty"`

	// FullSync fields (type == "full_sync")
	Sessions []SessionInfo `json:"sessions,omitempty"`
}

// --- Auth ---

// AuthHeader is the HTTP header name for daemon authorization.
const AuthHeader = "X-Daemon-Authorization"
