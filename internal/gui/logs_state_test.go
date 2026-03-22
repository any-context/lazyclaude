package gui

import (
	"testing"
)

func TestLogsState_InitialState(t *testing.T) {
	ls := NewLogsState()
	if ls.CursorY() != 0 {
		t.Errorf("initial cursorY = %d, want 0", ls.CursorY())
	}
	if ls.IsSelecting() {
		t.Error("initial selecting = true, want false")
	}
	start, end := ls.SelectionRange()
	if start != -1 || end != -1 {
		t.Errorf("initial selection = (%d,%d), want (-1,-1)", start, end)
	}
}

func TestLogsState_CursorDown(t *testing.T) {
	ls := NewLogsState()
	ls.SetLineCount(10)
	ls.CursorDown()
	if ls.CursorY() != 1 {
		t.Errorf("after CursorDown: cursorY = %d, want 1", ls.CursorY())
	}
}

func TestLogsState_CursorDown_ClampAtEnd(t *testing.T) {
	ls := NewLogsState()
	ls.SetLineCount(3)
	ls.CursorDown()
	ls.CursorDown()
	ls.CursorDown() // should clamp
	if ls.CursorY() != 2 {
		t.Errorf("clamped cursorY = %d, want 2", ls.CursorY())
	}
}

func TestLogsState_CursorUp(t *testing.T) {
	ls := NewLogsState()
	ls.SetLineCount(10)
	ls.CursorDown()
	ls.CursorDown()
	ls.CursorUp()
	if ls.CursorY() != 1 {
		t.Errorf("after CursorUp: cursorY = %d, want 1", ls.CursorY())
	}
}

func TestLogsState_CursorUp_ClampAtZero(t *testing.T) {
	ls := NewLogsState()
	ls.SetLineCount(10)
	ls.CursorUp() // already at 0
	if ls.CursorY() != 0 {
		t.Errorf("clamped cursorY = %d, want 0", ls.CursorY())
	}
}

func TestLogsState_ToEnd(t *testing.T) {
	ls := NewLogsState()
	ls.SetLineCount(50)
	ls.ToEnd()
	if ls.CursorY() != 49 {
		t.Errorf("ToEnd: cursorY = %d, want 49", ls.CursorY())
	}
}

func TestLogsState_ToTop(t *testing.T) {
	ls := NewLogsState()
	ls.SetLineCount(50)
	ls.ToEnd()
	ls.ToTop()
	if ls.CursorY() != 0 {
		t.Errorf("ToTop: cursorY = %d, want 0", ls.CursorY())
	}
}

func TestLogsState_ToggleSelect(t *testing.T) {
	ls := NewLogsState()
	ls.SetLineCount(10)
	ls.CursorDown() // cursor at 1
	ls.CursorDown() // cursor at 2
	ls.ToggleSelect()
	if !ls.IsSelecting() {
		t.Error("after ToggleSelect: selecting = false")
	}
	// Selection anchored at cursor position (2)
	start, end := ls.SelectionRange()
	if start != 2 || end != 2 {
		t.Errorf("anchor selection = (%d,%d), want (2,2)", start, end)
	}
	// Move cursor down, selection expands
	ls.CursorDown() // cursor at 3
	start, end = ls.SelectionRange()
	if start != 2 || end != 3 {
		t.Errorf("expanded selection = (%d,%d), want (2,3)", start, end)
	}
	// Toggle off
	ls.ToggleSelect()
	if ls.IsSelecting() {
		t.Error("after second ToggleSelect: selecting = true")
	}
}

func TestLogsState_SelectionRange_Reversed(t *testing.T) {
	ls := NewLogsState()
	ls.SetLineCount(10)
	ls.CursorDown()
	ls.CursorDown()
	ls.CursorDown() // cursor at 3
	ls.ToggleSelect() // anchor at 3
	ls.CursorUp()     // cursor at 2
	start, end := ls.SelectionRange()
	if start != 2 || end != 3 {
		t.Errorf("reversed selection = (%d,%d), want (2,3)", start, end)
	}
}

func TestLogsState_CopyText(t *testing.T) {
	ls := NewLogsState()
	lines := []string{"line0", "line1", "line2", "line3"}
	ls.SetLineCount(len(lines))
	ls.CursorDown() // cursor at 1
	// No selection: copies current line
	text := ls.CopyText(lines)
	if text != "line1" {
		t.Errorf("single line copy = %q, want %q", text, "line1")
	}
	// With selection
	ls.ToggleSelect() // anchor at 1
	ls.CursorDown()   // cursor at 2
	ls.CursorDown()   // cursor at 3
	text = ls.CopyText(lines)
	if text != "line1\nline2\nline3" {
		t.Errorf("multi line copy = %q, want %q", text, "line1\nline2\nline3")
	}
}

func TestLogsState_ClearSelection(t *testing.T) {
	ls := NewLogsState()
	ls.SetLineCount(10)
	ls.ToggleSelect()
	ls.ClearSelection()
	if ls.IsSelecting() {
		t.Error("after ClearSelection: selecting = true")
	}
}

func TestLogsState_ZeroLineCount(t *testing.T) {
	ls := NewLogsState()
	ls.CursorDown() // lineCount=0, should not crash
	if ls.CursorY() != 0 {
		t.Errorf("zero lines cursorY = %d, want 0", ls.CursorY())
	}
	ls.ToEnd()
	if ls.CursorY() != 0 {
		t.Errorf("zero lines ToEnd cursorY = %d, want 0", ls.CursorY())
	}
}
