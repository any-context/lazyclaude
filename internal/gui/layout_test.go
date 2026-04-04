package gui_test

import (
	"testing"

	"github.com/any-context/lazyclaude/internal/gui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Rect helpers
// ---------------------------------------------------------------------------

func TestRect_Width(t *testing.T) {
	t.Parallel()
	r := gui.Rect{X0: 0, Y0: 0, X1: 79, Y1: 23}
	assert.Equal(t, 79, r.Width())
}

func TestRect_Height(t *testing.T) {
	t.Parallel()
	r := gui.Rect{X0: 0, Y0: 0, X1: 79, Y1: 23}
	assert.Equal(t, 23, r.Height())
}

func TestRect_ZeroSize(t *testing.T) {
	t.Parallel()
	r := gui.Rect{X0: 5, Y0: 5, X1: 5, Y1: 5}
	assert.Equal(t, 0, r.Width())
	assert.Equal(t, 0, r.Height())
}

// ---------------------------------------------------------------------------
// ComputeLayout - main screen
// ---------------------------------------------------------------------------

func TestComputeLayout_NormalSize(t *testing.T) {
	t.Parallel()
	// Standard 80x24 terminal.
	l := gui.ComputeLayout(80, 24)

	// splitX = 80/3 = 26. leftH = 22, thirdH = 7.
	// sessions: Y0=0, Y1=7. plugins: Y0=8, Y1=14. logs: Y0=15, Y1=22.
	require.False(t, l.Compact, "80-wide should not be compact")

	assert.Equal(t, gui.Rect{X0: 0, Y0: 0, X1: 25, Y1: 7}, l.Sessions)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 8, X1: 25, Y1: 14}, l.Plugins)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 15, X1: 25, Y1: 22}, l.Server)
	assert.Equal(t, gui.Rect{X0: 26, Y0: 0, X1: 79, Y1: 22}, l.Main)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 22, X1: 79, Y1: 24}, l.Options)
}

func TestComputeLayout_WideTerminal(t *testing.T) {
	t.Parallel()
	// 200x50 terminal.
	l := gui.ComputeLayout(200, 50)

	// splitX=66. leftH=48, thirdH=16.
	// sessions: Y1=16. plugins: Y0=17, Y1=32. logs: Y0=33, Y1=48.
	require.False(t, l.Compact)

	assert.Equal(t, gui.Rect{X0: 0, Y0: 0, X1: 65, Y1: 16}, l.Sessions)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 17, X1: 65, Y1: 32}, l.Plugins)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 33, X1: 65, Y1: 48}, l.Server)
	assert.Equal(t, gui.Rect{X0: 66, Y0: 0, X1: 199, Y1: 48}, l.Main)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 48, X1: 199, Y1: 50}, l.Options)
}

func TestComputeLayout_NarrowTerminal(t *testing.T) {
	t.Parallel()
	// 50x24. splitX=20. leftH=22, thirdH=7.
	l := gui.ComputeLayout(50, 24)

	require.True(t, l.Compact, "50-wide terminal should be compact")

	assert.Equal(t, gui.Rect{X0: 0, Y0: 0, X1: 19, Y1: 7}, l.Sessions)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 8, X1: 19, Y1: 14}, l.Plugins)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 15, X1: 19, Y1: 22}, l.Server)
	assert.Equal(t, gui.Rect{X0: 20, Y0: 0, X1: 49, Y1: 22}, l.Main)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 22, X1: 49, Y1: 24}, l.Options)
}

func TestComputeLayout_VeryNarrow(t *testing.T) {
	t.Parallel()
	// 30x24. splitX=15. leftH=22, thirdH=7.
	l := gui.ComputeLayout(30, 24)

	require.True(t, l.Compact)

	assert.Equal(t, gui.Rect{X0: 0, Y0: 0, X1: 14, Y1: 7}, l.Sessions)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 8, X1: 14, Y1: 14}, l.Plugins)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 15, X1: 14, Y1: 22}, l.Server)
	assert.Equal(t, gui.Rect{X0: 15, Y0: 0, X1: 29, Y1: 22}, l.Main)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 22, X1: 29, Y1: 24}, l.Options)
}

func TestComputeLayout_MinimumSize(t *testing.T) {
	t.Parallel()
	// 10x5. splitX=5. leftH=3, thirdH=1.
	l := gui.ComputeLayout(10, 5)

	require.True(t, l.Compact)

	assert.Equal(t, gui.Rect{X0: 0, Y0: 0, X1: 4, Y1: 1}, l.Sessions)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 2, X1: 4, Y1: 2}, l.Plugins)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 3, X1: 4, Y1: 3}, l.Server)
	assert.Equal(t, gui.Rect{X0: 5, Y0: 0, X1: 9, Y1: 3}, l.Main)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 3, X1: 9, Y1: 5}, l.Options)
}

func TestComputeLayout_SplitX_MinWidth(t *testing.T) {
	t.Parallel()
	// When width/3 < 20, splitX must be clamped to 20.
	// Use width=51 so 51/3=17 < 20, and 20 < 51-10=41, so stays 20.
	l := gui.ComputeLayout(51, 24)

	assert.Equal(t, 0, l.Sessions.X0)
	assert.Equal(t, 19, l.Sessions.X1, "splitX-1 should be 19 (splitX=20)")
	assert.Equal(t, 20, l.Main.X0, "main starts at splitX=20")
}

func TestComputeLayout_SplitX_TooLargeClamp(t *testing.T) {
	t.Parallel()
	// When splitX >= maxX-10, clamp to maxX/2.
	// Use width=25: splitX=25/3=8, clamped to 20; 20 >= 25-10=15, so clamped to 25/2=12.
	l := gui.ComputeLayout(25, 24)

	assert.Equal(t, 11, l.Sessions.X1, "splitX-1 should be 11 (splitX=12=25/2)")
	assert.Equal(t, 12, l.Main.X0)
}

func TestComputeLayout_OptionsBar(t *testing.T) {
	t.Parallel()
	// Options bar must always be the last two rows.
	l := gui.ComputeLayout(80, 24)

	assert.Equal(t, 22, l.Options.Y0, "options bar starts at maxY-2")
	assert.Equal(t, 24, l.Options.Y1, "options bar ends at maxY")
	assert.Equal(t, 0, l.Options.X0)
	assert.Equal(t, 79, l.Options.X1)
}

func TestComputeLayout_ServerPanel(t *testing.T) {
	t.Parallel()
	// Server panel must start just below the plugins panel.
	l := gui.ComputeLayout(80, 24)

	assert.Equal(t, l.Plugins.Y1+1, l.Server.Y0, "server starts one row below plugins")
	assert.Equal(t, l.Sessions.X0, l.Server.X0, "server shares left edge")
	assert.Equal(t, l.Sessions.X1, l.Server.X1, "server shares right edge")
}

func TestComputeLayout_PluginsPanel(t *testing.T) {
	t.Parallel()
	// Plugins panel must start just below sessions.
	l := gui.ComputeLayout(80, 24)

	assert.Equal(t, l.Sessions.Y1+1, l.Plugins.Y0, "plugins starts one row below sessions")
	assert.Equal(t, l.Sessions.X0, l.Plugins.X0, "plugins shares left edge")
	assert.Equal(t, l.Sessions.X1, l.Plugins.X1, "plugins shares right edge")
}

func TestComputeLayout_SessionsAndMainShareTopEdge(t *testing.T) {
	t.Parallel()
	l := gui.ComputeLayout(80, 24)

	assert.Equal(t, l.Sessions.Y0, l.Main.Y0, "sessions and main both start at row 0")
}

// ---------------------------------------------------------------------------
// ComputeFullScreenLayout
// ---------------------------------------------------------------------------

func TestComputeFullScreenLayout(t *testing.T) {
	t.Parallel()
	// Main takes full width, status bar sits at bottom two rows.
	l := gui.ComputeFullScreenLayout(80, 24)

	assert.Equal(t, gui.Rect{X0: 0, Y0: 0, X1: 79, Y1: 22}, l.Main)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 22, X1: 79, Y1: 24}, l.Options)
}

func TestComputeFullScreenLayout_Wide(t *testing.T) {
	t.Parallel()
	l := gui.ComputeFullScreenLayout(200, 50)

	assert.Equal(t, gui.Rect{X0: 0, Y0: 0, X1: 199, Y1: 48}, l.Main)
	assert.Equal(t, gui.Rect{X0: 0, Y0: 48, X1: 199, Y1: 50}, l.Options)
}

// ---------------------------------------------------------------------------
// printableLen
// ---------------------------------------------------------------------------

func TestPrintableLen(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"plain text", "hello", 5},
		{"empty", "", 0},
		{"ansi green", "\x1b[32mhello\x1b[0m", 5},
		{"multiple ansi", "\x1b[1m\x1b[32mhi\x1b[0m", 2},
		{"no text just ansi", "\x1b[32m\x1b[0m", 0},
		{"mixed text and ansi", "a\x1b[31mb\x1b[0mc", 3},
		{"256-color", "\x1b[38;5;141mtest\x1b[0m", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gui.PrintableLenForTest(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Connection status formatting
// ---------------------------------------------------------------------------

func TestFormatConnectionStatus_NoProvider(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 120, 40)
	require.NoError(t, err)

	assert.Equal(t, "", app.FormatConnectionStatusForTest())
}

func TestFormatConnectionStatus_Connected(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 120, 40)
	require.NoError(t, err)

	app.SetConnectionStatus(func() []gui.ConnectionStatus {
		return []gui.ConnectionStatus{{Host: "AERO", State: "connected"}}
	})
	got := app.FormatConnectionStatusForTest()
	assert.Contains(t, got, "AERO")
	assert.NotContains(t, got, "offline")
	assert.NotContains(t, got, "reconnecting")
}

func TestFormatConnectionStatus_VersionMismatch(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 120, 40)
	require.NoError(t, err)

	app.SetConnectionStatus(func() []gui.ConnectionStatus {
		return []gui.ConnectionStatus{{Host: "AERO", State: "connected", VersionMismatch: true}}
	})
	got := app.FormatConnectionStatusForTest()
	assert.Contains(t, got, "version mismatch")
}

func TestFormatConnectionStatus_Error(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 120, 40)
	require.NoError(t, err)

	app.SetConnectionStatus(func() []gui.ConnectionStatus {
		return []gui.ConnectionStatus{{Host: "AERO", State: "error"}}
	})
	got := app.FormatConnectionStatusForTest()
	assert.Contains(t, got, "offline")
}

func TestFormatConnectionStatus_Reconnecting(t *testing.T) {
	app, err := gui.NewAppHeadless(gui.ModeMain, 120, 40)
	require.NoError(t, err)

	app.SetConnectionStatus(func() []gui.ConnectionStatus {
		return []gui.ConnectionStatus{{Host: "AERO", State: "reconnecting"}}
	})
	got := app.FormatConnectionStatusForTest()
	assert.Contains(t, got, "reconnecting")
}

// ---------------------------------------------------------------------------
// Existing tab / bar tests (unchanged)
