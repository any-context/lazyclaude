package keydispatch_test

import (
	"testing"

	"github.com/any-context/lazyclaude/internal/core/choice"
	"github.com/any-context/lazyclaude/internal/gui/keydispatch"
	"github.com/any-context/lazyclaude/internal/gui/keyhandler"
	"github.com/any-context/lazyclaude/internal/gui/keymap"
	"github.com/jesseduffield/gocui"
)

// mockActions implements keyhandler.AppActions for dispatcher integration tests.
// The dispatcher routes through all handlers, so it needs the full composite.
type mockActions struct {
	calls      []string
	hasPopup   bool
	fullScreen bool
	mode       int
}

func (m *mockActions) record(name string) { m.calls = append(m.calls, name) }
func (m *mockActions) lastCall() string {
	if len(m.calls) == 0 {
		return ""
	}
	return m.calls[len(m.calls)-1]
}

// SessionActions
func (m *mockActions) MoveCursorDown()          { m.record("MoveCursorDown") }
func (m *mockActions) MoveCursorUp()            { m.record("MoveCursorUp") }
func (m *mockActions) CreateSession()           { m.record("CreateSession") }
func (m *mockActions) CreateSessionAtCWD()      { m.record("CreateSessionAtCWD") }
func (m *mockActions) DeleteSession()           { m.record("DeleteSession") }
func (m *mockActions) AttachSession()           { m.record("AttachSession") }
func (m *mockActions) LaunchLazygit()           { m.record("LaunchLazygit") }
func (m *mockActions) EnterFullScreen()         { m.record("EnterFullScreen") }
func (m *mockActions) StartRename()             { m.record("StartRename") }
func (m *mockActions) StartWorktreeInput()      { m.record("StartWorktreeInput") }
func (m *mockActions) SelectWorktree()          { m.record("SelectWorktree") }
func (m *mockActions) PurgeOrphans()            { m.record("PurgeOrphans") }
func (m *mockActions) StartPMSession()          { m.record("StartPMSession") }
func (m *mockActions) SendKeyToPane(key string) { m.record("SendKeyToPane:" + key) }
func (m *mockActions) ToggleProjectExpanded()   { m.record("ToggleProjectExpanded") }
func (m *mockActions) CollapseProject()         { m.record("CollapseProject") }
func (m *mockActions) ExpandProject()           { m.record("ExpandProject") }
func (m *mockActions) CursorIsProject() bool    { return false }
func (m *mockActions) ConnectRemote()           { m.record("ConnectRemote") }

// PopupActions
func (m *mockActions) HasPopup() bool                   { return m.hasPopup }
func (m *mockActions) DismissPopup(c choice.Choice)     { m.record("DismissPopup") }
func (m *mockActions) DismissAllPopups(c choice.Choice) { m.record("DismissAllPopups") }
func (m *mockActions) SuspendPopups()                   { m.record("SuspendPopups") }
func (m *mockActions) PopupFocusNext()                  { m.record("PopupFocusNext") }
func (m *mockActions) PopupFocusPrev()                  { m.record("PopupFocusPrev") }
func (m *mockActions) PopupScrollDown()                 { m.record("PopupScrollDown") }
func (m *mockActions) PopupScrollUp()                   { m.record("PopupScrollUp") }

// FullScreenActions
func (m *mockActions) IsFullScreen() bool           { return m.fullScreen }
func (m *mockActions) ExitFullScreen()              { m.record("ExitFullScreen") }
func (m *mockActions) ForwardSpecialKey(key string) { m.record("ForwardSpecialKey:" + key) }

// ScrollActions
func (m *mockActions) IsScrollMode() bool          { return false }
func (m *mockActions) ScrollModeEnter()            { m.record("ScrollModeEnter") }
func (m *mockActions) ScrollModeExit()             { m.record("ScrollModeExit") }
func (m *mockActions) ScrollModeUp()               { m.record("ScrollModeUp") }
func (m *mockActions) ScrollModeDown()             { m.record("ScrollModeDown") }
func (m *mockActions) ScrollModeHalfUp()           { m.record("ScrollModeHalfUp") }
func (m *mockActions) ScrollModeHalfDown()         { m.record("ScrollModeHalfDown") }
func (m *mockActions) ScrollModeToTop()            { m.record("ScrollModeToTop") }
func (m *mockActions) ScrollModeToBottom()         { m.record("ScrollModeToBottom") }
func (m *mockActions) ScrollModeToggleSelect()     { m.record("ScrollModeToggleSelect") }
func (m *mockActions) ScrollModeCopy()             { m.record("ScrollModeCopy") }

// LogsActions
func (m *mockActions) LogsCursorDown()    { m.record("LogsCursorDown") }
func (m *mockActions) LogsCursorUp()      { m.record("LogsCursorUp") }
func (m *mockActions) LogsCursorToEnd()   { m.record("LogsCursorToEnd") }
func (m *mockActions) LogsCursorToTop()   { m.record("LogsCursorToTop") }
func (m *mockActions) LogsToggleSelect()  { m.record("LogsToggleSelect") }
func (m *mockActions) LogsCopySelection() { m.record("LogsCopySelection") }

// PluginsPanelActions
func (m *mockActions) ActivePanelTabIndex() int { return 0 }
func (m *mockActions) PluginSetTab(_ int)       { m.record("PluginSetTab") }
func (m *mockActions) PluginCursorDown()        { m.record("PluginCursorDown") }
func (m *mockActions) PluginCursorUp()          { m.record("PluginCursorUp") }
func (m *mockActions) PluginInstall()           { m.record("PluginInstall") }
func (m *mockActions) PluginUninstall()         { m.record("PluginUninstall") }
func (m *mockActions) PluginToggleEnabled()     { m.record("PluginToggleEnabled") }
func (m *mockActions) PluginUpdate()            { m.record("PluginUpdate") }
func (m *mockActions) PluginRefresh()           { m.record("PluginRefresh") }
func (m *mockActions) MCPCursorDown()           { m.record("MCPCursorDown") }
func (m *mockActions) MCPCursorUp()             { m.record("MCPCursorUp") }
func (m *mockActions) MCPToggleDenied()         { m.record("MCPToggleDenied") }
func (m *mockActions) MCPRefresh()              { m.record("MCPRefresh") }

// GlobalActions
func (m *mockActions) Mode() int        { return m.mode }
func (m *mockActions) Quit()            { m.record("Quit") }
func (m *mockActions) UnsuspendPopups() { m.record("UnsuspendPopups") }
func (m *mockActions) PanelNextTab()    { m.record("PanelNextTab") }
func (m *mockActions) PanelPrevTab()    { m.record("PanelPrevTab") }
func (m *mockActions) ShowKeybindHelp() { m.record("ShowKeybindHelp") }

// Search (shared across panels)
func (m *mockActions) StartSearch() { m.record("StartSearch") }

func newDispatcher() *keydispatch.Dispatcher {
	reg := keymap.Default()
	pm := keyhandler.NewPanelManager(keyhandler.NewSessionsPanel(reg), keyhandler.NewLogsPanel(reg))
	return keydispatch.New(pm, reg)
}

func TestDispatcher_PopupPriority(t *testing.T) {
	d := newDispatcher()
	a := &mockActions{hasPopup: true}

	r := d.Dispatch(keyhandler.KeyEvent{Rune: 'j'}, a)
	if r != keyhandler.Handled || a.lastCall() != "PopupScrollDown" {
		t.Errorf("popup j: result=%v, call=%q", r, a.lastCall())
	}
}

func TestDispatcher_SessionsPanel(t *testing.T) {
	d := newDispatcher()
	a := &mockActions{}

	r := d.Dispatch(keyhandler.KeyEvent{Rune: 'j'}, a)
	if r != keyhandler.Handled || a.lastCall() != "MoveCursorDown" {
		t.Errorf("sessions j: result=%v, call=%q", r, a.lastCall())
	}
}

func TestDispatcher_LogsPanel(t *testing.T) {
	d := newDispatcher()
	a := &mockActions{}

	d.PanelManager().FocusNext() // switch to logs
	r := d.Dispatch(keyhandler.KeyEvent{Rune: 'j'}, a)
	if r != keyhandler.Handled || a.lastCall() != "LogsCursorDown" {
		t.Errorf("logs j: result=%v, call=%q", r, a.lastCall())
	}
}

func TestDispatcher_FullScreenExitKey(t *testing.T) {
	d := newDispatcher()
	a := &mockActions{fullScreen: true}

	r := d.Dispatch(keyhandler.KeyEvent{Key: gocui.KeyCtrlD}, a)
	if r != keyhandler.Handled || a.lastCall() != "ExitFullScreen" {
		t.Errorf("fullscreen Ctrl+D: result=%v, call=%q", r, a.lastCall())
	}
}

func TestDispatcher_GlobalQuit(t *testing.T) {
	d := newDispatcher()
	a := &mockActions{}

	r := d.Dispatch(keyhandler.KeyEvent{Rune: 'q'}, a)
	if r != keyhandler.Handled || a.lastCall() != "Quit" {
		t.Errorf("global q: result=%v, call=%q", r, a.lastCall())
	}
}

func TestDispatcher_ActiveOptionsBar(t *testing.T) {
	d := newDispatcher()
	a := &mockActions{}

	bar1 := d.ActiveOptionsBar(a)
	if bar1 == "" {
		t.Fatal("sessions options bar empty")
	}

	d.PanelManager().FocusNext()
	bar2 := d.ActiveOptionsBar(a)
	if bar2 == "" {
		t.Fatal("logs options bar empty")
	}
	if bar1 == bar2 {
		t.Error("panels should have different options bars")
	}

	// Popup -> empty
	a2 := &mockActions{hasPopup: true}
	if d.ActiveOptionsBar(a2) != "" {
		t.Error("popup should return empty options bar")
	}
}
