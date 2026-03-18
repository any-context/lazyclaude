package gui

import (
	"fmt"
	"os"
	"os/exec"
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
	ModeMain AppMode = iota // lazyclaude      → session list + preview
	ModeDiff                // lazyclaude diff  → diff popup viewer
	ModeTool                // lazyclaude tool  → tool popup viewer
)

// SessionProvider abstracts session operations for the GUI layer.
type SessionProvider interface {
	Sessions() []SessionItem
	Create(path, host string) error
	Delete(id string) error
	Rename(id, newName string) error
	PurgeOrphans() (int, error)
	CapturePreview(id string, width, height int) (string, error)
	AttachCmd(id string) (*exec.Cmd, error)
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
	gui            *gocui.Gui
	mode           AppMode
	contextMgr     *context.Manager
	activeTabIdx   int
	sessions       SessionProvider
	cursor         int // selected session index
	previewMu      sync.Mutex
	previewCache   string    // cached preview content
	previewCursor  int       // cursor position when cache was taken
	previewBusy    bool      // async capture in progress
	previewTime    time.Time // last fetch timestamp
	lastWidth      int
	lastHeight     int
	pendingTool      *notify.ToolNotification          // active tool popup
	popupScrollY     int                               // scroll position for diff popup
	popupDiffCache   []string                           // cached diff lines
	popupDiffKinds   []presentation.DiffLineKind        // cached diff line kinds
	fullScreen       bool                               // true when in full-screen mode
	fullScreenTarget string                             // session ID for full-screen view
	inputForwarder   InputForwarder                     // forwards keys to tmux pane in full-screen
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
		gui:        g,
		mode:       mode,
		contextMgr: context.NewManager(),
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
		gui:        g,
		mode:       mode,
		contextMgr: context.NewManager(),
	}

	g.SetManagerFunc(app.layout)

	if err := app.setupGlobalKeybindings(); err != nil {
		g.Close()
		return nil, err
	}

	return app, nil
}

// TestLayout exposes layout for testing. Not for production use.
func (a *App) TestLayout(g *gocui.Gui) error {
	return a.layout(g)
}

// ShowToolPopupForTest exposes showToolPopup for testing.
func (a *App) ShowToolPopupForTest(n *notify.ToolNotification) {
	a.showToolPopup(n)
}

// DismissPopupForTest exposes dismissPopup for testing.
func (a *App) DismissPopupForTest(choice Choice) {
	a.dismissPopup(choice)
}

// HasPopupForTest exposes hasPopup for testing.
func (a *App) HasPopupForTest() bool {
	return a.hasPopup()
}

// CursorForTest returns the current cursor position for testing.
func (a *App) CursorForTest() int {
	return a.cursor
}

// EnterFullScreenForTest enters full-screen mode for testing.
func (a *App) EnterFullScreenForTest(sessionID string) {
	a.enterFullScreen(sessionID)
}

// ExitFullScreenForTest exits full-screen mode for testing.
func (a *App) ExitFullScreenForTest() {
	a.exitFullScreen()
}

// IsFullScreenForTest returns full-screen state for testing.
func (a *App) IsFullScreenForTest() bool {
	return a.fullScreen
}

// ForwardKeyForTest simulates forwarding a key in full-screen mode.
func (a *App) ForwardKeyForTest(ch rune) {
	a.forwardKey(ch)
}

// PollNotificationForTest simulates what the ticker does: check for pending
// notifications and show popup. For testing without running the event loop.
func (a *App) PollNotificationForTest() {
	if a.sessions != nil && !a.hasPopup() {
		if n := a.sessions.PendingNotification(); n != nil {
			a.showToolPopup(n)
		}
	}
}

// Run starts the main event loop. Blocks until quit.
func (a *App) Run() error {
	defer a.gui.Close()

	// Periodic refresh for live preview and notification polling
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				// All pendingTool access inside gui.Update to avoid data race
				// with gocui's layout/keybinding goroutine.
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

// Gui returns the underlying gocui.Gui (for testing).
func (a *App) Gui() *gocui.Gui {
	return a.gui
}

// resolveForwardTarget returns the tmux target for key forwarding.
// Returns empty string if forwarding should be skipped.
func (a *App) resolveForwardTarget() string {
	if !a.fullScreen || a.inputForwarder == nil || a.hasPopup() || a.sessions == nil {
		return ""
	}
	items := a.sessions.Sessions()
	if a.cursor < 0 || a.cursor >= len(items) {
		return ""
	}
	t := items[a.cursor].TmuxWindow
	if t == "" {
		return ""
	}
	return "lazyclaude:" + t
}

// forwardKey sends a rune key to the Claude Code pane in full-screen mode.
// Called synchronously from gocui event loop — tmux send-keys is fast (~5ms).
func (a *App) forwardKey(ch rune) {
	if target := a.resolveForwardTarget(); target != "" {
		if err := a.inputForwarder.ForwardKey(target, RuneToTmuxKey(ch)); err != nil {
			a.setStatusAsync(fmt.Sprintf("forward key: %v", err))
		}
	}
}

// forwardSpecialKey sends a named special key (Enter, Tab, Up, Down, etc.).
func (a *App) forwardSpecialKey(tmuxKey string) {
	if target := a.resolveForwardTarget(); target != "" {
		if err := a.inputForwarder.ForwardKey(target, tmuxKey); err != nil {
			a.setStatusAsync(fmt.Sprintf("forward key: %v", err))
		}
	}
}

// ForwardSpecialKeyForTest simulates forwarding a special key in full-screen mode.
func (a *App) ForwardSpecialKeyForTest(tmuxKey string) {
	a.forwardSpecialKey(tmuxKey)
}

func (a *App) setStatusAsync(msg string) {
	if a.gui == nil {
		return
	}
	a.gui.Update(func(g *gocui.Gui) error {
		a.setStatus(g, msg)
		return nil
	})
}

func (a *App) enterFullScreen(sessionID string) {
	a.fullScreen = true
	a.fullScreenTarget = sessionID
	a.previewCache = ""
	// Set cursor to the target session once at entry (not in layout)
	if a.sessions != nil {
		for i, item := range a.sessions.Sessions() {
			if item.ID == sessionID {
				a.cursor = i
				break
			}
		}
	}
}

func (a *App) exitFullScreen() {
	a.fullScreen = false
	a.fullScreenTarget = ""
	a.previewCache = ""
}

func (a *App) layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()

	// Detect terminal resize → clear preview cache
	if maxX != a.lastWidth || maxY != a.lastHeight {
		a.previewCache = ""
		a.lastWidth = maxX
		a.lastHeight = maxY
	}

	switch a.mode {
	case ModeMain:
		if a.fullScreen {
			if err := a.layoutFullScreen(g, maxX, maxY); err != nil {
				return err
			}
		} else {
			if err := a.layoutMain(g, maxX, maxY); err != nil {
				return err
			}
		}
		return a.layoutToolPopup(g, maxX, maxY)
	case ModeDiff, ModeTool:
		return a.layoutPopup(g, maxX, maxY)
	}
	return nil
}

// ActiveTabIdx returns the active side panel tab index.
func (a *App) ActiveTabIdx() int {
	return a.activeTabIdx
}

// SetActiveTab switches the side panel tab.
func (a *App) SetActiveTab(idx int) {
	tabs := SideTabs()
	if idx >= 0 && idx < len(tabs) {
		a.activeTabIdx = idx
	}
}

func (a *App) layoutMain(g *gocui.Gui, maxX, maxY int) error {
	g.DeleteView("fullscreen-bar") // clean up after full-screen mode

	splitX := maxX / 3
	if splitX < 20 {
		splitX = 20
	}
	if splitX >= maxX-10 {
		splitX = maxX / 2
	}

	tabs := SideTabs()
	tabTitle := " " + TabBar(tabs, a.activeTabIdx) + " "

	// Left side panel: split into upper (sessions) and lower (server)
	leftMidY := (maxY - 2) * 2 / 3

	// Sessions view (upper left)
	v, err := g.SetView("sessions", 0, 0, splitX-1, leftMidY, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v.Title = tabTitle
	v.Highlight = true
	v.SelBgColor = gocui.ColorBlue
	v.SelFgColor = gocui.ColorWhite
	v.Clear()
	// Take a single snapshot of sessions for this frame (prevents TOCTOU with GC)
	var items []SessionItem
	if a.sessions != nil {
		items = a.sessions.Sessions()
	}
	a.renderSessionList(v, items)

	// Server view (lower left)
	v2, err := g.SetView("server", 0, leftMidY+1, splitX-1, maxY-2, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v2.Title = " Server "
	v2.Wrap = true
	if isUnknownView(err) {
		fmt.Fprintln(v2, "  MCP: not running")
	}

	// Main panel (right side)
	v3, err := g.SetView("main", splitX, 0, maxX-1, maxY-2, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v3.Wrap = false
	v3.Clear()
	// Pass preview panel inner dimensions (exclude borders)
	previewW := maxX - splitX - 2
	previewH := maxY - 4
	a.renderPreview(v3, items, previewW, previewH)

	// Options bar (bottom, frameless)
	v4, err := g.SetView("options", 0, maxY-2, maxX-1, maxY, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v4.Frame = false
	if isUnknownView(err) {
		fmt.Fprint(v4, " n: new  d: del  enter: full  r: resume  R: rename  q: quit")
	}

	// Set focus to active tab's view
	activeView := tabs[a.activeTabIdx].Name
	if _, err := g.SetCurrentView(activeView); err != nil && !isUnknownView(err) {
		return err
	}
	return nil
}

func (a *App) layoutFullScreen(g *gocui.Gui, maxX, maxY int) error {
	// Remove split-panel views
	g.DeleteView("sessions")
	g.DeleteView("server")
	g.DeleteView("options")

	// Full-screen main view
	v, err := g.SetView("main", 0, 0, maxX-1, maxY-2, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v.Wrap = false
	v.Clear()

	// Render preview content (same pipeline as split-panel mode)
	var items []SessionItem
	if a.sessions != nil {
		items = a.sessions.Sessions()
	}
	// Find the full-screen target session
	targetIdx := -1
	for i, item := range items {
		if item.ID == a.fullScreenTarget {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		a.exitFullScreen()
		return nil
	}
	previewW := maxX - 2
	previewH := maxY - 3
	a.renderPreview(v, items, previewW, previewH)

	// Status bar
	v2, err := g.SetView("fullscreen-bar", 0, maxY-2, maxX-1, maxY, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v2.Frame = false
	v2.Clear()
	fmt.Fprintf(v2, " %s | Ctrl+D: exit full mode  y/a/n: popup choices", items[targetIdx].Name)

	if _, err := g.SetCurrentView("main"); err != nil && !isUnknownView(err) {
		return err
	}
	return nil
}

func (a *App) layoutPopup(g *gocui.Gui, maxX, maxY int) error {
	// Content area (top)
	v, err := g.SetView("content", 0, 0, maxX-1, maxY-3, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v.Wrap = false

	// Actions bar (bottom)
	v2, err := g.SetView("actions", 0, maxY-2, maxX-1, maxY, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v2.Frame = false
	if isUnknownView(err) {
		fmt.Fprint(v2, " y: yes  a: allow always  n: no  Esc: cancel")
	}

	if _, err := g.SetCurrentView("content"); err != nil && !isUnknownView(err) {
		return err
	}
	return nil
}

func (a *App) renderPreview(v *gocui.View, items []SessionItem, previewW, previewH int) {
	if items == nil {
		v.Title = " Main "
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, "  lazyclaude")
		fmt.Fprintln(v, "  A standalone TUI for Claude Code")
		return
	}

	if len(items) == 0 || a.cursor >= len(items) {
		v.Title = " Main "
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, "  Select a session or press 'n' to create one.")
		return
	}

	item := items[a.cursor]
	v.Title = fmt.Sprintf(" %s ", item.Name)

	if item.Status == "Orphan" {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, "  Session not found in tmux.")
		fmt.Fprintln(v, "  Press 'd' to remove.")
		return
	}

	// Async preview: launch capture in background, render from cache
	a.previewMu.Lock()
	cache := a.previewCache
	cachedCursor := a.previewCursor
	stale := time.Since(a.previewTime) > 500*time.Millisecond
	needFetch := !a.previewBusy && (a.previewCursor != a.cursor || a.previewCache == "" || stale)
	if needFetch {
		a.previewBusy = true
	}
	a.previewMu.Unlock()

	if needFetch {
		id := item.ID
		cursorSnapshot := a.cursor
		go func() {
			content, err := a.sessions.CapturePreview(id, previewW, previewH)
			a.previewMu.Lock()
			if err == nil && strings.TrimSpace(content) != "" {
				a.previewCache = content
				a.previewCursor = cursorSnapshot
			}
			a.previewBusy = false
			a.previewTime = time.Now()
			a.previewMu.Unlock()
		}()
	}

	if cache != "" && cachedCursor == a.cursor {
		fmt.Fprint(v, cache)
		return
	}

	// Fallback while loading
	fmt.Fprintln(v, "")
	fmt.Fprintf(v, "  %s [%s]\n", item.Name, item.Status)
	fmt.Fprintf(v, "  %s\n", item.Path)
}

func (a *App) renderSessionList(v *gocui.View, items []SessionItem) {
	if len(items) == 0 {
		fmt.Fprintln(v, "  (no sessions)")
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, "  Press 'n' to create")
		return
	}

	if a.cursor >= len(items) {
		a.cursor = len(items) - 1
	}
	if a.cursor < 0 {
		a.cursor = 0
	}

	for i, item := range items {
		prefix := "  "
		if i == a.cursor {
			prefix = "> "
		}

		status := ""
		switch item.Status {
		case "Running":
			status = " *"
		case "Dead":
			status = " !"
		case "Orphan":
			status = " x"
		case "Detached":
			status = " -"
		}

		name := item.Name
		if item.Host != "" {
			name = item.Host + ":" + name
		}
		fmt.Fprintf(v, "%s%-20s%s\n", prefix, name, status)
	}

	v.SetCursor(0, a.cursor)
}

func (a *App) setupGlobalKeybindings() error {
	// Ctrl+C to quit (always)
	if err := a.gui.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return gocui.ErrQuit
	}); err != nil {
		return err
	}

	// q: quit or forward in full-screen
	if err := a.gui.SetKeybinding("", 'q', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			return nil
		}
		if a.fullScreen {
			a.forwardKey('q')
			return nil
		}
		if a.mode == ModeMain {
			return gocui.ErrQuit
		}
		return nil
	}); err != nil {
		return err
	}

	// Esc on popup view: cancel
	if err := a.gui.SetKeybinding(popupViewName, gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.dismissPopup(ChoiceCancel)
		return nil
	}); err != nil {
		return err
	}

	// Esc: dismiss popup, forward in full-screen, quit in popup mode
	if err := a.gui.SetKeybinding("", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissPopup(ChoiceCancel)
			return nil
		}
		if a.fullScreen {
			a.forwardSpecialKey("Escape")
			return nil
		}
		if a.mode == ModeDiff || a.mode == ModeTool {
			return gocui.ErrQuit
		}
		if a.contextMgr.Depth() > 1 {
			a.contextMgr.Pop()
		}
		return nil
	}); err != nil {
		return err
	}

	// Cursor/scroll handler factory: runeKey for j/k literal, tmuxSpecial for arrow keys
	makeCursorHandler := func(runeKey rune, tmuxSpecial string, isDown bool) func(*gocui.Gui, *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			if a.hasPopup() {
				if isDown && a.pendingTool.IsDiff() && a.popupDiffCache != nil {
					if a.popupScrollY < len(a.popupDiffCache)-1 {
						a.popupScrollY++
					}
				}
				if !isDown && a.pendingTool.IsDiff() && a.popupScrollY > 0 {
					a.popupScrollY--
				}
				return nil
			}
			if a.fullScreen {
				if tmuxSpecial != "" {
					a.forwardSpecialKey(tmuxSpecial)
				} else {
					a.forwardKey(runeKey)
				}
				return nil
			}
			if a.mode != ModeMain {
				return nil
			}
			if isDown && a.sessions != nil {
				if a.cursor < len(a.sessions.Sessions())-1 {
					a.cursor++
				}
			}
			if !isDown && a.cursor > 0 {
				a.cursor--
			}
			return nil
		}
	}

	jDown := makeCursorHandler('j', "", true)
	kUp := makeCursorHandler('k', "", false)
	arrowDown := makeCursorHandler(0, "Down", true)
	arrowUp := makeCursorHandler(0, "Up", false)

	if err := a.gui.SetKeybinding("", 'j', gocui.ModNone, jDown); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("", gocui.KeyArrowDown, gocui.ModNone, arrowDown); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("", 'k', gocui.ModNone, kUp); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("", gocui.KeyArrowUp, gocui.ModNone, arrowUp); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, 'j', gocui.ModNone, jDown); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, 'k', gocui.ModNone, kUp); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, gocui.KeyArrowDown, gocui.ModNone, arrowDown); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, gocui.KeyArrowUp, gocui.ModNone, arrowUp); err != nil {
		return err
	}

	// n: create session, reject popup, or forward in full-screen
	if err := a.gui.SetKeybinding("", 'n', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissPopup(ChoiceReject)
			return nil
		}
		if a.fullScreen {
			a.forwardKey('n')
			return nil
		}
		if a.mode != ModeMain || a.sessions == nil {
			return nil
		}
		if err := a.sessions.Create(".", ""); err != nil {
			a.setStatus(g, fmt.Sprintf("Error: %v", err))
			return nil
		}
		a.setStatus(g, "Session created")
		return nil
	}); err != nil {
		return err
	}

	// Popup choice handlers — bind on BOTH global ("") and popup view name.
	// jesseduffield/gocui dispatches view-specific bindings first when a view has focus.
	// Global bindings may not fire when the popup view is focused.
	popupAccept := func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissPopup(ChoiceAccept)
		} else if a.fullScreen {
			a.forwardKey('y')
		}
		return nil
	}
	popupAllow := func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissPopup(ChoiceAllow)
		} else if a.fullScreen {
			a.forwardKey('a')
		}
		return nil
	}
	popupReject := func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			a.dismissPopup(ChoiceReject)
		}
		return nil
	}

	// y: accept
	if err := a.gui.SetKeybinding("", 'y', gocui.ModNone, popupAccept); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, 'y', gocui.ModNone, popupAccept); err != nil {
		return err
	}

	// a: allow always
	if err := a.gui.SetKeybinding("", 'a', gocui.ModNone, popupAllow); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, 'a', gocui.ModNone, popupAllow); err != nil {
		return err
	}

	// 1/2/3: direct number selection (same as y/a/n)
	if err := a.gui.SetKeybinding(popupViewName, '1', gocui.ModNone, popupAccept); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, '2', gocui.ModNone, popupAllow); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, '3', gocui.ModNone, popupReject); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(popupViewName, 'n', gocui.ModNone, popupReject); err != nil {
		return err
	}

	// d: delete or forward in full-screen
	if err := a.gui.SetKeybinding("", 'd', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			return nil
		}
		if a.fullScreen {
			a.forwardKey('d')
			return nil
		}
		if a.mode != ModeMain || a.sessions == nil {
			return nil
		}
		items := a.sessions.Sessions()
		if a.cursor >= 0 && a.cursor < len(items) {
			if err := a.sessions.Delete(items[a.cursor].ID); err != nil {
				a.setStatus(g, fmt.Sprintf("Error: %v", err))
				return nil
			}
			if a.cursor > 0 && a.cursor >= len(a.sessions.Sessions()) {
				a.cursor--
			}
			a.setStatus(g, "Session deleted")
		}
		return nil
	}); err != nil {
		return err
	}

	// enter: enter full-screen or forward
	if err := a.gui.SetKeybinding("", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			return nil
		}
		if a.fullScreen {
			a.forwardSpecialKey("Enter")
			return nil
		}
		if a.mode != ModeMain || a.sessions == nil {
			return nil
		}
		items := a.sessions.Sessions()
		if a.cursor >= 0 && a.cursor < len(items) {
			a.enterFullScreen(items[a.cursor].ID)
		}
		return nil
	}); err != nil {
		return err
	}

	// Ctrl+D: exit full-screen mode
	if err := a.gui.SetKeybinding("", gocui.KeyCtrlD, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.fullScreen {
			a.exitFullScreen()
		}
		return nil
	}); err != nil {
		return err
	}

	// r: resume or forward in full-screen
	if err := a.gui.SetKeybinding("", 'r', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			return nil
		}
		if a.fullScreen {
			a.forwardKey('r')
			return nil
		}
		// TODO: pass --resume flag to the session
		return a.attachSelected(g)
	}); err != nil {
		return err
	}

	// R: rename or forward in full-screen
	if err := a.gui.SetKeybinding("", 'R', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			return nil
		}
		if a.fullScreen {
			a.forwardKey('R')
			return nil
		}
		if a.mode != ModeMain || a.sessions == nil {
			return nil
		}
		// TODO: prompt for new name (requires input popup)
		// For now, append "-renamed" as placeholder
		items := a.sessions.Sessions()
		if a.cursor >= 0 && a.cursor < len(items) {
			newName := items[a.cursor].Name + "-renamed"
			if err := a.sessions.Rename(items[a.cursor].ID, newName); err != nil {
				a.setStatus(g, fmt.Sprintf("Error: %v", err))
				return nil
			}
			a.setStatus(g, "Renamed to "+newName)
		}
		return nil
	}); err != nil {
		return err
	}

	// D: purge or forward in full-screen
	if err := a.gui.SetKeybinding("", 'D', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.hasPopup() {
			return nil
		}
		if a.fullScreen {
			a.forwardKey('D')
			return nil
		}
		if a.mode != ModeMain || a.sessions == nil {
			return nil
		}
		count, err := a.sessions.PurgeOrphans()
		if err != nil {
			a.setStatus(g, fmt.Sprintf("Error: %v", err))
			return nil
		}
		a.setStatus(g, fmt.Sprintf("Purged %d orphans", count))
		return nil
	}); err != nil {
		return err
	}

	// Full-screen: forward remaining printable ASCII on "main" view.
	// Keys already handled above (q/j/k/n/d/r/R/D/y/a) have fullScreen guards.
	for ch := rune(32); ch <= 126; ch++ {
		c := ch
		if err := a.gui.SetKeybinding("main", c, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			if a.fullScreen && !a.hasPopup() {
				a.forwardKey(c)
			}
			return nil
		}); err != nil {
			return err
		}
	}

	// Full-screen: forward special keys on "main" view
	fwdSpecialKeys := map[gocui.Key]string{
		gocui.KeyTab:        "Tab",
		gocui.KeyBackspace:  "BSpace",
		gocui.KeyBackspace2: "BSpace",
		gocui.KeyArrowLeft:  "Left",
		gocui.KeyArrowRight: "Right",
	}
	for gKey, tmuxKey := range fwdSpecialKeys {
		tk := tmuxKey
		if err := a.gui.SetKeybinding("main", gKey, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			if a.fullScreen && !a.hasPopup() {
				a.forwardSpecialKey(tk)
			}
			return nil
		}); err != nil {
			return err
		}
	}

	return nil
}

func (a *App) attachSelected(g *gocui.Gui) error {
	if a.mode != ModeMain || a.sessions == nil {
		return nil
	}
	items := a.sessions.Sessions()
	if a.cursor < 0 || a.cursor >= len(items) {
		return nil
	}
	item := items[a.cursor]
	if item.Status == "Orphan" {
		a.setStatus(g, "Cannot attach: session is orphaned")
		return nil
	}

	cmd, err := a.sessions.AttachCmd(item.ID)
	if err != nil {
		a.setStatus(g, fmt.Sprintf("Error: %v", err))
		return nil
	}

	// Suspend gocui, run tmux attach, then resume
	if err := g.Suspend(); err != nil {
		a.setStatus(g, fmt.Sprintf("Suspend error: %v", err))
		return nil
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run() // blocks until user detaches

	if err := g.Resume(); err != nil {
		return err
	}

	// Clear preview cache to refresh after detach
	a.previewCache = ""
	return nil
}

func (a *App) setStatus(g *gocui.Gui, msg string) {
	v, err := g.View("server")
	if err != nil {
		return
	}
	v.Clear()
	fmt.Fprintln(v, "  "+msg)
}
