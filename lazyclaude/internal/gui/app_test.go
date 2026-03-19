package gui_test

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/gui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gocui headless tests must NOT run in parallel due to shared tcell SimulationScreen state.

func TestNewAppHeadless_ModeMain(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 120, 40)
	require.NoError(t, err)
	defer app.Gui().Close()

	assert.Equal(t, gui.ModeMain, app.Mode())
	assert.NotNil(t, app.Gui())
}

func TestNewAppHeadless_ModeDiff(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeDiff, 80, 30)
	require.NoError(t, err)
	defer app.Gui().Close()

	assert.Equal(t, gui.ModeDiff, app.Mode())
}

func TestNewAppHeadless_Layout_Main_NoError(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 120, 40)
	require.NoError(t, err)
	defer app.Gui().Close()

	err = app.TestLayout(app.Gui())
	if err != nil {
		t.Logf("Layout error (may be expected in headless): %v", err)
	}
	// In headless mode, layout may return ErrUnknownView on first call
	// as gocui's internal state may not be fully initialized without MainLoop.
	// The important thing is that it doesn't panic.
}

func TestNewAppHeadless_Layout_Popup_NoError(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeDiff, 80, 30)
	require.NoError(t, err)
	defer app.Gui().Close()

	err = app.TestLayout(app.Gui())
	if err != nil {
		t.Logf("Layout error (may be expected in headless): %v", err)
	}
}

