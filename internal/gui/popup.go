package gui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/gui/keymap"
	"github.com/any-context/lazyclaude/internal/gui/presentation"
	"github.com/jesseduffield/gocui"
)

const popupViewName = "tool-popup"
const popupActionsViewName = "tool-popup-actions"

// showToolPopup pushes a notification onto the popup stack.
func (a *App) showToolPopup(n *model.ToolNotification) {
	a.popups.PushPopup(newPopupFromNotification(n))
}

// dismissPopup removes the focused popup from the stack and sends the choice to the session.
func (a *App) dismissPopup(choice Choice) {
	window := a.popups.DismissActive(choice)
	if window == "" {
		return
	}
	// Transition NeedsInput -> Running immediately on the gocui goroutine,
	// before the async SendChoice completes.
	a.setWindowActivity(window, WindowActivityEntry{State: model.ActivityRunning})
	if a.sessions != nil {
		go func() {
			_ = a.sessions.SendChoice(window, choice)
		}()
	}
}

// dismissAllPopups clears the stack and sends the choice to all sessions.
func (a *App) dismissAllPopups(choice Choice) {
	windows := a.popups.DismissAll(choice)
	if len(windows) == 0 {
		return
	}
	// Transition NeedsInput -> Running immediately on the gocui goroutine,
	// before the async SendChoice completes.
	for _, w := range windows {
		a.setWindowActivity(w, WindowActivityEntry{State: model.ActivityRunning})
	}
	if a.sessions != nil {
		go func() {
			for _, w := range windows {
				_ = a.sessions.SendChoice(w, choice)
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
		setRoundedFrame(v)
		v.Clear()

		// Store viewport height before rendering so scroll bounds stay consistent.
		_, viewH := v.Size()
		visibleLines := viewH - 1
		e.popup.SetViewportHeight(visibleLines)
		// Clamp scroll position to new max after viewport resize.
		if max := e.popup.MaxScroll(visibleLines); e.popup.ScrollY() > max {
			e.popup.SetScrollY(max)
		}

		if e.popup.IsDiff() {
			renderDiffPopup(v, e.popup)
		} else {
			renderToolPopup(v, e.popup)
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
		ay0 := cy + popH - 2
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
		v2.BgColor = gocui.Get256Color(236)
		v2.Clear()
		g.SetViewOnTop(popupActionsViewName)

		visible := a.popups.VisibleCount()
		p := focusedEntry.popup
		maxOpt := p.MaxOption()

		vh := p.ViewportHeight()
		if vh <= 0 {
			vh = 20
		}

		popupHints := a.keyRegistry.HintsForScope(keymap.ScopePopup)
		var defs []presentation.HintDef
		for _, h := range popupHints {
			// Conditionally skip hints based on popup state
			switch h.Action {
			case keymap.ActionPopupAllow:
				if maxOpt < 3 {
					continue
				}
			case keymap.ActionPopupAcceptAll:
				if visible <= 1 {
					continue
				}
			case keymap.ActionPopupFocusNext:
				if visible <= 1 {
					continue
				}
			case keymap.ActionPopupScrollDown:
				if p.MaxScroll(vh) == 0 {
					continue
				}
			}
			defs = append(defs, presentation.HintDef{
				Key:   h.HintKeyLabel(),
				Label: h.HintLabel,
			})
		}
		base := presentation.BuildOptionsBar(defs)
		if visible > 1 {
			base += fmt.Sprintf(" "+presentation.Dim+"[%d/%d]"+presentation.Reset,
				a.popups.VisibleIndexOf(a.popups.FocusIndex())+1, visible)
		}
		fmt.Fprint(v2, base)

		// Popup always gets focus. If a dialog is active, layoutMain
		// will restore dialog focus after all popups are dismissed.
		if _, err := g.SetCurrentView(activeViewName); err != nil && !isUnknownView(err) {
			return err
		}
		g.Cursor = false
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

func (a *App) hasPopup() bool                              { return a.popups.HasVisible() }
func (a *App) popupCount() int                             { return a.popups.Count() }
func (a *App) activePopup() *model.ToolNotification        { return a.popups.ActiveNotification() }
func (a *App) activeEntry() *popupEntry                    { return a.popups.ActiveEntry() }
func (a *App) pushPopup(n *model.ToolNotification)         { a.popups.PushPopup(newPopupFromNotification(n)) }
func (a *App) dismissActivePopup()                         { a.popups.DismissActive(ChoiceCancel) }
func (a *App) popupFocusNext()                             { a.popups.FocusNext() }
func (a *App) popupFocusPrev()                             { a.popups.FocusPrev() }
func (a *App) suspendAllPopups()                           { a.popups.SuspendAll() }
func (a *App) unsuspendAll()                               { a.popups.UnsuspendAll() }

func popupCascadeOffset(baseX, baseY, index int) (int, int) {
	return baseX + index*2, baseY + index
}
