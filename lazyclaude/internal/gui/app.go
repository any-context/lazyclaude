package gui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/gui/context"
	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/KEMSHlM/lazyclaude/internal/notify"
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
	CapturePreview(id string, width, height int) (string, error)
	PendingNotification() *notify.ToolNotification
	SendChoice(window string, choice Choice) error
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

// App is the root TUI application (lazygit Gui equivalent).
type App struct {
	gui              *gocui.Gui
	mode             AppMode
	contextMgr       *context.Manager
	activeTabIdx     int
	sessions         SessionProvider
	cursor           int // selected session index
	previewMu        sync.Mutex
	previewCache     string    // cached preview content
	previewCursor    int       // cursor position when cache was taken
	previewBusy      bool      // async capture in progress
	previewTime      time.Time // last fetch timestamp
	lastWidth        int
	lastHeight       int
	pendingTool      *notify.ToolNotification   // active tool popup
	popupScrollY     int                         // scroll position for diff popup
	popupDiffCache   []string                    // cached diff lines
	popupDiffKinds   []presentation.DiffLineKind // cached diff line kinds
	fullScreen       bool                         // true when in full-screen mode
	fullScreenTarget string                        // session ID for full-screen view
	inputMode        InputMode                     // insert (forward) or normal (lazyclaude handles)
	inputForwarder   InputForwarder                // forwards keys to tmux pane in full-screen
	keyMap           *KeyMap                       // configurable key bindings
	outputNotify     chan struct{}                 // signals pane output (from control mode)
}

// NewApp creates a new App. Call Run() to start the event loop.
func NewApp(mode AppMode) (*App, error) {
	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode:      gocui.OutputTrue,
		SupportOverlaps: false,
	})
	if err != nil {
		return nil, fmt.Errorf("init gocui: %w", err)
	}

	app := &App{
		gui:          g,
		mode:         mode,
		contextMgr:   context.NewManager(),
		keyMap:       DefaultKeyMap(),
		outputNotify: make(chan struct{}, 1),
	}

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
		contextMgr:   context.NewManager(),
		keyMap:       DefaultKeyMap(),
		outputNotify: make(chan struct{}, 1),
	}

	g.SetManagerFunc(app.layout)

	if err := app.setupGlobalKeybindings(); err != nil {
		g.Close()
		return nil, err
	}

	return app, nil
}

// Run starts the main event loop. Blocks until quit.
func (a *App) Run() error {
	defer a.gui.Close()

	// Refresh loop: event-driven via outputNotify (from control mode),
	// with a fallback ticker for notification polling and non-control scenarios.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-a.outputNotify:
				// Pane output detected — clear preview cache to force re-capture
				a.previewMu.Lock()
				a.previewCache = ""
				a.previewMu.Unlock()
				a.gui.Update(func(g *gocui.Gui) error { return nil })
			case <-ticker.C:
				// Fallback: poll for tool notifications
				a.gui.Update(func(g *gocui.Gui) error {
					if a.sessions != nil && !a.hasPopup() {
						if n := a.sessions.PendingNotification(); n != nil {
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

// Mode returns the current app mode.
func (a *App) Mode() AppMode {
	return a.mode
}

// ContextMgr returns the context manager.
func (a *App) ContextMgr() *context.Manager {
	return a.contextMgr
}

// SetSessions sets the session provider for the main screen.
func (a *App) SetSessions(sp SessionProvider) {
	a.sessions = sp
}

// SetInputForwarder sets the input forwarder for full-screen mode.
func (a *App) SetInputForwarder(fwd InputForwarder) {
	a.inputForwarder = fwd
}

// NotifyOutput signals that a pane has new output.
// Called from the control mode callback. Non-blocking.
func (a *App) NotifyOutput() {
	select {
	case a.outputNotify <- struct{}{}:
	default: // already signaled, skip
	}
}

// Gui returns the underlying gocui.Gui (for testing).
func (a *App) Gui() *gocui.Gui {
	return a.gui
}

func (a *App) setStatus(g *gocui.Gui, msg string) {
	v, err := g.View("server")
	if err != nil {
		return
	}
	v.Clear()
	fmt.Fprintln(v, "  "+msg)
}
