package gui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/KEMSHlM/lazyclaude/internal/notify"
	"github.com/jesseduffield/gocui"
)

const popupViewName = "tool-popup"
const popupActionsViewName = "tool-popup-actions"

// showToolPopup pushes a notification onto the popup stack.
func (a *App) showToolPopup(n *notify.ToolNotification) {
	a.popups.Push(n)
}

// dismissPopup sends the choice to the focused popup and removes it from the stack.
func (a *App) dismissPopup(choice Choice) {
	active := a.popups.ActivePopup()
	if active == nil {
		return
	}
	window := active.Window()
	a.popups.DismissActive(choice)

	if a.sessions != nil {
		go func() {
			_ = a.sessions.SendChoice(window, choice)
		}()
	}
}

// dismissAllPopups sends the choice to all popups and clears the stack.
func (a *App) dismissAllPopups(choice Choice) {
	stack := a.popups.Stack()
	if len(stack) == 0 {
		return
	}
	entries := make([]popupEntry, len(stack))
	copy(entries, stack)
	a.popups.DismissAll(choice)

	if a.sessions != nil {
		go func() {
			for _, e := range entries {
				_ = a.sessions.SendChoice(e.popup.Window(), choice)
			}
		}()
	}
}

// layoutToolPopup renders all visible popups as cascaded overlays.
func (a *App) layoutToolPopup(g *gocui.Gui, maxX, maxY int) error {
	a.cleanupPopupViews(g)

	if !a.popups.HasVisible() {
		g.DeleteView(popupActionsViewName)
		return nil
	}

	popW := maxX * 7 / 10
	popH := maxY * 6 / 10
	if popW < 40 {
		popW = maxX - 4
	}
	if popH < 10 {
		popH = maxY - 4
	}
	baseX := (maxX - popW) / 2
	baseY := (maxY - popH) / 2

	var activeViewName string
	visibleIdx := 0
	stack := a.popups.Stack()
	for i := range stack {
		e := &stack[i]
		if e.suspended {
			continue
		}

		viewName := fmt.Sprintf("tool-popup-%d", i)
		cx, cy := popupCascadeOffset(baseX, baseY, visibleIdx)
		x1 := cx + popW
		y1 := cy + popH - 2

		if x1 >= maxX {
			x1 = maxX - 1
		}
		if y1 >= maxY-2 {
			y1 = maxY - 3
		}

		v, err := g.SetView(viewName, cx, cy, x1, y1, 0)
		if err != nil && !isUnknownView(err) {
			return err
		}
		v.Clear()

		if e.popup.IsDiff() {
			a.renderDiffPopup(v, e.popup)
		} else {
			a.renderToolPopup(v, e.popup)
		}

		if i == a.popups.FocusIndex() {
			activeViewName = viewName
		}
		visibleIdx++
	}

	if activeViewName != "" {
		g.SetViewOnTop(activeViewName)
	}

	focusedEntry := a.popups.ActiveEntry()
	if focusedEntry != nil {
		cx, cy := popupCascadeOffset(baseX, baseY, a.popups.VisibleIndexOf(a.popups.FocusIndex()))
		ay0 := cy + popH - 1
		ay1 := ay0 + 2
		if ay1 >= maxY {
			ay1 = maxY - 1
		}
		ax1 := cx + popW
		if ax1 >= maxX {
			ax1 = maxX - 1
		}

		v2, err := g.SetView(popupActionsViewName, cx, ay0, ax1, ay1, 0)
		if err != nil && !isUnknownView(err) {
			return err
		}
		v2.Frame = false
		v2.Clear()
		g.SetViewOnTop(popupActionsViewName)

		visible := a.popups.VisibleCount()
		p := focusedEntry.popup

		// Determine number of options from notification if available.
		maxOpt := 0
		if n := notificationFromPopup(p); n != nil {
			maxOpt = n.MaxOption
		}
		if maxOpt == 0 {
			maxOpt = 3 // default
		}
		base := " y/n"
		if maxOpt >= 3 {
			base = " y/a/n"
		}
		if p.IsDiff() {
			base += " j/k:scroll"
		}
		base += " Esc:hide"
		if visible > 1 {
			base += fmt.Sprintf(" Y:all [%d/%d]", a.popups.VisibleIndexOf(a.popups.FocusIndex())+1, visible)
		}
		fmt.Fprint(v2, base)

		if _, err := g.SetCurrentView(activeViewName); err != nil && !isUnknownView(err) {
			return err
		}
	}

	return nil
}

// cleanupPopupViews deletes all tool-popup-N views that are no longer needed.
func (a *App) cleanupPopupViews(g *gocui.Gui) {
	stack := a.popups.Stack()
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("tool-popup-%d", i)
		if i < len(stack) && !stack[i].suspended {
			continue
		}
		g.DeleteView(name)
	}
}

func (a *App) renderToolPopup(v *gocui.View, p Popup) {
	v.Title = p.Title()
	for _, line := range p.ContentLines() {
		fmt.Fprintln(v, line)
	}
}

func (a *App) renderDiffPopup(v *gocui.View, p Popup) {
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

func generateDiffFromContents(oldFilePath, newContents string) string {
	tmpDir := os.TempDir()
	newFile, err := os.CreateTemp(tmpDir, "lazyclaude-diff-new-*")
	if err != nil {
		return fmt.Sprintf("(error creating temp file: %v)", err)
	}
	defer os.Remove(newFile.Name())
	if _, err := newFile.WriteString(newContents); err != nil {
		newFile.Close()
		return fmt.Sprintf("(error writing temp file: %v)", err)
	}
	if err := newFile.Close(); err != nil {
		return fmt.Sprintf("(error closing temp file: %v)", err)
	}

	if _, err := os.Stat(oldFilePath); os.IsNotExist(err) {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("--- /dev/null\n+++ %s\n@@ -0,0 +1 @@\n", filepath.Base(oldFilePath)))
		for _, line := range strings.Split(newContents, "\n") {
			if line != "" {
				sb.WriteString("+" + line + "\n")
			}
		}
		return sb.String()
	}

	cmd := exec.Command("git", "diff", "--no-index", "--unified=3", "--", oldFilePath, newFile.Name())
	out, err := cmd.Output()
	if err != nil && len(out) > 0 {
		return string(out)
	}
	if err != nil {
		return fmt.Sprintf("(no differences or error: %v)", err)
	}
	return string(out)
}

// --- App delegation to PopupController ---

func (a *App) hasPopup() bool                          { return a.popups.HasVisible() }
func (a *App) popupCount() int                         { return a.popups.Count() }
func (a *App) visiblePopupCount() int                  { return a.popups.VisibleCount() }
func (a *App) activePopup() *notify.ToolNotification   { return a.popups.ActiveNotification() }
func (a *App) activeEntry() *popupEntry                { return a.popups.ActiveEntry() }
func (a *App) pushPopup(n *notify.ToolNotification)    { a.popups.Push(n) }
func (a *App) dismissActivePopup()                     { a.popups.DismissActive(ChoiceCancel) }
func (a *App) popupFocusNext()                         { a.popups.FocusNext() }
func (a *App) popupFocusPrev()                         { a.popups.FocusPrev() }
func (a *App) suspendAllPopups()                       { a.popups.SuspendAll() }
func (a *App) unsuspendAll()                           { a.popups.UnsuspendAll() }
func (a *App) visibleIndexOf(stackIdx int) int         { return a.popups.VisibleIndexOf(stackIdx) }

func popupCascadeOffset(baseX, baseY, index int) (int, int) {
	return baseX + index*2, baseY + index
}
