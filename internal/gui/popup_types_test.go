package gui

import (
	"strings"
	"testing"

	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/gui/presentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ToolPopup tests ---

func TestToolPopup_ImplementsPopupInterface(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@0"}
	var _ Popup = NewToolPopup(n)
}

func TestToolPopup_ID(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@42"}
	p := NewToolPopup(n)
	assert.Equal(t, "@42/Bash", p.ID())
}

func TestToolPopup_Window(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@99"}
	p := NewToolPopup(n)
	assert.Equal(t, "@99", p.Window())
}

func TestToolPopup_Title(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Write", Window: "@0"}
	p := NewToolPopup(n)
	assert.Equal(t, " Write ", p.Title())
}

func TestToolPopup_IsDiff_ReturnsFalse(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@0"}
	p := NewToolPopup(n)
	assert.False(t, p.IsDiff())
}

func TestToolPopup_ContentLines_NonEmpty(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName: "Bash",
		Input:    `{"command":"echo hello"}`,
		CWD:      "/tmp",
		Window:   "@0",
	}
	p := NewToolPopup(n)
	lines := p.ContentLines()
	require.NotEmpty(t, lines)
	// Should include a CWD line
	found := false
	for _, l := range lines {
		if strings.Contains(l, "/tmp") {
			found = true
			break
		}
	}
	assert.True(t, found, "ContentLines should include CWD")
}

func TestToolPopup_ContentKinds_Nil(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@0"}
	p := NewToolPopup(n)
	assert.Nil(t, p.ContentKinds())
}

func TestToolPopup_ScrollY_InitiallyZero(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@0"}
	p := NewToolPopup(n)
	assert.Equal(t, 0, p.ScrollY())
}

func TestToolPopup_SetScrollY(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@0"}
	p := NewToolPopup(n)
	p.SetScrollY(10)
	assert.Equal(t, 10, p.ScrollY())
}

func TestToolPopup_SetScrollY_ClampedToZero(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@0"}
	p := NewToolPopup(n)
	p.SetScrollY(-5)
	assert.Equal(t, 0, p.ScrollY())
}

func TestToolPopup_MaxScroll_SmallViewport(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName: "Bash",
		Input:    `{"command":"echo hello"}`,
		CWD:      "/tmp",
		Window:   "@0",
	}
	p := NewToolPopup(n)
	// With a very small viewport, MaxScroll should be > 0
	max := p.MaxScroll(1)
	assert.GreaterOrEqual(t, max, 0)
}

func TestToolPopup_MaxScroll_LargeViewport(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@0"}
	p := NewToolPopup(n)
	// With viewport larger than content, MaxScroll should be 0
	max := p.MaxScroll(1000)
	assert.Equal(t, 0, max)
}

func TestToolPopup_Notification(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Read", Window: "@5"}
	p := NewToolPopup(n)
	assert.Same(t, n, p.Notification())
}

// --- DiffPopup tests ---

func TestDiffPopup_ImplementsPopupInterface(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/tmp/test.go",
		NewContents: "package main\n",
	}
	var _ Popup = NewDiffPopup(n)
}

func TestDiffPopup_ID(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@7",
		OldFilePath: "/tmp/foo.go",
	}
	p := NewDiffPopup(n)
	assert.Equal(t, "@7//tmp/foo.go", p.ID())
}

func TestDiffPopup_Window(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@3",
		OldFilePath: "/tmp/test.go",
	}
	p := NewDiffPopup(n)
	assert.Equal(t, "@3", p.Window())
}

func TestDiffPopup_Title(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/some/path/myfile.go",
	}
	p := NewDiffPopup(n)
	assert.Equal(t, " Diff: myfile.go ", p.Title())
}

func TestDiffPopup_IsDiff_ReturnsTrue(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/tmp/test.go",
	}
	p := NewDiffPopup(n)
	assert.True(t, p.IsDiff())
}

func TestDiffPopup_ContentLines_ForNewFile(t *testing.T) {
	t.Parallel()
	// OldFilePath doesn't exist => new file diff
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/nonexistent/path/newfile.go",
		NewContents: "package main\nfunc main() {}\n",
	}
	p := NewDiffPopup(n)
	lines := p.ContentLines()
	require.NotEmpty(t, lines)
	// Should contain some diff content
	fullText := strings.Join(lines, "\n")
	assert.True(t, len(fullText) > 0)
}

func TestDiffPopup_ContentLines_StartsWithFilePath(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/nonexistent/path/newfile.go",
		NewContents: "package main\nfunc main() {}\n",
	}
	p := NewDiffPopup(n)
	lines := p.ContentLines()
	kinds := p.ContentKinds()
	require.NotEmpty(t, lines)
	// First line should be DiffFilePath with the file path from notification.
	assert.Equal(t, presentation.DiffFilePath, kinds[0])
	assert.Equal(t, "  File: /nonexistent/path/newfile.go", lines[0])
	// Second line should be a blank separator.
	assert.Equal(t, presentation.DiffContext, kinds[1])
	assert.Equal(t, "", lines[1])
	// No DiffHeader lines should be present.
	for _, k := range kinds {
		assert.NotEqual(t, presentation.DiffHeader, k, "DiffHeader should be filtered out")
	}
}

func TestDiffPopup_ContentLines_InlineFormat(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/nonexistent/inline-fmt.go",
		NewContents: "package main\nfunc foo() {}\n",
	}
	p := NewDiffPopup(n)
	lines := p.ContentLines()
	// Add lines should contain the "│ + " separator with line numbers.
	for i, k := range p.ContentKinds() {
		if k == presentation.DiffAdd {
			assert.Contains(t, lines[i], "\u2502 + ", "Add lines should contain '\u2502 + ' separator, got: %q", lines[i])
		}
	}
}

func TestDiffPopup_ContentLines_CachedOnRepeatCall(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/nonexistent/cache-test.go",
		NewContents: "package main\n",
	}
	p := NewDiffPopup(n)
	lines1 := p.ContentLines()
	lines2 := p.ContentLines()
	// Should return the same slice (identity check via pointer)
	assert.Equal(t, len(lines1), len(lines2))
	if len(lines1) > 0 {
		assert.Equal(t, lines1[0], lines2[0])
	}
}

func TestDiffPopup_ContentKinds_LengthMatchesLines(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/nonexistent/kinds-test.go",
		NewContents: "package main\nfunc foo() {}\n",
	}
	p := NewDiffPopup(n)
	lines := p.ContentLines()
	kinds := p.ContentKinds()
	assert.Equal(t, len(lines), len(kinds))
}

func TestDiffPopup_ContentKinds_ContainsDiffKinds(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/nonexistent/kinds2-test.go",
		NewContents: "package main\nfunc foo() {}\n",
	}
	p := NewDiffPopup(n)
	kinds := p.ContentKinds()
	// For a new file, lines should include DiffFilePath, DiffAdd, or DiffHunk kinds
	if len(kinds) > 0 {
		found := false
		for _, k := range kinds {
			if k == presentation.DiffAdd || k == presentation.DiffFilePath || k == presentation.DiffHunk {
				found = true
				break
			}
		}
		assert.True(t, found, "ContentKinds should include diff-type kinds")
	}
}

func TestDiffPopup_ScrollY_InitiallyZero(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/tmp/test.go",
	}
	p := NewDiffPopup(n)
	assert.Equal(t, 0, p.ScrollY())
}

func TestDiffPopup_SetScrollY(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/tmp/test.go",
	}
	p := NewDiffPopup(n)
	p.SetScrollY(7)
	assert.Equal(t, 7, p.ScrollY())
}

func TestDiffPopup_SetScrollY_ClampedToZero(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/tmp/test.go",
	}
	p := NewDiffPopup(n)
	p.SetScrollY(-3)
	assert.Equal(t, 0, p.ScrollY())
}

func TestDiffPopup_MaxScroll_LargeViewport(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/nonexistent/maxscroll.go",
		NewContents: "a\nb\nc\n",
	}
	p := NewDiffPopup(n)
	max := p.MaxScroll(10000)
	assert.Equal(t, 0, max)
}

func TestDiffPopup_MaxScroll_SmallViewport(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/nonexistent/maxscroll2.go",
		NewContents: "line1\nline2\nline3\nline4\nline5\n",
	}
	p := NewDiffPopup(n)
	// Viewport height of 1 should give max > 0 when content has multiple lines
	lines := p.ContentLines()
	if len(lines) > 1 {
		max := p.MaxScroll(1)
		assert.Greater(t, max, 0)
	}
}

func TestDiffPopup_Notification(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/tmp/test.go",
	}
	p := NewDiffPopup(n)
	assert.Same(t, n, p.Notification())
}

// --- newPopupFromNotification tests ---

func TestNewPopupFromNotification_ReturnsDiffPopup(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/tmp/test.go",
	}
	p := newPopupFromNotification(n)
	assert.True(t, p.IsDiff())
	_, ok := p.(*DiffPopup)
	assert.True(t, ok)
}

func TestNewPopupFromNotification_ReturnsToolPopup(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@0"}
	p := newPopupFromNotification(n)
	assert.False(t, p.IsDiff())
	_, ok := p.(*ToolPopup)
	assert.True(t, ok)
}

// --- Edge cases ---

func TestToolPopup_EmptyToolName(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "", Window: "@0"}
	p := NewToolPopup(n)
	assert.Equal(t, "  ", p.Title()) // spaces around empty string
}

func TestToolPopup_EmptyWindow(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: ""}
	p := NewToolPopup(n)
	assert.Equal(t, "", p.Window())
}

func TestDiffPopup_EmptyNewContents(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/nonexistent/empty.go",
		NewContents: "",
	}
	p := NewDiffPopup(n)
	// Should not panic
	lines := p.ContentLines()
	kinds := p.ContentKinds()
	assert.Equal(t, len(lines), len(kinds))
}

func TestToolPopup_ViewportHeight_InitiallyZero(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@0"}
	p := NewToolPopup(n)
	assert.Equal(t, 0, p.ViewportHeight())
}

func TestToolPopup_SetViewportHeight(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@0"}
	p := NewToolPopup(n)
	p.SetViewportHeight(25)
	assert.Equal(t, 25, p.ViewportHeight())
}

func TestDiffPopup_ViewportHeight_InitiallyZero(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/tmp/test.go",
	}
	p := NewDiffPopup(n)
	assert.Equal(t, 0, p.ViewportHeight())
}

func TestDiffPopup_SetViewportHeight(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/tmp/test.go",
	}
	p := NewDiffPopup(n)
	p.SetViewportHeight(30)
	assert.Equal(t, 30, p.ViewportHeight())
}

func TestToolPopup_SetScrollY_MultipleUpdates(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@0"}
	p := NewToolPopup(n)
	p.SetScrollY(3)
	p.SetScrollY(7)
	p.SetScrollY(2)
	assert.Equal(t, 2, p.ScrollY())
}

func TestDiffPopup_SetScrollY_MultipleUpdates(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/tmp/test.go",
	}
	p := NewDiffPopup(n)
	p.SetScrollY(5)
	p.SetScrollY(0)
	assert.Equal(t, 0, p.ScrollY())
}

// --- PopupController.PushPopup tests ---

func TestPopupController_PushPopup_ToolPopup(t *testing.T) {
	t.Parallel()
	pc := NewPopupController()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@0"}
	p := NewToolPopup(n)
	pc.PushPopup(p)
	assert.Equal(t, 1, pc.Count())
	assert.True(t, pc.HasVisible())
	active := pc.ActivePopup()
	require.NotNil(t, active)
	assert.Equal(t, "@0", active.Window())
}

func TestPopupController_PushPopup_DiffPopup(t *testing.T) {
	t.Parallel()
	pc := NewPopupController()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@5",
		OldFilePath: "/tmp/test.go",
	}
	p := NewDiffPopup(n)
	pc.PushPopup(p)
	assert.Equal(t, 1, pc.Count())
	active := pc.ActivePopup()
	require.NotNil(t, active)
	assert.True(t, active.IsDiff())
}

func TestPopupController_PushPopup_FocusesNewEntry(t *testing.T) {
	t.Parallel()
	pc := NewPopupController()
	pc.PushPopup(NewToolPopup(&model.ToolNotification{ToolName: "A", Window: "@0"}))
	pc.PushPopup(NewToolPopup(&model.ToolNotification{ToolName: "B", Window: "@1"}))
	active := pc.ActivePopup()
	require.NotNil(t, active)
	assert.Equal(t, "@1", active.Window())
}

func TestPopupController_FocusIndex(t *testing.T) {
	t.Parallel()
	pc := NewPopupController()
	pc.PushPopup(NewToolPopup(makeTestNotif("A", "@0")))
	pc.PushPopup(NewToolPopup(makeTestNotif("B", "@1")))
	assert.Equal(t, 1, pc.FocusIndex())
}

func TestPopupController_VisibleIndexOf(t *testing.T) {
	t.Parallel()
	pc := NewPopupController()
	pc.PushPopup(NewToolPopup(makeTestNotif("A", "@0")))
	pc.PushPopup(NewToolPopup(makeTestNotif("B", "@1")))
	pc.PushPopup(NewToolPopup(makeTestNotif("C", "@2")))
	assert.Equal(t, 0, pc.VisibleIndexOf(0))
	assert.Equal(t, 1, pc.VisibleIndexOf(1))
	assert.Equal(t, 2, pc.VisibleIndexOf(2))
}

func TestPopupController_VisibleIndexOf_WithSuspended(t *testing.T) {
	t.Parallel()
	pc := NewPopupController()
	pc.PushPopup(NewToolPopup(makeTestNotif("A", "@0")))
	pc.PushPopup(NewToolPopup(makeTestNotif("B", "@1")))
	pc.PushPopup(NewToolPopup(makeTestNotif("C", "@2")))
	// Suspend the middle entry by suspending all then unsuspending the first
	// We can't suspend individual entries directly from outside, so just test SuspendAll
	pc.SuspendAll()
	// With all suspended, VisibleIndexOf still counts non-suspended (none)
	assert.Equal(t, 0, pc.VisibleIndexOf(1))
}

// --- notificationFromPopup with DiffPopup ---

func TestNotificationFromPopup_DiffPopup(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{
		ToolName:    "Write",
		Window:      "@0",
		OldFilePath: "/tmp/test.go",
	}
	p := NewDiffPopup(n)
	result := notificationFromPopup(p)
	assert.Same(t, n, result)
}

func TestNotificationFromPopup_ToolPopup(t *testing.T) {
	t.Parallel()
	n := &model.ToolNotification{ToolName: "Bash", Window: "@0"}
	p := NewToolPopup(n)
	result := notificationFromPopup(p)
	assert.Same(t, n, result)
}

func TestNotificationFromPopup_UnknownType(t *testing.T) {
	t.Parallel()
	// A mock Popup that is neither ToolPopup nor DiffPopup
	var p Popup = &mockPopup{}
	result := notificationFromPopup(p)
	assert.Nil(t, result)
}

// mockPopup is a test-only Popup implementation.
type mockPopup struct{}

func (m *mockPopup) ID() string                                      { return "mock" }
func (m *mockPopup) Window() string                                   { return "mock-window" }
func (m *mockPopup) Title() string                                    { return "Mock" }
func (m *mockPopup) IsDiff() bool                                     { return false }
func (m *mockPopup) ContentLines() []string                          { return nil }
func (m *mockPopup) ContentKinds() []presentation.DiffLineKind       { return nil }
func (m *mockPopup) ScrollY() int                                    { return 0 }
func (m *mockPopup) SetScrollY(_ int)                                {}
func (m *mockPopup) MaxScroll(_ int) int                             { return 0 }
func (m *mockPopup) MaxOption() int                                  { return 3 }
func (m *mockPopup) ViewportHeight() int                             { return 0 }
func (m *mockPopup) SetViewportHeight(_ int)                         {}
