package tmux

// ClientInfo represents a tmux client (attached terminal).
type ClientInfo struct {
	Name      string // client tty path (e.g., /dev/ttys001)
	Session   string // session name
	Width     int    // terminal width
	Height    int    // terminal height
	Activity  int64  // last activity timestamp
	LastPaneW string // window containing the last active pane
}

// WindowInfo represents a tmux window.
type WindowInfo struct {
	ID      string // window unique ID (e.g., @1)
	Index   int    // window index
	Name    string // window name
	Session string // session name
	Active  bool   // is the active window
}

// PaneInfo represents a tmux pane.
type PaneInfo struct {
	ID     string // pane unique ID (e.g., %1)
	Window string // window ID
	PID    int    // pane process PID
	Dead   bool   // is the pane dead
}

// NewSessionOpts configures a new tmux session.
type NewSessionOpts struct {
	Name         string
	WindowName   string
	Command      string
	Env          map[string]string
	Detached     bool
	Width        int        // -x flag (0 = tmux default)
	Height       int        // -y flag (0 = tmux default)
	PostCommands [][]string // tmux commands to chain after new-session (e.g. ["set-option", "status", "off"])
}

// NewWindowOpts configures a new tmux window.
type NewWindowOpts struct {
	Session string
	Name    string
	Command string
	Env     map[string]string
}

// PopupOpts configures a tmux display-popup.
type PopupOpts struct {
	Client string
	Width  int // percentage
	Height int // percentage
	Cmd    string
}