package daemon

import (
	"testing"
)

func TestSplitHostPort(t *testing.T) {
	tests := []struct {
		input    string
		wantHost string
		wantPort string
	}{
		{"host", "host", ""},
		{"host:22", "host", "22"},
		{"user@host", "user@host", ""},
		{"user@host:22", "user@host", "22"},
		{"user@host:2222", "user@host", "2222"},
		{"user@host:", "user@host:", ""},
		{"user@host:abc", "user@host:abc", ""},
		{"[::1]", "[::1]", ""},
		{"[::1]:22", "[::1]", "22"},
		{"[::1]:2222", "[::1]", "2222"},
		{"[::1]:", "[::1]:", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotHost, gotPort := SplitHostPort(tt.input)
			if gotHost != tt.wantHost || gotPort != tt.wantPort {
				t.Errorf("SplitHostPort(%q) = (%q, %q), want (%q, %q)",
					tt.input, gotHost, gotPort, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestPosixQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "'simple'"},
		{"/home/user/.local/bin", "'/home/user/.local/bin'"},
		{"it's", "'it'\"'\"'s'"},
		{"a b", "'a b'"},
		{"$(cmd)", "'$(cmd)'"},
		{"; rm -rf /", "'; rm -rf /'"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := PosixQuote(tt.input); got != tt.want {
				t.Errorf("PosixQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"22", true},
		{"2222", true},
		{"", false},
		{"abc", false},
		{"22a", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isNumeric(tt.input); got != tt.want {
				t.Errorf("isNumeric(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
