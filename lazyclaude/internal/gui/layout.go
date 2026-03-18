package gui

import (
	"fmt"
	"strings"
	"time"

	"github.com/jesseduffield/gocui"
)

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
	v3.Editable = false
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
	// Insert mode: Editable=true → Editor catches all keys for forwarding.
	// Normal mode: Editable=false → global keybindings (q/j/k/i) work normally.
	v.Editable = (a.inputMode == ModeInsert)
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

	// Normal mode: show cursor with hjkl movement
	if a.inputMode == ModeNormal {
		g.Cursor = true
		v.SetCursor(a.fullScreenCursorX, a.fullScreenCursorY)
	} else {
		g.Cursor = false
		v.SetCursor(0, 0)
		v.SetOrigin(0, 0)
	}

	// Status bar
	v2, err := g.SetView("fullscreen-bar", 0, maxY-2, maxX-1, maxY, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v2.Frame = false
	v2.Clear()
	if a.inputMode == ModeInsert {
		fmt.Fprintf(v2, " INSERT | %s | Ctrl+\\: normal mode", items[targetIdx].Name)
	} else {
		fmt.Fprintf(v2, " NORMAL | %s | i: insert  q: exit  j/k: scroll", items[targetIdx].Name)
	}

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
			content, err := a.sessions.CapturePreview(id, previewW, previewH)
			a.previewMu.Lock()
			if err == nil && strings.TrimSpace(content) != "" {
				a.previewCache = content
				a.previewCursor = cursorSnapshot
			}
			a.previewBusy = false
			a.previewTime = time.Now()
			a.previewMu.Unlock()
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
