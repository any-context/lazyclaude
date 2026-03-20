package gui

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/notify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestNotif(tool, window string) *notify.ToolNotification {
	return &notify.ToolNotification{ToolName: tool, Window: window}
}

func TestPopupController_PushAndCount(t *testing.T) {
	t.Parallel()
	pc := NewPopupController(nil)
	assert.Equal(t, 0, pc.Count())
	assert.False(t, pc.HasVisible())

	pc.Push(makeTestNotif("Bash", "@0"))
	assert.Equal(t, 1, pc.Count())
	assert.True(t, pc.HasVisible())
}

func TestPopupController_ActiveEntry(t *testing.T) {
	t.Parallel()
	pc := NewPopupController(nil)
	pc.Push(makeTestNotif("Bash", "@0"))
	pc.Push(makeTestNotif("Write", "@1"))

	active := pc.ActiveNotification()
	require.NotNil(t, active)
	assert.Equal(t, "Write", active.ToolName)
}

func TestPopupController_Dismiss(t *testing.T) {
	t.Parallel()
	var sent []sentChoicePair
	choiceFn := func(window string, choice Choice) {
		sent = append(sent, sentChoicePair{window, choice})
	}
	pc := NewPopupController(choiceFn)
	pc.Push(makeTestNotif("Bash", "@0"))
	pc.Push(makeTestNotif("Write", "@1"))

	pc.DismissActive(ChoiceAccept)
	assert.Equal(t, 1, pc.Count())
	assert.Equal(t, "Bash", pc.ActiveNotification().ToolName)
	require.Len(t, sent, 1)
	assert.Equal(t, "@1", sent[0].window)
	assert.Equal(t, ChoiceAccept, sent[0].choice)
}

func TestPopupController_DismissAll(t *testing.T) {
	t.Parallel()
	var sent []sentChoicePair
	choiceFn := func(window string, choice Choice) {
		sent = append(sent, sentChoicePair{window, choice})
	}
	pc := NewPopupController(choiceFn)
	pc.Push(makeTestNotif("Bash", "@0"))
	pc.Push(makeTestNotif("Write", "@1"))

	pc.DismissAll(ChoiceAccept)
	assert.Equal(t, 0, pc.Count())
	assert.False(t, pc.HasVisible())
	require.Len(t, sent, 2)
}

func TestPopupController_SuspendAndUnsuspend(t *testing.T) {
	t.Parallel()
	pc := NewPopupController(nil)
	pc.Push(makeTestNotif("Bash", "@0"))
	pc.Push(makeTestNotif("Write", "@1"))

	pc.SuspendAll()
	assert.False(t, pc.HasVisible())
	assert.Equal(t, 2, pc.Count())

	pc.UnsuspendAll()
	assert.True(t, pc.HasVisible())
	assert.Equal(t, "Write", pc.ActiveNotification().ToolName)
}

func TestPopupController_FocusCycle(t *testing.T) {
	t.Parallel()
	pc := NewPopupController(nil)
	pc.Push(makeTestNotif("A", "@0"))
	pc.Push(makeTestNotif("B", "@1"))
	pc.Push(makeTestNotif("C", "@2"))

	assert.Equal(t, "C", pc.ActiveNotification().ToolName)

	pc.FocusPrev()
	assert.Equal(t, "B", pc.ActiveNotification().ToolName)

	pc.FocusNext()
	assert.Equal(t, "C", pc.ActiveNotification().ToolName)
}

func TestPopupController_DismissOnEmpty(t *testing.T) {
	t.Parallel()
	pc := NewPopupController(nil)
	pc.DismissActive(ChoiceAccept) // should not panic
	assert.Equal(t, 0, pc.Count())
}

func TestPopupController_VisibleCount(t *testing.T) {
	t.Parallel()
	pc := NewPopupController(nil)
	pc.Push(makeTestNotif("A", "@0"))
	pc.Push(makeTestNotif("B", "@1"))
	assert.Equal(t, 2, pc.VisibleCount())

	pc.SuspendAll()
	assert.Equal(t, 0, pc.VisibleCount())
}

func TestPopupController_ActiveEntry_Scroll(t *testing.T) {
	t.Parallel()
	pc := NewPopupController(nil)
	pc.Push(makeTestNotif("Bash", "@0"))

	entry := pc.ActiveEntry()
	require.NotNil(t, entry)
	entry.popup.SetScrollY(5)
	assert.Equal(t, 5, pc.ActiveEntry().popup.ScrollY())
}

type sentChoicePair struct {
	window string
	choice Choice
}
