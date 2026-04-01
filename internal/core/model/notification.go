package model

import "time"

// ActivityState represents the Claude session activity state.
// Currently 4 states are actively set via hooks; Dead will be added
// when tmux-sync-based process detection is wired (future PR).
type ActivityState int

const (
	// ActivityUnknown is the zero value — no activity information available.
	ActivityUnknown ActivityState = iota
	// ActivityRunning means Claude is actively working (SessionStart, UserPromptSubmit, PreToolUse).
	ActivityRunning
	// ActivityNeedsInput means Claude is waiting for user approval (permission_prompt).
	ActivityNeedsInput
	// ActivityIdle means the turn completed normally (Stop with end_turn).
	ActivityIdle
	// ActivityError means the turn stopped with an error or interrupt.
	ActivityError
)

// String returns a human-readable label for the activity state.
func (s ActivityState) String() string {
	switch s {
	case ActivityRunning:
		return "running"
	case ActivityNeedsInput:
		return "needs_input"
	case ActivityIdle:
		return "idle"
	case ActivityError:
		return "error"
	default:
		return "unknown"
	}
}

// Event is published on the event broker when a tool permission request arrives,
// or when a session lifecycle event (stop/start/prompt-submit) occurs.
type Event struct {
	Notification             *ToolNotification
	StopNotification         *StopNotification
	SessionStartNotification *SessionStartNotification
	PromptSubmitNotification *PromptSubmitNotification
	ActivityNotification     *ActivityNotification
}

// ToolNotification represents a pending tool permission request from Claude Code.
type ToolNotification struct {
	ToolName    string    `json:"tool_name"`
	Input       string    `json:"input"`
	CWD         string    `json:"cwd,omitempty"`
	Window      string    `json:"window"`
	Timestamp   time.Time `json:"timestamp"`
	OldFilePath string    `json:"old_file_path,omitempty"` // set for Edit/Write diff
	NewContents string    `json:"new_contents,omitempty"`  // set for Edit/Write diff
	MaxOption   int       `json:"max_option,omitempty"`    // 2 or 3 (0 = unset, treat as 3)
}

// IsDiff returns true if this notification contains diff information.
func (n *ToolNotification) IsDiff() bool {
	return n.OldFilePath != ""
}

// StopNotification represents a Claude Code turn completion event.
type StopNotification struct {
	Window     string    `json:"window"`
	StopReason string    `json:"stop_reason"`
	SessionID  string    `json:"session_id"`
	Timestamp  time.Time `json:"timestamp"`
}

// SessionStartNotification represents a Claude Code session start event.
type SessionStartNotification struct {
	Window    string    `json:"window"`
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
}

// PromptSubmitNotification represents a user prompt submission event.
// Triggers idle/finished -> running transition.
type PromptSubmitNotification struct {
	Window    string    `json:"window"`
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
}

// ActivityNotification carries a computed activity state change for the GUI.
// Published via the event broker to update sidebar rendering.
type ActivityNotification struct {
	Window    string        `json:"window"`
	State     ActivityState `json:"state"`
	ToolName  string        `json:"tool_name,omitempty"` // last tool name (for context info)
	Timestamp time.Time     `json:"timestamp"`
}
