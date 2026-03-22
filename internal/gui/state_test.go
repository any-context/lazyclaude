package gui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppState_IsFullScreen(t *testing.T) {
	t.Parallel()
	assert.False(t, StateMain.IsFullScreen())
	assert.True(t, StateFullScreen.IsFullScreen())
}

func TestFullScreen_EnterExit(t *testing.T) {
	t.Parallel()
	fs := NewFullScreenState(&PreviewCache{})
	fs.Enter("sess-1")
	assert.True(t, fs.IsActive())
	assert.Equal(t, "sess-1", fs.Target())
}

func TestFullScreen_ExitClearsTarget(t *testing.T) {
	t.Parallel()
	pc := &PreviewCache{content: "cached"}
	fs := NewFullScreenState(pc)
	fs.Enter("sess-1")
	fs.Exit()
	assert.False(t, fs.IsActive())
	assert.Empty(t, fs.Target())
	assert.Empty(t, pc.Content(), "cache cleared on exit")
}

func TestFullScreen_EnterResetsScroll(t *testing.T) {
	t.Parallel()
	pc := &PreviewCache{content: "old"}
	fs := NewFullScreenState(pc)
	fs.Enter("sess-1")
	assert.Empty(t, pc.Content(), "cache cleared on enter")
	assert.Equal(t, 0, fs.ScrollY(), "scroll reset on enter")
}
