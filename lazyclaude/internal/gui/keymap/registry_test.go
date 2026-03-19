package keymap_test

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/gui/keymap"
	"github.com/jesseduffield/gocui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_Register_And_Lookup(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionQuit,
		Name:     "Quit",
		Bindings: []keymap.KeyBinding{{Rune: 'q'}},
		States:   []keymap.AppState{keymap.StateMain},
	})

	defs := r.AllActions()
	require.Len(t, defs, 1)
	assert.Equal(t, "Quit", defs[0].Name)
	assert.Equal(t, keymap.ActionQuit, defs[0].Action)
}

func TestRegistry_Match_RuneKey(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionQuit,
		Name:     "Quit",
		Bindings: []keymap.KeyBinding{{Rune: 'q'}},
		States:   []keymap.AppState{keymap.StateMain},
	})

	def, ok := r.Match('q', 0, gocui.ModNone, keymap.StateMain)
	require.True(t, ok)
	assert.Equal(t, keymap.ActionQuit, def.Action)
}

func TestRegistry_Match_WrongState_NoMatch(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionQuit,
		Name:     "Quit",
		Bindings: []keymap.KeyBinding{{Rune: 'q'}},
		States:   []keymap.AppState{keymap.StateMain},
	})

	_, ok := r.Match('q', 0, gocui.ModNone, keymap.StateFullInsert)
	assert.False(t, ok)
}

func TestRegistry_Match_MultipleBindings(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionCursorUp,
		Name:     "Cursor Up",
		Bindings: []keymap.KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		States:   []keymap.AppState{keymap.StateMain},
	})

	def, ok := r.Match('k', 0, gocui.ModNone, keymap.StateMain)
	require.True(t, ok)
	assert.Equal(t, keymap.ActionCursorUp, def.Action)

	def, ok = r.Match(0, gocui.KeyArrowUp, gocui.ModNone, keymap.StateMain)
	require.True(t, ok)
	assert.Equal(t, keymap.ActionCursorUp, def.Action)
}

func TestRegistry_Match_MultipleStates(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionExitFull,
		Name:     "Exit",
		Bindings: []keymap.KeyBinding{{Key: gocui.KeyCtrlD}},
		States:   []keymap.AppState{keymap.StateFullInsert, keymap.StateFullNormal},
	})

	_, ok := r.Match(0, gocui.KeyCtrlD, gocui.ModNone, keymap.StateFullInsert)
	assert.True(t, ok)
	_, ok = r.Match(0, gocui.KeyCtrlD, gocui.ModNone, keymap.StateMain)
	assert.False(t, ok)
}

func TestRegistry_AllActions_Order(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{Action: keymap.ActionQuit, Name: "Quit", Bindings: []keymap.KeyBinding{{Rune: 'q'}}, States: []keymap.AppState{keymap.StateMain}})
	r.Register(keymap.ActionDef{Action: keymap.ActionEnterFull, Name: "Enter", Bindings: []keymap.KeyBinding{{Key: gocui.KeyEnter}}, States: []keymap.AppState{keymap.StateMain}})

	defs := r.AllActions()
	require.Len(t, defs, 2)
	assert.Equal(t, "Quit", defs[0].Name)
	assert.Equal(t, "Enter", defs[1].Name)
}

func TestRegistry_BindingsFor(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionCursorUp,
		Name:     "Up",
		Bindings: []keymap.KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		States:   []keymap.AppState{keymap.StateMain},
	})

	bindings := r.BindingsFor(keymap.ActionCursorUp)
	require.Len(t, bindings, 2)
	assert.Equal(t, 'k', bindings[0].Rune)
}

func TestRegistry_FirstRune(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionCursorUp,
		Name:     "Up",
		Bindings: []keymap.KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		States:   []keymap.AppState{keymap.StateMain},
	})

	assert.Equal(t, 'k', r.FirstRune(keymap.ActionCursorUp))
	assert.Equal(t, rune(0), r.FirstRune(keymap.ActionQuit))
}

func TestRegistry_FirstKey(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionEnterFull,
		Name:     "Enter",
		Bindings: []keymap.KeyBinding{{Key: gocui.KeyEnter}},
		States:   []keymap.AppState{keymap.StateMain},
	})

	assert.Equal(t, gocui.KeyEnter, r.FirstKey(keymap.ActionEnterFull))
	assert.Equal(t, gocui.Key(0), r.FirstKey(keymap.ActionQuit))
}

func TestDefault_HasAllActions(t *testing.T) {
	t.Parallel()
	r := keymap.Default()
	defs := r.AllActions()
	assert.GreaterOrEqual(t, len(defs), 13, "default registry should have all actions")
}
