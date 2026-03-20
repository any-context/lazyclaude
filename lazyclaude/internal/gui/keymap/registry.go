package keymap

import "github.com/jesseduffield/gocui"

// ActionDef defines a logical action with its key bindings and valid states.
type ActionDef struct {
	Action      KeyAction
	Name        string       // human-readable name (for help screen)
	Description string       // tooltip
	Bindings    []KeyBinding // physical keys
	States      []AppState   // which states this action is active in
}

// Registry is the single source of truth for all key bindings.
// It supports lookup by key event + state, and enumeration for help screens.
type Registry struct {
	defs  []ActionDef       // ordered list (registration order)
	index map[KeyAction]int // maps action to index in defs (stable across appends)
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		index: make(map[KeyAction]int),
	}
}

// Register adds an action definition to the registry.
func (r *Registry) Register(def ActionDef) {
	r.defs = append(r.defs, def)
	r.index[def.Action] = len(r.defs) - 1
}

// Match finds an action matching the key event in the given state.
func (r *Registry) Match(ch rune, key gocui.Key, mod gocui.Modifier, state AppState) (ActionDef, bool) {
	for _, def := range r.defs {
		if !stateMatch(def.States, state) {
			continue
		}
		for _, b := range def.Bindings {
			if b.Matches(key, ch, mod) {
				return def, true
			}
		}
	}
	return ActionDef{}, false
}

// AllActions returns all registered actions in registration order.
func (r *Registry) AllActions() []ActionDef {
	result := make([]ActionDef, len(r.defs))
	copy(result, r.defs)
	return result
}

// BindingsFor returns the key bindings for a specific action.
func (r *Registry) BindingsFor(action KeyAction) []KeyBinding {
	idx, ok := r.index[action]
	if !ok {
		return nil
	}
	src := r.defs[idx].Bindings
	result := make([]KeyBinding, len(src))
	copy(result, src)
	return result
}

// FirstRune returns the first rune binding for an action, or 0 if none.
func (r *Registry) FirstRune(action KeyAction) rune {
	idx, ok := r.index[action]
	if !ok {
		return 0
	}
	for _, b := range r.defs[idx].Bindings {
		if b.Rune != 0 {
			return b.Rune
		}
	}
	return 0
}

// FirstKey returns the first gocui.Key binding for an action, or 0 if none.
func (r *Registry) FirstKey(action KeyAction) gocui.Key {
	idx, ok := r.index[action]
	if !ok {
		return 0
	}
	for _, b := range r.defs[idx].Bindings {
		if b.Rune == 0 {
			return b.Key
		}
	}
	return 0
}

func stateMatch(states []AppState, target AppState) bool {
	for _, s := range states {
		if s == target {
			return true
		}
	}
	return false
}

// AllAppStates returns all valid AppState values.
func AllAppStates() []AppState {
	return []AppState{StateMain, StateFullScreen}
}

// Default returns the default lazyclaude key registry.
func Default() *Registry {
	r := NewRegistry()

	r.Register(ActionDef{
		Action:   ActionQuit,
		Name:     "Quit",
		Bindings: []KeyBinding{{Rune: 'q'}},
		States:   []AppState{StateMain},
	})
	r.Register(ActionDef{
		Action:   ActionEnterFull,
		Name:     "Enter Full Screen",
		Bindings: []KeyBinding{{Key: gocui.KeyEnter}},
		States:   []AppState{StateMain},
	})
	r.Register(ActionDef{
		Action:   ActionExitFull,
		Name:     "Exit Full Screen",
		Bindings: []KeyBinding{{Key: gocui.KeyCtrlD}, {Key: gocui.KeyCtrlBackslash}},
		States:   []AppState{StateFullScreen},
	})
	r.Register(ActionDef{
		Action:   ActionCursorUp,
		Name:     "Cursor Up",
		Bindings: []KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		States:   AllAppStates(),
	})
	r.Register(ActionDef{
		Action:   ActionCursorDown,
		Name:     "Cursor Down",
		Bindings: []KeyBinding{{Rune: 'j'}, {Key: gocui.KeyArrowDown}},
		States:   AllAppStates(),
	})
	r.Register(ActionDef{
		Action:   ActionNewSession,
		Name:     "New Session",
		Bindings: []KeyBinding{{Rune: 'n'}},
		States:   []AppState{StateMain},
	})
	r.Register(ActionDef{
		Action:   ActionDeleteSession,
		Name:     "Delete Session",
		Bindings: []KeyBinding{{Rune: 'd'}},
		States:   []AppState{StateMain},
	})
	r.Register(ActionDef{
		Action:   ActionPopupAccept,
		Name:     "Accept",
		Bindings: []KeyBinding{{Rune: 'y'}, {Rune: '1'}},
		States:   AllAppStates(),
	})
	r.Register(ActionDef{
		Action:   ActionPopupAllow,
		Name:     "Allow Always",
		Bindings: []KeyBinding{{Rune: 'a'}, {Rune: '2'}},
		States:   AllAppStates(),
	})
	r.Register(ActionDef{
		Action:   ActionPopupReject,
		Name:     "Reject",
		Bindings: []KeyBinding{{Rune: '3'}},
		States:   AllAppStates(),
	})
	r.Register(ActionDef{
		Action:   ActionPopupCancel,
		Name:     "Cancel / Suspend",
		Bindings: []KeyBinding{{Key: gocui.KeyEsc}},
		States:   AllAppStates(),
	})

	return r
}
