package gui

import (
	"fmt"
	"path/filepath"

	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/gui/presentation"
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
	// MaxOption returns the number of dialog options (2 or 3).
	MaxOption() int
	// ContentLines returns the formatted lines to display.
	ContentLines() []string
	// ContentKinds returns the line kinds for coloring (only meaningful for diff).
	ContentKinds() []presentation.DiffLineKind
	// ScrollY returns the current scroll offset.
	ScrollY() int
	// SetScrollY sets the scroll offset (clamped to [0, content length]).
	SetScrollY(y int)
	// MaxScroll returns the maximum scroll offset given a viewport height.
	MaxScroll(viewportHeight int) int
	// ViewportHeight returns the visible line count set by layout.
	ViewportHeight() int
	// SetViewportHeight stores the visible line count determined during layout.
	SetViewportHeight(h int)
}

// ToolPopup implements Popup for non-diff tool notifications.
type ToolPopup struct {
	notification   *model.ToolNotification
	scrollY        int
	viewportHeight int
}

// NewToolPopup creates a ToolPopup from a ToolNotification.
func NewToolPopup(n *model.ToolNotification) *ToolPopup {
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

// MaxOption returns the number of dialog options from the notification.
func (p *ToolPopup) MaxOption() int {
	if p.notification.MaxOption > 0 {
		return p.notification.MaxOption
	}
	return 3
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

// MaxScroll returns the maximum scroll offset given a viewport height.
func (p *ToolPopup) MaxScroll(viewportHeight int) int {
	return maxScrollFor(len(p.ContentLines()), viewportHeight)
}

// ViewportHeight returns the visible line count set by layout.
func (p *ToolPopup) ViewportHeight() int { return p.viewportHeight }

// SetViewportHeight stores the visible line count determined during layout.
func (p *ToolPopup) SetViewportHeight(h int) { p.viewportHeight = h }

// Notification returns the underlying ToolNotification.
// Used for backward compatibility with App-level code.
func (p *ToolPopup) Notification() *model.ToolNotification {
	return p.notification
}

// DiffPopup implements Popup for diff (Write/Edit) notifications.
type DiffPopup struct {
	notification   *model.ToolNotification
	scrollY        int
	viewportHeight int
	lines          []string
	kinds          []presentation.DiffLineKind
}

// NewDiffPopup creates a DiffPopup from a ToolNotification.
// The notification must have IsDiff() == true.
func NewDiffPopup(n *model.ToolNotification) *DiffPopup {
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

// MaxOption returns the number of dialog options from the notification.
func (p *DiffPopup) MaxOption() int {
	if p.notification.MaxOption > 0 {
		return p.notification.MaxOption
	}
	return 3
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
	return maxScrollFor(len(p.ContentLines()), viewportHeight)
}

// ViewportHeight returns the visible line count set by layout.
func (p *DiffPopup) ViewportHeight() int { return p.viewportHeight }

// SetViewportHeight stores the visible line count determined during layout.
func (p *DiffPopup) SetViewportHeight(h int) { p.viewportHeight = h }

// Notification returns the underlying ToolNotification.
func (p *DiffPopup) Notification() *model.ToolNotification {
	return p.notification
}

// ensureCache computes diff lines if not already cached.
// Must be called from the gocui layout goroutine only.
func (p *DiffPopup) ensureCache() {
	if p.lines != nil {
		return
	}
	n := p.notification
	diffOutput := generateDiffFromContents(n.OldFilePath, n.NewContents)
	parsed := presentation.ParseUnifiedDiff(diffOutput)

	// Compute line-number column width from the maximum line number.
	numWidth := presentation.NumWidth(presentation.MaxLineNum(parsed))

	// Prepend file path line from notification (not from diff headers).
	var lines []string
	var kinds []presentation.DiffLineKind
	fpLine := presentation.DiffLine{Kind: presentation.DiffFilePath, Content: n.OldFilePath}
	lines = append(lines, presentation.FormatInlineDiffLine(fpLine, numWidth))
	kinds = append(kinds, presentation.DiffFilePath)

	// Blank line after file path (also serves as separator before first hunk).
	lines = append(lines, "")
	kinds = append(kinds, presentation.DiffContext)

	// Skip DiffHeader lines (diff --git, ---, +++); use inline format.
	// Insert blank line before each subsequent hunk for visual separation.
	firstHunk := true
	for _, dl := range parsed {
		if dl.Kind == presentation.DiffHeader {
			continue
		}
		if dl.Kind == presentation.DiffHunk && !firstHunk {
			lines = append(lines, "")
			kinds = append(kinds, presentation.DiffContext)
		}
		if dl.Kind == presentation.DiffHunk {
			firstHunk = false
		}
		lines = append(lines, presentation.FormatInlineDiffLine(dl, numWidth))
		kinds = append(kinds, dl.Kind)
	}
	p.lines = lines
	p.kinds = kinds
}

// maxScrollFor computes the maximum scroll offset for a given line count and viewport height.
func maxScrollFor(lineCount, viewportHeight int) int {
	if m := lineCount - viewportHeight; m > 0 {
		return m
	}
	return 0
}

// newPopupFromNotification constructs the appropriate Popup type from a notification.
func newPopupFromNotification(n *model.ToolNotification) Popup {
	if n.IsDiff() {
		return NewDiffPopup(n)
	}
	return NewToolPopup(n)
}
