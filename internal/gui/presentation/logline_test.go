package presentation

import (
	"testing"
)

func TestClassifyLogLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want LogLevel
	}{
		{
			name: "server error",
			line: "2024/04/13 10:00:00 server error: bind failed",
			want: LogLevelError,
		},
		{
			name: "warning prefix",
			line: "2024/04/13 10:00:00 warning: write port file: permission denied",
			want: LogLevelWarn,
		},
		{
			name: "ws read debug",
			line: "2024/04/13 10:00:00 ws read conn123: EOF",
			want: LogLevelDebug,
		},
		{
			name: "ws parse debug",
			line: "2024/04/13 10:00:00 ws parse conn123: invalid json",
			want: LogLevelDebug,
		},
		{
			name: "ws marshal debug",
			line: "2024/04/13 10:00:00 ws marshal conn123: unsupported type",
			want: LogLevelDebug,
		},
		{
			name: "ws write debug",
			line: "2024/04/13 10:00:00 ws write conn123: broken pipe",
			want: LogLevelDebug,
		},
		{
			name: "info notify",
			line: "2024/04/13 10:00:00 notify: type=tool pid=1234 window=session1",
			want: LogLevelInfo,
		},
		{
			name: "info ws connected",
			line: "2024/04/13 10:00:00 ws connected: abc123",
			want: LogLevelInfo,
		},
		{
			name: "info listening",
			line: "2024/04/13 10:00:00 listening on 127.0.0.1:8080",
			want: LogLevelInfo,
		},
		{
			name: "short line",
			line: "short",
			want: LogLevelInfo,
		},
		{
			name: "empty line",
			line: "",
			want: LogLevelInfo,
		},
		// Regression: false positive guards
		{
			name: "no false positive on error in payload",
			line: "2024/04/13 10:00:00 msg/create: nil result with no error",
			want: LogLevelInfo,
		},
		{
			name: "no false positive on warn in file path",
			line: "2024/04/13 10:00:00 openDiff: window=@0 file=/home/user/warnings.go",
			want: LogLevelInfo,
		},
		{
			name: "no false positive on fail in function name",
			line: "2024/04/13 10:00:00 notify: resolved failover handler",
			want: LogLevelInfo,
		},
		{
			name: "no false positive on error in notify",
			line: "2024/04/13 10:00:00 notify: encode error: EOF",
			want: LogLevelInfo,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyLogLine(tt.line)
			if got != tt.want {
				t.Errorf("ClassifyLogLine(%q) = %d, want %d", tt.line, got, tt.want)
			}
		})
	}
}

func TestColorizeLogLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantPfx  string // expected prefix escape
		wantSfx  string // expected suffix (Reset)
		noEscape bool   // true if no coloring expected
	}{
		{
			name:    "error gets red",
			line:    "2024/04/13 10:00:00 server error: bind",
			wantPfx: fgLogError,
			wantSfx: Reset,
		},
		{
			name:    "warning gets yellow",
			line:    "2024/04/13 10:00:00 warning: port file",
			wantPfx: fgLogWarn,
			wantSfx: Reset,
		},
		{
			name:    "debug gets dim",
			line:    "2024/04/13 10:00:00 ws read conn1: EOF",
			wantPfx: fgLogDebug,
			wantSfx: Reset,
		},
		{
			name:     "info unchanged",
			line:     "2024/04/13 10:00:00 listening on 127.0.0.1:8080",
			noEscape: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ColorizeLogLine(tt.line)
			if tt.noEscape {
				if got != tt.line {
					t.Errorf("expected no color, got %q", got)
				}
				return
			}
			want := tt.wantPfx + tt.line + tt.wantSfx
			if got != want {
				t.Errorf("ColorizeLogLine(%q)\n  got  %q\n  want %q", tt.line, got, want)
			}
		})
	}
}
