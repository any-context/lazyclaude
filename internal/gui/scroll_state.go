package gui

// ScrollState manages scrollback browsing and text selection in fullscreen mode.
// No gocui dependency -- independently testable.
type ScrollState struct {
	active       bool
	scrollOffset int // 0 = live (bottom), positive = lines scrolled up
	maxOffset    int // upper bound for scrollOffset (0 = unlimited)
	viewHeight   int
	cursorY      int // cursor line within viewport
	selecting    bool
	selAnchor    int
	lines        []string // captured scrollback lines
	generation   int      // incremented on scroll; used to discard stale async results
}

// NewScrollState creates a ScrollState with default values.
func NewScrollState() *ScrollState {
	return &ScrollState{}
}

// IsActive returns true if scroll mode is active.
func (s *ScrollState) IsActive() bool { return s.active }

// ScrollOffset returns how many lines are scrolled up from the bottom.
func (s *ScrollState) ScrollOffset() int { return s.scrollOffset }

// ViewHeight returns the viewport height set on Enter.
func (s *ScrollState) ViewHeight() int { return s.viewHeight }

// CursorY returns the cursor line index within the viewport.
func (s *ScrollState) CursorY() int { return s.cursorY }

// Lines returns the captured scrollback lines.
func (s *ScrollState) Lines() []string { return s.lines }

// Generation returns the current generation counter.
func (s *ScrollState) Generation() int { return s.generation }

// Enter activates scroll mode. Initial offset is one screen up.
// Enter activates scroll mode. Starts at the bottom (most recent output)
// with the cursor on the last line.
func (s *ScrollState) Enter(viewHeight int) {
	s.active = true
	s.viewHeight = viewHeight
	s.scrollOffset = 0
	s.cursorY = viewHeight - 1 // bottom of viewport
	s.selecting = false
	s.selAnchor = 0
	s.lines = nil
	s.maxOffset = 0
}

// Exit deactivates scroll mode and resets all state.
func (s *ScrollState) Exit() {
	s.active = false
	s.scrollOffset = 0
	s.maxOffset = 0
	s.viewHeight = 0
	s.cursorY = 0
	s.selecting = false
	s.selAnchor = 0
	s.lines = nil
}

// SetMaxOffset sets the upper bound for scroll offset.
func (s *ScrollState) SetMaxOffset(max int) { s.maxOffset = max }

// ScrollUp increases the scroll offset by n lines.
func (s *ScrollState) ScrollUp(n int) {
	s.scrollOffset += n
	if s.maxOffset > 0 && s.scrollOffset > s.maxOffset {
		s.scrollOffset = s.maxOffset
	}
}

// ScrollDown decreases the scroll offset by n lines, clamped to 0.
func (s *ScrollState) ScrollDown(n int) {
	s.scrollOffset -= n
	if s.scrollOffset < 0 {
		s.scrollOffset = 0
	}
}

// ToTop scrolls to the maximum offset (top of scrollback) and moves cursor to first line.
func (s *ScrollState) ToTop() {
	if s.maxOffset > 0 {
		s.scrollOffset = s.maxOffset
	}
	s.cursorY = 0
}

// ToBottom scrolls to offset 0 (live position) and moves cursor to last line.
func (s *ScrollState) ToBottom() {
	s.scrollOffset = 0
	s.cursorY = s.viewHeight - 1
}

// CursorDown moves the cursor down one line within the viewport.
func (s *ScrollState) CursorDown() {
	max := len(s.lines) - 1
	if max < 0 {
		max = s.viewHeight - 1
	}
	if s.cursorY < max {
		s.cursorY++
	}
}

// CursorUp moves the cursor up one line within the viewport.
func (s *ScrollState) CursorUp() {
	if s.cursorY > 0 {
		s.cursorY--
	}
}

// SetLines stores the captured scrollback lines and adjusts state.
// Auto-detects maxOffset when fewer lines than viewHeight are returned
// (top of scrollback reached).
func (s *ScrollState) SetLines(lines []string) {
	s.lines = lines
	// If capture returned fewer lines than viewport, we've hit the top
	if len(lines) > 0 && len(lines) < s.viewHeight && s.maxOffset == 0 {
		s.maxOffset = s.scrollOffset
	}
	// Clamp cursor to available lines
	if len(lines) > 0 && s.cursorY >= len(lines) {
		s.cursorY = len(lines) - 1
	}
}

// IsSelecting returns true if visual selection mode is active.
func (s *ScrollState) IsSelecting() bool { return s.selecting }

// ToggleSelect toggles visual selection. When entering, anchors at current cursor.
func (s *ScrollState) ToggleSelect() {
	if s.selecting {
		s.selecting = false
	} else {
		s.selecting = true
		s.selAnchor = s.cursorY
	}
}

// SelectionRange returns the start and end line indices of the selection.
// Returns (-1, -1) if not selecting.
func (s *ScrollState) SelectionRange() (start, end int) {
	if !s.selecting {
		return -1, -1
	}
	start, end = s.selAnchor, s.cursorY
	if start > end {
		start, end = end, start
	}
	return start, end
}

// CopyText returns the text for the current selection (or current line if not selecting).
func (s *ScrollState) CopyText() string {
	if len(s.lines) == 0 {
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
	if end >= len(s.lines) {
		end = len(s.lines) - 1
	}
	var text string
	for i := start; i <= end; i++ {
		if i > start {
			text += "\n"
		}
		text += s.lines[i]
	}
	return text
}

// BumpGeneration increments the generation counter.
func (s *ScrollState) BumpGeneration() { s.generation++ }

// CaptureRange computes the -S and -E flags for capture-pane based on current scroll state.
// Returns (start, end) as negative offsets from the bottom of the scrollback.
// CaptureRange computes the -S and -E flags for capture-pane.
// tmux coordinates: 0 = top of visible area, negative = scrollback history.
//
//	offset=0 → (0, viewH-1)     visible area
//	offset=N → (-N, viewH-1-N)  N lines into scrollback
func (s *ScrollState) CaptureRange() (start, end int) {
	start = -s.scrollOffset
	end = s.viewHeight - 1 - s.scrollOffset
	return start, end
}
