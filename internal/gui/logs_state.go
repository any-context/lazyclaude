package gui

// LogsState manages cursor, selection, and line count for the logs panel.
// No gocui dependency — independently testable.
type LogsState struct {
	cursorY   int
	selecting bool
	selAnchor int
	lineCount int
}

// NewLogsState creates a LogsState with default values.
func NewLogsState() *LogsState {
	return &LogsState{}
}

// CursorY returns the current cursor line index.
func (s *LogsState) CursorY() int { return s.cursorY }

// SetLineCount updates the total number of log lines.
func (s *LogsState) SetLineCount(n int) { s.lineCount = n }

// LineCount returns the total number of log lines.
func (s *LogsState) LineCount() int { return s.lineCount }

// CursorDown moves the cursor down by one line.
func (s *LogsState) CursorDown() {
	if s.lineCount > 0 && s.cursorY < s.lineCount-1 {
		s.cursorY++
	}
}

// CursorUp moves the cursor up by one line.
func (s *LogsState) CursorUp() {
	if s.cursorY > 0 {
		s.cursorY--
	}
}

// ToEnd moves the cursor to the last line.
func (s *LogsState) ToEnd() {
	if s.lineCount > 0 {
		s.cursorY = s.lineCount - 1
	}
}

// ToTop moves the cursor to the first line.
func (s *LogsState) ToTop() {
	s.cursorY = 0
}

// ToggleSelect toggles selection mode. When entering, anchors at current cursor.
func (s *LogsState) ToggleSelect() {
	if s.selecting {
		s.selecting = false
	} else {
		s.selecting = true
		s.selAnchor = s.cursorY
	}
}

// IsSelecting returns true if selection mode is active.
func (s *LogsState) IsSelecting() bool { return s.selecting }

// SelectionRange returns the start and end line indices of the selection.
// Returns (-1, -1) if not selecting. Start <= End.
func (s *LogsState) SelectionRange() (start, end int) {
	if !s.selecting {
		return -1, -1
	}
	start, end = s.selAnchor, s.cursorY
	if start > end {
		start, end = end, start
	}
	return start, end
}

// ClearSelection exits selection mode.
func (s *LogsState) ClearSelection() {
	s.selecting = false
}

// CopyText returns the text for the current selection (or current line if not selecting).
func (s *LogsState) CopyText(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var start, end int
	if s.selecting {
		start, end = s.SelectionRange()
	} else {
		start = s.cursorY
		end = s.cursorY
	}
	if start < 0 {
		start = 0
	}
	if end >= len(lines) {
		end = len(lines) - 1
	}
	var text string
	for i := start; i <= end; i++ {
		if i > start {
			text += "\n"
		}
		text += lines[i]
	}
	return text
}

// ClampCursor ensures the cursor is within valid bounds.
func (s *LogsState) ClampCursor() {
	if s.cursorY >= s.lineCount {
		s.cursorY = s.lineCount - 1
	}
	if s.cursorY < 0 {
		s.cursorY = 0
	}
}
