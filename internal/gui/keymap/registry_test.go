package keymap_test

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/gui/keymap"
	"github.com/jesseduffield/gocui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_Register_And_AllActions(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionQuit,
		Bindings: []keymap.KeyBinding{{Rune: 'q'}},
		Scope:    keymap.ScopeGlobal,
	})

	defs := r.AllActions()
	require.Len(t, defs, 1)
	assert.Equal(t, keymap.ActionQuit, defs[0].Action)
}

func TestRegistry_Match_RuneKey(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionQuit,
		Bindings: []keymap.KeyBinding{{Rune: 'q'}},
		Scope:    keymap.ScopeGlobal,
	})

	def, ok := r.Match('q', 0, gocui.ModNone, keymap.ScopeGlobal)
	require.True(t, ok)
	assert.Equal(t, keymap.ActionQuit, def.Action)
}

func TestRegistry_Match_WrongScope_NoMatch(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionQuit,
		Bindings: []keymap.KeyBinding{{Rune: 'q'}},
		Scope:    keymap.ScopeGlobal,
	})

	_, ok := r.Match('q', 0, gocui.ModNone, keymap.ScopeSession)
	assert.False(t, ok)
}

func TestRegistry_Match_MultipleBindings(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionCursorUp,
		Bindings: []keymap.KeyBinding{{Rune: 'k'}, {Key: gocui.KeyArrowUp}},
		Scope:    keymap.ScopeSession,
	})

	def, ok := r.Match('k', 0, gocui.ModNone, keymap.ScopeSession)
	require.True(t, ok)
	assert.Equal(t, keymap.ActionCursorUp, def.Action)

	def, ok = r.Match(0, gocui.KeyArrowUp, gocui.ModNone, keymap.ScopeSession)
	require.True(t, ok)
	assert.Equal(t, keymap.ActionCursorUp, def.Action)
}

func TestRegistry_Match_SpecialKey(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionExitFull,
		Bindings: []keymap.KeyBinding{{Key: gocui.KeyCtrlD}},
		Scope:    keymap.ScopeFullScreen,
	})

	_, ok := r.Match(0, gocui.KeyCtrlD, gocui.ModNone, keymap.ScopeFullScreen)
	assert.True(t, ok)
	_, ok = r.Match(0, gocui.KeyCtrlD, gocui.ModNone, keymap.ScopeGlobal)
	assert.False(t, ok)
}

func TestRegistry_AllActions_Order(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{Action: keymap.ActionQuit, Bindings: []keymap.KeyBinding{{Rune: 'q'}}, Scope: keymap.ScopeGlobal})
	r.Register(keymap.ActionDef{Action: keymap.ActionEnterFull, Bindings: []keymap.KeyBinding{{Key: gocui.KeyEnter}}, Scope: keymap.ScopeSession})

	defs := r.AllActions()
	require.Len(t, defs, 2)
	assert.Equal(t, keymap.ActionQuit, defs[0].Action)
	assert.Equal(t, keymap.ActionEnterFull, defs[1].Action)
}

func TestRegistry_HintsForScope(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:    keymap.ActionQuit,
		Bindings:  []keymap.KeyBinding{{Rune: 'q'}},
		Scope:     keymap.ScopeGlobal,
		HintLabel: "quit",
	})
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionFocusNextPanel,
		Bindings: []keymap.KeyBinding{{Key: gocui.KeyTab}},
		Scope:    keymap.ScopeGlobal,
	})
	r.Register(keymap.ActionDef{
		Action:    keymap.ActionNewSession,
		Bindings:  []keymap.KeyBinding{{Rune: 'n'}},
		Scope:     keymap.ScopeSession,
		HintLabel: "new",
	})

	globalHints := r.HintsForScope(keymap.ScopeGlobal)
	require.Len(t, globalHints, 1)
	assert.Equal(t, "quit", globalHints[0].HintLabel)

	sessionHints := r.HintsForScope(keymap.ScopeSession)
	require.Len(t, sessionHints, 1)
	assert.Equal(t, "new", sessionHints[0].HintLabel)
}

func TestRegistry_BindingsForScope(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{Action: keymap.ActionQuit, Bindings: []keymap.KeyBinding{{Rune: 'q'}}, Scope: keymap.ScopeGlobal})
	r.Register(keymap.ActionDef{Action: keymap.ActionNewSession, Bindings: []keymap.KeyBinding{{Rune: 'n'}}, Scope: keymap.ScopeSession})
	r.Register(keymap.ActionDef{Action: keymap.ActionDeleteSession, Bindings: []keymap.KeyBinding{{Rune: 'd'}}, Scope: keymap.ScopeSession})

	sessionDefs := r.BindingsForScope(keymap.ScopeSession)
	require.Len(t, sessionDefs, 2)
}

func TestDefault_HasAllScopes(t *testing.T) {
	t.Parallel()
	r := keymap.Default()
	defs := r.AllActions()
	assert.GreaterOrEqual(t, len(defs), 40, "default registry should have all actions")

	scopes := make(map[keymap.Scope]bool)
	for _, d := range defs {
		scopes[d.Scope] = true
	}
	assert.True(t, scopes[keymap.ScopeGlobal])
	assert.True(t, scopes[keymap.ScopeSession])
	assert.True(t, scopes[keymap.ScopePlugins])
	// ScopeMarketplace removed — plugins use Tab field instead
	assert.True(t, scopes[keymap.ScopeLog])
	assert.True(t, scopes[keymap.ScopePopup])
	assert.True(t, scopes[keymap.ScopeFullScreen])
}

func TestDefault_CtrlBackslash_ExitsFullScreen(t *testing.T) {
	t.Parallel()
	r := keymap.Default()
	def, ok := r.Match(0, gocui.KeyCtrlBackslash, gocui.ModNone, keymap.ScopeFullScreen)
	require.True(t, ok, "Ctrl+\\ should match in fullscreen scope")
	assert.Equal(t, keymap.ActionExitFull, def.Action)
}

func TestDefault_CtrlBackslash_NotInGlobal(t *testing.T) {
	t.Parallel()
	r := keymap.Default()
	// Ctrl+\ in global scope is ActionQuitCtrlBackslash, not ActionExitFull
	def, ok := r.Match(0, gocui.KeyCtrlBackslash, gocui.ModNone, keymap.ScopeGlobal)
	require.True(t, ok)
	assert.Equal(t, keymap.ActionQuitCtrlBackslash, def.Action)
}

func TestHintKeyLabel_WithHintKey(t *testing.T) {
	t.Parallel()
	def := keymap.ActionDef{
		Action:    keymap.ActionCollapseProject,
		Bindings:  []keymap.KeyBinding{{Rune: 'h'}},
		HintLabel: "fold",
		HintKey:   "h/l",
	}
	assert.Equal(t, "h/l", def.HintKeyLabel())
}

func TestHintKeyLabel_AutoFromBinding(t *testing.T) {
	t.Parallel()
	def := keymap.ActionDef{
		Action:    keymap.ActionQuit,
		Bindings:  []keymap.KeyBinding{{Rune: 'q'}},
		HintLabel: "quit",
	}
	assert.Equal(t, "q", def.HintKeyLabel())
}

func TestHintKeyLabel_SpecialKey(t *testing.T) {
	t.Parallel()
	def := keymap.ActionDef{
		Action:    keymap.ActionPopupSuspend,
		Bindings:  []keymap.KeyBinding{{Key: gocui.KeyEsc}},
		HintLabel: "hide",
	}
	assert.Equal(t, "Esc", def.HintKeyLabel())
}

func TestKeyBinding_HintKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		binding keymap.KeyBinding
		want    string
	}{
		{keymap.KeyBinding{Rune: 'q'}, "q"},
		{keymap.KeyBinding{Key: gocui.KeyEnter}, "Enter"},
		{keymap.KeyBinding{Key: gocui.KeyEsc}, "Esc"},
		{keymap.KeyBinding{Key: gocui.KeyTab}, "Tab"},
		{keymap.KeyBinding{Key: gocui.KeyCtrlY}, "C-y"},
		{keymap.KeyBinding{Key: gocui.KeyCtrlBackslash}, "C-\\"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.binding.HintKey())
	}
}

func TestMatchTab_FiltersCorrectly(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionPluginUninstall,
		Bindings: []keymap.KeyBinding{{Rune: 'd'}},
		Scope:    keymap.ScopePlugins,
		Tab:      0,
	})
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionPluginInstall,
		Bindings: []keymap.KeyBinding{{Rune: 'i'}},
		Scope:    keymap.ScopePlugins,
		Tab:      1,
	})
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionPluginRefresh,
		Bindings: []keymap.KeyBinding{{Rune: 'r'}},
		Scope:    keymap.ScopePlugins,
		Tab:      keymap.TabAll,
	})

	// 'd' matches on tab 0 (Installed) but not on tab 1
	_, ok := r.MatchTab('d', 0, gocui.ModNone, keymap.ScopePlugins, 0)
	assert.True(t, ok)
	_, ok = r.MatchTab('d', 0, gocui.ModNone, keymap.ScopePlugins, 1)
	assert.False(t, ok)

	// 'i' matches on tab 1 (Marketplace) but not on tab 0
	_, ok = r.MatchTab('i', 0, gocui.ModNone, keymap.ScopePlugins, 1)
	assert.True(t, ok)
	_, ok = r.MatchTab('i', 0, gocui.ModNone, keymap.ScopePlugins, 0)
	assert.False(t, ok)

	// 'r' matches on both tabs (TabAll)
	_, ok = r.MatchTab('r', 0, gocui.ModNone, keymap.ScopePlugins, 0)
	assert.True(t, ok)
	_, ok = r.MatchTab('r', 0, gocui.ModNone, keymap.ScopePlugins, 1)
	assert.True(t, ok)
}

func TestBindingsForScopeTab(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action: keymap.ActionPluginToggleEnabled, Bindings: []keymap.KeyBinding{{Rune: 'e'}},
		Scope: keymap.ScopePlugins, Tab: 0, HintLabel: "toggle",
		Description: "Toggle plugin enabled",
	})
	r.Register(keymap.ActionDef{
		Action: keymap.ActionPluginInstall, Bindings: []keymap.KeyBinding{{Rune: 'i'}},
		Scope: keymap.ScopePlugins, Tab: 1, HintLabel: "install",
		Description: "Install plugin",
	})
	r.Register(keymap.ActionDef{
		Action: keymap.ActionPluginRefresh, Bindings: []keymap.KeyBinding{{Rune: 'r'}},
		Scope: keymap.ScopePlugins, Tab: keymap.TabAll, HintLabel: "refresh",
		Description: "Refresh plugin list",
	})
	r.Register(keymap.ActionDef{
		Action: keymap.ActionPluginCursorDown, Bindings: []keymap.KeyBinding{{Rune: 'j'}},
		Scope: keymap.ScopePlugins, Tab: 0,
		Description: "Move cursor down",
	})

	// Tab 0: toggle + refresh + cursor (includes no-hint items)
	defs0 := r.BindingsForScopeTab(keymap.ScopePlugins, 0)
	require.Len(t, defs0, 3)

	// Tab 1: install + refresh
	defs1 := r.BindingsForScopeTab(keymap.ScopePlugins, 1)
	require.Len(t, defs1, 2)

	// Tab 99: only TabAll items
	defs99 := r.BindingsForScopeTab(keymap.ScopePlugins, 99)
	require.Len(t, defs99, 1)
	assert.Equal(t, keymap.ActionPluginRefresh, defs99[0].Action)
}

func TestActionDef_Description(t *testing.T) {
	t.Parallel()
	r := keymap.Default()
	defs := r.AllActions()

	for _, d := range defs {
		assert.NotEmpty(t, d.Description, "action %s should have a Description", d.Action)
	}
}

func TestHintsForScopeTab(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action: keymap.ActionPluginToggleEnabled, Bindings: []keymap.KeyBinding{{Rune: 'e'}},
		Scope: keymap.ScopePlugins, Tab: 0, HintLabel: "toggle",
	})
	r.Register(keymap.ActionDef{
		Action: keymap.ActionPluginInstall, Bindings: []keymap.KeyBinding{{Rune: 'i'}},
		Scope: keymap.ScopePlugins, Tab: 1, HintLabel: "install",
	})
	r.Register(keymap.ActionDef{
		Action: keymap.ActionPluginRefresh, Bindings: []keymap.KeyBinding{{Rune: 'r'}},
		Scope: keymap.ScopePlugins, Tab: keymap.TabAll, HintLabel: "refresh",
	})

	// Tab 0: toggle + refresh
	hints0 := r.HintsForScopeTab(keymap.ScopePlugins, 0)
	require.Len(t, hints0, 2)
	assert.Equal(t, "toggle", hints0[0].HintLabel)
	assert.Equal(t, "refresh", hints0[1].HintLabel)

	// Tab 1: install + refresh
	hints1 := r.HintsForScopeTab(keymap.ScopePlugins, 1)
	require.Len(t, hints1, 2)
	assert.Equal(t, "install", hints1[0].HintLabel)
	assert.Equal(t, "refresh", hints1[1].HintLabel)
}

func TestRegistry_Runes(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionQuit,
		Bindings: []keymap.KeyBinding{{Rune: 'q'}},
		Scope:    keymap.ScopeGlobal,
	})
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionCursorDown,
		Bindings: []keymap.KeyBinding{{Rune: 'j'}, {Key: gocui.KeyArrowDown}},
		Scope:    keymap.ScopeSession,
	})
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionPopupScrollDown,
		Bindings: []keymap.KeyBinding{{Rune: 'j'}},
		Scope:    keymap.ScopePopup,
	})

	runes := r.Runes()
	assert.Contains(t, runes, 'q')
	assert.Contains(t, runes, 'j')
	assert.Len(t, runes, 2, "duplicate rune 'j' should be deduplicated")
}

func TestRegistry_SpecialKeys(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionCursorDown,
		Bindings: []keymap.KeyBinding{{Rune: 'j'}, {Key: gocui.KeyArrowDown}},
		Scope:    keymap.ScopeSession,
	})
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionFocusNextPanel,
		Bindings: []keymap.KeyBinding{{Key: gocui.KeyTab}},
		Scope:    keymap.ScopeGlobal,
	})

	keys := r.SpecialKeys()
	assert.Contains(t, keys, gocui.KeyArrowDown)
	assert.Contains(t, keys, gocui.KeyTab)
	assert.Len(t, keys, 2, "rune-only bindings should not appear in special keys")
}

func TestRegistry_RunesForScope(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionQuit,
		Bindings: []keymap.KeyBinding{{Rune: 'q'}},
		Scope:    keymap.ScopeGlobal,
	})
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionPopupScrollDown,
		Bindings: []keymap.KeyBinding{{Rune: 'j'}},
		Scope:    keymap.ScopePopup,
	})
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionPopupAccept,
		Bindings: []keymap.KeyBinding{{Key: gocui.KeyCtrlY}, {Rune: '1'}},
		Scope:    keymap.ScopePopup,
	})

	popupRunes := r.RunesForScope(keymap.ScopePopup)
	assert.Contains(t, popupRunes, 'j')
	assert.Contains(t, popupRunes, '1')
	assert.NotContains(t, popupRunes, 'q', "global runes should not appear in popup scope")
	assert.Len(t, popupRunes, 2)
}

func TestRegistry_SpecialKeysForScope(t *testing.T) {
	t.Parallel()
	r := keymap.NewRegistry()
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionPopupAccept,
		Bindings: []keymap.KeyBinding{{Key: gocui.KeyCtrlY}, {Rune: '1'}},
		Scope:    keymap.ScopePopup,
	})
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionPopupSuspend,
		Bindings: []keymap.KeyBinding{{Key: gocui.KeyEsc}},
		Scope:    keymap.ScopePopup,
	})
	r.Register(keymap.ActionDef{
		Action:   keymap.ActionFocusNextPanel,
		Bindings: []keymap.KeyBinding{{Key: gocui.KeyTab}},
		Scope:    keymap.ScopeGlobal,
	})

	popupKeys := r.SpecialKeysForScope(keymap.ScopePopup)
	assert.Contains(t, popupKeys, gocui.KeyCtrlY)
	assert.Contains(t, popupKeys, gocui.KeyEsc)
	assert.NotContains(t, popupKeys, gocui.KeyTab, "global keys should not appear in popup scope")
	assert.Len(t, popupKeys, 2)
}

func TestDefault_Runes_MatchAllScopes(t *testing.T) {
	t.Parallel()
	r := keymap.Default()
	runes := r.Runes()

	// Verify that runes from all non-fullscreen scopes are included.
	// FullScreen scope has no rune bindings in the default registry.
	runeSet := make(map[rune]bool)
	for _, ch := range runes {
		runeSet[ch] = true
	}

	// Spot-check representative runes from each scope
	assert.True(t, runeSet['q'], "global: quit")
	assert.True(t, runeSet['n'], "session: new")
	assert.True(t, runeSet['e'], "plugins: toggle")
	assert.True(t, runeSet['G'], "log: cursor to end")
	assert.True(t, runeSet['Y'], "popup: accept all")
}
