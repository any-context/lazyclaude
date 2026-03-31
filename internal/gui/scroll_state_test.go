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
	if s.ScrollOffset() != 40 {
		t.Errorf("after Enter: scrollOffset = %d, want 40 (one screen up)", s.ScrollOffset())
	}
	if s.ViewHeight() != 40 {
		t.Errorf("viewHeight = %d, want 40", s.ViewHeight())
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

	// Scroll up from initial position (40)
	s.ScrollUp(10)
	if s.ScrollOffset() != 50 {
		t.Errorf("after ScrollUp(10): offset = %d, want 50", s.ScrollOffset())
	}

	// Scroll down
	s.ScrollDown(5)
	if s.ScrollOffset() != 45 {
		t.Errorf("after ScrollDown(5): offset = %d, want 45", s.ScrollOffset())
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

	// Without maxOffset, ToTop should jump to a large value
	s.ToTop()
	if s.ScrollOffset() == 40 {
		t.Error("ToTop without maxOffset should jump beyond initial offset")
	}
	if s.ScrollOffset() == 0 {
		t.Error("ToTop without maxOffset should not stay at 0")
	}
}

func TestScrollState_CursorUpDown(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)

	// Initial cursor at 0
	if s.CursorY() != 0 {
		t.Errorf("initial cursorY = %d, want 0", s.CursorY())
	}

	s.CursorDown()
	if s.CursorY() != 1 {
		t.Errorf("after CursorDown: cursorY = %d, want 1", s.CursorY())
	}

	s.CursorUp()
	if s.CursorY() != 0 {
		t.Errorf("after CursorUp: cursorY = %d, want 0", s.CursorY())
	}

	// Clamp at 0
	s.CursorUp()
	if s.CursorY() != 0 {
		t.Errorf("after CursorUp at 0: cursorY = %d, want 0", s.CursorY())
	}
}

func TestScrollState_CursorClampsToLineCount(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)
	s.SetLines([]string{"line0", "line1", "line2"})

	// Move cursor to last line
	s.CursorDown()
	s.CursorDown()
	if s.CursorY() != 2 {
		t.Errorf("cursorY = %d, want 2", s.CursorY())
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

	// Not selecting initially
	if s.IsSelecting() {
		t.Error("should not be selecting initially")
	}
	start, end := s.SelectionRange()
	if start != -1 || end != -1 {
		t.Errorf("no selection: range = (%d,%d), want (-1,-1)", start, end)
	}

	// Move to line 1, start selection
	s.CursorDown()
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
	s.CursorDown()
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

	// Start at line 2, select, move up
	s.CursorDown()
	s.CursorDown()
	s.ToggleSelect()
	s.CursorUp()
	start, end := s.SelectionRange()
	if start != 1 || end != 2 {
		t.Errorf("reversed selection: range = (%d,%d), want (1,2)", start, end)
	}
}

func TestScrollState_CopyText(t *testing.T) {
	s := NewScrollState()
	s.Enter(40)
	s.SetLines([]string{"aaa", "bbb", "ccc", "ddd"})

	// No selection: copy current line
	s.CursorDown() // line 1
	got := s.CopyText()
	if got != "bbb" {
		t.Errorf("CopyText no selection = %q, want %q", got, "bbb")
	}

	// With selection: copy range
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
			name:         "one screen up",
			scrollOffset: 40,
			viewHeight:   40,
			wantStart:    -80,
			wantEnd:      -41,
		},
		{
			name:         "ten lines up",
			scrollOffset: 10,
			viewHeight:   40,
			wantStart:    -50,
			wantEnd:      -11,
		},
		{
			name:         "at bottom (live)",
			scrollOffset: 0,
			viewHeight:   40,
			wantStart:    -40,
			wantEnd:      -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScrollState()
			s.Enter(tt.viewHeight)
			// Adjust offset from the default (viewHeight)
			if tt.scrollOffset > tt.viewHeight {
				s.ScrollUp(tt.scrollOffset - tt.viewHeight)
			} else {
				s.ScrollDown(tt.viewHeight - tt.scrollOffset)
			}

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
