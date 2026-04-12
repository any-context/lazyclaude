package main

import (
	"context"
	"testing"

	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/daemon"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMirrorManager_CreateMirror_StoresLocalTmuxID verifies the Bug 4
// hotfix: mirror creation must seed sess.TmuxWindow with the mirror's
// LOCAL tmux window ID ("@N"), not the mirror-name ("rm-xxxx"). Without
// this, activity events published between CreateMirror and the first
// SyncWithTmux pass are keyed by the wrong value and the sidebar reverts
// to "Unknown" once sync runs.
func TestMirrorManager_CreateMirror_StoresLocalTmuxID(t *testing.T) {
	mock := tmux.NewMockClient()
	// The lazyclaude session needs to exist so that MirrorManager takes
	// the NewWindow path (rather than NewSession).
	require.NoError(t, mock.NewSession(context.Background(), tmux.NewSessionOpts{
		Name:       "lazyclaude",
		WindowName: "placeholder",
	}))

	store := session.NewStore("")
	mgr := &MirrorManager{
		tmux:  mock,
		store: store,
	}

	const (
		host        = "AERO"
		sessionID   = "sess1234-abcd-ef01-2345-6789abcdef01"
		groupPath   = "/remote/proj"
		sessionName = "my-remote"
	)
	resp := &daemon.SessionCreateResponse{
		ID:         sessionID,
		Name:       sessionName,
		Path:       groupPath,
		TmuxWindow: "lc-sess1234", // remote tmux window name, as daemon reports
		Role:       string(session.RoleNone),
	}

	require.NoError(t, mgr.CreateMirror(host, groupPath, resp))

	stored := store.FindByID(sessionID)
	require.NotNil(t, stored, "session must be in the local store")
	assert.Equal(t, host, stored.Host)

	// The mirror name is "rm-" + first 8 chars of ID.
	mirrorName := session.MirrorWindowName(sessionID)
	require.NotEqual(t, "", mirrorName)

	// The key assertion: TmuxWindow must be the local tmux window ID
	// assigned by tmux (starts with "@"), NOT the mirror window name.
	// The MockClient assigns sequential @N IDs starting at @0 for the
	// first window (placeholder) and @1 for the freshly created mirror.
	assert.True(t, len(stored.TmuxWindow) >= 2 && stored.TmuxWindow[0] == '@',
		"TmuxWindow = %q, want an @-prefixed local tmux ID (not the mirror name %q)",
		stored.TmuxWindow, mirrorName)
	assert.NotEqual(t, mirrorName, stored.TmuxWindow,
		"TmuxWindow must not be the mirror name before SyncWithTmux runs")

	// Sanity: the resolved ID matches the actual window tmux created.
	windows, err := mock.ListWindows(context.Background(), "lazyclaude")
	require.NoError(t, err)
	var mirrorID string
	for _, w := range windows {
		if w.Name == mirrorName {
			mirrorID = w.ID
			break
		}
	}
	require.NotEqual(t, "", mirrorID, "mock must have created the mirror window")
	assert.Equal(t, mirrorID, stored.TmuxWindow)
}

// TestResolveMirrorTmuxID_FallbackOnListError verifies that a ListWindows
// error leaves the mirror name unchanged rather than blanking TmuxWindow.
// This preserves the historical behavior (sync will correct it on the
// next pass) when tmux is transiently unavailable.
func TestResolveMirrorTmuxID_FallbackOnListError(t *testing.T) {
	mock := tmux.NewMockClient()
	mock.ErrListWindows = assert.AnError
	got := resolveMirrorTmuxID(mock, "rm-abcd1234")
	assert.Equal(t, "rm-abcd1234", got)
}

// TestResolveMirrorTmuxID_FallbackOnNotFound verifies that a missing
// window (e.g., killed between NewWindow and ListWindows) also falls
// back to the mirror name instead of returning empty.
func TestResolveMirrorTmuxID_FallbackOnNotFound(t *testing.T) {
	mock := tmux.NewMockClient()
	// lazyclaude session exists but no matching window.
	require.NoError(t, mock.NewSession(context.Background(), tmux.NewSessionOpts{
		Name:       "lazyclaude",
		WindowName: "other-window",
	}))
	got := resolveMirrorTmuxID(mock, "rm-absent")
	assert.Equal(t, "rm-absent", got)
}
