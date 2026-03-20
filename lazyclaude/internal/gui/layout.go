package gui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/jesseduffield/gocui"
)

// roundedFrame is the set of runes for rounded border corners.
// Order: horizontal, vertical, top-left, top-right, bottom-left, bottom-right.
var roundedFrame = []rune{'─', '│', '╭', '╮', '╰', '╯'}

// setRoundedFrame applies rounded border corners to a gocui view.
func setRoundedFrame(v *gocui.View) {
	v.FrameRunes = roundedFrame
}

// Rect is a simple rectangle from (X0, Y0) to (X1, Y1) inclusive,
// matching gocui's SetView coordinate convention.
type Rect struct {
	X0, Y0, X1, Y1 int
}

// Width returns the number of columns the rectangle spans.
func (r Rect) Width() int {
	return r.X1 - r.X0
}

// Height returns the number of rows the rectangle spans.
func (r Rect) Height() int {
	return r.Y1 - r.Y0
}

// Layout holds pre-computed view positions for the main screen.
type Layout struct {
	Sessions Rect // upper-left panel
	Server   Rect // lower-left panel
	Main     Rect // right panel (preview)
	Options  Rect // bottom bar
	Compact  bool // true when terminal is too narrow for a split
}

// CompactThreshold is the terminal width below which compact mode activates.
const CompactThreshold = 60

// ComputeLayout calculates view positions for the given terminal size.
// It mirrors the inline coordinate logic from layoutMain exactly.
func ComputeLayout(width, height int) Layout {
	maxX := width
	maxY := height

	splitX := maxX / 3
	if splitX < 20 {
		splitX = 20
	}
	if splitX >= maxX-10 {
		splitX = maxX / 2
	}

	compact := width < CompactThreshold

	leftMidY := (maxY - 2) * 2 / 3

	sessions := Rect{X0: 0, Y0: 0, X1: splitX - 1, Y1: leftMidY}
	server := Rect{X0: 0, Y0: leftMidY + 1, X1: splitX - 1, Y1: maxY - 2}
	main := Rect{X0: splitX, Y0: 0, X1: maxX - 1, Y1: maxY - 2}
	options := Rect{X0: 0, Y0: maxY - 2, X1: maxX - 1, Y1: maxY}

	return Layout{
		Sessions: sessions,
		Server:   server,
		Main:     main,
		Options:  options,
		Compact:  compact,
	}
}

// ComputeFullScreenLayout calculates view positions for full-screen mode.
// The main panel takes the full terminal width; a status bar sits at the bottom.
func ComputeFullScreenLayout(width, height int) Layout {
	maxX := width
	maxY := height

	main := Rect{X0: 0, Y0: 0, X1: maxX - 1, Y1: maxY - 2}
	statusBar := Rect{X0: 0, Y0: maxY - 2, X1: maxX - 1, Y1: maxY}

	return Layout{
		Main:    main,
		Options: statusBar,
	}
}

// ComputePopupLayout calculates view positions for popup (diff/tool) mode.
// The content area fills all but the last two rows; the actions bar occupies the bottom.
func ComputePopupLayout(width, height int) Layout {
	maxX := width
	maxY := height

	content := Rect{X0: 0, Y0: 0, X1: maxX - 1, Y1: maxY - 3}
	actions := Rect{X0: 0, Y0: maxY - 2, X1: maxX - 1, Y1: maxY}

	return Layout{
		Main:    content,
		Options: actions,
	}
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

func (a *App) layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()

	// Detect terminal resize -> clear preview cache
	if maxX != a.lastWidth || maxY != a.lastHeight {
		a.previewCache = ""
		a.lastWidth = maxX
		a.lastHeight = maxY
	}

	switch a.mode {
	case ModeMain:
		if a.state.IsFullScreen() {
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

func (a *App) layoutMain(g *gocui.Gui, maxX, maxY int) error {
	g.DeleteView("fullscreen-bar") // clean up after full-screen mode
	g.Cursor = false
	fmt.Fprint(os.Stdout, "\033[0 q") // restore default cursor

	l := ComputeLayout(maxX, maxY)

	tabs := SideTabs()
	tabTitle := " " + TabBar(tabs, a.activeTabIdx) + " "

	// Sessions view (upper left)
	v, err := g.SetView("sessions", l.Sessions.X0, l.Sessions.Y0, l.Sessions.X1, l.Sessions.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	setRoundedFrame(v)
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
	v2, err := g.SetView("server", l.Server.X0, l.Server.Y0, l.Server.X1, l.Server.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	setRoundedFrame(v2)
	v2.Title = " Server "
	v2.Wrap = true
	if isUnknownView(err) {
		fmt.Fprintln(v2, "  MCP: not running")
	}

	// Main panel (right side)
	v3, err := g.SetView("main", l.Main.X0, l.Main.Y0, l.Main.X1, l.Main.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	setRoundedFrame(v3)
	v3.Wrap = false
	v3.Editable = false
	v3.Clear()
	// Pass preview panel inner dimensions (exclude borders).
	// Width: gocui border takes 1 col on each side of the right panel.
	// Height: top border + bottom border + options bar = 2 top + 2 bottom rows.
	previewW := l.Main.Width() - 1
	previewH := l.Main.Height() - 2
	a.renderPreview(v3, items, previewW, previewH)

	// Options bar (bottom, frameless)
	v4, err := g.SetView("options", l.Options.X0, l.Options.Y0, l.Options.X1, l.Options.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v4.Frame = false
	if isUnknownView(err) {
		fmt.Fprint(v4, " ",
			presentation.StyledKey("n", "new"), "  ",
			presentation.StyledKey("d", "del"), "  ",
			presentation.StyledKey("enter", "full"), "  ",
			presentation.StyledKey("r", "resume"), "  ",
			presentation.StyledKey("R", "rename"), "  ",
			presentation.StyledKey("q", "quit"),
		)
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

	l := ComputeFullScreenLayout(maxX, maxY)

	// Full-screen main view
	v, err := g.SetView("main", l.Main.X0, l.Main.Y0, l.Main.X1, l.Main.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v.Wrap = false
	v.Editable = true
	v.Editor = &inputEditor{app: a}
	v.Clear()
	g.Cursor = true
	fmt.Fprint(os.Stdout, "\033[2 q") // steady block cursor (no blink)

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
	// Inner dimensions: subtract borders (1 col each side, 1 row each side).
	previewW := l.Main.Width() - 1
	previewH := l.Main.Height() - 1
	a.renderPreview(v, items, previewW, previewH)

	// Scroll offset for mouse scroll
	v.SetOrigin(0, a.fullScreenScrollY)

	// Status bar
	v2, err := g.SetView("fullscreen-bar", l.Options.X0, l.Options.Y0, l.Options.X1, l.Options.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v2.Frame = false
	v2.Clear()
	fmt.Fprintf(v2, " %s %s %s",
		items[targetIdx].Name,
		presentation.FgDimGray+presentation.IconSep+presentation.Reset,
		presentation.Dim+"Ctrl+\\:exit"+presentation.Reset)

	if _, err := g.SetCurrentView("main"); err != nil && !isUnknownView(err) {
		return err
	}
	return nil
}

func (a *App) layoutPopup(g *gocui.Gui, maxX, maxY int) error {
	l := ComputePopupLayout(maxX, maxY)

	// Content area (top)
	v, err := g.SetView("content", l.Main.X0, l.Main.Y0, l.Main.X1, l.Main.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	setRoundedFrame(v)
	v.Wrap = false

	// Actions bar (bottom)
	v2, err := g.SetView("actions", l.Options.X0, l.Options.Y0, l.Options.X1, l.Options.Y1, 0)
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
		v.Title = " Preview "
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Bold+"  lazyclaude"+presentation.Reset)
		fmt.Fprintln(v, presentation.Dim+"  A standalone TUI for Claude Code"+presentation.Reset)
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.FgDimGray+"  "+presentation.IconRunning+" running  "+
			presentation.IconDetached+" detached  "+
			presentation.IconDead+" dead  "+
			presentation.IconOrphan+" orphan"+presentation.Reset)
		return
	}

	if len(items) == 0 || a.cursor >= len(items) {
		v.Title = " Preview "
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  Select a session or press "+
			presentation.Reset+presentation.Bold+"n"+presentation.Reset+
			presentation.Dim+" to create one."+presentation.Reset)
		return
	}

	item := items[a.cursor]
	v.Title = fmt.Sprintf(" %s ", item.Name)

	if item.Status == "Orphan" {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.FgYellow+"  "+presentation.IconOrphan+" Session not found in tmux."+presentation.Reset)
		fmt.Fprintln(v, "  Press "+presentation.Bold+"d"+presentation.Reset+" to remove.")
		return
	}

	// Async preview: launch capture in background, render from cache
	a.previewMu.Lock()
	cache := a.previewCache
	cachedCursor := a.previewCursor
	stale := time.Since(a.previewTime) > 500*time.Millisecond
	// Only fetch if: not busy, AND (cursor changed OR cache is stale).
	// Do NOT use previewCache=="" as a trigger — empty capture results
	// (Claude Code starting up) would cause a tight fetch loop.
	needFetch := !a.previewBusy && (a.previewCursor != a.cursor || stale)
	if needFetch {
		a.previewBusy = true
	}
	a.previewMu.Unlock()

	if needFetch {
		id := item.ID
		cursorSnapshot := a.cursor
		go func() {
			result, err := a.sessions.CapturePreview(id, previewW, previewH)
			a.previewMu.Lock()
			a.previewCursor = cursorSnapshot
			if err == nil && strings.TrimSpace(result.Content) != "" {
				a.previewCache = result.Content
				a.paneCursorX = result.CursorX
				a.paneCursorY = result.CursorY
			}
			a.previewBusy = false
			a.previewTime = time.Now()
			a.previewMu.Unlock()
			a.gui.Update(func(g *gocui.Gui) error { return nil })
		}()
	}

	if cache != "" && cachedCursor == a.cursor {
		fmt.Fprint(v, cache)
		if a.state.IsFullScreen() {
			v.SetCursor(a.paneCursorX, a.paneCursorY)
		}
		return
	}

	// Fallback while loading
	fmt.Fprintln(v, "")
	fmt.Fprintf(v, "  %s [%s]\n", item.Name, item.Status)
	fmt.Fprintf(v, "  %s\n", item.Path)
}

func (a *App) renderSessionList(v *gocui.View, items []SessionItem) {
	if len(items) == 0 {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  No sessions"+presentation.Reset)
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, "  Press "+presentation.Bold+"n"+presentation.Reset+" to create")
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
			prefix = presentation.FgCyan + presentation.Bold + "> " + presentation.Reset
		}

		var icon string
		switch item.Status {
		case "Running":
			icon = " " + presentation.IconRunning
		case "Dead":
			icon = " " + presentation.IconDead
		case "Orphan":
			icon = " " + presentation.IconOrphan
		case "Detached":
			icon = " " + presentation.IconDetached
		}

		name := item.Name
		if item.Host != "" {
			name = presentation.FgPurple + item.Host + presentation.Reset + ":" + name
		}
		fmt.Fprintf(v, "%s%-20s%s\n", prefix, name, icon)
	}

	v.SetCursor(0, a.cursor)
}

// SideTab represents a tab in the left side panel.
type SideTab struct {
	Label string
	Name  string
}

// SideTabs returns the side panel tabs for the main screen.
func SideTabs() []SideTab {
	return []SideTab{
		{Label: "Sessions", Name: "sessions"},
		{Label: "Server", Name: "server"},
	}
}

// TabBar renders the tab bar string for the side panel title.
func TabBar(tabs []SideTab, activeIdx int) string {
	parts := make([]string, len(tabs))
	for i, tab := range tabs {
		if i == activeIdx {
			parts[i] = "[" + tab.Label + "]"
		} else {
			parts[i] = tab.Label
		}
	}
	return strings.Join(parts, "  ")
}
