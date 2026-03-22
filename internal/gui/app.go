package gui

import (
	"fmt"
	"os"
	"path/filepath"
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
	CreateWorktree(name, prompt, projectRoot string) error
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

	// TUI lock file: signals to MCP server that TUI is open.
	// Server skips display-popup when this file exists.
	tuiLock := filepath.Join(os.TempDir(), "lazyclaude-tui.lock")
	os.WriteFile(tuiLock, []byte(fmt.Sprintf("%d", os.Getpid())), 0o644)
	defer os.Remove(tuiLock)

	// Serial key forwarder: preserves keystroke order (critical for IME input).
	done := make(chan struct{})
	go a.fullscreen.RunKeyForwarder(done)

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
					if a.sessions != nil {
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
	target string
	key    string
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
