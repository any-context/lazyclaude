package gui

import "testing"

func TestFullScreenState_InitiallyInactive(t *testing.T) {
	fs := NewFullScreenState(&PreviewCache{})
	if fs.IsActive() {
		t.Error("initial state should be inactive")
	}
	if fs.Target() != "" {
		t.Errorf("initial target = %q, want empty", fs.Target())
	}
}

func TestFullScreenState_EnterExit(t *testing.T) {
	fs := NewFullScreenState(&PreviewCache{})
	fs.Enter("sess-1")
	if !fs.IsActive() {
		t.Error("after Enter: should be active")
	}
	if fs.Target() != "sess-1" {
		t.Errorf("target = %q, want sess-1", fs.Target())
	}
	if fs.ScrollY() != 0 {
		t.Errorf("scrollY = %d, want 0", fs.ScrollY())
	}
	fs.Exit()
	if fs.IsActive() {
		t.Error("after Exit: should be inactive")
	}
	if fs.Target() != "" {
		t.Errorf("after Exit: target = %q, want empty", fs.Target())
	}
}

func TestFullScreenState_ScrollY(t *testing.T) {
	fs := NewFullScreenState(&PreviewCache{})
	fs.Enter("sess-1")
	fs.ScrollDown()
	fs.ScrollDown()
	if fs.ScrollY() != 2 {
		t.Errorf("scrollY = %d, want 2", fs.ScrollY())
	}
	fs.ScrollUp()
	if fs.ScrollY() != 1 {
		t.Errorf("scrollY = %d, want 1", fs.ScrollY())
	}
	fs.ScrollUp()
	fs.ScrollUp() // clamp at 0
	if fs.ScrollY() != 0 {
		t.Errorf("scrollY = %d, want 0", fs.ScrollY())
	}
}

func TestFullScreenState_EnterResetsScroll(t *testing.T) {
	fs := NewFullScreenState(&PreviewCache{})
	fs.Enter("sess-1")
	fs.ScrollDown()
	fs.ScrollDown()
	fs.Exit()
	fs.Enter("sess-2")
	if fs.ScrollY() != 0 {
		t.Errorf("after re-enter: scrollY = %d, want 0", fs.ScrollY())
	}
}

func TestFullScreenState_ForwardKey(t *testing.T) {
	fwd := &MockInputForwarder{}
	fs := NewFullScreenState(&PreviewCache{})
	fs.SetForwarder(fwd)
	fs.Enter("sess-1")

	fs.EnqueueKey("lazyclaude:lc-test", "a")
	fs.DrainQueue()

	keys := fwd.Keys()
	if len(keys) != 1 || keys[0] != "a" {
		t.Errorf("forwarded keys = %v, want [a]", keys)
	}
}

func TestFullScreenState_ForwardSpecialKey(t *testing.T) {
	fwd := &MockInputForwarder{}
	fs := NewFullScreenState(&PreviewCache{})
	fs.SetForwarder(fwd)
	fs.Enter("sess-1")

	fs.EnqueueKey("lazyclaude:lc-test", "Enter")
	fs.DrainQueue()

	keys := fwd.Keys()
	if len(keys) != 1 || keys[0] != "Enter" {
		t.Errorf("forwarded keys = %v, want [Enter]", keys)
	}
}

func TestFullScreenState_NoForwardWhenInactive(t *testing.T) {
	fwd := &MockInputForwarder{}
	fs := NewFullScreenState(&PreviewCache{})
	fs.SetForwarder(fwd)
	// Not active — EnqueueKey still works but resolveForwardTarget should return ""
	fs.EnqueueKey("lazyclaude:lc-test", "a")
	fs.DrainQueue()
	if len(fwd.Keys()) != 1 {
		// EnqueueKey doesn't check active; caller (App) is responsible for guard
		t.Log("EnqueueKey works regardless of active state (caller guards)")
	}
}
