package gui

import "testing"

func TestConfigDirForSession_WorkerInWorktree(t *testing.T) {
	app := &App{}
	s := &SessionItem{
		Path: "/project/.claude/worktrees/feat-x",
		Role: "worker",
	}
	got := app.configDirForSession(s)
	if got != "/project/.claude/worktrees/feat-x" {
		t.Errorf("worker worktree session: got %q, want worktree path", got)
	}
}

func TestConfigDirForSession_RegularSession(t *testing.T) {
	app := &App{}
	s := &SessionItem{
		Path: "/project",
		Role: "",
	}
	got := app.configDirForSession(s)
	if got != "/project" {
		t.Errorf("regular session: got %q, want %q", got, "/project")
	}
}

func TestConfigDirForSession_WorktreeNonWorker(t *testing.T) {
	app := &App{}
	s := &SessionItem{
		Path: "/project/.claude/worktrees/feat-y",
		Role: "",
	}
	got := app.configDirForSession(s)
	if got != "/project" {
		t.Errorf("non-worker worktree session: got %q, want project root", got)
	}
}

func TestConfigDirForSession_PMSession(t *testing.T) {
	app := &App{}
	s := &SessionItem{
		Path: "/project",
		Role: "pm",
	}
	got := app.configDirForSession(s)
	if got != "/project" {
		t.Errorf("PM session: got %q, want %q", got, "/project")
	}
}

func TestConfigDirForSession_NilSession(t *testing.T) {
	app := &App{}
	got := app.configDirForSession(nil)
	if got != "" {
		t.Errorf("nil session: got %q, want empty", got)
	}
}

func TestConfigDirForSession_EmptyPath(t *testing.T) {
	app := &App{}
	s := &SessionItem{
		Path: "",
		Role: "worker",
	}
	got := app.configDirForSession(s)
	if got != "" {
		t.Errorf("empty path: got %q, want empty", got)
	}
}
