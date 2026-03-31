package gui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/gui/keymap"
	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/KEMSHlM/lazyclaude/internal/session"
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
	Plugins  Rect // middle-left panel
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

	// Left side: 3 panels split into thirds
	leftH := maxY - 2 // available height (minus options bar)
	thirdH := leftH / 3

	sessY1 := thirdH
	plugY0 := sessY1 + 1
	plugY1 := sessY1 + thirdH
	logsY0 := plugY1 + 1

	sessions := Rect{X0: 0, Y0: 0, X1: splitX - 1, Y1: sessY1}
	plugins := Rect{X0: 0, Y0: plugY0, X1: splitX - 1, Y1: plugY1}
	server := Rect{X0: 0, Y0: logsY0, X1: splitX - 1, Y1: maxY - 2}
	main := Rect{X0: splitX, Y0: 0, X1: maxX - 1, Y1: maxY - 2}
	options := Rect{X0: 0, Y0: maxY - 2, X1: maxX - 1, Y1: maxY}

	return Layout{
		Sessions: sessions,
		Plugins:  plugins,
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

func (a *App) layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()

	// Rebuild tree nodes once per layout cycle for consistency.
	a.refreshTreeNodes()

	// Sync plugin panel with current project (lazy init on first layout).
	a.syncPluginProjectOnce()

	// Detect terminal resize -> clear preview cache
	if maxX != a.lastWidth || maxY != a.lastHeight {
		a.preview.Invalidate()
		a.lastWidth = maxX
		a.lastHeight = maxY
	}

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
	if q := a.effectiveQuery("sessions"); q != "" {
		v.Title = fmt.Sprintf(" Sessions [/%s] ", q)
	} else {
		v.Title = " Sessions "
	}
	v.Highlight = true
	v.SelBgColor = gocui.Get256Color(24)
	v.SelFgColor = gocui.ColorWhite
	v.HighlightInactive = focusedName != "sessions"
	v.InactiveViewSelBgColor = gocui.Get256Color(238)
	v.Clear()
	var nodes []TreeNode
	if a.sessions != nil {
		nodes = a.filteredTreeNodes()
	}
	if len(nodes) > 0 {
		if a.cursor >= len(nodes) {
			a.cursor = len(nodes) - 1
		}
		if a.cursor < 0 {
			a.cursor = 0
		}
	}
	renderTree(v, nodes, a.cursor)

	// Plugins view (middle left)
	vp, err := g.SetView("plugins", l.Plugins.X0, l.Plugins.Y0, l.Plugins.X1, l.Plugins.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	setRoundedFrame(vp)
	vp.Highlight = true
	vp.SelBgColor = gocui.Get256Color(24)
	vp.SelFgColor = gocui.ColorWhite
	vp.HighlightInactive = focusedName != "plugins"
	vp.InactiveViewSelBgColor = gocui.Get256Color(238)
	vp.Clear()
	a.renderPluginPanel(vp, l.Plugins.Width()-2)

	// Logs view (lower left)
	v2, err := g.SetView("logs", l.Server.X0, l.Server.Y0, l.Server.X1, l.Server.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	setRoundedFrame(v2)
	if q := a.effectiveQuery("logs"); q != "" {
		v2.Title = fmt.Sprintf(" Logs [/%s] ", q)
	} else {
		v2.Title = " Logs "
	}
	v2.Wrap = true
	a.renderServerLog(v2, a.logs, focusedName == "logs")

	// Main panel (right side) — content depends on focused panel
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
	if render, ok := a.previewByScope[a.panelManager.ActivePanel().Scope()]; ok {
		render(v3, previewW, previewH)
	} else {
		var items []SessionItem
		if a.sessions != nil {
			items = a.sessions.Sessions()
		}
		a.renderPreview(v3, items, previewW, previewH)
	}

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

	// Keybind help overlay (rendered before focus logic so views exist).
	if a.dialog.Kind == DialogKeybindHelp {
		if err := a.layoutKeybindHelp(g, maxX, maxY); err != nil {
			return err
		}
	} else {
		// Clean up help views when dialog is not active.
		g.DeleteView(helpInputView)
		g.DeleteView(helpListView)
		g.DeleteView(helpPreviewView)
		g.DeleteView(helpHintView)
		g.DeleteView(helpBorderView)
	}

	// Search input overlay (inline at bottom of active panel).
	if a.dialog.Kind == DialogSearch {
		var panelRect Rect
		switch a.dialog.SearchPanel {
		case "sessions":
			panelRect = l.Sessions
		case "plugins":
			panelRect = l.Plugins
		case "logs":
			panelRect = l.Server
		}
		if err := a.layoutSearchInput(g, panelRect); err != nil {
			return err
		}
		g.DeleteView(filterIndicatorView)
	} else {
		g.DeleteView(searchInputView)
		// Show persistent filter indicator when a confirmed filter is active.
		if a.dialog.ActiveFilter != "" {
			var panelRect Rect
			switch a.dialog.ActiveFilterPanel {
			case "sessions":
				panelRect = l.Sessions
			case "plugins":
				panelRect = l.Plugins
			case "logs":
				panelRect = l.Server
			}
			if err := a.layoutFilterIndicator(g, panelRect); err != nil {
				return err
			}
		} else {
			g.DeleteView(filterIndicatorView)
		}
	}

	// Focus priority: popup > dialog > panel.
	// When popup is visible, layoutToolPopup handles focus.
	// When dialog is active (no popup), restore focus to the dialog view.
	// Otherwise, focus the active panel.
	if a.hasPopup() {
		// layoutToolPopup manages focus — skip here.
	} else if a.HasActiveDialog() {
		viewName := a.dialogFocusView()
		if viewName != "" {
			if _, err := g.SetCurrentView(viewName); err != nil && !isUnknownView(err) {
				return err
			}
			if a.dialog.Kind != DialogWorktreeChooser {
				g.Cursor = true
			}
		}
	} else {
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
	if a.editor == nil {
		a.editor = &inputEditor{app: a}
	}
	v.Editor = a.editor
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

	if a.scroll.IsActive() {
		v.Editable = false
		a.renderScrollContent(v)
	} else {
		a.renderPreview(v, items, previewW, previewH)
		// Scroll offset for mouse scroll
		v.SetOrigin(0, a.fullscreen.ScrollY())
	}

	// Status bar
	v2, err := g.SetView("fullscreen-bar", l.Options.X0, l.Options.Y0, l.Options.X1, l.Options.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v2.Frame = false
	v2.Clear()

	if a.scroll.IsActive() {
		a.renderScrollStatusBar(v2, items[targetIdx].Name)
	} else {
		fsHints := a.keyRegistry.HintsForScope(keymap.ScopeFullScreen)
		var fsBar string
		for _, h := range fsHints {
			if fsBar != "" {
				fsBar += "  "
			}
			fsBar += presentation.StyledKey(h.HintKeyLabel(), h.HintLabel)
		}
		fmt.Fprintf(v2, " %s %s %s",
			items[targetIdx].Name,
			presentation.FgDimGray+presentation.IconSep+presentation.Reset,
			fsBar)
	}

	g.Cursor = true
	if _, err := g.SetCurrentView("main"); err != nil && !isUnknownView(err) {
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

	// Resolve current item from tree node
	node := a.currentNode()
	if node == nil {
		v.Title = " Preview "
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  Select a session or press "+
			presentation.Reset+presentation.Bold+"n"+presentation.Reset+
			presentation.Dim+" to create one."+presentation.Reset)
		return
	}

	// Project node: show project info, no preview
	if node.Kind == ProjectNode {
		v.Title = fmt.Sprintf(" %s ", node.Project.Name)
		fmt.Fprintln(v, "")
		fmt.Fprintf(v, "  %s%s%s\n", presentation.Bold, node.Project.Name, presentation.Reset)
		fmt.Fprintf(v, "  %s%s%s\n", presentation.Dim, node.Project.Path, presentation.Reset)
		sessCount := len(node.Project.Sessions)
		if node.Project.PM != nil {
			sessCount++
		}
		fmt.Fprintf(v, "  %s%d session(s)%s\n", presentation.Dim, sessCount, presentation.Reset)
		return
	}

	item := *node.Session
	if session.IsWorktreePath(item.Path) {
		v.Title = fmt.Sprintf(" [worktree] %s ", item.Name)
	} else {
		v.Title = fmt.Sprintf(" %s ", item.Name)
	}

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
	paneCursorX := a.preview.CursorX()
	paneCursorY := a.preview.CursorY()
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
		v.SetCursor(paneCursorX, paneCursorY)
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
	setRoundedFrame(v)
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
	a.dialog.Kind = DialogRename
	return true
}

// closeRenameInput removes the rename input view and restores focus.
func (a *App) closeRenameInput(g *gocui.Gui) {
	a.dialog.RenameID = ""
	a.dialog.Kind = DialogNone
	g.DeleteView("rename-input")
	g.Cursor = false
	if _, err := g.SetCurrentView("sessions"); err != nil && !isUnknownView(err) {
		// Fallback: sessions view may not exist in some modes.
		_ = err
	}
}

// showWorktreeDialog creates the worktree input views (branch name + prompt).
// Returns true if all views were created successfully and dialog is active.
func (a *App) showWorktreeDialog(g *gocui.Gui) bool {
	maxX, maxY := g.Size()
	w := 50
	if w > maxX-4 {
		w = maxX - 4
	}
	x0 := (maxX - w) / 2
	branchY0 := maxY/2 - 6
	if branchY0 < 1 {
		branchY0 = 1
	}
	branchY1 := branchY0 + 2

	// Branch name input (1 line)
	v, err := g.SetView("worktree-branch", x0, branchY0, x0+w, branchY1, 0)
	if err != nil && !isUnknownView(err) {
		return false
	}
	v.Title = " Branch "
	v.Editable = true
	v.Editor = gocui.DefaultEditor
	setRoundedFrame(v)

	// Prompt input (6 lines)
	promptY0 := branchY1 + 1
	promptY1 := promptY0 + 7
	if promptY1 >= maxY-2 {
		promptY1 = maxY - 3
	}

	v2, err := g.SetView("worktree-prompt", x0, promptY0, x0+w, promptY1, 0)
	if err != nil && !isUnknownView(err) {
		a.closeWorktreeDialog(g)
		return false
	}
	v2.Title = " Prompt "
	v2.Editable = true
	v2.Editor = gocui.DefaultEditor
	v2.Wrap = true
	v2.TextArea.AutoWrap = true
	v2.TextArea.AutoWrapWidth = w - 2 // view 幅からフレーム分を引く
	setRoundedFrame(v2)

	// Hint bar (frameless)
	hintY0 := promptY1
	hintY1 := hintY0 + 2
	if hintY1 >= maxY {
		hintY1 = maxY - 1
	}
	v3, err := g.SetView("worktree-hint", x0, hintY0, x0+w, hintY1, 0)
	if err != nil && !isUnknownView(err) {
		a.closeWorktreeDialog(g)
		return false
	}
	v3.Frame = false
	v3.Clear()
	fmt.Fprint(v3, " "+presentation.StyledKey("Enter", "create")+"  "+
		presentation.StyledKey("Tab", "switch")+"  "+
		presentation.StyledKey("Esc", "cancel"))

	if _, err := g.SetCurrentView("worktree-branch"); err != nil && !isUnknownView(err) {
		a.closeWorktreeDialog(g)
		return false
	}
	g.Cursor = true
	a.dialog.Kind = DialogWorktree
	return true
}

// closeWorktreeDialog removes all worktree dialog views and restores focus.
func (a *App) closeWorktreeDialog(g *gocui.Gui) {
	a.dialog.Kind = DialogNone
	a.dialog.ActiveField = ""
	g.DeleteView("worktree-branch")
	g.DeleteView("worktree-prompt")
	g.DeleteView("worktree-hint")
	g.Cursor = false
	if _, err := g.SetCurrentView("sessions"); err != nil && !isUnknownView(err) {
		_ = err
	}
}

// showWorktreeChooser creates a list view for selecting an existing worktree.
// The last item is always "+ New worktree".
func (a *App) showWorktreeChooser(g *gocui.Gui, items []WorktreeInfo) bool {
	a.dialog.WorktreeItems = items
	a.dialog.WorktreeCursor = 0

	maxX, maxY := g.Size()
	totalItems := len(items) + 1 // +1 for "New worktree"
	w := 50
	if w > maxX-4 {
		w = maxX - 4
	}
	h := totalItems + 2 // +2 for frame
	if h > maxY/2 {
		h = maxY / 2
	}
	x0 := (maxX - w) / 2
	y0 := (maxY - h) / 2
	if y0 < 1 {
		y0 = 1
	}

	v, err := g.SetView("worktree-chooser", x0, y0, x0+w, y0+h, 0)
	if err != nil && !isUnknownView(err) {
		return false
	}
	v.Title = " Select Worktree "
	v.Editable = false
	v.Highlight = true
	v.SelBgColor = gocui.Get256Color(24)
	v.SelFgColor = gocui.ColorWhite
	setRoundedFrame(v)
	renderWorktreeChooser(v, items, a.dialog.WorktreeCursor)

	if _, err := g.SetCurrentView("worktree-chooser"); err != nil && !isUnknownView(err) {
		return false
	}
	a.dialog.Kind = DialogWorktreeChooser
	return true
}

// closeWorktreeChooser removes the chooser view and restores focus.
func (a *App) closeWorktreeChooser(g *gocui.Gui) {
	a.dialog.Kind = DialogNone
	a.dialog.WorktreeItems = nil
	g.DeleteView("worktree-chooser")
	if _, err := g.SetCurrentView("sessions"); err != nil && !isUnknownView(err) {
		_ = err
	}
}

// showWorktreeResumePrompt creates a prompt-only dialog for an existing worktree.
func (a *App) showWorktreeResumePrompt(g *gocui.Gui, worktreeName string) bool {
	maxX, maxY := g.Size()
	w := 50
	if w > maxX-4 {
		w = maxX - 4
	}
	x0 := (maxX - w) / 2
	promptY0 := maxY/2 - 4
	if promptY0 < 1 {
		promptY0 = 1
	}
	promptY1 := promptY0 + 7
	if promptY1 >= maxY-2 {
		promptY1 = maxY - 3
	}

	v, err := g.SetView("worktree-resume-prompt", x0, promptY0, x0+w, promptY1, 0)
	if err != nil && !isUnknownView(err) {
		return false
	}
	v.Title = fmt.Sprintf(" Prompt (%s) ", worktreeName)
	v.Editable = true
	v.Editor = gocui.DefaultEditor
	v.Wrap = true
	v.TextArea.AutoWrap = true
	v.TextArea.AutoWrapWidth = w - 2
	setRoundedFrame(v)

	hintY0 := promptY1
	hintY1 := hintY0 + 2
	if hintY1 >= maxY {
		hintY1 = maxY - 1
	}
	v2, err := g.SetView("worktree-resume-hint", x0, hintY0, x0+w, hintY1, 0)
	if err != nil && !isUnknownView(err) {
		a.closeWorktreeResumePrompt(g)
		return false
	}
	v2.Frame = false
	v2.Clear()
	fmt.Fprint(v2, " "+presentation.StyledKey("Enter", "launch")+"  "+
		presentation.StyledKey("Esc", "cancel"))

	if _, err := g.SetCurrentView("worktree-resume-prompt"); err != nil && !isUnknownView(err) {
		a.closeWorktreeResumePrompt(g)
		return false
	}
	g.Cursor = true
	a.dialog.Kind = DialogWorktreeResume
	return true
}

// closeWorktreeResumePrompt removes the resume prompt dialog and restores focus.
func (a *App) closeWorktreeResumePrompt(g *gocui.Gui) {
	a.dialog.Kind = DialogNone
	a.dialog.SelectedPath = ""
	g.DeleteView("worktree-resume-prompt")
	g.DeleteView("worktree-resume-hint")
	g.Cursor = false
	if _, err := g.SetCurrentView("sessions"); err != nil && !isUnknownView(err) {
		_ = err
	}
}
