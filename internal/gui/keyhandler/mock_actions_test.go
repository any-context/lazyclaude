package keyhandler_test

import "github.com/any-context/lazyclaude/internal/core/choice"

// ---------------------------------------------------------------------------
// Per-domain mock structs.
// Each mock implements only the narrow interface its handler needs.
// ---------------------------------------------------------------------------

// callRecorder is a shared helper for tracking method calls.
type callRecorder struct {
	calls []string
}

func (r *callRecorder) record(name string) { r.calls = append(r.calls, name) }
func (r *callRecorder) lastCall() string {
	if len(r.calls) == 0 {
		return ""
	}
	return r.calls[len(r.calls)-1]
}

// --- SessionActions mock ---

type mockSessionActions struct {
	callRecorder
	cursorIsProject bool
}

func (m *mockSessionActions) MoveCursorDown()          { m.record("MoveCursorDown") }
func (m *mockSessionActions) MoveCursorUp()            { m.record("MoveCursorUp") }
func (m *mockSessionActions) CreateSession()           { m.record("CreateSession") }
func (m *mockSessionActions) CreateSessionAtCWD()      { m.record("CreateSessionAtCWD") }
func (m *mockSessionActions) DeleteSession()           { m.record("DeleteSession") }
func (m *mockSessionActions) AttachSession()           { m.record("AttachSession") }
func (m *mockSessionActions) LaunchLazygit()           { m.record("LaunchLazygit") }
func (m *mockSessionActions) EnterFullScreen()         { m.record("EnterFullScreen") }
func (m *mockSessionActions) StartRename()             { m.record("StartRename") }
func (m *mockSessionActions) StartWorktreeInput()      { m.record("StartWorktreeInput") }
func (m *mockSessionActions) SelectWorktree()          { m.record("SelectWorktree") }
func (m *mockSessionActions) PurgeOrphans()            { m.record("PurgeOrphans") }
func (m *mockSessionActions) StartPMSession()          { m.record("StartPMSession") }
func (m *mockSessionActions) SendKeyToPane(key string) { m.record("SendKeyToPane:" + key) }
func (m *mockSessionActions) ToggleProjectExpanded()   { m.record("ToggleProjectExpanded") }
func (m *mockSessionActions) CollapseProject()         { m.record("CollapseProject") }
func (m *mockSessionActions) ExpandProject()           { m.record("ExpandProject") }
func (m *mockSessionActions) CursorIsProject() bool    { return m.cursorIsProject }
func (m *mockSessionActions) StartSearch()             { m.record("StartSearch") }
func (m *mockSessionActions) ConnectRemote()           { m.record("ConnectRemote") }

// --- PopupActions mock ---

type mockPopupActions struct {
	callRecorder
	hasPopup bool
}

func (m *mockPopupActions) HasPopup() bool                   { return m.hasPopup }
func (m *mockPopupActions) DismissPopup(c choice.Choice)     { m.record("DismissPopup") }
func (m *mockPopupActions) DismissAllPopups(c choice.Choice) { m.record("DismissAllPopups") }
func (m *mockPopupActions) SuspendPopups()                   { m.record("SuspendPopups") }
func (m *mockPopupActions) PopupFocusNext()                  { m.record("PopupFocusNext") }
func (m *mockPopupActions) PopupFocusPrev()                  { m.record("PopupFocusPrev") }
func (m *mockPopupActions) PopupScrollDown()                 { m.record("PopupScrollDown") }
func (m *mockPopupActions) PopupScrollUp()                   { m.record("PopupScrollUp") }

// --- FullScreenActions mock (also implements ScrollActions for scroll dispatch) ---

type mockFullScreenActions struct {
	callRecorder
	fullScreen bool
	scrollMode bool
}

func (m *mockFullScreenActions) IsFullScreen() bool           { return m.fullScreen }
func (m *mockFullScreenActions) ExitFullScreen()              { m.record("ExitFullScreen") }
func (m *mockFullScreenActions) ForwardSpecialKey(key string) { m.record("ForwardSpecialKey:" + key) }
func (m *mockFullScreenActions) IsScrollMode() bool           { return m.scrollMode }
func (m *mockFullScreenActions) ScrollModeEnter()             { m.record("ScrollModeEnter") }
func (m *mockFullScreenActions) ScrollModeExit()              { m.record("ScrollModeExit") }
func (m *mockFullScreenActions) ScrollModeUp()                { m.record("ScrollModeUp") }
func (m *mockFullScreenActions) ScrollModeDown()              { m.record("ScrollModeDown") }
func (m *mockFullScreenActions) ScrollModeHalfUp()            { m.record("ScrollModeHalfUp") }
func (m *mockFullScreenActions) ScrollModeHalfDown()          { m.record("ScrollModeHalfDown") }
func (m *mockFullScreenActions) ScrollModeToTop()             { m.record("ScrollModeToTop") }
func (m *mockFullScreenActions) ScrollModeToBottom()          { m.record("ScrollModeToBottom") }
func (m *mockFullScreenActions) ScrollModeToggleSelect()      { m.record("ScrollModeToggleSelect") }
func (m *mockFullScreenActions) ScrollModeCopy()              { m.record("ScrollModeCopy") }

// --- LogsActions mock ---

type mockLogsActions struct {
	callRecorder
}

func (m *mockLogsActions) LogsCursorDown()    { m.record("LogsCursorDown") }
func (m *mockLogsActions) LogsCursorUp()      { m.record("LogsCursorUp") }
func (m *mockLogsActions) LogsCursorToEnd()   { m.record("LogsCursorToEnd") }
func (m *mockLogsActions) LogsCursorToTop()   { m.record("LogsCursorToTop") }
func (m *mockLogsActions) LogsToggleSelect()  { m.record("LogsToggleSelect") }
func (m *mockLogsActions) LogsCopySelection() { m.record("LogsCopySelection") }
func (m *mockLogsActions) StartSearch()       { m.record("StartSearch") }

// --- PluginsPanelActions mock ---

type mockPluginsPanelActions struct {
	callRecorder
	tabIndex int
}

func (m *mockPluginsPanelActions) ActivePanelTabIndex() int { return m.tabIndex }
func (m *mockPluginsPanelActions) PluginSetTab(_ int)       { m.record("PluginSetTab") }
func (m *mockPluginsPanelActions) PluginCursorDown()        { m.record("PluginCursorDown") }
func (m *mockPluginsPanelActions) PluginCursorUp()          { m.record("PluginCursorUp") }
func (m *mockPluginsPanelActions) PluginInstall()           { m.record("PluginInstall") }
func (m *mockPluginsPanelActions) PluginUninstall()         { m.record("PluginUninstall") }
func (m *mockPluginsPanelActions) PluginToggleEnabled()     { m.record("PluginToggleEnabled") }
func (m *mockPluginsPanelActions) PluginUpdate()            { m.record("PluginUpdate") }
func (m *mockPluginsPanelActions) PluginRefresh()           { m.record("PluginRefresh") }
func (m *mockPluginsPanelActions) MCPCursorDown()           { m.record("MCPCursorDown") }
func (m *mockPluginsPanelActions) MCPCursorUp()             { m.record("MCPCursorUp") }
func (m *mockPluginsPanelActions) MCPToggleDenied()         { m.record("MCPToggleDenied") }
func (m *mockPluginsPanelActions) MCPRefresh()              { m.record("MCPRefresh") }
func (m *mockPluginsPanelActions) StartSearch()             { m.record("StartSearch") }

// --- GlobalActions mock ---

type mockGlobalActions struct {
	callRecorder
	mode int
}

func (m *mockGlobalActions) Mode() int        { return m.mode }
func (m *mockGlobalActions) Quit()            { m.record("Quit") }
func (m *mockGlobalActions) UnsuspendPopups() { m.record("UnsuspendPopups") }
func (m *mockGlobalActions) PanelNextTab()    { m.record("PanelNextTab") }
func (m *mockGlobalActions) PanelPrevTab()    { m.record("PanelPrevTab") }
func (m *mockGlobalActions) ShowKeybindHelp() { m.record("ShowKeybindHelp") }
