package gui

import "testing"

func TestHasActiveDialog_None(t *testing.T) {
	app, err := NewAppHeadless(ModeMain, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Gui().Close()

	if app.HasActiveDialog() {
		t.Error("no dialog should be active initially")
	}
}

func TestHasActiveDialog_Rename(t *testing.T) {
	app, err := NewAppHeadless(ModeMain, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Gui().Close()

	app.activeDialog = DialogRename
	if !app.HasActiveDialog() {
		t.Error("should detect active rename dialog")
	}
	if app.ActiveDialogKind() != DialogRename {
		t.Errorf("kind = %d, want DialogRename", app.ActiveDialogKind())
	}
}

func TestHasActiveDialog_Worktree(t *testing.T) {
	app, err := NewAppHeadless(ModeMain, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Gui().Close()

	app.activeDialog = DialogWorktree
	if !app.HasActiveDialog() {
		t.Error("should detect active worktree dialog")
	}
	if app.ActiveDialogKind() != DialogWorktree {
		t.Errorf("kind = %d, want DialogWorktree", app.ActiveDialogKind())
	}
}
