package gui

import (
	"fmt"
	"strings"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/core/event"
	"github.com/KEMSHlM/lazyclaude/internal/core/model"
	"github.com/KEMSHlM/lazyclaude/internal/gui/keydispatch"
	"github.com/KEMSHlM/lazyclaude/internal/gui/keyhandler"
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
	ModeMain AppMode = iota // lazyclaude      -> session list + preview
	ModeDiff                // lazyclaude diff  -> diff popup viewer
	ModeTool                // lazyclaude tool  -> tool popup viewer
)

// SessionProvider abstracts session operations for the GUI layer.
type SessionProvider interface {
	Sessions() []SessionItem
	Create(path, host string) error
	Delete(id string) error
	Rename(id, newName string) error
	PurgeOrphans() (int, error)
	CapturePreview(id string, width, height int) (PreviewResult, error)
	PendingNotifications() []*model.ToolNotification
	SendChoice(window string, choice Choice) error
	AttachSession(id string) error
	LaunchLazygit(path, host string) error
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
	Activity   string // "pending" or "" (empty = normal)
	Role       string // "pm", "worker", or "" (empty = regular session)
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
	keyRegistry        *KeyRegistry                   // single source of truth for key bindings
	dispatcher         *keydispatch.Dispatcher       // key dispatch chain
	panelManager       *keyhandler.PanelManager      // panel focus management
	logs               *LogsState                    // logs panel cursor/selection state
	notify             *NotifyLoop                   // notification delivery (output, broker, tick)
	quitRequested      bool                         // set by Quit(), checked after Dispatch
	popupMode          config.PopupMode             // how popups are displayed (auto/tmux/overlay)
	renameSessionID    string                     // session ID being renamed (empty = no rename in progress)
	activeDialog       DialogKind                 // current input dialog (DialogNone = no dialog)
	worktreeActiveField string                    // which worktree dialog field has focus ("worktree-branch" or "worktree-prompt")
	worktreeChoices    []WorktreeInfo             // items in worktree chooser
	worktreeCursor     int                        // selected index in chooser (len(choices) = "New")
	selectedWorktree   string                     // path of chosen existing worktree
	editor             *inputEditor               // fullscreen key editor (for paste flush)
	watchdogDone       chan struct{}               // signals watchdog to stop
	watchdogStarted    bool                        // prevents multiple watchdog goroutines
}

// startPasteWatchdog starts the watchdog goroutine if not already running.
// Called from layout when the editor is first created.
func (a *App) startPasteWatchdog() {
	if a.watchdogStarted || a.editor == nil || a.watchdogDone == nil {
		return
	}
	a.watchdogStarted = true
	ch := a.editor.pasteNotify
	done := a.watchdogDone
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ch:
				// Drain loop: keep flushing partial content while paste is ongoing.
				// Large pastes overflow tcell's event channel (256 slots), blocking
				// the event loop. Each drain unblocks the channel so more characters
				// can arrive. The loop exits when inPaste becomes false (paste end
				// marker was processed by the event loop).
			drain:
				for {
					select {
					case <-done:
						return
					case <-time.After(pasteWatchdogTimeout):
					}
					if a.editor == nil {
						break drain
					}
					a.editor.pasteMu.Lock()
					stillPasting := a.editor.inPaste
					hasData := a.editor.pasteBuf.Len() > 0
					a.editor.pasteMu.Unlock()
					if !stillPasting {
						break drain
					}
					if hasData {
						a.editor.drainPaste()
					}
				}
			}
		}
	}()
}

// SetPopupMode sets the popup display mode.
func (a *App) SetPopupMode(mode config.PopupMode) {
	a.popupMode = mode
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

	app := &App{
		gui:          g,
		mode:         mode,
		popups:       NewPopupController(),
		preview:      &PreviewCache{},
		keyRegistry:  DefaultKeyRegistry(),
		logs:         NewLogsState(),
		notify:       NewNotifyLoop(),
	}
	app.fullscreen = NewFullScreenState(app.preview)
	app.initDispatcher()

	g.SetManagerFunc(app.layout)
	g.Mouse = true

	if err := app.setupGlobalKeybindings(); err != nil {
		g.Close()
		return nil, err
	}

	return app, nil
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

	app := &App{
		gui:          g,
		mode:         mode,
		popups:       NewPopupController(),
		preview:      &PreviewCache{},
		keyRegistry:  DefaultKeyRegistry(),
		logs:         NewLogsState(),
		notify:       NewNotifyLoop(),
	}
	app.fullscreen = NewFullScreenState(app.preview)
	app.initDispatcher()

	g.SetManagerFunc(app.layout)

	if err := app.setupGlobalKeybindings(); err != nil {
		g.Close()
		return nil, err
	}

	return app, nil
}

// initDispatcher creates the panel manager and key dispatcher.
func (a *App) initDispatcher() {
	pm := keyhandler.NewPanelManager(
		&keyhandler.SessionsPanel{},
		&keyhandler.LogsPanel{},
	)
	a.panelManager = pm
	a.dispatcher = keydispatch.New(pm)
}

// Run starts the main event loop. Blocks until quit.
func (a *App) Run() error {
	defer a.gui.Close()

	// Serial key forwarder: preserves keystroke order (critical for IME input).
	done := make(chan struct{})
	go a.fullscreen.RunKeyForwarder(done)

	// Paste watchdog: started lazily when the editor is first created.
	// See runPasteWatchdog().
	a.watchdogDone = done

	// Refresh loop: event-driven via notify channels + ticker fallback.
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		brokerCh := a.notify.BrokerCh()

		for {
			select {
			case <-done:
				a.notify.Cancel()
				return
			case <-a.notify.OutputCh():
				a.preview.Lock()
				if !a.preview.Busy() {
					a.preview.InvalidateTimestamp()
				}
				a.preview.Unlock()
				a.gui.Update(func(g *gocui.Gui) error { return nil })
			case ev, ok := <-brokerCh:
				if !ok {
					brokerCh = nil
					continue
				}
				if ev.Notification != nil {
					a.gui.Update(func(g *gocui.Gui) error {
						a.showToolPopup(ev.Notification)
						return nil
					})
				}
			case <-ticker.C:
				a.notify.OnTick()
				a.gui.Update(func(g *gocui.Gui) error {
					// When broker is wired (in-process server), notifications
					// arrive via brokerCh — skip file polling to avoid duplicates.
					if a.sessions != nil && !a.notify.HasBroker() {
						for _, n := range a.sessions.PendingNotifications() {
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

// Gui returns the underlying gocui.Gui (for testing).
func (a *App) Gui() *gocui.Gui {
	return a.gui
}

func (a *App) setStatus(g *gocui.Gui, msg string) {
	v, err := g.View("logs")
	if err != nil {
		return
	}
	v.Clear()
	fmt.Fprintln(v, "  "+msg)
}
