package gui

import (
	"testing"
)

func TestScrollState_InitialState(t *testing.T) {
	s := NewScrollState()
	if s.IsActive() {
		t.Error("new ScrollState should not be active")
	}
	if s.ScrollOffset() != 0 {
		t.Errorf("initial scrollOffset = %d, want 0", s.ScrollOffset())
	}
}

func TestScrollState_EnterExit(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)
	if !s.IsActive() {
		t.Error("after Enter: not active")
	}
	if s.ScrollOffset() != 0 {
		t.Errorf("after Enter: scrollOffset = %d, want 0 (start at bottom)", s.ScrollOffset())
	}
	if s.ViewHeight() != 40 {
		t.Errorf("viewHeight = %d, want 40", s.ViewHeight())
	}
	if s.CursorY() != 39 {
		t.Errorf("after Enter: cursorY = %d, want 39 (bottom of viewport)", s.CursorY())
	}

	s.Exit()
	if s.IsActive() {
		t.Error("after Exit: still active")
	}
	if s.ScrollOffset() != 0 {
		t.Errorf("after Exit: scrollOffset = %d, want 0", s.ScrollOffset())
	}
}

func TestScrollState_ScrollUpDown(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)

	// Scroll up from initial position (0)
	s.ScrollUp(10)
	if s.ScrollOffset() != 10 {
		t.Errorf("after ScrollUp(10): offset = %d, want 10", s.ScrollOffset())
	}

	// Scroll down
	s.ScrollDown(5)
	if s.ScrollOffset() != 5 {
		t.Errorf("after ScrollDown(5): offset = %d, want 5", s.ScrollOffset())
	}

	// Scroll down past 0 clamps to 0
	s.ScrollDown(100)
	if s.ScrollOffset() != 0 {
		t.Errorf("after ScrollDown(100): offset = %d, want 0 (clamped)", s.ScrollOffset())
	}
}

func TestScrollState_ScrollUpClampsToMax(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)
	s.SetMaxOffset(100)

	s.ScrollUp(200)
	if s.ScrollOffset() != 100 {
		t.Errorf("offset = %d, want 100 (clamped to max)", s.ScrollOffset())
	}
}

func TestScrollState_ToTopToBottom(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)
	s.SetMaxOffset(500)

	s.ToTop()
	if s.ScrollOffset() != 500 {
		t.Errorf("after ToTop: offset = %d, want 500", s.ScrollOffset())
	}

	s.ToBottom()
	if s.ScrollOffset() != 0 {
		t.Errorf("after ToBottom: offset = %d, want 0", s.ScrollOffset())
	}
}

func TestScrollState_ToTopWithoutMaxOffset(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)

	// Without maxOffset, ToTop is a no-op (stays at current offset)
	s.ScrollUp(5)
	s.ToTop()
	if s.ScrollOffset() != 5 {
		t.Errorf("ToTop without maxOffset should not change offset, got %d", s.ScrollOffset())
	}
}

func TestScrollState_ToTopWithMaxOffset(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)
	s.SetMaxOffset(200)

	s.ToTop()
	if s.ScrollOffset() != 200 {
		t.Errorf("ToTop with maxOffset=200: got %d", s.ScrollOffset())
	}
}

func TestScrollState_CursorUpDown(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)

	// Initial cursor at bottom (39)
	if s.CursorY() != 39 {
		t.Errorf("initial cursorY = %d, want 39", s.CursorY())
	}

	s.CursorUp()
	if s.CursorY() != 38 {
		t.Errorf("after CursorUp: cursorY = %d, want 38", s.CursorY())
	}

	s.CursorDown()
	if s.CursorY() != 39 {
		t.Errorf("after CursorDown: cursorY = %d, want 39", s.CursorY())
	}

	// Clamp at max
	s.CursorDown()
	if s.CursorY() != 39 {
		t.Errorf("after CursorDown at max: cursorY = %d, want 39", s.CursorY())
	}
}

func TestScrollState_CursorClampsToLineCount(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)
	// SetLines with 3 lines clamps cursor from 39 to 2
	s.SetLines([]string{"line0", "line1", "line2"})
	if s.CursorY() != 2 {
		t.Errorf("cursorY = %d, want 2 (clamped to line count)", s.CursorY())
	}

	// Cannot go past line count
	s.CursorDown()
	if s.CursorY() != 2 {
		t.Errorf("cursorY = %d, want 2 (clamped)", s.CursorY())
	}
}

func TestScrollState_Selection(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)
	s.SetLines([]string{"aaa", "bbb", "ccc", "ddd"})
	// After SetLines(4 items), cursorY clamped to 3

	// Not selecting initially
	if s.IsSelecting() {
		t.Error("should not be selecting initially")
	}
	start, end := s.SelectionRange()
	if start != -1 || end != -1 {
		t.Errorf("no selection: range = (%d,%d), want (-1,-1)", start, end)
	}

	// Move to line 1
	s.CursorUp()
	s.CursorUp() // now at 1
	s.ToggleSelect()
	if !s.IsSelecting() {
		t.Error("should be selecting after ToggleSelect")
	}
	start, end = s.SelectionRange()
	if start != 1 || end != 1 {
		t.Errorf("anchor selection: range = (%d,%d), want (1,1)", start, end)
	}

	// Extend selection downward
	s.CursorDown()
	s.CursorDown() // now at 3
	start, end = s.SelectionRange()
	if start != 1 || end != 3 {
		t.Errorf("extended selection: range = (%d,%d), want (1,3)", start, end)
	}

	// Toggle off
	s.ToggleSelect()
	if s.IsSelecting() {
		t.Error("should not be selecting after second toggle")
	}
}

func TestScrollState_SelectionReversed(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)
	s.SetLines([]string{"aaa", "bbb", "ccc"})
	// cursorY clamped to 2

	// Start at line 2, select, move up
	s.ToggleSelect()
	s.CursorUp() // now at 1
	start, end := s.SelectionRange()
	if start != 1 || end != 2 {
		t.Errorf("reversed selection: range = (%d,%d), want (1,2)", start, end)
	}
}

func TestScrollState_CopyText(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)
	s.SetLines([]string{"aaa", "bbb", "ccc", "ddd"})
	// cursorY clamped to 3

	// No selection: copy current line (line 3)
	got := s.CopyText()
	if got != "ddd" {
		t.Errorf("CopyText no selection = %q, want %q", got, "ddd")
	}

	// Move to line 1, start selection, extend to line 3
	s.CursorUp()
	s.CursorUp() // line 1
	s.ToggleSelect()
	s.CursorDown()
	s.CursorDown() // line 3
	got = s.CopyText()
	want := "bbb\nccc\nddd"
	if got != want {
		t.Errorf("CopyText with selection = %q, want %q", got, want)
	}
}

func TestScrollState_CopyTextEmpty(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)

	got := s.CopyText()
	if got != "" {
		t.Errorf("CopyText empty = %q, want empty", got)
	}
}

func TestScrollState_CaptureRange(t *testing.T) {
	tests := []struct {
		name         string
		scrollOffset int
		viewHeight   int
		wantStart    int
		wantEnd      int
	}{
		{
			name:         "at bottom (live)",
			scrollOffset: 0,
			viewHeight:   40,
			wantStart:    0,
			wantEnd:      39,
		},
		{
			name:         "ten lines up",
			scrollOffset: 10,
			viewHeight:   40,
			wantStart:    -10,
			wantEnd:      29,
		},
		{
			name:         "one screen up",
			scrollOffset: 40,
			viewHeight:   40,
			wantStart:    -40,
			wantEnd:      -1,
		},
		{
			name:         "two screens up",
			scrollOffset: 80,
			viewHeight:   40,
			wantStart:    -80,
			wantEnd:      -41,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScrollState()
			s.Enter(tt.viewHeight)
			// Adjust offset from the default (0)
			s.ScrollUp(tt.scrollOffset)

			start, end := s.CaptureRange()
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("CaptureRange() = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestScrollState_Generation(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)

	gen0 := s.Generation()
	s.BumpGeneration()
	gen1 := s.Generation()
	if gen1 != gen0+1 {
		t.Errorf("after BumpGeneration: gen = %d, want %d", gen1, gen0+1)
	}
}

func TestScrollState_SetLinesPreservesTrailingEmpty(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)
	s.SetLines([]string{"line0", "line1", "", "  ", ""})
	if len(s.Lines()) != 5 {
		t.Errorf("Lines() = %d, want 5 (trailing empty preserved)", len(s.Lines()))
	}
}

func TestScrollState_SetLinesAutoDetectsMaxOffset(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)
	s.ScrollUp(100)
	// Simulate capture returning only 10 lines (less than viewHeight=40)
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = "content"
	}
	s.SetLines(lines)
	// After SetLines with fewer lines, ToTop should clamp to the auto-detected maxOffset
	s.ToTop()
	if s.ScrollOffset() != 100 {
		t.Errorf("after auto-detect, ToTop scrollOffset = %d, want 100", s.ScrollOffset())
	}
}

func TestScrollState_ExitClearsSelection(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)
	s.SetLines([]string{"a", "b"})
	s.ToggleSelect()
	s.CursorDown()

	s.Exit()
	if s.IsSelecting() {
		t.Error("Exit should clear selection")
	}
	if len(s.Lines()) != 0 {
		t.Errorf("Exit should clear lines, got %d", len(s.Lines()))
	}
}

func TestScrollState_LinesVersion(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)

	v0 := s.LinesVersion()

	s.SetLines([]string{"a", "b", "c"})
	v1 := s.LinesVersion()
	if v1 != v0+1 {
		t.Errorf("after first SetLines: version = %d, want %d", v1, v0+1)
	}

	// Same content still bumps version (change detection, not equality check)
	s.SetLines([]string{"a", "b", "c"})
	v2 := s.LinesVersion()
	if v2 != v1+1 {
		t.Errorf("after second SetLines: version = %d, want %d", v2, v1+1)
	}
}

func TestScrollState_LinesVersionStartsAtZero(t *testing.T) {
	s := NewScrollState()
	if s.LinesVersion() != 0 {
		t.Errorf("initial LinesVersion = %d, want 0", s.LinesVersion())
	}
}

func TestScrollState_LinesVersionResetOnExit(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)
	s.SetLines([]string{"a", "b"})
	s.SetLines([]string{"c", "d"})
	if s.LinesVersion() != 2 {
		t.Fatalf("before Exit: LinesVersion = %d, want 2", s.LinesVersion())
	}

	s.Exit()
	if s.LinesVersion() != 0 {
		t.Errorf("after Exit: LinesVersion = %d, want 0", s.LinesVersion())
	}

	// Re-enter and SetLines should start from 0 again
	s.Enter(40)
	s.SetLines([]string{"x"})
	if s.LinesVersion() != 1 {
		t.Errorf("after re-enter + SetLines: LinesVersion = %d, want 1", s.LinesVersion())
	}
}
