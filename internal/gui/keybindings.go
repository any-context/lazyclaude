package gui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/any-context/lazyclaude/internal/gui/keyhandler"
	"github.com/any-context/lazyclaude/internal/gui/keymap"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/jesseduffield/gocui"
)

// dispatchRune creates a gocui handler that dispatches a rune key through the Dispatcher.
func (a *App) dispatchRune(ch rune) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		ev := keyhandler.KeyEvent{Rune: ch}
		a.dispatcher.Dispatch(ev, a)
		if a.quitRequested {
			a.quitRequested = false
			return gocui.ErrQuit
		}
		return nil
	}
}

// dispatchKey creates a gocui handler that dispatches a special key through the Dispatcher.
func (a *App) dispatchKey(key gocui.Key) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		ev := keyhandler.KeyEvent{Key: key}
		a.dispatcher.Dispatch(ev, a)
		if a.quitRequested {
			a.quitRequested = false
			return gocui.ErrQuit
		}
		return nil
	}
}

// setupGlobalKeybindings registers physical keys and delegates to the Dispatcher.
func (a *App) setupGlobalKeybindings() error {
	// 1. Rune keys dispatched through the chain (auto-generated from registry)
	for _, ch := range a.keyRegistry.Runes() {
		if err := a.gui.SetKeybinding("", ch, gocui.ModNone, a.dispatchRune(ch)); err != nil {
			return err
		}
	}

	// 2. Special keys dispatched through the chain (auto-generated from registry)
	for _, key := range a.keyRegistry.SpecialKeys() {
		if err := a.gui.SetKeybinding("", key, gocui.ModNone, a.dispatchKey(key)); err != nil {
			return err
		}
	}



	// 3. Popup view bindings (gocui may skip global rune bindings when popup has focus)
	for _, ch := range a.keyRegistry.RunesForScope(keymap.ScopePopup) {
		if err := a.gui.SetKeybinding(popupViewName, ch, gocui.ModNone, a.dispatchRune(ch)); err != nil {
			return err
		}
	}
	for _, key := range a.keyRegistry.SpecialKeysForScope(keymap.ScopePopup) {
		if err := a.gui.SetKeybinding(popupViewName, key, gocui.ModNone, a.dispatchKey(key)); err != nil {
			return err
		}
	}

	// 4. Mouse scroll — enters scroll mode if needed, then scrolls viewport
	if err := a.gui.SetKeybinding("", gocui.MouseWheelUp, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.fullscreen.IsActive() {
			a.ScrollModeMouseUp()
		}
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("", gocui.MouseWheelDown, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if a.fullscreen.IsActive() {
			a.ScrollModeMouseDown()
		}
		return nil
	}); err != nil {
		return err
	}

	// 5-6. Dialog bindings (view-specific, outside dispatcher).
	// Pattern: Editable views use DefaultEditor for text input.
	// Action keys (Enter/Esc/Tab) are view-specific bindings that
	// gocui dispatches BEFORE Editor.Edit(), so they intercept
	// the key before the editor can consume it.
	//
	// 5. Rename input
	if err := a.gui.SetKeybinding("rename-input", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		newName := strings.TrimSpace(v.TextArea.GetContent())
		if newName != "" && a.dialog.RenameID != "" {
			if err := a.sessions.Rename(a.dialog.RenameID, newName); err != nil {
				a.showError(g, fmt.Sprintf("Error: %v", err))
			} else {
				a.setStatus(g, "Renamed to "+newName)
			}
		}
		a.closeRenameInput(g)
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("rename-input", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.closeRenameInput(g)
		return nil
	}); err != nil {
		return err
	}

	// 6. Worktree dialog bindings (view-specific, outside dispatcher)
	worktreeConfirm := func(g *gocui.Gui, v *gocui.View) error {
		branchView, err := g.View("worktree-branch")
		if err != nil {
			return nil
		}
		promptView, err := g.View("worktree-prompt")
		if err != nil {
			return nil
		}
		branchName := strings.TrimSpace(branchView.TextArea.GetContent())
		userPrompt := promptView.TextArea.GetContent()

		// Validate before closing the dialog so errors appear inline.
		if err := session.ValidateWorktreeName(branchName); err != nil {
			a.showError(g, fmt.Sprintf("Error: %v", err))
			return nil
		}

		projectRoot := a.currentProjectRoot()
		a.closeWorktreeDialog(g)

		go func() {
			if a.sessions == nil {
				return
			}
			if err := a.sessions.CreateWorktree(branchName, userPrompt, projectRoot); err != nil {
				a.gui.Update(func(g *gocui.Gui) error {
					a.showError(g, fmt.Sprintf("Error: %v", err))
					return nil
				})
				return
			}
			a.gui.Update(func(g *gocui.Gui) error {
				a.setStatus(g, "Worktree "+branchName+" created")
				return nil
			})
		}()
		return nil
	}

	worktreeCancel := func(g *gocui.Gui, v *gocui.View) error {
		a.closeWorktreeDialog(g)
		return nil
	}

	for _, viewName := range []string{"worktree-branch", "worktree-prompt"} {
		if err := a.gui.SetKeybinding(viewName, gocui.KeyEnter, gocui.ModNone, worktreeConfirm); err != nil {
			return err
		}
		if err := a.gui.SetKeybinding(viewName, gocui.KeyEsc, gocui.ModNone, worktreeCancel); err != nil {
			return err
		}
	}

	// Ctrl+J: insert newline in prompt field (Enter is used for confirm)
	if err := a.gui.SetKeybinding("worktree-prompt", gocui.KeyCtrlJ, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		v.TextArea.TypeCharacter("\n")
		v.RenderTextArea()
		return nil
	}); err != nil {
		return err
	}

	// Tab: switch between branch and prompt fields
	if err := a.gui.SetKeybinding("worktree-branch", gocui.KeyTab, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.dialog.ActiveField = "worktree-prompt"
		if _, err := g.SetCurrentView("worktree-prompt"); err != nil && !isUnknownView(err) {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("worktree-prompt", gocui.KeyTab, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.dialog.ActiveField = "worktree-branch"
		if _, err := g.SetCurrentView("worktree-branch"); err != nil && !isUnknownView(err) {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	// 7. Worktree chooser bindings (j/k/Enter/Esc)
	chooserMove := func(delta int) func(*gocui.Gui, *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			maxIdx := len(a.dialog.WorktreeItems) // last index = "New worktree"
			a.dialog.WorktreeCursor += delta
			if a.dialog.WorktreeCursor < 0 {
				a.dialog.WorktreeCursor = 0
			}
			if a.dialog.WorktreeCursor > maxIdx {
				a.dialog.WorktreeCursor = maxIdx
			}
			renderWorktreeChooser(v, a.dialog.WorktreeItems, a.dialog.WorktreeCursor)
			return nil
		}
	}
	for _, binding := range []struct {
		key gocui.Key
		ch  rune
	}{
		{key: gocui.KeyArrowDown}, {key: gocui.KeyArrowUp},
	} {
		delta := 1
		if binding.key == gocui.KeyArrowUp {
			delta = -1
		}
		if err := a.gui.SetKeybinding("worktree-chooser", binding.key, gocui.ModNone, chooserMove(delta)); err != nil {
			return err
		}
	}
	for _, ch := range []rune{'j', 'k'} {
		delta := 1
		if ch == 'k' {
			delta = -1
		}
		if err := a.gui.SetKeybinding("worktree-chooser", ch, gocui.ModNone, chooserMove(delta)); err != nil {
			return err
		}
	}

	if err := a.gui.SetKeybinding("worktree-chooser", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		idx := a.dialog.WorktreeCursor
		items := a.dialog.WorktreeItems
		a.closeWorktreeChooser(g)

		if idx >= len(items) {
			// "New worktree" selected
			if !a.showWorktreeDialog(g) {
				a.setStatus(g, "Error: could not open worktree dialog")
			}
		} else {
			// Existing worktree selected
			a.dialog.SelectedPath = items[idx].Path
			if !a.showWorktreeResumePrompt(g, items[idx].Name) {
				a.setStatus(g, "Error: could not open prompt dialog")
			}
		}
		return nil
	}); err != nil {
		return err
	}

	if err := a.gui.SetKeybinding("worktree-chooser", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.closeWorktreeChooser(g)
		return nil
	}); err != nil {
		return err
	}

	// 8. Worktree resume prompt bindings (Enter/Esc/Ctrl+J)
	if err := a.gui.SetKeybinding("worktree-resume-prompt", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		userPrompt := v.TextArea.GetContent()
		wtPath := a.dialog.SelectedPath
		projectRoot := a.currentProjectRoot()
		a.closeWorktreeResumePrompt(g)

		go func() {
			if a.sessions == nil {
				return
			}
			if err := a.sessions.ResumeWorktree(wtPath, userPrompt, projectRoot); err != nil {
				a.gui.Update(func(g *gocui.Gui) error {
					a.showError(g, fmt.Sprintf("Error: %v", err))
					return nil
				})
				return
			}
			name := filepath.Base(wtPath)
			a.gui.Update(func(g *gocui.Gui) error {
				a.setStatus(g, "Worktree "+name+" resumed")
				return nil
			})
		}()
		return nil
	}); err != nil {
		return err
	}

	if err := a.gui.SetKeybinding("worktree-resume-prompt", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.closeWorktreeResumePrompt(g)
		return nil
	}); err != nil {
		return err
	}

	if err := a.gui.SetKeybinding("worktree-resume-prompt", gocui.KeyCtrlJ, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		v.TextArea.TypeCharacter("\n")
		v.RenderTextArea()
		return nil
	}); err != nil {
		return err
	}

	// 9. Search input bindings (Enter to confirm, Esc to cancel)
	if err := a.gui.SetKeybinding(searchInputView, gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.closeSearch(g, false)
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(searchInputView, gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.closeSearch(g, true)
		return nil
	}); err != nil {
		return err
	}

	// 10. Connect dialog bindings (Enter to connect, Esc to cancel)
	if err := a.gui.SetKeybinding("connect-input", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		host := strings.TrimSpace(v.TextArea.GetContent())
		a.closeConnectDialog(g)
		if host == "" {
			return nil
		}
		if a.connectFn == nil {
			a.showError(g, "Remote connection not available")
			return nil
		}
		a.setStatus(g, "Connecting to "+host+"...")
		go func() {
			err := a.connectFn(host)
			a.gui.Update(func(g *gocui.Gui) error {
				if err != nil {
					a.showError(g, fmt.Sprintf("Connection failed: %v", err))
				} else {
					a.setStatus(g, "Connected to "+host)
				}
				return nil
			})
		}()
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("connect-input", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.closeConnectDialog(g)
		return nil
	}); err != nil {
		return err
	}

	// 11. Keybind help overlay bindings
	// Esc: close help
	for _, viewName := range []string{helpInputView, helpListView} {
		if err := a.gui.SetKeybinding(viewName, gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			a.closeKeybindHelp(g)
			return nil
		}); err != nil {
			return err
		}
	}

	// j/k and arrow keys: cursor movement in help list (from input view)
	helpCursorMove := func(delta int) func(*gocui.Gui, *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			if a.dialog.Kind != DialogKeybindHelp {
				return nil
			}
			a.dialog.HelpCursor += delta
			if a.dialog.HelpCursor < 0 {
				a.dialog.HelpCursor = 0
			}
			if a.dialog.HelpCursor >= len(a.dialog.HelpItems) {
				a.dialog.HelpCursor = len(a.dialog.HelpItems) - 1
			}
			if a.dialog.HelpCursor < 0 {
				a.dialog.HelpCursor = 0
			}
			a.dialog.HelpScrollY = 0
			return nil
		}
	}

	// Ctrl+J / Ctrl+K for list navigation from input field (j/k goes to editor)
	if err := a.gui.SetKeybinding(helpInputView, gocui.KeyCtrlJ, gocui.ModNone, helpCursorMove(1)); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(helpInputView, gocui.KeyCtrlK, gocui.ModNone, helpCursorMove(-1)); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(helpInputView, gocui.KeyArrowDown, gocui.ModNone, helpCursorMove(1)); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding(helpInputView, gocui.KeyArrowUp, gocui.ModNone, helpCursorMove(-1)); err != nil {
		return err
	}

	return nil
}
