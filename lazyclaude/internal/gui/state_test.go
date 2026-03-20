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

func TestTransition_MainToFullScreen(t *testing.T) {
	t.Parallel()
	app := &App{state: StateMain}
	app.transition(StateFullScreen)
	assert.Equal(t, StateFullScreen, app.state)
}

func TestTransition_FullScreenToMain_ClearsTarget(t *testing.T) {
	t.Parallel()
	app := &App{state: StateFullScreen, fullScreenTarget: "sess-1", previewCache: "cached"}
	app.transition(StateMain)
	assert.Equal(t, StateMain, app.state)
	assert.Empty(t, app.fullScreenTarget, "target cleared on exit fullscreen")
	assert.Empty(t, app.previewCache, "cache cleared on exit fullscreen")
}

func TestTransition_SameState_NoOp(t *testing.T) {
	t.Parallel()
	app := &App{state: StateFullScreen, fullScreenTarget: "sess-1"}
	app.transition(StateFullScreen)
	assert.Equal(t, "sess-1", app.fullScreenTarget, "no change on same state")
}

func TestTransition_MainToFullScreen_ClearsCache(t *testing.T) {
	t.Parallel()
	app := &App{state: StateMain, previewCache: "old"}
	app.transition(StateFullScreen)
	assert.Empty(t, app.previewCache, "cache cleared on enter fullscreen")
	assert.Equal(t, 0, app.fullScreenScrollY, "scroll reset on enter fullscreen")
}
