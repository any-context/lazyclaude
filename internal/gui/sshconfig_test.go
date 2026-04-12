package gui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSSHHosts(t *testing.T) {
	tests := []struct {
		name    string
		content string // file content; empty string means file exists but empty
		noFile  bool   // true = file does not exist
		want    []string
		wantErr bool
	}{
		{
			name: "basic hosts",
			content: `Host alpha
Host beta
Host gamma
`,
			want: []string{"alpha", "beta", "gamma"},
		},
		{
			name: "wildcard skipped",
			content: `Host *
Host dev-*
Host staging
Host prod?.example.com
`,
			want: []string{"staging"},
		},
		{
			name: "multiple patterns per line",
			content: `Host foo bar baz
`,
			want: []string{"bar", "baz", "foo"},
		},
		{
			name: "multiple patterns with wildcard",
			content: `Host web1 web2 web-*
Host db1
`,
			want: []string{"db1", "web1", "web2"},
		},
		{
			name:    "empty file",
			content: "",
			want:    nil,
		},
		{
			name:   "file not found",
			noFile: true,
			want:   nil,
		},
		{
			name: "comments and blank lines",
			content: `# this is a comment
Host myhost

  # indented comment
Host another
`,
			want: []string{"another", "myhost"},
		},
		{
			name: "case insensitive Host directive",
			content: `host lower
HOST UPPER
Host Mixed
`,
			want: []string{"Mixed", "UPPER", "lower"},
		},
		{
			name: "tab after Host keyword",
			content: "Host\ttabbed\n",
			want:    []string{"tabbed"},
		},
		{
			name: "duplicate hosts deduplicated",
			content: `Host duphost
Host duphost
Host other
`,
			want: []string{"duphost", "other"},
		},
		{
			name: "non-host directives ignored",
			content: `HostName 192.168.1.1
Hostname example.com
Host realhost
Match host something
`,
			want: []string{"realhost"},
		},
		{
			name: "indented host line",
			content: `  Host indented
`,
			want: []string{"indented"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var path string
			if tt.noFile {
				path = filepath.Join(t.TempDir(), "nonexistent")
			} else {
				dir := t.TempDir()
				path = filepath.Join(dir, "config")
				if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
					t.Fatal(err)
				}
			}

			got, err := ParseSSHHosts(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got %v (len %d), want %v (len %d)", got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
