package gui

import (
	"bytes"
	"fmt"
	"os"

	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/KEMSHlM/lazyclaude/internal/session"
	"github.com/jesseduffield/gocui"
)

const serverLogPath = "/tmp/lazyclaude/server.log"
const serverLogLines = 30

// sessionStatusIcon returns the status icon for a session item.
func sessionStatusIcon(item *SessionItem) string {
	switch {
	case item.Status == "Dead":
		return " " + presentation.IconDead
	case item.Status == "Orphan":
		return " " + presentation.IconOrphan
	case item.Activity == "pending":
		return " " + presentation.IconPending
	case item.Status == "Running":
		return " " + presentation.IconRunning
	case item.Status == "Detached":
		return " " + presentation.IconDetached
	default:
		return ""
	}
}

// sessionDisplayName returns the decorated name for a session item.
func sessionDisplayName(item *SessionItem) string {
	name := item.Name
	if item.Host != "" {
		name = presentation.FgPurple + item.Host + presentation.Reset + ":" + name
	}
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
			fmt.Fprintf(v, " %s %s\n", expandIcon, node.Project.Name)

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
	modTime  int64
	focused  bool
	cursorY  int
	selStart int
	selEnd   int
	width    int
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
	raw := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
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
	lines := a.readLogLines()
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

	// Skip re-render if nothing changed since last time.
	rc := &a.logRender
	if rc.modTime == a.logCache.modTime &&
		rc.focused == focused &&
		rc.cursorY == cursorY &&
		rc.selStart == selStart &&
		rc.selEnd == selEnd &&
		rc.width == w {
		return
	}
	rc.modTime = a.logCache.modTime
	rc.focused = focused
	rc.cursorY = cursorY
	rc.selStart = selStart
	rc.selEnd = selEnd
	rc.width = w

	v.Clear()
	selecting := logs.IsSelecting()

	for i, raw := range lines {
		line := " " + raw
		inSelection := focused && selecting && i >= selStart && i <= selEnd
		isCursor := focused && i == cursorY

		if inSelection && isCursor {
			fmt.Fprintf(v, "\x1b[48;5;33;1;37m%-*s\x1b[0m\n", w, line)
		} else if inSelection {
			fmt.Fprintf(v, "\x1b[48;5;24;37m%-*s\x1b[0m\n", w, line)
		} else if isCursor && selecting {
			fmt.Fprintf(v, "\x1b[48;5;238;1m%-*s\x1b[0m\n", w, line)
		} else if isCursor {
			fmt.Fprintf(v, "\x1b[48;5;240m%-*s\x1b[0m\n", w, line)
		} else {
			fmt.Fprintln(v, line)
		}
	}

	if focused {
		v.SetCursor(0, cursorY)
		_, oy := v.Origin()
		h := v.InnerHeight()
		if cursorY < oy {
			v.SetOrigin(0, cursorY)
		} else if cursorY >= oy+h {
			v.SetOrigin(0, cursorY-h+1)
		}
	}
}

// renderToolPopup writes a tool popup to a view.
func renderToolPopup(v *gocui.View, p Popup) {
	v.Title = p.Title()
	for _, line := range p.ContentLines() {
		fmt.Fprintln(v, line)
	}
}

// renderDiffPopup writes a diff popup to a view.
func renderDiffPopup(v *gocui.View, p Popup) {
	v.Title = p.Title()

	diffLines := p.ContentLines()
	diffKinds := p.ContentKinds()
	_, viewH := v.Size()
	visibleLines := viewH - 1

	start := p.ScrollY()
	end := start + visibleLines
	if end > len(diffLines) {
		end = len(diffLines)
	}
	if start < 0 {
		start = 0
	}

	for i := start; i < end; i++ {
		line := diffLines[i]
		kind := diffKinds[i]
		switch kind {
		case presentation.DiffAdd:
			fmt.Fprintf(v, "\x1b[32m%s\x1b[0m\n", line)
		case presentation.DiffDel:
			fmt.Fprintf(v, "\x1b[31m%s\x1b[0m\n", line)
		case presentation.DiffHunk:
			fmt.Fprintf(v, "\x1b[36m%s\x1b[0m\n", line)
		case presentation.DiffHeader:
			fmt.Fprintf(v, "\x1b[1m%s\x1b[0m\n", line)
		default:
			fmt.Fprintln(v, line)
		}
	}
}
