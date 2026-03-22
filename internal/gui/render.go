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

// renderSessionList writes the session list to a gocui view.
func renderSessionList(v *gocui.View, items []SessionItem, cursor int) {
	if len(items) == 0 {
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, presentation.Dim+"  No sessions"+presentation.Reset)
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, "  Press "+presentation.Bold+"n"+presentation.Reset+" to create")
		return
	}

	for i, item := range items {
		prefix := "  "
		if i == cursor {
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
		if session.IsWorktreePath(item.Path) {
			name = presentation.IconWorktree + " " + name
		}
		fmt.Fprintf(v, "%s%-20s%s\n", prefix, name, icon)
	}

	v.SetCursor(0, cursor)
}

// readLogLines returns all log lines in reverse order (newest first).
func readLogLines() []string {
	data, err := os.ReadFile(serverLogPath)
	if err != nil {
		return nil
	}
	raw := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	lines := make([]string, len(raw))
	for i, b := range raw {
		lines[len(raw)-1-i] = string(b)
	}
	return lines
}

// renderServerLog writes log lines with cursor/selection highlighting.
func renderServerLog(v *gocui.View, logs *LogsState, focused bool) {
	lines := readLogLines()
	if len(lines) == 0 {
		fmt.Fprintln(v, presentation.Dim+"  MCP: no log"+presentation.Reset)
		logs.SetLineCount(0)
		return
	}
	logs.SetLineCount(len(lines))
	logs.ClampCursor()

	selStart, selEnd := logs.SelectionRange()
	cursorY := logs.CursorY()
	selecting := logs.IsSelecting()
	w := v.InnerWidth()

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
