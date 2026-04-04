package keyhandler

import "github.com/any-context/lazyclaude/internal/gui/keymap"

// FullScreenHandler handles special keys in full-screen mode.
// Rune keys are NOT handled here — inputEditor.Edit() handles those.
type FullScreenHandler struct {
	reg *keymap.Registry
}

// NewFullScreenHandler creates a FullScreenHandler with injected registry.
func NewFullScreenHandler(reg *keymap.Registry) *FullScreenHandler {
	return &FullScreenHandler{reg: reg}
}

// HandleKey dispatches fullscreen-scoped key events.
// When scroll mode is active, dispatches to ScopeScroll instead.
func (h *FullScreenHandler) HandleKey(ev KeyEvent, actions FullScreenActions) HandlerResult {
	if !actions.IsFullScreen() {
		return Unhandled
	}

	// Scroll mode sub-state: dispatch scroll actions and consume all keys.
	scrollActions, ok := actions.(ScrollActions)
	if ok && scrollActions.IsScrollMode() {
		return h.handleScrollKey(ev, scrollActions)
	}

	def, ok := h.reg.Match(ev.Rune, ev.Key, ev.Mod, keymap.ScopeFullScreen)
	if !ok {
		return Unhandled
	}

	switch def.Action {
	case keymap.ActionExitFull:
		actions.ExitFullScreen()
	case keymap.ActionScrollEnter:
		if sa, ok := actions.(ScrollActions); ok {
			sa.ScrollModeEnter()
		}
	case keymap.ActionForwardEnter:
		actions.ForwardSpecialKey("Enter")
	case keymap.ActionForwardEsc:
		actions.ForwardSpecialKey("Escape")
	case keymap.ActionForwardDown:
		actions.ForwardSpecialKey("Down")
	case keymap.ActionForwardUp:
		actions.ForwardSpecialKey("Up")
	case keymap.ActionDismissError:
		actions.DismissError()
	case keymap.ActionCopyError:
		actions.CopyError()
	default:
		return Unhandled
	}
	return Handled
}

// handleScrollKey dispatches scroll-mode keys. All keys are consumed to prevent leaking.
func (h *FullScreenHandler) handleScrollKey(ev KeyEvent, actions ScrollActions) HandlerResult {
	def, ok := h.reg.Match(ev.Rune, ev.Key, ev.Mod, keymap.ScopeScroll)
	if !ok {
		return Handled // consume unbound keys in scroll mode
	}

	switch def.Action {
	case keymap.ActionScrollUp:
		actions.ScrollModeUp()
	case keymap.ActionScrollDown:
		actions.ScrollModeDown()
	case keymap.ActionScrollHalfUp:
		actions.ScrollModeHalfUp()
	case keymap.ActionScrollHalfDown:
		actions.ScrollModeHalfDown()
	case keymap.ActionScrollToTop:
		actions.ScrollModeToTop()
	case keymap.ActionScrollToBottom:
		actions.ScrollModeToBottom()
	case keymap.ActionScrollToggleSelect:
		actions.ScrollModeToggleSelect()
	case keymap.ActionScrollCopy:
		actions.ScrollModeCopy()
	case keymap.ActionScrollExit:
		actions.ScrollModeExit()
	}
	return Handled
}
