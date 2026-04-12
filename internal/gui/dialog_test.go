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

	app.dialog.Kind = DialogRename
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

	app.dialog.Kind = DialogWorktree
	if !app.HasActiveDialog() {
		t.Error("should detect active worktree dialog")
	}
}

func TestDialogFocusView_Mapping(t *testing.T) {
	app, err := NewAppHeadless(ModeMain, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Gui().Close()

	cases := []struct {
		dialog DialogKind
		field  string // ActiveField
		want   string
	}{
		{DialogNone, "", ""},
		{DialogRename, "", "rename-input"},
		{DialogWorktreeChooser, "", "worktree-chooser"},
		{DialogWorktreeResume, "", "worktree-resume-prompt"},
		{DialogWorktree, "", "worktree-branch"},              // default
		{DialogWorktree, "worktree-prompt", "worktree-prompt"}, // after Tab
		{DialogConnectChooser, "", "connect-chooser"},
	}
	for _, tc := range cases {
		app.dialog.Kind = tc.dialog
		app.dialog.ActiveField = tc.field
		got := app.dialogFocusView()
		if got != tc.want {
			t.Errorf("dialogFocusView(dialog=%d, field=%q) = %q, want %q",
				tc.dialog, tc.field, got, tc.want)
		}
	}
}
