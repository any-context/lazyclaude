package gui

import (
	"fmt"
	"path/filepath"

	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/KEMSHlM/lazyclaude/internal/notify"
)

// Popup represents a displayable popup in the stack.
type Popup interface {
	// ID returns a unique identifier for this popup.
	ID() string
	// Window returns the tmux window ID for choice delivery.
	Window() string
	// Title returns the display title.
	Title() string
	// IsDiff returns true if this popup shows a diff view.
	IsDiff() bool
	// ContentLines returns the formatted lines to display.
	ContentLines() []string
	// ContentKinds returns the line kinds for coloring (only meaningful for diff).
	ContentKinds() []presentation.DiffLineKind
	// ScrollY returns the current scroll offset.
	ScrollY() int
	// SetScrollY sets the scroll offset.
	SetScrollY(y int)
	// MaxScroll returns the maximum scroll offset given a viewport height.
	MaxScroll(viewportHeight int) int
}

// ToolPopup implements Popup for non-diff tool notifications.
type ToolPopup struct {
	notification *notify.ToolNotification
	scrollY      int
}

// NewToolPopup creates a ToolPopup from a ToolNotification.
func NewToolPopup(n *notify.ToolNotification) *ToolPopup {
	return &ToolPopup{notification: n}
}

// ID returns the window+toolname as a unique identifier.
func (p *ToolPopup) ID() string {
	return fmt.Sprintf("%s/%s", p.notification.Window, p.notification.ToolName)
}

// Window returns the tmux window ID.
func (p *ToolPopup) Window() string {
	return p.notification.Window
}

// Title returns the popup title.
func (p *ToolPopup) Title() string {
	return fmt.Sprintf(" %s ", p.notification.ToolName)
}

// IsDiff always returns false for ToolPopup.
func (p *ToolPopup) IsDiff() bool {
	return false
}

// ContentLines returns the formatted tool description lines.
func (p *ToolPopup) ContentLines() []string {
	n := p.notification
	td := presentation.ParseToolInput(n.ToolName, n.Input, n.CWD)
	return presentation.FormatToolLines(td)
}

// ContentKinds returns nil — ToolPopup has no diff coloring.
func (p *ToolPopup) ContentKinds() []presentation.DiffLineKind {
	return nil
}

// ScrollY returns the current scroll offset.
func (p *ToolPopup) ScrollY() int {
	return p.scrollY
}

// SetScrollY sets the scroll offset (clamped to >= 0).
func (p *ToolPopup) SetScrollY(y int) {
	if y < 0 {
		y = 0
	}
	p.scrollY = y
}

// MaxScroll returns 0 — ToolPopup does not scroll.
func (p *ToolPopup) MaxScroll(viewportHeight int) int {
	lines := len(p.ContentLines())
	max := lines - viewportHeight
	if max < 0 {
		return 0
	}
	return max
}

// Notification returns the underlying ToolNotification.
// Used for backward compatibility with App-level code.
func (p *ToolPopup) Notification() *notify.ToolNotification {
	return p.notification
}

// DiffPopup implements Popup for diff (Write/Edit) notifications.
type DiffPopup struct {
	notification *notify.ToolNotification
	scrollY      int
	lines        []string
	kinds        []presentation.DiffLineKind
}

// NewDiffPopup creates a DiffPopup from a ToolNotification.
// The notification must have IsDiff() == true.
func NewDiffPopup(n *notify.ToolNotification) *DiffPopup {
	return &DiffPopup{notification: n}
}

// ID returns the window+oldFilePath as a unique identifier.
func (p *DiffPopup) ID() string {
	return fmt.Sprintf("%s/%s", p.notification.Window, p.notification.OldFilePath)
}

// Window returns the tmux window ID.
func (p *DiffPopup) Window() string {
	return p.notification.Window
}

// Title returns the popup title including the file name.
func (p *DiffPopup) Title() string {
	return fmt.Sprintf(" Diff: %s ", filepath.Base(p.notification.OldFilePath))
}

// IsDiff always returns true for DiffPopup.
func (p *DiffPopup) IsDiff() bool {
	return true
}

// ContentLines returns the formatted diff lines, computing and caching on first call.
func (p *DiffPopup) ContentLines() []string {
	p.ensureCache()
	return p.lines
}

// ContentKinds returns the line kinds for coloring.
func (p *DiffPopup) ContentKinds() []presentation.DiffLineKind {
	p.ensureCache()
	return p.kinds
}

// ScrollY returns the current scroll offset.
func (p *DiffPopup) ScrollY() int {
	return p.scrollY
}

// SetScrollY sets the scroll offset (clamped to >= 0).
func (p *DiffPopup) SetScrollY(y int) {
	if y < 0 {
		y = 0
	}
	p.scrollY = y
}

// MaxScroll returns the maximum scroll offset given a viewport height.
func (p *DiffPopup) MaxScroll(viewportHeight int) int {
	lines := len(p.ContentLines())
	max := lines - viewportHeight
	if max < 0 {
		return 0
	}
	return max
}

// Notification returns the underlying ToolNotification.
func (p *DiffPopup) Notification() *notify.ToolNotification {
	return p.notification
}

// ensureCache computes diff lines if not already cached.
func (p *DiffPopup) ensureCache() {
	if p.lines != nil {
		return
	}
	n := p.notification
	diffOutput := generateDiffFromContents(n.OldFilePath, n.NewContents)
	parsed := presentation.ParseUnifiedDiff(diffOutput)

	lines := make([]string, len(parsed))
	kinds := make([]presentation.DiffLineKind, len(parsed))
	for i, dl := range parsed {
		lines[i] = presentation.FormatDiffLine(dl, 4)
		kinds[i] = dl.Kind
	}
	p.lines = lines
	p.kinds = kinds
}

// newPopupFromNotification constructs the appropriate Popup type from a notification.
func newPopupFromNotification(n *notify.ToolNotification) Popup {
	if n.IsDiff() {
		return NewDiffPopup(n)
	}
	return NewToolPopup(n)
}
