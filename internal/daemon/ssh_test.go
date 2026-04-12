package daemon

import (
	"testing"

	"github.com/any-context/lazyclaude/internal/core/shell"
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

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "'simple'"},
		{"/home/user/.local/bin", "'/home/user/.local/bin'"},
		{"it's", "'it'\\''s'"},
		{"a b", "'a b'"},
		{"$(cmd)", "'$(cmd)'"},
		{"; rm -rf /", "'; rm -rf /'"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := shell.Quote(tt.input); got != tt.want {
				t.Errorf("shell.Quote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSSHEnv_WithAskpass(t *testing.T) {
	e := &ExecSSHExecutor{
		AskpassScript: "/tmp/lazyclaude/askpass-123.sh",
		AskpassSock:   "/tmp/lazyclaude/askpass-123.sock",
	}
	env := e.SSHEnv()

	// Check required env vars (DISPLAY is conditional on os.Getenv).
	wantPrefix := []string{
		"SSH_ASKPASS=/tmp/lazyclaude/askpass-123.sh",
		"SSH_ASKPASS_REQUIRE=prefer",
		"LAZYCLAUDE_ASKPASS_SOCK=/tmp/lazyclaude/askpass-123.sock",
	}
	if len(env) < len(wantPrefix) {
		t.Fatalf("SSHEnv() len = %d, want >= %d", len(env), len(wantPrefix))
	}
	for i, w := range wantPrefix {
		if env[i] != w {
			t.Errorf("SSHEnv()[%d] = %q, want %q", i, env[i], w)
		}
	}
}

func TestSSHEnv_WithoutAskpass(t *testing.T) {
	e := &ExecSSHExecutor{}
	env := e.SSHEnv()
	if env != nil {
		t.Errorf("SSHEnv() = %v, want nil", env)
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
