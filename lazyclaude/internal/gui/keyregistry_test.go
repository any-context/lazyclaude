package gui

import (
	"testing"

	"github.com/jesseduffield/gocui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeyRegistry_Register_And_Lookup(t *testing.T) {
	t.Parallel()
	r := NewKeyRegistry()
	r.Register(ActionDef{
		Action:      ActionQuit,
		Name:        "Quit",
		Description: "Quit the application",
		Bindings:    []KeyBinding{{Rune: 'q'}},
		States:      []AppState{StateMain},
	})

	defs := r.AllActions()
	require.Len(t, defs, 1)
	assert.Equal(t, "Quit", defs[0].Name)
	assert.Equal(t, ActionQuit, defs[0].Action)
}

func TestKeyRegistry_Match_RuneKey(t *testing.T) {
	t.Parallel()
	r := NewKeyRegistry()
	r.Register(ActionDef{
		Action:   ActionQuit,
		Name:     "Quit",
		Bindings: []KeyBinding{{Rune: 'q'}},
		States:   []AppState{StateMain},
	})

	def, ok := r.Match('q', 0, gocui.ModNone, StateMain)
	require.True(t, ok)
	assert.Equal(t, ActionQuit, def.Action)
}

func TestKeyRegistry_Match_SpecialKey(t *testing.T) {
	t.Parallel()
	r := NewKeyRegistry()
	r.Register(ActionDef{
		Action:   ActionEnterFull,
		Name:     "Enter Full Screen",
		Bindings: []KeyBinding{{Key: gocui.KeyEnter}},
		States:   []AppState{StateMain},
	})

	def, ok := r.Match(0, gocui.KeyEnter, gocui.ModNone, StateMain)
	require.True(t, ok)
	assert.Equal(t, ActionEnterFull, def.Action)
}

func TestKeyRegistry_Match_WrongState_NoMatch(t *testing.T) {
	t.Parallel()
	r := NewKeyRegistry()
	r.Register(ActionDef{
		Action:   ActionQuit,
		Name:     "Quit",
		Bindings: []KeyBinding{{Rune: 'q'}},
		States:   []AppState{StateMain},
	})

	_, ok := r.Match('q', 0, gocui.ModNone, StateFullInsert)
	assert.False(t, ok, "should not match in wrong state")
}

func TestKeyRegistry_Match_MultipleBindings(t *testing.T) {
	t.Parallel()
	r := NewKeyRegistry()
	r.Register(ActionDef{
		Action: ActionCursorUp,
		Name:   "Cursor Up",
		Bindings: []KeyBinding{
			{Rune: 'k'},
			{Key: gocui.KeyArrowUp},
		},
		States: []AppState{StateMain},
	})

	def, ok := r.Match('k', 0, gocui.ModNone, StateMain)
	require.True(t, ok)
	assert.Equal(t, ActionCursorUp, def.Action)

	def, ok = r.Match(0, gocui.KeyArrowUp, gocui.ModNone, StateMain)
	require.True(t, ok)
	assert.Equal(t, ActionCursorUp, def.Action)
}

func TestKeyRegistry_Match_MultipleStates(t *testing.T) {
	t.Parallel()
	r := NewKeyRegistry()
	r.Register(ActionDef{
		Action:   ActionExitFull,
		Name:     "Exit Full Screen",
		Bindings: []KeyBinding{{Key: gocui.KeyCtrlD}},
		States:   []AppState{StateFullInsert, StateFullNormal},
	})

	_, ok := r.Match(0, gocui.KeyCtrlD, gocui.ModNone, StateFullInsert)
	assert.True(t, ok)

	_, ok = r.Match(0, gocui.KeyCtrlD, gocui.ModNone, StateFullNormal)
	assert.True(t, ok)

	_, ok = r.Match(0, gocui.KeyCtrlD, gocui.ModNone, StateMain)
	assert.False(t, ok)
}

func TestKeyRegistry_AllActions_Order(t *testing.T) {
	t.Parallel()
	r := NewKeyRegistry()
	r.Register(ActionDef{Action: ActionQuit, Name: "Quit", Bindings: []KeyBinding{{Rune: 'q'}}, States: []AppState{StateMain}})
	r.Register(ActionDef{Action: ActionEnterFull, Name: "Enter", Bindings: []KeyBinding{{Key: gocui.KeyEnter}}, States: []AppState{StateMain}})
	r.Register(ActionDef{Action: ActionCursorDown, Name: "Down", Bindings: []KeyBinding{{Rune: 'j'}}, States: []AppState{StateMain}})

	defs := r.AllActions()
	require.Len(t, defs, 3)
	// Registration order preserved
	assert.Equal(t, "Quit", defs[0].Name)
	assert.Equal(t, "Enter", defs[1].Name)
	assert.Equal(t, "Down", defs[2].Name)
}

func TestKeyRegistry_BindingsForAction(t *testing.T) {
	t.Parallel()
	r := NewKeyRegistry()
	r.Register(ActionDef{
		Action: ActionCursorUp,
		Name:   "Cursor Up",
		Bindings: []KeyBinding{
			{Rune: 'k'},
			{Key: gocui.KeyArrowUp},
		},
		States: []AppState{StateMain},
	})

	bindings := r.BindingsFor(ActionCursorUp)
	require.Len(t, bindings, 2)
	assert.Equal(t, 'k', bindings[0].Rune)
	assert.Equal(t, gocui.KeyArrowUp, bindings[1].Key)
}

func TestKeyRegistry_BindingsForAction_NotFound(t *testing.T) {
	t.Parallel()
	r := NewKeyRegistry()
	bindings := r.BindingsFor(ActionQuit)
	assert.Empty(t, bindings)
}

func TestKeyRegistry_FirstRune(t *testing.T) {
	t.Parallel()
	r := NewKeyRegistry()
	r.Register(ActionDef{
		Action:   ActionCursorUp,
		Name:     "Cursor Up",
		Bindings: []KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		States:   []AppState{StateMain},
	})

	assert.Equal(t, 'k', r.FirstRune(ActionCursorUp))
	assert.Equal(t, rune(0), r.FirstRune(ActionQuit))
}

func TestKeyRegistry_FirstKey(t *testing.T) {
	t.Parallel()
	r := NewKeyRegistry()
	r.Register(ActionDef{
		Action:   ActionEnterFull,
		Name:     "Enter",
		Bindings: []KeyBinding{{Key: gocui.KeyEnter}},
		States:   []AppState{StateMain},
	})

	assert.Equal(t, gocui.KeyEnter, r.FirstKey(ActionEnterFull))
	assert.Equal(t, gocui.Key(0), r.FirstKey(ActionQuit))
}
