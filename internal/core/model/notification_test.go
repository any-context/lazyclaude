package model_test

import (
	"testing"

	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/stretchr/testify/assert"
)

func TestIsDiff_True(t *testing.T) {
	t.Parallel()
	n := model.ToolNotification{ToolName: "Write", OldFilePath: "/tmp/test.go"}
	assert.True(t, n.IsDiff())
}

func TestIsDiff_False(t *testing.T) {
	t.Parallel()
	n := model.ToolNotification{ToolName: "Bash"}
	assert.False(t, n.IsDiff())
}

func TestActivityState_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		state model.ActivityState
		want  string
	}{
		{"Unknown", model.ActivityUnknown, "unknown"},
		{"Running", model.ActivityRunning, "running"},
		{"NeedsInput", model.ActivityNeedsInput, "needs_input"},
		{"Idle", model.ActivityIdle, "idle"},
		{"Error", model.ActivityError, "error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.state.String())
		})
	}
}

func TestActivityState_ZeroValue(t *testing.T) {
	t.Parallel()
	var s model.ActivityState
	assert.Equal(t, model.ActivityUnknown, s)
	assert.Equal(t, "unknown", s.String())
}
