package gui

import (
	"fmt"
	"strings"
	"time"

	"github.com/any-context/lazyclaude/internal/core/event"
	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/gui/keydispatch"
	"github.com/any-context/lazyclaude/internal/gui/keyhandler"
	"github.com/any-context/lazyclaude/internal/gui/keymap"
	"github.com/jesseduffield/gocui"
)

// isUnknownView checks for gocui's ErrUnknownView.
// jesseduffield/gocui uses go-errors Wrap, so == and errors.Is don't work.
func isUnknownView(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unknown view")
}

// AppMode determines which set of views to display.
type AppMode int

const (
	ModeMain AppMode = iota // lazyclaude -> session list + preview
)

// ConnectionStatus represents the remote connection state for TUI display.
type ConnectionStatus struct {
	Host            string // remote hostname (e.g. "AERO")
	State           string // "connected", "reconnecting", "error", "disconnected"
	VersionMismatch bool   // true if remote binary version differs from local
}

// ConnectionStatusProvider returns the current remote connection status.
// Returns nil if no remote connection is configured.
type ConnectionStatusProvider func() []ConnectionStatus

// SessionProvider abstracts session operations for the GUI layer.
// NotificationCacher caches pending notifications for badge rendering.
// Implemented by sessionAdapter to avoid redundant (and destructive)
// ReadAll calls during layout. Separate from SessionProvider because
// notification cache management is not a session data concern.
type NotificationCacher interface {
	RefreshPendingFrom([]*model.ToolNotification)
}

type SessionProvider interface {
	Sessions() []SessionItem
	Projects() []ProjectItem
	ToggleProjectExpanded(projectID string)
	Create(path string) error
	Delete(id string) error
	Rename(id, newName string) error
	PurgeOrphans() (int, error)
	CapturePreview(id string, width, height int) (PreviewResult, error)
	CaptureScrollback(id string, width, startLine, endLine int) (PreviewResult, error)
	HistorySize(id string) (int, error)
	PendingNotifications() []*model.ToolNotification
	SendChoice(window string, choice Choice) error
	AttachSession(id string) error
	LaunchLazygit(path string) error
	CreateWorktree(name, prompt, projectRoot string) error
	ResumeWorktree(worktreePath, prompt, projectRoot string) error
	ListWorktrees(projectRoot string) ([]WorktreeInfo, error)
	CreatePMSession(projectRoot string) error
	CreateWorkerSession(name, prompt, projectRoot string) error
}

// WorktreeInfo describes an existing worktree for the chooser.
type WorktreeInfo struct {
	Name   string
	Path   string
	Branch string
}

// SessionItem is a read-only view of a session for display.
type SessionItem struct {
	ID         string
	Name       string
	Path       string
	Host       string
	Status     string
	Flags      []string
	TmuxWindow string
	Activity model.ActivityState // 4-stage activity state (Running, NeedsInput, Idle, Error)
	ToolName string              // last tool name; only meaningful when Activity == Running or NeedsInput
	Role       string              // "pm", "worker", or "" (empty = regular session)
}

// PreviewResult holds captured pane content and cursor position.
type PreviewResult struct {
	Content string
	CursorX int
	CursorY int
}

// App is the root TUI application (lazygit Gui equivalent).
type App struct {
	gui              *gocui.Gui
	mode             AppMode
	sessions         SessionProvider
	cursor           int // selected session index
	preview          *PreviewCache
	lastWidth        int
	lastHeight       int
	popups           PopupManager                  // popup stack management
	fullscreen       *FullScreenState              // fullscreen mode state + key forwarding
	scroll           *ScrollState                 // scrollback browsing state
	keyRegistry        *keymap.Registry                // single source of truth for key bindings
	dispatcher         *keydispatch.Dispatcher       // key dispatch chain
	panelManager       *keyhandler.PanelManager      // panel focus management
	logs               *LogsState                    // logs panel cursor/selection state
	notify             *NotifyLoop                   // notification delivery (output, broker, tick)
	quitRequested      bool                         // set by Quit(), checked after Dispatch
	dialog             DialogState                  // input dialog state (rename, worktree, etc.)
	editor             *inputEditor               // fullscreen key editor
	cachedNodes        []TreeNode                   // rebuilt once per layout cycle
	panelTabs          map[string]int               // panel name -> active tab index
	pluginState        *PluginState                 // plugin panel UI state
	plugins            PluginProvider               // plugin operations (nil until wired)
	mcpState           *MCPState                    // MCP tab UI state
	mcpServers         MCPProvider                  // MCP operations (nil until wired)
	logCache           logFileCache                 // cached server log file content
	logRender          logRenderCache               // tracks last rendered log state
	previewByScope     map[keymap.Scope]func(*gocui.View, int, int) // scope -> preview renderer
	// windowActivity tracks the 5-stage activity state per tmux window.
	// All reads/writes happen on the gocui event loop goroutine (gui.Update callbacks
	// and layout), so no mutex is needed.
	windowActivity   map[string]WindowActivityEntry
	connectionStatus ConnectionStatusProvider // remote connection status for options bar
	connectFn        func(host string) error  // connects to a remote host (injected from root.go)
}


// newApp initializes a new App with the given gocui.Gui. Shared by NewApp and NewAppHeadless.
func newApp(mode AppMode, g *gocui.Gui, enableMouse bool) (*App, error) {
	app := &App{
		gui:         g,
		mode:        mode,
		popups:      NewPopupController(),
		preview:     &PreviewCache{},
		keyRegistry: keymap.Default(),
		logs:        NewLogsState(),
		notify:      NewNotifyLoop(),
		pluginState: NewPluginState(),
		mcpState:    NewMCPState(),
		panelTabs:      make(map[string]int),
		windowActivity: make(map[string]WindowActivityEntry),
	}
	app.scroll = NewScrollState()
	app.fullscreen = NewFullScreenState(app.preview)
	app.initDispatcher()

	g.Highlight = true
	g.SelFrameColor = gocui.ColorCyan

	g.SetManagerFunc(app.layout)
	if enableMouse {
		g.Mouse = true
	}

	// Register paste callback: when gocui detects a complete bracketed paste
	// (either via native EventPaste or raw ESC[200~ fallback), the full text
	// arrives here as a single string, bypassing the per-character event loop.
	g.OnPasteContent = func(text string) error {
		app.handlePasteContent(text)
		return nil
	}

	if err := app.setupGlobalKeybindings(); err != nil {
		g.Close()
		return nil, err
	}

	return app, nil
}

// NewApp creates a new App. Call Run() to start the event loop.
func NewApp(mode AppMode) (*App, error) {
	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode:      gocui.OutputTrue,
		SupportOverlaps: true,
	})
	if err != nil {
		return nil, fmt.Errorf("init gocui: %w", err)
	}
	return newApp(mode, g, true)
}

// NewAppHeadless creates an App in headless mode for testing.
func NewAppHeadless(mode AppMode, width, height int) (*App, error) {
	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode: gocui.OutputTrue,
		Headless:   true,
		Width:      width,
		Height:     height,
	})
	if err != nil {
		return nil, fmt.Errorf("init gocui headless: %w", err)
	}
	return newApp(mode, g, false)
}

// initDispatcher creates the panel manager and key dispatcher.
func (a *App) initDispatcher() {
	reg := a.keyRegistry
	pm := keyhandler.NewPanelManager(
		keyhandler.NewSessionsPanel(reg),
		keyhandler.NewPluginsPanel(reg),
		keyhandler.NewLogsPanel(reg),
	)
	a.panelManager = pm
	a.dispatcher = keydispatch.New(pm, reg)

	// Register scope-specific preview renderers.
	// Scopes not in this map use the default session preview.
	a.previewByScope = map[keymap.Scope]func(*gocui.View, int, int){
		keymap.ScopePlugins: func(v *gocui.View, _, _ int) { a.renderPluginPreview(v) },
	}
}

// Run starts the main event loop. Blocks until quit.
func (a *App) Run() error {
	defer a.gui.Close()

	// Plugin data is loaded lazily by syncPluginProjectOnce() in layout()
	// when session data becomes available and a project context is determined.

	// Serial key forwarder: preserves keystroke order (critical for IME input).
	done := make(chan struct{})
	go a.fullscreen.RunKeyForwarder(done)

	// Refresh loop: event-driven via notify channels + ticker fallback.
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		brokerCh := a.notify.BrokerCh()

		// Debounce output events: coalesce rapid pane output into a single
		// preview invalidation per ticker cycle. Without this, every tmux
		// %output line triggers a CapturePreview goroutine, which spawns
		// tmux subprocesses and drives CPU to 100%+ over time.
		outputPending := false

		for {
			select {
			case <-done:
				a.notify.Cancel()
				return
			case <-a.notify.OutputCh():
				// Mark that output arrived; the next ticker will
				// invalidate the preview and trigger a single refresh.
				outputPending = true
			case ev, ok := <-brokerCh:
				if !ok {
					brokerCh = nil
					continue
				}
				if ev.Notification != nil {
					n := ev.Notification
					a.gui.Update(func(g *gocui.Gui) error {
						a.setWindowActivity(n.Window, WindowActivityEntry{
							State:    model.ActivityNeedsInput,
							ToolName: n.ToolName,
						})
						a.showToolPopup(n)
						return nil
					})
				}
				if ev.StopNotification != nil {
					n := ev.StopNotification
					a.gui.Update(func(g *gocui.Gui) error {
						a.setWindowActivity(n.Window, WindowActivityEntry{
							State: stopReasonToActivity(n.StopReason),
						})
						return nil
					})
				}
				if ev.SessionStartNotification != nil {
					n := ev.SessionStartNotification
					a.gui.Update(func(g *gocui.Gui) error {
						a.setWindowActivity(n.Window, WindowActivityEntry{
							State: model.ActivityRunning,
						})
						return nil
					})
				}
				if ev.PromptSubmitNotification != nil {
					n := ev.PromptSubmitNotification
					a.gui.Update(func(g *gocui.Gui) error {
						a.setWindowActivity(n.Window, WindowActivityEntry{
							State: model.ActivityRunning,
						})
						return nil
					})
				}
				if ev.ActivityNotification != nil {
					n := ev.ActivityNotification
					a.gui.Update(func(g *gocui.Gui) error {
						a.setWindowActivity(n.Window, WindowActivityEntry{
							State:    n.State,
							ToolName: n.ToolName,
						})
						return nil
					})
				}
			case <-ticker.C:
				if outputPending {
					a.preview.Lock()
					if !a.preview.Busy() {
						a.preview.InvalidateTimestamp()
					}
					a.preview.Unlock()
					outputPending = false
				}
				a.notify.OnTick()
				a.gui.Update(func(g *gocui.Gui) error {
					// When broker is wired (in-process server), notifications
					// arrive via brokerCh — skip file polling to avoid duplicates.
					if a.sessions != nil && !a.notify.HasBroker() {
						pending := a.sessions.PendingNotifications()
						// Cache the pending set for badge rendering in layout.
						// Must happen before showToolPopup because ReadAll
						// deletes the notification files.
						if nc, ok := a.sessions.(NotificationCacher); ok {
							nc.RefreshPendingFrom(pending)
						}
						for _, n := range pending {
							a.showToolPopup(n)
						}
					}
					return nil
				})
			}
		}
	}()

	err := a.gui.MainLoop()
	close(done)
	if err != nil {
		if strings.Contains(err.Error(), "quit") {
			return nil
		}
		return err
	}
	return nil
}

// AppMode returns the current app mode (typed).
func (a *App) AppMode() AppMode {
	return a.mode
}

// Mode returns the current app mode as int (satisfies keyhandler.AppActions).
func (a *App) Mode() int {
	return int(a.mode)
}

// SetSessions sets the session provider for the main screen.
func (a *App) SetSessions(sp SessionProvider) {
	a.sessions = sp
}

// SetPlugins sets the plugin provider for the plugins panel.
func (a *App) SetPlugins(pp PluginProvider) {
	a.plugins = pp
}

// SetMCP sets the MCP provider for the MCP tab.
func (a *App) SetMCP(mp MCPProvider) {
	a.mcpServers = mp
}

// SetInputForwarder sets the input forwarder for full-screen mode.
func (a *App) SetInputForwarder(fwd InputForwarder) {
	a.fullscreen.SetForwarder(fwd)
}

// SetOnTick sets a callback invoked every ticker cycle (for control mode health checks).
func (a *App) SetOnTick(fn func()) {
	a.notify.SetOnTick(fn)
}

// SetNotifyBroker attaches an event broker to the App so that server-side
// notifications are delivered immediately without waiting for the 100ms ticker.
// Passing nil is a no-op: the app falls back to file-based polling only.
// Must be called before Run().
func (a *App) SetNotifyBroker(broker *event.Broker[model.Event]) {
	a.notify.SetBroker(broker)
}

// SetConnectionStatus sets the provider for remote connection status display.
// Must be called before Run().
func (a *App) SetConnectionStatus(fn ConnectionStatusProvider) {
	a.connectionStatus = fn
}

// SetConnectFn sets the handler called when the user requests a remote
// connection via the connect dialog. The function should establish the
// connection and return an error if it fails. Must be called before Run().
func (a *App) SetConnectFn(fn func(host string) error) {
	a.connectFn = fn
}

// WindowActivityEntry stores activity state and context for a tmux window.
type WindowActivityEntry struct {
	State    model.ActivityState
	ToolName string // last tool name (for running context)
}

// keyCmd is a queued key forwarding command.
type keyCmd struct {
	target  string
	key     string
	literal bool // true = send via send-keys -l (literal text, not key names)
}


// NotifyOutput signals that a pane has new output.
// Called from the control mode callback. Non-blocking.
func (a *App) NotifyOutput() {
	a.notify.NotifyOutput()
}

// WindowActivityMap returns a shallow copy of the window activity map.
// Used by the session adapter to merge lifecycle state into SessionItem.Activity.
// Must be called from the gocui event loop goroutine only (layout callbacks).
func (a *App) WindowActivityMap() map[string]WindowActivityEntry {
	if len(a.windowActivity) == 0 {
		return nil
	}
	cp := make(map[string]WindowActivityEntry, len(a.windowActivity))
	for k, v := range a.windowActivity {
		cp[k] = v
	}
	return cp
}

// setWindowActivity records a lifecycle activity for a tmux window.
// Called from the broker event handler on the gocui goroutine.
func (a *App) setWindowActivity(window string, entry WindowActivityEntry) {
	if window == "" {
		return
	}
	a.windowActivity[window] = entry
}

// clearUnreadActivity clears the activity state for a window only if it is
// in an "unread" state (Idle or Error). Running and NeedsInput are not cleared
// because those represent active work that should remain visible.
func (a *App) clearUnreadActivity(window string) {
	if window == "" {
		return
	}
	entry, ok := a.windowActivity[window]
	if !ok {
		return
	}
	if entry.State == model.ActivityIdle || entry.State == model.ActivityError {
		delete(a.windowActivity, window)
	}
}

// stopReasonToActivity converts a Claude Code stop_reason to an ActivityState.
func stopReasonToActivity(reason string) model.ActivityState {
	switch reason {
	case "error", "interrupt":
		return model.ActivityError
	default:
		return model.ActivityIdle
	}
}

// Gui returns the underlying gocui.Gui (for testing).
func (a *App) Gui() *gocui.Gui {
	return a.gui
}

// ShowError displays an error in the logs and main panels.
// Public wrapper for callers outside the gui package (e.g. root.go via gui.Update).
func (a *App) ShowError(g *gocui.Gui, msg string) {
	a.showError(g, msg)
}

func (a *App) setStatus(g *gocui.Gui, msg string) {
	v, err := g.View("logs")
	if err != nil {
		return
	}
	v.Clear()
	fmt.Fprintln(v, "  "+msg)
	// Invalidate the log render cache so the next layout cycle
	// re-renders the log content over this status message.
	a.logRender.modTime = -1
}

// showError displays an error in both the logs panel and the main panel
// so it is immediately visible regardless of which panel the user is viewing.
func (a *App) showError(g *gocui.Gui, msg string) {
	a.setStatus(g, msg)
	if v, err := g.View("main"); err == nil {
		v.Clear()
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, "  "+msg)
	}
}
