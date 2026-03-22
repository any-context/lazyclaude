package gui

import (
	"fmt"
	"os/exec"
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

func (a *App) layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()

	// Detect terminal resize -> clear preview cache
	if maxX != a.lastWidth || maxY != a.lastHeight {
		a.preview.Invalidate()
		a.lastWidth = maxX
		a.lastHeight = maxY
	}

	switch a.mode {
	case ModeMain:
		if a.fullscreen.IsActive() {
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

	l := ComputeLayout(maxX, maxY)
	focusedName := a.panelManager.ActivePanel().Name()

	// Sessions view (upper left)
	v, err := g.SetView("sessions", l.Sessions.X0, l.Sessions.Y0, l.Sessions.X1, l.Sessions.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	setRoundedFrame(v)
	v.Title = " Sessions "
	v.Highlight = focusedName == "sessions"
	v.SelBgColor = gocui.ColorBlue
	v.SelFgColor = gocui.ColorWhite
	if focusedName == "sessions" {
		v.FrameColor = gocui.ColorCyan
	} else {
		v.FrameColor = gocui.ColorDefault
	}
	v.Clear()
	var items []SessionItem
	if a.sessions != nil {
		items = a.sessions.Sessions()
	}
	if len(items) > 0 {
		if a.cursor >= len(items) {
			a.cursor = len(items) - 1
		}
		if a.cursor < 0 {
			a.cursor = 0
		}
	}
	renderSessionList(v, items, a.cursor)

	// Logs view (lower left)
	v2, err := g.SetView("logs", l.Server.X0, l.Server.Y0, l.Server.X1, l.Server.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	setRoundedFrame(v2)
	v2.Title = " Logs "
	v2.Wrap = true
	if focusedName == "logs" {
		v2.FrameColor = gocui.ColorCyan
	} else {
		v2.FrameColor = gocui.ColorDefault
	}
	v2.Clear()
	renderServerLog(v2, a.logs, focusedName == "logs")

	// Main panel (right side)
	v3, err := g.SetView("main", l.Main.X0, l.Main.Y0, l.Main.X1, l.Main.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	setRoundedFrame(v3)
	v3.Wrap = false
	v3.Editable = false
	v3.Clear()
	previewW := l.Main.Width() - 1
	previewH := l.Main.Height() - 2
	a.renderPreview(v3, items, previewW, previewH)

	// Options bar (bottom, frameless) — dynamic per focused panel
	v4, err := g.SetView("options", l.Options.X0, l.Options.Y0, l.Options.X1, l.Options.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v4.Frame = false
	v4.Clear()
	if optionsText := a.dispatcher.ActiveOptionsBar(a); optionsText != "" {
		fmt.Fprint(v4, optionsText)
	}

	// Set focus to active panel's view (skip if rename input is active).
	if a.renameSessionID == "" {
		if _, err := g.SetCurrentView(focusedName); err != nil && !isUnknownView(err) {
			return err
		}
	}
	return nil
}

func (a *App) layoutFullScreen(g *gocui.Gui, maxX, maxY int) error {
	// Remove split-panel views
	g.DeleteView("sessions")
	g.DeleteView("logs")
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

	// Render preview content (same pipeline as split-panel mode)
	var items []SessionItem
	if a.sessions != nil {
		items = a.sessions.Sessions()
	}
	// Find the full-screen target session
	targetIdx := -1
	for i, item := range items {
		if item.ID == a.fullscreen.Target() {
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
	v.SetOrigin(0, a.fullscreen.ScrollY())

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
	a.preview.Lock()
	cache := a.preview.Content()
	cachedCursor := a.preview.Cursor()
	stale := a.preview.Stale(500 * time.Millisecond)
	// Only fetch if: not busy, AND (cursor changed OR cache is stale).
	// Do NOT use cache=="" as a trigger — empty capture results
	// (Claude Code starting up) would cause a tight fetch loop.
	needFetch := !a.preview.Busy() && (a.preview.Cursor() != a.cursor || stale)
	if needFetch {
		a.preview.SetBusy(true)
	}
	a.preview.Unlock()

	if needFetch {
		id := item.ID
		cursorSnapshot := a.cursor
		go func() {
			result, err := a.sessions.CapturePreview(id, previewW, previewH)
			a.preview.Lock()
			if err == nil && strings.TrimSpace(result.Content) != "" {
				a.preview.Update(result.Content, cursorSnapshot, result.CursorX, result.CursorY)
			} else {
				a.preview.MarkFetched(cursorSnapshot)
			}
			a.preview.Unlock()
			a.gui.Update(func(g *gocui.Gui) error { return nil })
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


// copyToClipboard copies text to the system clipboard using pbcopy (macOS).
func copyToClipboard(text string) {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	_ = cmd.Run()
}

// showRenameInput creates a small input view for renaming a session.
// Returns false if the view could not be created.
func (a *App) showRenameInput(g *gocui.Gui, currentName string) bool {
	maxX, maxY := g.Size()
	w := 40
	if w > maxX-4 {
		w = maxX - 4
	}
	x0 := (maxX - w) / 2
	y0 := maxY/2 - 1
	x1 := x0 + w
	y1 := y0 + 2

	v, err := g.SetView("rename-input", x0, y0, x1, y1, 0)
	if err != nil && !isUnknownView(err) {
		return false
	}
	v.Title = " Rename "
	v.Editable = true
	v.Editor = gocui.DefaultEditor
	v.TextArea.Clear()
	for _, ch := range currentName {
		v.TextArea.TypeCharacter(string(ch))
	}
	v.RenderTextArea()
	if _, err := g.SetCurrentView("rename-input"); err != nil && !isUnknownView(err) {
		return false
	}
	g.Cursor = true
	return true
}

// closeRenameInput removes the rename input view and restores focus.
func (a *App) closeRenameInput(g *gocui.Gui) {
	a.renameSessionID = ""
	g.DeleteView("rename-input")
	g.Cursor = false
	if _, err := g.SetCurrentView("sessions"); err != nil && !isUnknownView(err) {
		// Fallback: sessions view may not exist in some modes.
		_ = err
	}
}
