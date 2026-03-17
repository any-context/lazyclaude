package tmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseClients(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want []ClientInfo
	}{
		{
			name: "empty",
			in:   "",
			want: nil,
		},
		{
			name: "single client",
			in:   "/dev/ttys001\tmain\t200\t50\t1710000000",
			want: []ClientInfo{
				{Name: "/dev/ttys001", Session: "main", Width: 200, Height: 50, Activity: 1710000000},
			},
		},
		{
			name: "multiple clients",
			in:   "/dev/ttys001\tmain\t200\t50\t100\n/dev/ttys002\tclaude\t180\t40\t200",
			want: []ClientInfo{
				{Name: "/dev/ttys001", Session: "main", Width: 200, Height: 50, Activity: 100},
				{Name: "/dev/ttys002", Session: "claude", Width: 180, Height: 40, Activity: 200},
			},
		},
		{
			name: "malformed line",
			in:   "bad\tdata",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseClients(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseWindows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want []WindowInfo
	}{
		{
			name: "empty",
			in:   "",
			want: nil,
		},
		{
			name: "single window",
			in:   "@1\t0\tlc-abc12345\tclaude\t1",
			want: []WindowInfo{
				{ID: "@1", Index: 0, Name: "lc-abc12345", Session: "claude", Active: true},
			},
		},
		{
			name: "multiple windows",
			in:   "@1\t0\tlc-abc\tclaude\t1\n@2\t1\tlc-def\tclaude\t0",
			want: []WindowInfo{
				{ID: "@1", Index: 0, Name: "lc-abc", Session: "claude", Active: true},
				{ID: "@2", Index: 1, Name: "lc-def", Session: "claude", Active: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseWindows(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParsePanes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want []PaneInfo
	}{
		{
			name: "empty",
			in:   "",
			want: nil,
		},
		{
			name: "alive pane",
			in:   "%1\t@1\t12345\t0",
			want: []PaneInfo{
				{ID: "%1", Window: "@1", PID: 12345, Dead: false},
			},
		},
		{
			name: "dead pane",
			in:   "%2\t@1\t0\t1",
			want: []PaneInfo{
				{ID: "%2", Window: "@1", PID: 0, Dead: true},
			},
		},
		{
			name: "multiple panes",
			in:   "%1\t@1\t1001\t0\n%2\t@2\t1002\t0\n%3\t@2\t0\t1",
			want: []PaneInfo{
				{ID: "%1", Window: "@1", PID: 1001, Dead: false},
				{ID: "%2", Window: "@2", PID: 1002, Dead: false},
				{ID: "%3", Window: "@2", PID: 0, Dead: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parsePanes(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Verify ExecClient implements Client interface at compile time.
var _ Client = (*ExecClient)(nil)

func TestNewExecClient(t *testing.T) {
	t.Parallel()
	c := NewExecClient()
	require.NotNil(t, c)
	assert.Equal(t, "", c.Socket())
}

func TestNewExecClientWithSocket(t *testing.T) {
	t.Parallel()
	c := NewExecClientWithSocket("lazyclaude")
	require.NotNil(t, c)
	assert.Equal(t, "lazyclaude", c.Socket())
}

func TestPrependSocket(t *testing.T) {
	t.Parallel()

	t.Run("no socket", func(t *testing.T) {
		c := NewExecClient()
		args := c.prependSocket([]string{"list-sessions"})
		assert.Equal(t, []string{"list-sessions"}, args)
	})

	t.Run("with socket", func(t *testing.T) {
		c := NewExecClientWithSocket("lc")
		args := c.prependSocket([]string{"list-sessions"})
		assert.Equal(t, []string{"-L", "lc", "list-sessions"}, args)
	})
}