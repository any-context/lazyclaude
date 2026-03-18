package tmux_test

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/stretchr/testify/assert"
)

func TestParseControlLine_Output(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line     string
		wantType tmux.ControlEventType
		wantPane string
		wantData string
	}{
		{"%output %0 hello world", tmux.EventOutput, "%0", "hello world"},
		{"%output %1 ls /\\015\\012", tmux.EventOutput, "%1", "ls /\\015\\012"},
		{"%output %42 ", tmux.EventOutput, "%42", ""},
		{"%begin 123 456 1", tmux.EventBegin, "", "123 456 1"},
		{"%end 123 456 1", tmux.EventEnd, "", "123 456 1"},
		{"%error 123 456 1", tmux.EventError, "", "123 456 1"},
		{"%session-changed $1 mysession", tmux.EventOther, "", ""},
		{"some random line", tmux.EventOther, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			ev := tmux.ParseControlLine(tt.line)
			assert.Equal(t, tt.wantType, ev.Type)
			if tt.wantPane != "" {
				assert.Equal(t, tt.wantPane, ev.PaneID)
			}
			if tt.wantData != "" {
				assert.Equal(t, tt.wantData, ev.Data)
			}
		})
	}
}
