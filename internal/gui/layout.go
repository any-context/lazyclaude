package gui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/any-context/lazyclaude/internal/gui/chooser"
	"github.com/any-context/lazyclaude/internal/gui/keymap"
	"github.com/any-context/lazyclaude/internal/gui/presentation"
	"github.com/any-context/lazyclaude/internal/profile"
	"github.com/any-context/lazyclaude/internal/session"
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

	// Re-sync every layout after the first so that an out-of-band
	// tree rebuild (session GC, project removed without going through
	// DeleteSession, remote mirror window added/removed) cannot leave
	// pluginState.projectDir / remoteDisabled pointing at a project
	// that has just slid out from under the cursor. syncPluginProject
	// is idempotent on the same node because it short-circuits when
	// projectPath equals the cached projectDir, so the cost is one
	// cursor lookup per frame when nothing has changed.
	a.syncPluginProject()

	// Detect terminal resize -> clear preview and scroll render caches
	if maxX != a.lastWidth || maxY != a.lastHeight {
		a.preview.Invalidate()
		a.scrollRender = scrollRenderCache{}
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
		clamped := false
		if a.cursor >= len(nodes) {
			a.cursor = len(nodes) - 1
			clamped = true
		}
		if a.cursor < 0 {
			a.cursor = 0
			clamped = true
		}
		if clamped {
			// The layout-level syncPluginProject call (a.layout) ran
			// BEFORE this clamp, so re-sync again now that the cursor
			// actually points at a new node. Without this, the cached
			// projectDir would still reference the pre-clamp row and
			// the next write key press would hit stale context.
			a.syncPluginProject()
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
		a.renderPreview(v3, a.cachedSessionItems, previewW, previewH)
	}

	// Options bar (bottom, frameless) — dynamic per focused panel
	v4, err := g.SetView("options", l.Options.X0, l.Options.Y0, l.Options.X1, l.Options.Y1, 0)
	if err != nil && !isUnknownView(err) {
		return err
	}
	v4.Frame = false
	v4.Clear()
	optionsText := a.dispatcher.ActiveOptionsBar(a)
	connText := a.formatConnectionStatus()
	if optionsText != "" || connText != "" {
		barWidth := l.Options.Width() - 1 // inner width
		a.renderOptionsBarWithStatus(v4, optionsText, connText, barWidth)
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
			// Chooser views use Highlight for selection; text-input views need cursor.
			if isChooserView(viewName) {
				g.Cursor = false
			} else {
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

	// Render preview content (same pipeline as split-panel mode)
	items := a.cachedSessionItems
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
		// Skip expensive v.Clear() + renderScrollContent when scroll render
		// state has not changed. This prevents unnecessary redraws during
		// active Claude Code output that would cause visual artifacts.
		cursorY := a.scroll.CursorY()
		selecting := a.scroll.IsSelecting()
		selStart, selEnd := a.scroll.SelectionRange()
		w := v.InnerWidth()
		rc := &a.scrollRender
		if rc.linesVersion != a.scroll.LinesVersion() ||
			rc.cursorY != cursorY ||
			rc.selecting != selecting ||
			rc.selStart != selStart ||
			rc.selEnd != selEnd ||
			rc.width != w {
			v.Clear()
			a.renderScrollContent(v)
			// Cache the rendered state. During the loading phase (lines
			// empty), linesVersion is 0 and the loading message is static,
			// so caching here correctly skips redundant loading redraws.
			// When SetLines arrives, linesVersion bumps and triggers the
			// next real render.
			rc.linesVersion = a.scroll.LinesVersion()
			rc.cursorY = cursorY
			rc.selecting = selecting
			rc.selStart = selStart
			rc.selEnd = selEnd
			rc.width = w
		}
	} else {
		v.Clear()
		a.renderPreview(v, items, previewW, previewH)
		// Scroll offset for mouse scroll
		v.SetOrigin(0, a.fullscreen.ScrollY())
		// Invalidate scroll render cache so next scroll mode entry re-renders
		a.scrollRender = scrollRenderCache{}
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


// formatConnectionStatus returns a styled string showing remote connection state.
// Returns "" if no remote connections are configured.
func (a *App) formatConnectionStatus() string {
	if a.connectionStatus == nil {
		return ""
	}
	statuses := a.connectionStatus()
	if len(statuses) == 0 {
		return ""
	}
	var parts []string
	for _, s := range statuses {
		switch s.State {
		case "connected":
			if s.VersionMismatch {
				parts = append(parts, presentation.FgYellow+s.Host+" (version mismatch)"+presentation.Reset)
			} else {
				parts = append(parts, presentation.FgGreen+s.Host+presentation.Reset)
			}
		case "reconnecting":
			parts = append(parts, presentation.FgYellow+s.Host+" (reconnecting...)"+presentation.Reset)
		case "error":
			parts = append(parts, presentation.FgRed+s.Host+" (offline)"+presentation.Reset)
		case "connecting":
			parts = append(parts, presentation.FgYellow+s.Host+" (connecting...)"+presentation.Reset)
		default:
			// disconnected or unknown — don't display
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// renderOptionsBarWithStatus writes the options bar with connection status
// right-aligned. The status is separated from hints by padding.
func (a *App) renderOptionsBarWithStatus(v *gocui.View, options, status string, barWidth int) {
	if status == "" {
		fmt.Fprint(v, options)
		return
	}
	if options == "" {
		// Right-align the status text.
		pad := barWidth - printableLen(status) - 1
		if pad < 0 {
			pad = 0
		}
		fmt.Fprintf(v, "%*s%s", pad, "", status)
		return
	}
	// Both present: left-align options, right-align status.
	optLen := printableLen(options)
	statusLen := printableLen(status)
	gap := barWidth - optLen - statusLen
	if gap < 2 {
		// Not enough space — just show options.
		fmt.Fprint(v, options)
		return
	}
	fmt.Fprintf(v, "%s%*s%s", options, gap, "", status)
}

// printableLen returns the number of printable characters in a string,
// stripping ANSI escape sequences.
func printableLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		n++
	}
	return n
}

// showConnectDialog creates a small input view for entering a remote hostname.
// Returns false if the view could not be created.
func (a *App) showConnectDialog(g *gocui.Gui) bool {
	maxX, maxY := g.Size()
	w := 40
	if w > maxX-4 {
		w = maxX - 4
	}
	x0 := (maxX - w) / 2
	y0 := maxY/2 - 1
	x1 := x0 + w
	y1 := y0 + 2

	v, err := g.SetView("connect-input", x0, y0, x1, y1, 0)
	if err != nil && !isUnknownView(err) {
		return false
	}
	setRoundedFrame(v)
	v.Title = " Host "
	v.Editable = true
	v.Editor = gocui.DefaultEditor
	v.TextArea.Clear()
	v.RenderTextArea()
	if _, err := g.SetCurrentView("connect-input"); err != nil && !isUnknownView(err) {
		return false
	}
	g.Cursor = true
	a.dialog.Kind = DialogConnect
	return true
}

// closeConnectDialog removes the connect input view and restores focus.
func (a *App) closeConnectDialog(g *gocui.Gui) {
	a.dialog.Kind = DialogNone
	g.DeleteView("connect-input")
	g.Cursor = false
	if _, err := g.SetCurrentView("sessions"); err != nil && !isUnknownView(err) {
		_ = err
	}
}

// sanitizePrompt strips control characters, ANSI escape sequences, and
// newlines from a server-supplied SSH prompt string. The result is capped
// to maxPromptLen printable characters for safe display in a gocui title.
func sanitizePrompt(s string) string {
	const maxPromptLen = 60
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		// Replace control characters and newlines with space.
		if r < 0x20 || r == 0x7f {
			if b.Len() > 0 {
				b.WriteRune(' ')
			}
			continue
		}
		if b.Len() >= maxPromptLen {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// showAskpassDialog creates a masked input view for SSH password entry.
// The prompt string is displayed as the view title.
func (a *App) showAskpassDialog(g *gocui.Gui, prompt string) bool {
	maxX, maxY := g.Size()
	w := 50
	if w > maxX-4 {
		w = maxX - 4
	}
	x0 := (maxX - w) / 2
	y0 := maxY/2 - 1
	x1 := x0 + w
	y1 := y0 + 2

	v, err := g.SetView("askpass-input", x0, y0, x1, y1, 0)
	if err != nil && !isUnknownView(err) {
		return false
	}
	setRoundedFrame(v)

	// Sanitize server-supplied prompt: strip control characters and
	// ANSI escapes, collapse to a single printable line, and cap length.
	// SSH prompt strings are server-controlled (keyboard-interactive).
	title := sanitizePrompt(prompt)
	if title == "" {
		title = "Password"
	}
	v.Title = " " + title + " "
	v.Editable = true
	v.Editor = gocui.DefaultEditor
	v.Mask = "*"
	v.TextArea.Clear()
	v.RenderTextArea()
	if _, err := g.SetCurrentView("askpass-input"); err != nil && !isUnknownView(err) {
		return false
	}
	g.Cursor = true
	a.dialog.Kind = DialogAskpass
	return true
}

// closeAskpassDialog removes the askpass input view and restores focus.
func (a *App) closeAskpassDialog(g *gocui.Gui) {
	a.dialog.Kind = DialogNone
	g.DeleteView("askpass-input")
	g.Cursor = false
	_, _ = g.SetCurrentView("sessions")
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

// showWorktreeDialog creates the worktree input views (branch + prompt + profile + options).
// Returns true if all views were created successfully and dialog is active.
func (a *App) showWorktreeDialog(g *gocui.Gui) bool {
	items := a.sessionProfileItems()
	defaultIdx := chooser.IndexOfDefault(items)
	a.dialog.ProfileItems = items
	a.dialog.ProfileCursor = defaultIdx
	a.dialog.OptionsText = ""

	maxX, maxY := g.Size()
	w := 50
	if w > maxX-4 {
		w = maxX - 4
	}
	x0 := (maxX - w) / 2

	numItems := len(items)
	if numItems < 1 {
		numItems = 1
	}
	profileH := numItems + 2
	if profileH > 8 {
		profileH = 8
	}
	// Compute a top-aligned start that centres the whole stack vertically.
	// Layout: branch(3) + gap + prompt(6) + gap + profile(profileH) + gap + options(3) + hint(2)
	totalH := 3 + 1 + 6 + 1 + profileH + 1 + 3 + 2
	startY := (maxY - totalH) / 2
	if startY < 1 {
		startY = 1
	}

	branchY0 := startY
	branchY1 := branchY0 + 2

	// Branch name input (1 visible line)
	v, err := g.SetView("worktree-branch", x0, branchY0, x0+w, branchY1, 0)
	if err != nil && !isUnknownView(err) {
		return false
	}
	v.Title = " Branch "
	v.Editable = true
	v.Editor = gocui.DefaultEditor
	v.Clear()
	v.TextArea.Clear()
	v.RenderTextArea()
	setRoundedFrame(v)

	// Prompt input (4 visible lines)
	promptY0 := branchY1 + 1
	promptY1 := promptY0 + 5
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
	v2.Clear()
	v2.TextArea.Clear()
	v2.TextArea.AutoWrap = true
	v2.TextArea.AutoWrapWidth = w - 2
	v2.RenderTextArea()
	setRoundedFrame(v2)

	// Profile chooser
	profileY0 := promptY1 + 1
	profileY1 := profileY0 + profileH
	if profileY1 >= maxY-2 {
		profileY1 = maxY - 3
	}

	v4, err := g.SetView("worktree-profile-chooser", x0, profileY0, x0+w, profileY1, 0)
	if err != nil && !isUnknownView(err) {
		a.closeWorktreeDialog(g)
		return false
	}
	v4.Title = " Profile "
	v4.Editable = false
	v4.Highlight = false
	setRoundedFrame(v4)
	renderProfileChooser(v4, a.dialog.ProfileItems, a.dialog.ProfileCursor)

	// Options input (1 visible line)
	optionsY0 := profileY1 + 1
	optionsY1 := optionsY0 + 2
	if optionsY1 >= maxY-2 {
		optionsY1 = maxY - 3
	}

	v5, err := g.SetView("worktree-options", x0, optionsY0, x0+w, optionsY1, 0)
	if err != nil && !isUnknownView(err) {
		a.closeWorktreeDialog(g)
		return false
	}
	v5.Title = " Options "
	v5.Editable = true
	v5.Editor = gocui.DefaultEditor
	v5.Clear()
	v5.TextArea.Clear()
	v5.RenderTextArea()
	setRoundedFrame(v5)

	// Hint bar (frameless)
	hintY0 := optionsY1
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
	a.dialog.ActiveField = "worktree-branch"
	a.dialog.Kind = DialogWorktree
	return true
}

// closeWorktreeDialog removes all worktree dialog views and restores focus.
func (a *App) closeWorktreeDialog(g *gocui.Gui) {
	a.dialog.Kind = DialogNone
	a.dialog.ActiveField = ""
	a.dialog.ProfileItems = nil
	a.dialog.OptionsText = ""
	g.DeleteView("worktree-branch")
	g.DeleteView("worktree-prompt")
	g.DeleteView("worktree-profile-chooser")
	g.DeleteView("worktree-options")
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

// showWorktreeResumePrompt creates the resume dialog (prompt + profile + options).
func (a *App) showWorktreeResumePrompt(g *gocui.Gui, worktreeName string) bool {
	items := a.sessionProfileItems()
	defaultIdx := chooser.IndexOfDefault(items)
	a.dialog.ProfileItems = items
	a.dialog.ProfileCursor = defaultIdx
	a.dialog.OptionsText = ""

	maxX, maxY := g.Size()
	w := 50
	if w > maxX-4 {
		w = maxX - 4
	}
	x0 := (maxX - w) / 2

	numItems := len(items)
	if numItems < 1 {
		numItems = 1
	}
	profileH := numItems + 2
	if profileH > 8 {
		profileH = 8
	}
	// Layout: prompt(6) + gap + profile(profileH) + gap + options(3) + hint(2)
	totalH := 6 + 1 + profileH + 1 + 3 + 2
	startY := (maxY - totalH) / 2
	if startY < 1 {
		startY = 1
	}

	promptY0 := startY
	promptY1 := promptY0 + 5
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
	v.TextArea.Clear()
	v.TextArea.AutoWrap = true
	v.TextArea.AutoWrapWidth = w - 2
	v.RenderTextArea()
	setRoundedFrame(v)

	// Profile chooser
	profileY0 := promptY1 + 1
	profileY1 := profileY0 + profileH
	if profileY1 >= maxY-2 {
		profileY1 = maxY - 3
	}

	v3, err := g.SetView("worktree-resume-profile-chooser", x0, profileY0, x0+w, profileY1, 0)
	if err != nil && !isUnknownView(err) {
		a.closeWorktreeResumePrompt(g)
		return false
	}
	v3.Title = " Profile "
	v3.Editable = false
	v3.Highlight = false
	setRoundedFrame(v3)
	renderProfileChooser(v3, a.dialog.ProfileItems, a.dialog.ProfileCursor)

	// Options input
	optionsY0 := profileY1 + 1
	optionsY1 := optionsY0 + 2
	if optionsY1 >= maxY-2 {
		optionsY1 = maxY - 3
	}

	v4, err := g.SetView("worktree-resume-options", x0, optionsY0, x0+w, optionsY1, 0)
	if err != nil && !isUnknownView(err) {
		a.closeWorktreeResumePrompt(g)
		return false
	}
	v4.Title = " Options "
	v4.Editable = true
	v4.Editor = gocui.DefaultEditor
	v4.Clear()
	v4.TextArea.Clear()
	v4.RenderTextArea()
	setRoundedFrame(v4)

	hintY0 := optionsY1
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
		presentation.StyledKey("Tab", "switch")+"  "+
		presentation.StyledKey("Esc", "cancel"))

	if _, err := g.SetCurrentView("worktree-resume-prompt"); err != nil && !isUnknownView(err) {
		a.closeWorktreeResumePrompt(g)
		return false
	}
	g.Cursor = true
	a.dialog.ActiveField = "worktree-resume-prompt"
	a.dialog.Kind = DialogWorktreeResume
	return true
}

// showConnectChooser creates a list view for selecting an SSH host.
// The last item is always "+ Manual input".
func (a *App) showConnectChooser(g *gocui.Gui, hosts []string) bool {
	a.dialog.ConnectHosts = hosts
	a.dialog.ConnectCursor = 0

	maxX, maxY := g.Size()
	totalItems := len(hosts) + 1 // +1 for "Manual input"
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

	v, err := g.SetView("connect-chooser", x0, y0, x0+w, y0+h, 0)
	if err != nil && !isUnknownView(err) {
		return false
	}
	v.Title = " Connect to Host "
	v.Editable = false
	v.Highlight = true
	v.SelBgColor = gocui.Get256Color(24)
	v.SelFgColor = gocui.ColorWhite
	setRoundedFrame(v)
	renderConnectChooser(v, hosts, a.dialog.ConnectCursor)

	if _, err := g.SetCurrentView("connect-chooser"); err != nil && !isUnknownView(err) {
		return false
	}
	a.dialog.Kind = DialogConnectChooser
	return true
}

// closeConnectChooser removes the connect chooser view and restores focus.
func (a *App) closeConnectChooser(g *gocui.Gui) {
	a.dialog.Kind = DialogNone
	a.dialog.ConnectHosts = nil
	g.DeleteView("connect-chooser")
	if _, err := g.SetCurrentView("sessions"); err != nil && !isUnknownView(err) {
		_ = err
	}
}

// closeWorktreeResumePrompt removes the resume prompt dialog and restores focus.
func (a *App) closeWorktreeResumePrompt(g *gocui.Gui) {
	a.dialog.Kind = DialogNone
	a.dialog.ActiveField = ""
	a.dialog.SelectedPath = ""
	a.dialog.ProfileItems = nil
	a.dialog.OptionsText = ""
	g.DeleteView("worktree-resume-prompt")
	g.DeleteView("worktree-resume-hint")
	g.DeleteView("worktree-resume-profile-chooser")
	g.DeleteView("worktree-resume-options")
	g.Cursor = false
	if _, err := g.SetCurrentView("sessions"); err != nil && !isUnknownView(err) {
		_ = err
	}
}

// loadProfileItems loads profiles from $HOME/.lazyclaude/config.json and
// converts them to chooser.Item values. The effective default profile is
// marked with Default=true. Falls back to a single builtin-default item if
// the config file is absent or cannot be parsed.
func loadProfileItems() []chooser.Item {
	home, err := os.UserHomeDir()
	if err != nil {
		return []chooser.Item{{Label: profile.BuiltinDefaultName, Default: true, Data: profile.BuiltinDefaultName}}
	}
	configPath := filepath.Join(home, ".lazyclaude", "config.json")
	_, profiles, loadErr := profile.Load(configPath)
	if loadErr != nil || len(profiles) == 0 {
		debugLog("loadProfileItems: %v", loadErr)
		return []chooser.Item{{Label: profile.BuiltinDefaultName, Default: true, Data: profile.BuiltinDefaultName}}
	}
	defProfile, _ := profile.ResolveDefault(profiles)
	items := make([]chooser.Item, len(profiles))
	for i, p := range profiles {
		items[i] = chooser.Item{
			Label:   p.Name,
			Default: p.Name == defProfile.Name,
			Data:    p.Name,
		}
	}
	return items
}

// sessionProfileItems returns profile chooser items for the session provider.
// When a.sessions.ProfileItems() returns a non-empty list, that list is used
// so that the session Manager and the GUI always share the same profile
// snapshot (re-read from disk and synced into the Manager each time a dialog
// opens). Falls back to the package-level loadProfileItems() when sessions is
// nil or returns empty (e.g. mock providers in headless tests).
func (a *App) sessionProfileItems() []chooser.Item {
	if a.sessions != nil {
		if items := a.sessions.ProfileItems(); len(items) > 0 {
			return items
		}
	}
	return loadProfileItems()
}

// showProfileDialog opens the standalone profile chooser dialog used by the
// n, N, and P actions. confirmKind specifies which session type to create
// ("session", "session_cwd", "pm_session") and sessionPath is the path to
// pass on confirm (empty for session_cwd, projectRoot for pm_session).
func (a *App) showProfileDialog(g *gocui.Gui, confirmKind, sessionPath string) bool {
	items := a.sessionProfileItems()
	defaultIdx := chooser.IndexOfDefault(items)
	a.dialog.ProfileItems = items
	a.dialog.ProfileCursor = defaultIdx
	a.dialog.OptionsText = ""
	a.dialog.ProfileConfirmKind = confirmKind
	a.dialog.ProfileSessionPath = sessionPath
	a.dialog.ActiveField = "profile-chooser"

	maxX, maxY := g.Size()
	w := 50
	if w > maxX-4 {
		w = maxX - 4
	}
	x0 := (maxX - w) / 2

	numItems := len(items)
	if numItems < 1 {
		numItems = 1
	}
	chooserH := numItems + 2
	if chooserH > 10 {
		chooserH = 10
	}
	// Total height: chooserH + 1(gap) + 3(options) + 2(hint)
	totalH := chooserH + 1 + 3 + 2
	startY := (maxY - totalH) / 2
	if startY < 1 {
		startY = 1
	}

	chooserY0 := startY
	chooserY1 := chooserY0 + chooserH
	if chooserY1 >= maxY-2 {
		chooserY1 = maxY - 3
	}

	v, err := g.SetView("profile-chooser", x0, chooserY0, x0+w, chooserY1, 0)
	if err != nil && !isUnknownView(err) {
		return false
	}
	v.Title = " Profile "
	v.Editable = false
	v.Highlight = false
	setRoundedFrame(v)
	renderProfileChooser(v, a.dialog.ProfileItems, a.dialog.ProfileCursor)

	optionsY0 := chooserY1 + 1
	optionsY1 := optionsY0 + 2
	if optionsY1 >= maxY-2 {
		optionsY1 = maxY - 3
	}

	v2, err := g.SetView("profile-options", x0, optionsY0, x0+w, optionsY1, 0)
	if err != nil && !isUnknownView(err) {
		a.closeProfileDialog(g)
		return false
	}
	v2.Title = " Options "
	v2.Editable = true
	v2.Editor = gocui.DefaultEditor
	v2.Clear()
	v2.TextArea.Clear()
	v2.RenderTextArea()
	setRoundedFrame(v2)

	hintY0 := optionsY1
	hintY1 := hintY0 + 2
	if hintY1 >= maxY {
		hintY1 = maxY - 1
	}
	v3, err := g.SetView("profile-hint", x0, hintY0, x0+w, hintY1, 0)
	if err != nil && !isUnknownView(err) {
		a.closeProfileDialog(g)
		return false
	}
	v3.Frame = false
	v3.Clear()
	fmt.Fprint(v3, " "+presentation.StyledKey("Enter", "launch")+"  "+
		presentation.StyledKey("Tab", "switch")+"  "+
		presentation.StyledKey("Esc", "cancel"))

	if _, err := g.SetCurrentView("profile-chooser"); err != nil && !isUnknownView(err) {
		a.closeProfileDialog(g)
		return false
	}
	g.Cursor = false
	a.dialog.Kind = DialogProfile
	return true
}

// closeProfileDialog removes all profile dialog views and resets dialog state.
func (a *App) closeProfileDialog(g *gocui.Gui) {
	a.dialog.Kind = DialogNone
	a.dialog.ActiveField = ""
	a.dialog.ProfileItems = nil
	a.dialog.OptionsText = ""
	a.dialog.ProfileConfirmKind = ""
	a.dialog.ProfileSessionPath = ""
	g.DeleteView("profile-chooser")
	g.DeleteView("profile-options")
	g.DeleteView("profile-hint")
	g.Cursor = false
	if _, err := g.SetCurrentView("sessions"); err != nil && !isUnknownView(err) {
		_ = err
	}
}

// showRemoteProfileErrorDialog displays an error panel for a malformed remote
// config.json (UI4). Only Esc is accepted; all other keys are suppressed.
func (a *App) showRemoteProfileErrorDialog(g *gocui.Gui, host, reason string) bool {
	msg := fmt.Sprintf("Failed to parse $HOME/.lazyclaude/config.json on %s: %s", host, reason)
	a.dialog.RemoteProfileErrorMsg = msg

	maxX, maxY := g.Size()
	w := 60
	if w > maxX-4 {
		w = maxX - 4
	}
	x0 := (maxX - w) / 2

	errorH := 5 // 3 inner lines + 2 frame
	startY := (maxY - errorH - 2) / 2
	if startY < 1 {
		startY = 1
	}

	v, err := g.SetView("remote-profile-error", x0, startY, x0+w, startY+errorH, 0)
	if err != nil && !isUnknownView(err) {
		return false
	}
	v.Title = " Profile Error "
	v.Editable = false
	v.Wrap = true
	setRoundedFrame(v)
	v.Clear()
	fmt.Fprintln(v, msg)

	hintY0 := startY + errorH
	hintY1 := hintY0 + 2
	if hintY1 >= maxY {
		hintY1 = maxY - 1
	}
	v2, err := g.SetView("remote-profile-error-hint", x0, hintY0, x0+w, hintY1, 0)
	if err != nil && !isUnknownView(err) {
		a.closeRemoteProfileErrorDialog(g)
		return false
	}
	v2.Frame = false
	v2.Clear()
	fmt.Fprint(v2, " "+presentation.StyledKey("Esc", "close"))

	if _, err := g.SetCurrentView("remote-profile-error"); err != nil && !isUnknownView(err) {
		a.closeRemoteProfileErrorDialog(g)
		return false
	}
	g.Cursor = false
	a.dialog.Kind = DialogRemoteProfileError
	return true
}

// closeRemoteProfileErrorDialog removes the error dialog and resets state.
func (a *App) closeRemoteProfileErrorDialog(g *gocui.Gui) {
	a.dialog.Kind = DialogNone
	a.dialog.RemoteProfileErrorMsg = ""
	g.DeleteView("remote-profile-error")
	g.DeleteView("remote-profile-error-hint")
	g.Cursor = false
	if _, err := g.SetCurrentView("sessions"); err != nil && !isUnknownView(err) {
		_ = err
	}
}
