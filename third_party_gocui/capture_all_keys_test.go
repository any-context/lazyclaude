package gocui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Headless tests must not run in parallel because SimulationScreen is shared.
func TestExecKeybindings_CaptureAllKeysUsesEditorOnNonEditableView(t *testing.T) {
	g, err := NewGui(NewGuiOpts{
		OutputMode: OutputTrue,
		Headless:   true,
		Width:      20,
		Height:     10,
	})
	require.NoError(t, err)
	defer g.Close()

	v, err := g.SetView("anchor", 0, 0, 2, 2, 0)
	if err != nil {
		require.True(t, strings.Contains(err.Error(), "unknown view"))
	}
	v.Editable = false

	called := false
	v.CaptureAllKeys = true
	v.Editor = EditorFunc(func(v *View, key Key, ch rune, mod Modifier) bool {
		called = true
		require.Equal(t, rune('あ'), ch)
		return true
	})

	_, err = g.SetCurrentView("anchor")
	require.NoError(t, err)

	err = g.execKeybindings(v, &GocuiEvent{Type: eventKey, Ch: 'あ'})
	require.NoError(t, err)
	require.True(t, called)
}
