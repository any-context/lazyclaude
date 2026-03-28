package session_test

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/session"
	"github.com/stretchr/testify/assert"
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
			path: "/home/user/projects/lazyclaude/.claude/worktrees/feat-auth",
			want: "/home/user/projects/lazyclaude",
		},
		{
			name: "nested project root",
			path: "/opt/repos/my-api/.claude/worktrees/fix-bug",
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

