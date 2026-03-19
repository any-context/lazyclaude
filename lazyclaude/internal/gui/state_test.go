package gui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppState_IsFullScreen(t *testing.T) {
	t.Parallel()
	assert.False(t, StateMain.IsFullScreen())
	assert.True(t, StateFullInsert.IsFullScreen())
	assert.True(t, StateFullNormal.IsFullScreen())
}

func TestTransition_MainToFullInsert(t *testing.T) {
	t.Parallel()
	app := &App{state: StateMain}
	app.transition(StateFullInsert)
	assert.Equal(t, StateFullInsert, app.state)
}

func TestTransition_FullInsertToFullNormal(t *testing.T) {
	t.Parallel()
	app := &App{state: StateFullInsert, fullScreenTarget: "sess-1"}
	app.transition(StateFullNormal)
	assert.Equal(t, StateFullNormal, app.state)
	assert.Equal(t, "sess-1", app.fullScreenTarget, "target preserved within fullscreen")
}

func TestTransition_FullNormalToMain_ClearsTarget(t *testing.T) {
	t.Parallel()
	app := &App{state: StateFullNormal, fullScreenTarget: "sess-1", previewCache: "cached"}
	app.transition(StateMain)
	assert.Equal(t, StateMain, app.state)
	assert.Empty(t, app.fullScreenTarget, "target cleared on exit fullscreen")
	assert.Empty(t, app.previewCache, "cache cleared on exit fullscreen")
}

func TestTransition_SameState_NoOp(t *testing.T) {
	t.Parallel()
	app := &App{state: StateFullInsert, fullScreenTarget: "sess-1"}
	app.transition(StateFullInsert)
	assert.Equal(t, "sess-1", app.fullScreenTarget, "no change on same state")
}

func TestTransition_MainToFullInsert_ClearsCache(t *testing.T) {
	t.Parallel()
	app := &App{state: StateMain, previewCache: "old"}
	app.transition(StateFullInsert)
	assert.Empty(t, app.previewCache, "cache cleared on enter fullscreen")
	assert.Equal(t, 0, app.fullScreenScrollY, "scroll reset on enter fullscreen")
}
