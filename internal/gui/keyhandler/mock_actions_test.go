package keyhandler_test

import "github.com/KEMSHlM/lazyclaude/internal/core/choice"

// mockActions records which actions were called.
type mockActions struct {
	calls          []string
	hasPopup       bool
	fullScreen     bool
	mode           int
	cursorIsProject bool
}

func newMockActions() *mockActions {
	return &mockActions{cursorIsProject: false} // default: SessionNode
}

func (m *mockActions) record(name string) { m.calls = append(m.calls, name) }
func (m *mockActions) lastCall() string {
	if len(m.calls) == 0 {
		return ""
	}
	return m.calls[len(m.calls)-1]
}

func (m *mockActions) HasPopup() bool    { return m.hasPopup }
func (m *mockActions) IsFullScreen() bool { return m.fullScreen }
func (m *mockActions) Mode() int         { return m.mode }

func (m *mockActions) MoveCursorDown()                  { m.record("MoveCursorDown") }
func (m *mockActions) MoveCursorUp()                    { m.record("MoveCursorUp") }
func (m *mockActions) CreateSession()                   { m.record("CreateSession") }
func (m *mockActions) DeleteSession()                   { m.record("DeleteSession") }
func (m *mockActions) AttachSession()                   { m.record("AttachSession") }
func (m *mockActions) LaunchLazygit()                   { m.record("LaunchLazygit") }
func (m *mockActions) EnterFullScreen()                 { m.record("EnterFullScreen") }
func (m *mockActions) StartRename()                     { m.record("StartRename") }
func (m *mockActions) StartWorktreeInput()              { m.record("StartWorktreeInput") }
func (m *mockActions) SelectWorktree()                  { m.record("SelectWorktree") }
func (m *mockActions) PurgeOrphans()                    { m.record("PurgeOrphans") }
func (m *mockActions) StartPMSession()                  { m.record("StartPMSession") }
func (m *mockActions) DismissPopup(c choice.Choice)     { m.record("DismissPopup") }
func (m *mockActions) DismissAllPopups(c choice.Choice) { m.record("DismissAllPopups") }
func (m *mockActions) SuspendPopups()                   { m.record("SuspendPopups") }
func (m *mockActions) UnsuspendPopups()                 { m.record("UnsuspendPopups") }
func (m *mockActions) PopupFocusNext()                  { m.record("PopupFocusNext") }
func (m *mockActions) PopupFocusPrev()                  { m.record("PopupFocusPrev") }
func (m *mockActions) PopupScrollDown()                 { m.record("PopupScrollDown") }
func (m *mockActions) PopupScrollUp()                   { m.record("PopupScrollUp") }
func (m *mockActions) ExitFullScreen()                  { m.record("ExitFullScreen") }
func (m *mockActions) ForwardSpecialKey(key string)     { m.record("ForwardSpecialKey:" + key) }
func (m *mockActions) SendKeyToPane(key string)         { m.record("SendKeyToPane:" + key) }
func (m *mockActions) LogsCursorDown()                  { m.record("LogsCursorDown") }
func (m *mockActions) LogsCursorUp()                    { m.record("LogsCursorUp") }
func (m *mockActions) LogsCursorToEnd()                 { m.record("LogsCursorToEnd") }
func (m *mockActions) LogsCursorToTop()                 { m.record("LogsCursorToTop") }
func (m *mockActions) LogsToggleSelect()                { m.record("LogsToggleSelect") }
func (m *mockActions) LogsCopySelection()               { m.record("LogsCopySelection") }
func (m *mockActions) Quit()                            { m.record("Quit") }
func (m *mockActions) ToggleProjectExpanded()            { m.record("ToggleProjectExpanded") }
func (m *mockActions) CursorIsProject() bool             { return m.cursorIsProject }
func (m *mockActions) PanelNextTab()                     { m.record("PanelNextTab") }
func (m *mockActions) PanelPrevTab()                     { m.record("PanelPrevTab") }
func (m *mockActions) ActivePanelTabIndex() int          { return 0 }
func (m *mockActions) PluginCursorDown()                 { m.record("PluginCursorDown") }
func (m *mockActions) PluginCursorUp()                   { m.record("PluginCursorUp") }
func (m *mockActions) PluginInstall()                    { m.record("PluginInstall") }
func (m *mockActions) PluginUninstall()                  { m.record("PluginUninstall") }
func (m *mockActions) PluginToggleEnabled()              { m.record("PluginToggleEnabled") }
func (m *mockActions) PluginUpdate()                     { m.record("PluginUpdate") }
func (m *mockActions) PluginRefresh()                    { m.record("PluginRefresh") }
