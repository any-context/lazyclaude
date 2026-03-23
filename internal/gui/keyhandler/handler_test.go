package keyhandler_test

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/gui/keyhandler"
	"github.com/jesseduffield/gocui"
)

func TestSessionsPanel_Keys(t *testing.T) {
	p := &keyhandler.SessionsPanel{}
	tests := []struct {
		ev   keyhandler.KeyEvent
		want string
	}{
		{keyhandler.KeyEvent{Rune: 'j'}, "MoveCursorDown"},
		{keyhandler.KeyEvent{Rune: 'k'}, "MoveCursorUp"},
		{keyhandler.KeyEvent{Key: gocui.KeyArrowDown}, "MoveCursorDown"},
		{keyhandler.KeyEvent{Rune: 'n'}, "CreateSession"},
		{keyhandler.KeyEvent{Rune: 'd'}, "DeleteSession"},
		{keyhandler.KeyEvent{Key: gocui.KeyEnter}, "EnterFullScreen"},
		{keyhandler.KeyEvent{Rune: 'a'}, "AttachSession"},
		{keyhandler.KeyEvent{Rune: 'r'}, "EnterFullScreen"},
		{keyhandler.KeyEvent{Rune: 'R'}, "StartRename"},
		{keyhandler.KeyEvent{Rune: 'w'}, "StartWorktreeInput"},
		{keyhandler.KeyEvent{Rune: 'g'}, "LaunchLazygit"},
		{keyhandler.KeyEvent{Rune: 'W'}, "SelectWorktree"},
		{keyhandler.KeyEvent{Rune: 'D'}, "PurgeOrphans"},
	}
	for _, tt := range tests {
		a := &mockActions{}
		r := p.HandleKey(tt.ev, a)
		if r != keyhandler.Handled {
			t.Errorf("key %v: want Handled", tt.ev)
		}
		if a.lastCall() != tt.want {
			t.Errorf("key %v: got %q, want %q", tt.ev, a.lastCall(), tt.want)
		}
	}
}

func TestSessionsPanel_UnknownKey(t *testing.T) {
	p := &keyhandler.SessionsPanel{}
	a := &mockActions{}
	r := p.HandleKey(keyhandler.KeyEvent{Rune: 'z'}, a)
	if r != keyhandler.Unhandled {
		t.Error("unknown key should be Unhandled")
	}
}

func TestLogsPanel_Keys(t *testing.T) {
	p := &keyhandler.LogsPanel{}
	tests := []struct {
		ev   keyhandler.KeyEvent
		want string
	}{
		{keyhandler.KeyEvent{Rune: 'j'}, "LogsCursorDown"},
		{keyhandler.KeyEvent{Rune: 'k'}, "LogsCursorUp"},
		{keyhandler.KeyEvent{Rune: 'G'}, "LogsCursorToEnd"},
		{keyhandler.KeyEvent{Rune: 'g'}, "LogsCursorToTop"},
		{keyhandler.KeyEvent{Rune: 'v'}, "LogsToggleSelect"},
		{keyhandler.KeyEvent{Rune: 'y'}, "LogsCopySelection"},
	}
	for _, tt := range tests {
		a := &mockActions{}
		r := p.HandleKey(tt.ev, a)
		if r != keyhandler.Handled {
			t.Errorf("key %v: want Handled", tt.ev)
		}
		if a.lastCall() != tt.want {
			t.Errorf("key %v: got %q, want %q", tt.ev, a.lastCall(), tt.want)
		}
	}
}

func TestPopupHandler_ConsumesAllKeys(t *testing.T) {
	h := &keyhandler.PopupHandler{}
	a := &mockActions{hasPopup: true}

	r := h.HandleKey(keyhandler.KeyEvent{Key: gocui.KeyCtrlY}, a)
	if r != keyhandler.Handled || a.lastCall() != "DismissPopup" {
		t.Errorf("popup Ctrl+Y: result=%v, call=%q", r, a.lastCall())
	}

	// Unknown key should still be Handled (consumed)
	a2 := &mockActions{hasPopup: true}
	r = h.HandleKey(keyhandler.KeyEvent{Rune: 'z'}, a2)
	if r != keyhandler.Handled {
		t.Error("popup unknown key should be Handled (consumed)")
	}

	// No popup -> Unhandled
	a3 := &mockActions{}
	r = h.HandleKey(keyhandler.KeyEvent{Rune: 'y'}, a3)
	if r != keyhandler.Unhandled {
		t.Error("no popup should be Unhandled")
	}
}

func TestFullScreenHandler_ExitKeys(t *testing.T) {
	h := &keyhandler.FullScreenHandler{}
	a := &mockActions{fullScreen: true}

	r := h.HandleKey(keyhandler.KeyEvent{Key: gocui.KeyCtrlD}, a)
	if r != keyhandler.Handled || a.lastCall() != "ExitFullScreen" {
		t.Errorf("Ctrl+D: result=%v, call=%q", r, a.lastCall())
	}

	// Not fullscreen -> Unhandled
	a2 := &mockActions{}
	r = h.HandleKey(keyhandler.KeyEvent{Key: gocui.KeyCtrlD}, a2)
	if r != keyhandler.Unhandled {
		t.Error("not fullscreen should be Unhandled")
	}
}

func TestGlobalHandler_Quit(t *testing.T) {
	pm := keyhandler.NewPanelManager(&keyhandler.SessionsPanel{})
	h := keyhandler.NewGlobalHandler(pm)
	a := &mockActions{}

	r := h.HandleKey(keyhandler.KeyEvent{Rune: 'q'}, a)
	if r != keyhandler.Handled || a.lastCall() != "Quit" {
		t.Errorf("q: result=%v, call=%q", r, a.lastCall())
	}
}

func TestGlobalHandler_Tab(t *testing.T) {
	pm := keyhandler.NewPanelManager(&keyhandler.SessionsPanel{}, &keyhandler.LogsPanel{})
	h := keyhandler.NewGlobalHandler(pm)
	a := &mockActions{}

	if pm.FocusIdx() != 0 {
		t.Fatal("initial focus should be 0")
	}
	r := h.HandleKey(keyhandler.KeyEvent{Key: gocui.KeyTab}, a)
	if r != keyhandler.Handled {
		t.Error("Tab should be Handled")
	}
	if pm.FocusIdx() != 1 {
		t.Errorf("after Tab: focusIdx = %d, want 1", pm.FocusIdx())
	}
}
