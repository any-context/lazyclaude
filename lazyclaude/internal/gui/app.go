package gui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/KEMSHlM/lazyclaude/internal/gui/context"
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
	previewCache   string // cached preview content
	previewCursor  int    // cursor position when cache was taken
	previewBusy    bool   // async capture in progress
	lastWidth      int
	lastHeight     int
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

// Run starts the main event loop. Blocks until quit.
func (a *App) Run() error {
	defer a.gui.Close()
	if err := a.gui.MainLoop(); err != nil {
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

// Gui returns the underlying gocui.Gui (for testing).
func (a *App) Gui() *gocui.Gui {
	return a.gui
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
		return a.layoutMain(g, maxX, maxY)
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
		fmt.Fprint(v4, " n: new  d: del  enter: attach  r: resume  R: rename  q: quit")
	}

	// Set focus to active tab's view
	activeView := tabs[a.activeTabIdx].Name
	if _, err := g.SetCurrentView(activeView); err != nil && !isUnknownView(err) {
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
	needFetch := !a.previewBusy && (a.previewCursor != a.cursor || a.previewCache == "")
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

	// q to quit in main mode
	if err := a.gui.SetKeybinding("", 'q', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.mode == ModeMain {
			return gocui.ErrQuit
		}
		return nil
	}); err != nil {
		return err
	}

	// Esc: quit in popup mode, pop context in main mode
	if err := a.gui.SetKeybinding("", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
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

	// j/k: cursor movement
	if err := a.gui.SetKeybinding("", 'j', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.mode == ModeMain && a.sessions != nil {
			if a.cursor < len(a.sessions.Sessions())-1 {
				a.cursor++
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("", 'k', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.mode == ModeMain && a.cursor > 0 {
			a.cursor--
		}
		return nil
	}); err != nil {
		return err
	}

	// n: create new session (CWD)
	if err := a.gui.SetKeybinding("", 'n', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
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

	// d: delete selected session
	if err := a.gui.SetKeybinding("", 'd', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
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

	// enter: attach to selected session
	if err := a.gui.SetKeybinding("", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return a.attachSelected(g)
	}); err != nil {
		return err
	}

	// r: resume (attach with --resume flag)
	if err := a.gui.SetKeybinding("", 'r', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		// TODO: pass --resume flag to the session
		return a.attachSelected(g)
	}); err != nil {
		return err
	}

	// R: rename selected session
	if err := a.gui.SetKeybinding("", 'R', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
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

	// D: purge orphans
	if err := a.gui.SetKeybinding("", 'D', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
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
