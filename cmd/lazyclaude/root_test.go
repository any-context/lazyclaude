package main

import (
	"testing"

	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/session"
)

// TestResolveActivityWindow_RemapsRemoteWindow verifies that a remote
// activity event's best-effort mirror-name Window is rewritten to the
// local tmux window ID of the matching mirror session. The original
// event must be left untouched (defensive copy), since callers may still
// reference it.
func TestResolveActivityWindow_RemapsRemoteWindow(t *testing.T) {
	store := session.NewStore("")
	store.Add(session.Session{
		ID:         "sess-123",
		Name:       "remote-1",
		Host:       "remote-host",
		Path:       "/project",
		TmuxWindow: "@42", // local tmux window ID of the mirror
	}, "/project")

	notif := &model.ActivityNotification{
		Window:   "rm-sess1234", // best-effort mirror name from RemoteProvider
		State:    model.ActivityRunning,
		ToolName: "Bash",
	}
	ev := model.Event{ActivityNotification: notif}

	out := resolveActivityWindow(store, ev, "sess-123")

	if out.ActivityNotification == nil {
		t.Fatal("out.ActivityNotification is nil")
	}
	if out.ActivityNotification.Window != "@42" {
		t.Errorf("out.Window = %q, want @42", out.ActivityNotification.Window)
	}
	if out.ActivityNotification.State != model.ActivityRunning {
		t.Errorf("out.State = %v, want Running", out.ActivityNotification.State)
	}
	if out.ActivityNotification.ToolName != "Bash" {
		t.Errorf("out.ToolName = %q, want Bash", out.ActivityNotification.ToolName)
	}

	// Original event / notification must be unchanged (defensive copy).
	if ev.ActivityNotification.Window != "rm-sess1234" {
		t.Errorf("original ev.Window mutated: got %q", ev.ActivityNotification.Window)
	}
	if notif.Window != "rm-sess1234" {
		t.Errorf("original notif.Window mutated: got %q", notif.Window)
	}
	if out.ActivityNotification == ev.ActivityNotification {
		t.Error("out.ActivityNotification aliases the input pointer; expected a copy")
	}
}

// TestResolveActivityWindow_NoSessionIDFallthrough verifies that local
// MCP events (which carry no session ID through this path) pass through
// unchanged. Local emission already has the correct tmux window ID.
func TestResolveActivityWindow_NoSessionIDFallthrough(t *testing.T) {
	store := session.NewStore("")
	ev := model.Event{ActivityNotification: &model.ActivityNotification{
		Window: "@7",
		State:  model.ActivityIdle,
	}}

	out := resolveActivityWindow(store, ev, "")

	if out.ActivityNotification.Window != "@7" {
		t.Errorf("out.Window = %q, want @7", out.ActivityNotification.Window)
	}
	if out.ActivityNotification.State != model.ActivityIdle {
		t.Errorf("out.State = %v, want Idle", out.ActivityNotification.State)
	}
}

// TestResolveActivityWindow_SessionNotFound verifies fallthrough behavior
// when the session ID does not match any local store entry. The event is
// returned unchanged so the best-effort Window (mirror name) is still
// published rather than silently dropped.
func TestResolveActivityWindow_SessionNotFound(t *testing.T) {
	store := session.NewStore("")
	ev := model.Event{ActivityNotification: &model.ActivityNotification{
		Window: "rm-xxxx",
		State:  model.ActivityRunning,
	}}

	out := resolveActivityWindow(store, ev, "unknown-session")

	if out.ActivityNotification.Window != "rm-xxxx" {
		t.Errorf("out.Window = %q, want rm-xxxx (unchanged)", out.ActivityNotification.Window)
	}
}

// TestResolveActivityWindow_TransitionalMirrorName documents behavior in
// the fallback path where MirrorManager.addMirrorSession could not
// resolve the tmux window ID eagerly (e.g. transient ListWindows error),
// so TmuxWindow stays as the mirror name until the next SyncWithTmux
// pass. In this state both emission and the sidebar key by "rm-xxxx",
// so the helper's rewrite keeps the event consistent. Primary Bug 4 fix
// is the eager resolve in mirror.go (TestMirrorManager_CreateMirror_
// StoresLocalTmuxID); this test documents the defensive fallback.
func TestResolveActivityWindow_TransitionalMirrorName(t *testing.T) {
	store := session.NewStore("")
	store.Add(session.Session{
		ID:         "sess-789",
		Name:       "fallback",
		Host:       "remote-host",
		Path:       "/project",
		TmuxWindow: "rm-sess7890", // fallback mirror-name state
	}, "/project")

	ev := model.Event{ActivityNotification: &model.ActivityNotification{
		Window: "rm-sess7890",
		State:  model.ActivityRunning,
	}}

	out := resolveActivityWindow(store, ev, "sess-789")

	if out.ActivityNotification.Window != "rm-sess7890" {
		t.Errorf("out.Window = %q, want rm-sess7890", out.ActivityNotification.Window)
	}
	if out.ActivityNotification.State != model.ActivityRunning {
		t.Errorf("out.State = %v, want Running", out.ActivityNotification.State)
	}
}

// TestResolveActivityWindow_SessionWithoutTmuxWindow verifies that a
// session present in the store but with an empty TmuxWindow (e.g. mirror
// not yet created by SyncWithTmux) falls through unchanged. Replacing
// with "" would break lookups downstream.
func TestResolveActivityWindow_SessionWithoutTmuxWindow(t *testing.T) {
	store := session.NewStore("")
	store.Add(session.Session{
		ID:         "sess-456",
		Name:       "pending",
		Host:       "remote-host",
		Path:       "/project",
		TmuxWindow: "", // not yet synced
	}, "/project")

	ev := model.Event{ActivityNotification: &model.ActivityNotification{
		Window: "rm-sess4567",
		State:  model.ActivityRunning,
	}}

	out := resolveActivityWindow(store, ev, "sess-456")

	if out.ActivityNotification.Window != "rm-sess4567" {
		t.Errorf("out.Window = %q, want rm-sess4567 (unchanged)", out.ActivityNotification.Window)
	}
}

// TestResolveActivityWindow_NilActivityNotification verifies the guard
// against nil ActivityNotification (other Event variants) — the event is
// returned unchanged and the function does not panic.
func TestResolveActivityWindow_NilActivityNotification(t *testing.T) {
	store := session.NewStore("")
	ev := model.Event{}

	out := resolveActivityWindow(store, ev, "sess-123")

	if out.ActivityNotification != nil {
		t.Errorf("out.ActivityNotification = %v, want nil", out.ActivityNotification)
	}
}
