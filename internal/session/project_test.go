package session_test

import (
	"testing"

	"github.com/any-context/lazyclaude/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInferProjectRoot_Worktree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "standard worktree path",
			path: "/home/user/projects/lazyclaude/.lazyclaude/worktrees/feat-auth",
			want: "/home/user/projects/lazyclaude",
		},
		{
			name: "nested project root",
			path: "/opt/repos/my-api/.lazyclaude/worktrees/fix-bug",
			want: "/opt/repos/my-api",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := session.InferProjectRoot(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInferProjectRoot_NonWorktree(t *testing.T) {
	t.Parallel()
	// Non-worktree paths return the path itself as project root
	got := session.InferProjectRoot("/home/user/projects/lazyclaude")
	assert.Equal(t, "/home/user/projects/lazyclaude", got)
}

func TestInferProjectRoot_EmptyPath(t *testing.T) {
	t.Parallel()
	got := session.InferProjectRoot("")
	assert.Equal(t, "", got)
}

// --- FindPM tests ---

func TestProject_FindPM_RootPM(t *testing.T) {
	t.Parallel()
	p := session.Project{
		Sessions: []session.Session{
			{ID: "w-1", Name: "feat-x", Role: session.RoleWorker},
			{ID: "pm-1", Name: "pm", Role: session.RolePM, ParentID: ""},
			{ID: "w-2", Name: "feat-y", Role: session.RoleWorker},
		},
	}
	pm := p.FindPM()
	require.NotNil(t, pm)
	assert.Equal(t, "pm-1", pm.ID)
}

func TestProject_FindPM_SkipsSubPM(t *testing.T) {
	t.Parallel()
	// A sub-PM (ParentID != "") should NOT be returned by FindPM.
	p := session.Project{
		Sessions: []session.Session{
			{ID: "sub-pm-1", Name: "sub-pm", Role: session.RolePM, ParentID: "some-parent"},
			{ID: "w-1", Name: "feat-x", Role: session.RoleWorker},
		},
	}
	pm := p.FindPM()
	assert.Nil(t, pm, "FindPM should only return root PM (ParentID=='')")
}

func TestProject_FindPM_NoPM(t *testing.T) {
	t.Parallel()
	p := session.Project{
		Sessions: []session.Session{
			{ID: "w-1", Name: "feat-x", Role: session.RoleWorker},
		},
	}
	pm := p.FindPM()
	assert.Nil(t, pm)
}

// --- RootSessions tests ---

func TestProject_RootSessions_MixedParentIDs(t *testing.T) {
	t.Parallel()
	p := session.Project{
		Sessions: []session.Session{
			{ID: "pm-1", Name: "pm", Role: session.RolePM, ParentID: ""},
			{ID: "w-root", Name: "feat-root", Role: session.RoleWorker, ParentID: ""},
			{ID: "w-child", Name: "feat-child", Role: session.RoleWorker, ParentID: "pm-1"},
			{ID: "sub-pm", Name: "sub-pm", Role: session.RolePM, ParentID: "pm-1"},
		},
	}
	roots := p.RootSessions()
	require.Len(t, roots, 2, "only sessions with ParentID=='' are roots")
	ids := []string{roots[0].ID, roots[1].ID}
	assert.Contains(t, ids, "pm-1")
	assert.Contains(t, ids, "w-root")
}

func TestProject_RootSessions_AllRoot(t *testing.T) {
	t.Parallel()
	p := session.Project{
		Sessions: []session.Session{
			{ID: "s-1", Name: "a", ParentID: ""},
			{ID: "s-2", Name: "b", ParentID: ""},
		},
	}
	roots := p.RootSessions()
	assert.Len(t, roots, 2)
}

func TestProject_RootSessions_Empty(t *testing.T) {
	t.Parallel()
	p := session.Project{}
	roots := p.RootSessions()
	assert.Empty(t, roots)
}

