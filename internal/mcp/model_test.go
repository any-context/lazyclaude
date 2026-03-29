package mcp

import "testing"

func TestEffectiveType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  ServerConfig
		want string
	}{
		{
			name: "explicit http",
			cfg:  ServerConfig{Type: "http", URL: "https://example.com"},
			want: "http",
		},
		{
			name: "explicit sse",
			cfg:  ServerConfig{Type: "sse", URL: "https://example.com/sse"},
			want: "sse",
		},
		{
			name: "explicit stdio",
			cfg:  ServerConfig{Type: "stdio", Command: "npx"},
			want: "stdio",
		},
		{
			name: "empty type defaults to stdio",
			cfg:  ServerConfig{Command: "npx", Args: []string{"-y", "server"}},
			want: "stdio",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.cfg.EffectiveType(); got != tt.want {
				t.Errorf("EffectiveType() = %q, want %q", got, tt.want)
			}
		})
	}
}
