package gui

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/gui/keymap"
	"github.com/any-context/lazyclaude/internal/gui/presentation"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/charmbracelet/x/ansi"
	"github.com/jesseduffield/gocui"
	"github.com/mattn/go-runewidth"
)

const serverLogPath = "/tmp/lazyclaude/server.log"
const serverLogLines = 30

// sessionStatusIcon returns the status icon for a session item.
func sessionStatusIcon(item *SessionItem) string {
	// tmux-level status takes priority for non-running states.
	switch item.Status {
	case "Dead":
		return " " + presentation.IconDead
	case "Orphan":
		return " " + presentation.IconOrphan
	case "Detached":
		return " " + presentation.IconDetached
	}

	// For running sessions, use the 5-stage activity state.
	switch item.Activity {
	case model.ActivityRunning:
		return " " + presentation.IconRunning
	case model.ActivityNeedsInput:
		return " " + presentation.IconNeedsInput
	case model.ActivityIdle:
		return " " + presentation.IconIdle
	case model.ActivityError:
		return " " + presentation.IconError
	}

	// Fallback: running session with no activity info yet (e.g. after restart).
	if item.Status == "Running" {
		return " " + presentation.IconUnknown
	}
	return ""
}

// sessionDisplayName returns the decorated name for a session item.
func sessionDisplayName(item *SessionItem) string {
	name := item.Name
	if session.IsWorktreePath(item.Path) {
		name = presentation.IconWorktree + " " + name
	}
	if item.Role == "pm" {
		name = presentation.IconPM + " " + name
	}
	return name
}

// renderTree writes the project/session tree to a gocui view.
func renderTree(v *gocui.View, nodes []TreeNode, cursor int) {
	if len(nodes) == 0 {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  No sessions"+presentation.Reset)
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, "  Press "+presentation.Bold+"n"+presentation.Reset+" to create")
		return
	}

	for _, node := range nodes {
		switch node.Kind {
		case ProjectNode:
			expandIcon := presentation.IconProjectCollapsed
			if node.Project.Expanded {
				expandIcon = presentation.IconProjectExpanded
			}
			projectLabel := node.Project.Name
			if node.Project.Host != "" {
				projectLabel = presentation.FgPurple + node.Project.Host + presentation.Reset + ": " + projectLabel
			}
			fmt.Fprintf(v, " %s %s\n", expandIcon, projectLabel)

		case SessionNode:
			name := sessionDisplayName(node.Session)
			icon := sessionStatusIcon(node.Session)
			fmt.Fprintf(v, "   %-18s%s\n", name, icon)
		}
	}

	v.SetCursor(0, cursor)
}

// renderWorktreeChooser writes the worktree selection list to a gocui view.
func renderWorktreeChooser(v *gocui.View, items []WorktreeInfo, cursor int) {
	v.Clear()
	for _, item := range items {
		branch := item.Branch
		if branch == "" {
			branch = "detached"
		}
		fmt.Fprintf(v, " %s %s (%s)\n",
			presentation.IconWorktree, item.Name,
			presentation.Dim+branch+presentation.Reset)
	}
	// "New worktree" entry
	fmt.Fprintf(v, " %s+ New worktree%s\n", presentation.FgGreen, presentation.Reset)

	v.SetCursor(0, cursor)
}

// renderConnectChooser writes the SSH host selection list to a gocui view.
func renderConnectChooser(v *gocui.View, hosts []string, cursor int) {
	v.Clear()
	for _, host := range hosts {
		fmt.Fprintf(v, "  %s\n", host)
	}
	// "Manual input" entry
	fmt.Fprintf(v, " %s+ Manual input%s\n", presentation.FgGreen, presentation.Reset)

	v.SetCursor(0, cursor)
}

// logFileCache caches readLogLines results, only re-reading when the
// file's modification time changes.  This prevents expensive os.ReadFile
// calls on every layout cycle.
// All access is from the gocui event loop goroutine only.
type logFileCache struct {
	modTime int64
	lines   []string
}

// logRenderCache tracks the last rendered state so that
// renderServerLog can skip the expensive v.Clear() + Write cycle
// when nothing has changed.
// All access is from the gocui event loop goroutine only.
type logRenderCache struct {
	modTime     int64
	focused     bool
	cursorY     int
	selStart    int
	selEnd      int
	width       int
	searchQuery string
}

// readLogLines returns all log lines in reverse order (newest first).
// Results are cached and only refreshed when the file changes.
// Must be called from the gocui event loop goroutine only.
func (a *App) readLogLines() []string {
	info, err := os.Stat(serverLogPath)
	if err != nil {
		return nil
	}
	mt := info.ModTime().UnixNano()
	if mt == a.logCache.modTime {
		return a.logCache.lines
	}
	data, err := os.ReadFile(serverLogPath)
	if err != nil {
		return nil
	}
	trimmed := bytes.TrimRight(data, "\n")
	if len(trimmed) == 0 {
		a.logCache.modTime = mt
		a.logCache.lines = nil
		return nil
	}
	raw := bytes.Split(trimmed, []byte("\n"))
	lines := make([]string, len(raw))
	for i, b := range raw {
		lines[len(raw)-1-i] = string(b)
	}
	a.logCache.modTime = mt
	a.logCache.lines = lines
	return lines
}

// renderServerLog writes log lines with cursor/selection highlighting.
// It skips the expensive v.Clear() + Write cycle when the render state
// (log content, focus, cursor, selection, width) has not changed.
// Must be called from the gocui event loop goroutine only.
func (a *App) renderServerLog(v *gocui.View, logs *LogsState, focused bool) {
	lines := a.filteredLogLines()
	if len(lines) == 0 {
		// Always re-render the empty state (cheap).
		v.Clear()
		fmt.Fprintln(v, presentation.Dim+"  MCP: no log"+presentation.Reset)
		logs.SetLineCount(0)
		return
	}
	logs.SetLineCount(len(lines))
	logs.ClampCursor()

	selStart, selEnd := logs.SelectionRange()
	cursorY := logs.CursorY()
	w := v.InnerWidth()
	searchQ := a.effectiveQuery("logs")

	// Skip re-render if nothing changed since last time.
	rc := &a.logRender
	if rc.modTime == a.logCache.modTime &&
		rc.focused == focused &&
		rc.cursorY == cursorY &&
		rc.selStart == selStart &&
		rc.selEnd == selEnd &&
		rc.width == w &&
		rc.searchQuery == searchQ {
		return
	}
	rc.modTime = a.logCache.modTime
	rc.focused = focused
	rc.cursorY = cursorY
	rc.selStart = selStart
	rc.selEnd = selEnd
	rc.width = w
	rc.searchQuery = searchQ

	v.Clear()
	selecting := logs.IsSelecting()

	for i, raw := range lines {
		line := " " + raw
		line = truncateToWidth(line, w)
		inSelection := focused && selecting && i >= selStart && i <= selEnd
		isCursor := focused && i == cursorY

		padded := padRight(line, w)
		if inSelection && isCursor {
			fmt.Fprintf(v, "\x1b[48;5;33;1;37m%s\x1b[0m\n", padded)
		} else if inSelection {
			fmt.Fprintf(v, "\x1b[48;5;24;37m%s\x1b[0m\n", padded)
		} else if isCursor && selecting {
			fmt.Fprintf(v, "\x1b[48;5;238;1m%s\x1b[0m\n", padded)
		} else if isCursor {
			fmt.Fprintf(v, "\x1b[48;5;240m%s\x1b[0m\n", padded)
		} else {
			fmt.Fprintln(v, presentation.ColorizeLogLine(line))
		}
	}

	if focused {
		scrollToCursor(v, cursorY)
	}
}

// scrollToCursor adjusts the scroll origin so the cursor stays within
// the visible viewport, then sets the cursor relative to the origin.
// gocui's SetCursor uses coordinates relative to origin, so the cursor
// must be set after the origin is finalised.
func scrollToCursor(v *gocui.View, cursorY int) {
	_, oy := v.Origin()
	h := v.InnerHeight()
	if cursorY < oy {
		oy = cursorY
		v.SetOrigin(0, oy)
	} else if cursorY >= oy+h {
		oy = cursorY - h + 1
		v.SetOrigin(0, oy)
	}
	v.SetCursor(0, cursorY-oy)
}

// renderToolPopup writes all tool popup lines to the view and uses
// SetOrigin to control the scroll position. Writing all lines allows
// gocui to compute scrollbar position from ViewLinesHeight/OriginY.
func renderToolPopup(v *gocui.View, p Popup) {
	v.Title = p.Title()
	for _, line := range p.ContentLines() {
		fmt.Fprintln(v, line)
	}
	v.SetOrigin(0, p.ScrollY())
}

// renderDiffPopup writes all diff lines to the view with ANSI coloring
// and uses SetOrigin to control the scroll position.
func renderDiffPopup(v *gocui.View, p Popup) {
	v.Title = p.Title()

	diffLines := p.ContentLines()
	diffKinds := p.ContentKinds()

	for i, line := range diffLines {
		kind := diffKinds[i]
		switch kind {
		case presentation.DiffAdd:
			fmt.Fprintf(v, "\x1b[32m%s\x1b[0m\n", line)
		case presentation.DiffDel:
			fmt.Fprintf(v, "\x1b[31m%s\x1b[0m\n", line)
		case presentation.DiffHunk:
			fmt.Fprintf(v, "\x1b[36m%s\x1b[0m\n", line)
		case presentation.DiffFilePath:
			fmt.Fprintf(v, "\x1b[2m%s\x1b[0m\n", line)
		default:
			fmt.Fprintln(v, line)
		}
	}
	v.SetOrigin(0, p.ScrollY())
}

// truncateToWidth truncates s so that its display width does not exceed maxW.
// This prevents gocui line wrapping from breaking cursor/origin tracking.
func truncateToWidth(s string, maxW int) string {
	w := 0
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > maxW {
			return s[:i]
		}
		w += rw
	}
	return s
}

// renderScrollContent renders scrollback lines with cursor/selection highlighting.
func (a *App) renderScrollContent(v *gocui.View) {
	lines := a.scroll.Lines()
	if len(lines) == 0 {
		fmt.Fprintln(v, presentation.Dim+"  Loading scrollback..."+presentation.Reset)
		return
	}

	selecting := a.scroll.IsSelecting()
	selStart, selEnd := a.scroll.SelectionRange()
	cursorY := a.scroll.CursorY()
	w := v.InnerWidth()

	for i, raw := range lines {
		// Use ANSI-aware truncation so escape sequences are not split.
		line := ansi.Truncate(raw, w, "")
		inSelection := selecting && i >= selStart && i <= selEnd
		isCursor := i == cursorY

		// ANSI-aware padding: use ansi.StringWidth instead of runewidth
		padded := padRightANSI(line, w)
		if inSelection && isCursor {
			fmt.Fprintf(v, "\x1b[48;5;33;1;37m%s\x1b[0m\n", padded)
		} else if inSelection {
			fmt.Fprintf(v, "\x1b[48;5;24;37m%s\x1b[0m\n", padded)
		} else if isCursor && selecting {
			fmt.Fprintf(v, "\x1b[48;5;238;1m%s\x1b[0m\n", padded)
		} else if isCursor {
			fmt.Fprintf(v, "\x1b[48;5;240m%s\x1b[0m\n", padded)
		} else {
			fmt.Fprintln(v, line)
		}
	}

	scrollToCursor(v, cursorY)
}

// renderScrollStatusBar renders the scroll mode status bar.
func (a *App) renderScrollStatusBar(v *gocui.View, sessionName string) {
	offset := a.scroll.ScrollOffset()
	mode := presentation.Bold + presentation.FgCyan + "SCROLL" + presentation.Reset
	if a.scroll.IsSelecting() {
		mode += " " + presentation.Bold + presentation.FgYellow + "VISUAL" + presentation.Reset
	}
	pos := fmt.Sprintf("[-%d]", offset)

	scrollHints := a.keyRegistry.HintsForScope(keymap.ScopeScroll)
	var hintBar string
	for _, h := range scrollHints {
		if h.HintLabel == "" {
			continue
		}
		if hintBar != "" {
			hintBar += "  "
		}
		hintBar += presentation.StyledKey(h.HintKeyLabel(), h.HintLabel)
	}

	fmt.Fprintf(v, " %s %s %s %s %s %s",
		mode,
		presentation.FgDimGray+pos+presentation.Reset,
		presentation.FgDimGray+presentation.IconSep+presentation.Reset,
		sessionName,
		presentation.FgDimGray+presentation.IconSep+presentation.Reset,
		hintBar)
}

// padRightANSI pads an ANSI-containing string with spaces to targetW display width.
func padRightANSI(s string, targetW int) string {
	sw := ansi.StringWidth(s)
	if sw >= targetW {
		return s
	}
	return s + strings.Repeat(" ", targetW-sw)
}

// padRight pads s with spaces so that its display width equals targetW.
// If s is already at least targetW wide, it is returned unchanged.
func padRight(s string, targetW int) string {
	sw := runewidth.StringWidth(s)
	if sw >= targetW {
		return s
	}
	return s + strings.Repeat(" ", targetW-sw)
}
