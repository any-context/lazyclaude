//go:build linux

package daemon

import (
	"testing"
)

func TestParseUID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{
			name: "typical status",
			input: "Name:\tbash\n" +
				"Umask:\t0022\n" +
				"State:\tS (sleeping)\n" +
				"Uid:\t1000\t1000\t1000\t1000\n" +
				"Gid:\t1000\t1000\t1000\t1000\n",
			want: 1000,
		},
		{
			name: "root user",
			input: "Name:\tsh\n" +
				"Uid:\t0\t0\t0\t0\n",
			want: 0,
		},
		{
			name:    "no uid line",
			input:   "Name:\tbash\nGid:\t1000\n",
			wantErr: true,
		},
		{
			name:    "malformed uid line",
			input:   "Uid:\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUID(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseUID = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseTTYFromStat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantPTY bool
		wantErr bool
	}{
		{
			name: "pts device (major 136)",
			// tty_nr = 136*256 + 1 = 34817 (pts/1)
			input:   "123 (bash) S 122 123 123 34817 123 4194304 ...",
			wantPTY: true,
		},
		{
			name: "tty device (major 4)",
			// tty_nr = 4*256 + 1 = 1025 (tty1)
			input:   "123 (bash) S 122 123 123 1025 123 4194304 ...",
			wantPTY: false,
		},
		{
			name: "no tty (0)",
			input:   "123 (bash) S 122 123 123 0 123 4194304 ...",
			wantPTY: false,
		},
		{
			name:    "comm with spaces",
			input:   "123 (my shell) S 122 123 123 34817 123 4194304 ...",
			wantPTY: true,
		},
		{
			name:    "comm with parens",
			input:   "123 (bash (v5)) S 122 123 123 34817 123 4194304 ...",
			wantPTY: true,
		},
		{
			name: "high PTY minor number over 255",
			// new_encode_dev: (minor & 0xff) | (major << 8) | ((minor & ~0xff) << 12)
			// major=136, minor=300: (44) | (136 << 8) | (1 << 20) = 1083436
			input:   "123 (bash) S 122 123 123 1083436 123 4194304 ...",
			wantPTY: true,
		},
		{
			name:    "malformed no paren",
			input:   "123 bash S 122 123",
			wantErr: true,
		},
		{
			name:    "too few fields",
			input:   "123 (bash) S 122",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTTYFromStat(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantPTY {
				t.Errorf("parseTTYFromStat = %v, want %v", got, tt.wantPTY)
			}
		})
	}
}

func TestParseSIDFromStatLine(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{
			name: "session leader",
			// pid=123, comm=(bash), state=S, ppid=122, pgrp=123, session=123, tty=34817
			input: "123 (bash) S 122 123 123 34817 123 4194304 ...",
			want:  123,
		},
		{
			name: "child process different sid",
			// pid=456, comm=(gitstatus), state=S, ppid=123, pgrp=456, session=123
			input: "456 (gitstatus) S 123 456 123 0 456 4194304 ...",
			want:  123,
		},
		{
			name: "comm with spaces",
			input: "789 (my shell) S 100 789 789 34817 789 4194304 ...",
			want:  789,
		},
		{
			name:    "malformed no paren",
			input:   "123 bash S 122",
			wantErr: true,
		},
		{
			name:    "too few fields",
			input:   "123 (bash) S 122 123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSIDFromStatLine(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseSIDFromStatLine = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIsShellName(t *testing.T) {
	for _, name := range []string{"bash", "zsh", "fish", "sh"} {
		if !isShellName(name) {
			t.Errorf("expected %q to be a shell name", name)
		}
	}
	for _, name := range []string{"python", "node", "claude", ""} {
		if isShellName(name) {
			t.Errorf("expected %q to NOT be a shell name", name)
		}
	}
}
