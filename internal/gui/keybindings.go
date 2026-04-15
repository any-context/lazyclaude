package gui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/any-context/lazyclaude/internal/gui/chooser"
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
		renameID := a.dialog.RenameID
		a.closeRenameInput(g)
		if newName == "" || renameID == "" {
			return nil
		}
		go func() {
			err := a.sessions.Rename(renameID, newName)
			a.gui.Update(func(g *gocui.Gui) error {
				if err != nil {
					a.showError(g, fmt.Sprintf("Error: %v", err))
				} else {
					a.setStatus(g, "Renamed to "+newName)
				}
				return nil
			})
		}()
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

		selectedProfile := ""
		if a.dialog.ProfileCursor < len(a.dialog.ProfileItems) {
			selectedProfile = a.dialog.ProfileItems[a.dialog.ProfileCursor].Label
		}
		options := ""
		if optView, err2 := g.View("worktree-options"); err2 == nil {
			options = strings.TrimSpace(optView.TextArea.GetContent())
		}

		projectRoot := a.currentProjectRoot()
		a.closeWorktreeDialog(g)

		go func() {
			if a.sessions == nil {
				return
			}
			err := a.sessions.CreateWorktreeWithOpts(branchName, userPrompt, projectRoot, selectedProfile, options)
			if err != nil {
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

	for _, viewName := range []string{"worktree-branch", "worktree-prompt", "worktree-profile-chooser", "worktree-options"} {
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

	// Tab navigation: Branch → Prompt → Profile → Options → Branch (loop)
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
		a.dialog.ActiveField = "worktree-profile-chooser"
		if _, err := g.SetCurrentView("worktree-profile-chooser"); err != nil && !isUnknownView(err) {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("worktree-profile-chooser", gocui.KeyTab, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.dialog.ActiveField = "worktree-options"
		if _, err := g.SetCurrentView("worktree-options"); err != nil && !isUnknownView(err) {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("worktree-options", gocui.KeyTab, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.dialog.ActiveField = "worktree-branch"
		if _, err := g.SetCurrentView("worktree-branch"); err != nil && !isUnknownView(err) {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	// j/k navigation in worktree profile chooser
	worktreeProfileMove := func(delta int) func(*gocui.Gui, *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			s := &chooser.State{Items: a.dialog.ProfileItems, Cursor: a.dialog.ProfileCursor}
			chooser.Move(s, delta)
			a.dialog.ProfileCursor = s.Cursor
			if pv, err2 := g.View("worktree-profile-chooser"); err2 == nil {
				renderProfileChooser(pv, a.dialog.ProfileItems, a.dialog.ProfileCursor)
			}
			return nil
		}
	}
	for _, key := range []gocui.Key{gocui.KeyArrowDown, gocui.KeyArrowUp} {
		delta := 1
		if key == gocui.KeyArrowUp {
			delta = -1
		}
		if err := a.gui.SetKeybinding("worktree-profile-chooser", key, gocui.ModNone, worktreeProfileMove(delta)); err != nil {
			return err
		}
	}
	for _, ch := range []rune{'j', 'k'} {
		delta := 1
		if ch == 'k' {
			delta = -1
		}
		if err := a.gui.SetKeybinding("worktree-profile-chooser", ch, gocui.ModNone, worktreeProfileMove(delta)); err != nil {
			return err
		}
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
				a.showError(g, "Error: could not open worktree dialog")
			}
		} else {
			// Existing worktree selected
			a.dialog.SelectedPath = items[idx].Path
			if !a.showWorktreeResumePrompt(g, items[idx].Name) {
				a.showError(g, "Error: could not open prompt dialog")
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

	// 8. Worktree resume prompt bindings (Enter/Esc/Ctrl+J + profile/options)
	resumeConfirm := func(g *gocui.Gui, v *gocui.View) error {
		promptView, err := g.View("worktree-resume-prompt")
		if err != nil {
			return nil
		}
		userPrompt := promptView.TextArea.GetContent()
		wtPath := a.dialog.SelectedPath
		projectRoot := a.currentProjectRoot()

		selectedProfile := ""
		if a.dialog.ProfileCursor < len(a.dialog.ProfileItems) {
			selectedProfile = a.dialog.ProfileItems[a.dialog.ProfileCursor].Label
		}
		options := ""
		if optView, err2 := g.View("worktree-resume-options"); err2 == nil {
			options = strings.TrimSpace(optView.TextArea.GetContent())
		}

		a.closeWorktreeResumePrompt(g)

		go func() {
			if a.sessions == nil {
				return
			}
			err := a.sessions.ResumeWorktreeWithOpts(wtPath, userPrompt, projectRoot, selectedProfile, options)
			if err != nil {
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
	}

	resumeCancel := func(g *gocui.Gui, v *gocui.View) error {
		a.closeWorktreeResumePrompt(g)
		return nil
	}

	for _, viewName := range []string{
		"worktree-resume-prompt",
		"worktree-resume-profile-chooser",
		"worktree-resume-options",
	} {
		if err := a.gui.SetKeybinding(viewName, gocui.KeyEnter, gocui.ModNone, resumeConfirm); err != nil {
			return err
		}
		if err := a.gui.SetKeybinding(viewName, gocui.KeyEsc, gocui.ModNone, resumeCancel); err != nil {
			return err
		}
	}

	if err := a.gui.SetKeybinding("worktree-resume-prompt", gocui.KeyCtrlJ, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		v.TextArea.TypeCharacter("\n")
		v.RenderTextArea()
		return nil
	}); err != nil {
		return err
	}

	// Tab navigation for resume dialog: Prompt → Profile → Options → Prompt (loop)
	if err := a.gui.SetKeybinding("worktree-resume-prompt", gocui.KeyTab, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.dialog.ActiveField = "worktree-resume-profile-chooser"
		if _, err := g.SetCurrentView("worktree-resume-profile-chooser"); err != nil && !isUnknownView(err) {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("worktree-resume-profile-chooser", gocui.KeyTab, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.dialog.ActiveField = "worktree-resume-options"
		if _, err := g.SetCurrentView("worktree-resume-options"); err != nil && !isUnknownView(err) {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("worktree-resume-options", gocui.KeyTab, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.dialog.ActiveField = "worktree-resume-prompt"
		if _, err := g.SetCurrentView("worktree-resume-prompt"); err != nil && !isUnknownView(err) {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	// j/k navigation in resume profile chooser
	resumeProfileMove := func(delta int) func(*gocui.Gui, *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			s := &chooser.State{Items: a.dialog.ProfileItems, Cursor: a.dialog.ProfileCursor}
			chooser.Move(s, delta)
			a.dialog.ProfileCursor = s.Cursor
			if pv, err2 := g.View("worktree-resume-profile-chooser"); err2 == nil {
				renderProfileChooser(pv, a.dialog.ProfileItems, a.dialog.ProfileCursor)
			}
			return nil
		}
	}
	for _, key := range []gocui.Key{gocui.KeyArrowDown, gocui.KeyArrowUp} {
		delta := 1
		if key == gocui.KeyArrowUp {
			delta = -1
		}
		if err := a.gui.SetKeybinding("worktree-resume-profile-chooser", key, gocui.ModNone, resumeProfileMove(delta)); err != nil {
			return err
		}
	}
	for _, ch := range []rune{'j', 'k'} {
		delta := 1
		if ch == 'k' {
			delta = -1
		}
		if err := a.gui.SetKeybinding("worktree-resume-profile-chooser", ch, gocui.ModNone, resumeProfileMove(delta)); err != nil {
			return err
		}
	}

	// 8.5. Profile dialog bindings (Enter/Esc/Tab + j/k for chooser)
	profileDialogConfirm := func(g *gocui.Gui, v *gocui.View) error {
		selectedProfile := ""
		if a.dialog.ProfileCursor < len(a.dialog.ProfileItems) {
			selectedProfile = a.dialog.ProfileItems[a.dialog.ProfileCursor].Label
		}
		options := ""
		if optView, err2 := g.View("profile-options"); err2 == nil {
			options = strings.TrimSpace(optView.TextArea.GetContent())
		}

		kind := a.dialog.ProfileConfirmKind
		sessionPath := a.dialog.ProfileSessionPath
		a.closeProfileDialog(g)

		switch kind {
		case "session":
			go func() {
				if a.sessions == nil {
					return
				}
				err := a.sessions.CreateWithOpts(sessionPath, selectedProfile, options)
				a.gui.Update(func(g *gocui.Gui) error {
					if err != nil {
						a.showError(g, fmt.Sprintf("Error: %v", err))
					} else {
						a.setStatus(g, "Session created")
						a.moveCursorToLastSession()
					}
					return nil
				})
			}()
		case "session_cwd":
			go func() {
				if a.sessions == nil {
					return
				}
				err := a.sessions.CreateAtPaneCWDWithOpts(selectedProfile, options)
				a.gui.Update(func(g *gocui.Gui) error {
					if err != nil {
						a.showError(g, fmt.Sprintf("Error: %v", err))
					} else {
						a.setStatus(g, "Session created")
						a.moveCursorToLastSession()
					}
					return nil
				})
			}()
		case "pm_session":
			go func() {
				if a.sessions == nil {
					return
				}
				err := a.sessions.CreatePMSessionWithOpts(sessionPath, selectedProfile, options)
				a.gui.Update(func(g *gocui.Gui) error {
					if err != nil {
						a.showError(g, fmt.Sprintf("Error: %v", err))
					} else {
						a.setStatus(g, "PM session started")
					}
					return nil
				})
			}()
		}
		return nil
	}

	profileDialogCancel := func(g *gocui.Gui, v *gocui.View) error {
		a.closeProfileDialog(g)
		return nil
	}

	for _, viewName := range []string{"profile-chooser", "profile-options"} {
		if err := a.gui.SetKeybinding(viewName, gocui.KeyEnter, gocui.ModNone, profileDialogConfirm); err != nil {
			return err
		}
		if err := a.gui.SetKeybinding(viewName, gocui.KeyEsc, gocui.ModNone, profileDialogCancel); err != nil {
			return err
		}
	}

	// Tab: Profile chooser → Options → Profile chooser (loop)
	if err := a.gui.SetKeybinding("profile-chooser", gocui.KeyTab, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.dialog.ActiveField = "profile-options"
		if _, err := g.SetCurrentView("profile-options"); err != nil && !isUnknownView(err) {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("profile-options", gocui.KeyTab, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.dialog.ActiveField = "profile-chooser"
		if _, err := g.SetCurrentView("profile-chooser"); err != nil && !isUnknownView(err) {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	// j/k and arrow navigation in profile chooser
	profileChooserMove := func(delta int) func(*gocui.Gui, *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			s := &chooser.State{Items: a.dialog.ProfileItems, Cursor: a.dialog.ProfileCursor}
			chooser.Move(s, delta)
			a.dialog.ProfileCursor = s.Cursor
			renderProfileChooser(v, a.dialog.ProfileItems, a.dialog.ProfileCursor)
			return nil
		}
	}
	for _, key := range []gocui.Key{gocui.KeyArrowDown, gocui.KeyArrowUp} {
		delta := 1
		if key == gocui.KeyArrowUp {
			delta = -1
		}
		if err := a.gui.SetKeybinding("profile-chooser", key, gocui.ModNone, profileChooserMove(delta)); err != nil {
			return err
		}
	}
	for _, ch := range []rune{'j', 'k'} {
		delta := 1
		if ch == 'k' {
			delta = -1
		}
		if err := a.gui.SetKeybinding("profile-chooser", ch, gocui.ModNone, profileChooserMove(delta)); err != nil {
			return err
		}
	}

	// 8.6. Remote profile error dialog (Esc only)
	if err := a.gui.SetKeybinding("remote-profile-error", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.closeRemoteProfileErrorDialog(g)
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

	// 10. Askpass dialog bindings (Enter to submit, Esc to cancel)
	if err := a.gui.SetKeybinding("askpass-input", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		password := v.TextArea.GetContent()
		ch := a.askpassCh
		a.closeAskpassDialog(g)
		if ch != nil {
			ch <- password
		}
		return nil
	}); err != nil {
		return err
	}
	if err := a.gui.SetKeybinding("askpass-input", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		ch := a.askpassCh
		a.closeAskpassDialog(g)
		if ch != nil {
			ch <- ""
		}
		return nil
	}); err != nil {
		return err
	}

	// 11. Connect dialog bindings (Enter to connect, Esc to cancel)
	if err := a.gui.SetKeybinding("connect-input", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		host := strings.TrimSpace(v.TextArea.GetContent())
		a.closeConnectDialog(g)
		if host == "" {
			return nil
		}
		a.connectToHost(g, host)
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

	// 11. Connect chooser bindings (j/k/Arrow/Enter/Esc)
	connectChooserMove := func(delta int) func(*gocui.Gui, *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			maxIdx := len(a.dialog.ConnectHosts) // last index = "Manual input"
			a.dialog.ConnectCursor += delta
			if a.dialog.ConnectCursor < 0 {
				a.dialog.ConnectCursor = 0
			}
			if a.dialog.ConnectCursor > maxIdx {
				a.dialog.ConnectCursor = maxIdx
			}
			renderConnectChooser(v, a.dialog.ConnectHosts, a.dialog.ConnectCursor)
			return nil
		}
	}
	for _, key := range []gocui.Key{gocui.KeyArrowDown, gocui.KeyArrowUp} {
		delta := 1
		if key == gocui.KeyArrowUp {
			delta = -1
		}
		if err := a.gui.SetKeybinding("connect-chooser", key, gocui.ModNone, connectChooserMove(delta)); err != nil {
			return err
		}
	}
	for _, ch := range []rune{'j', 'k'} {
		delta := 1
		if ch == 'k' {
			delta = -1
		}
		if err := a.gui.SetKeybinding("connect-chooser", ch, gocui.ModNone, connectChooserMove(delta)); err != nil {
			return err
		}
	}

	if err := a.gui.SetKeybinding("connect-chooser", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		idx := a.dialog.ConnectCursor
		hosts := a.dialog.ConnectHosts
		a.closeConnectChooser(g)

		if idx >= len(hosts) {
			// "Manual input" selected
			if !a.showConnectDialog(g) {
				a.showError(g, "Error: could not open connect dialog")
			}
		} else {
			a.connectToHost(g, hosts[idx])
		}
		return nil
	}); err != nil {
		return err
	}

	if err := a.gui.SetKeybinding("connect-chooser", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		a.closeConnectChooser(g)
		return nil
	}); err != nil {
		return err
	}

	// 12. Keybind help overlay bindings
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
