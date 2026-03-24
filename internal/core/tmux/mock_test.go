package tmux_test

import (
	"context"
	"errors"
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockClient_HasSession(t *testing.T) {
	t.Parallel()
	m := tmux.NewMockClient()
	m.Sessions["claude"] = []tmux.WindowInfo{
		{ID: "@1", Name: "test", Session: "claude"},
	}

	ctx := context.Background()

	ok, err := m.HasSession(ctx, "claude")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = m.HasSession(ctx, "nonexistent")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestMockClient_HasSession_Error(t *testing.T) {
	t.Parallel()
	m := tmux.NewMockClient()
	m.ErrHasSession = errors.New("tmux not running")

	ok, err := m.HasSession(context.Background(), "claude")
	assert.Error(t, err)
	assert.False(t, ok)
}

func TestMockClient_NewSession(t *testing.T) {
	t.Parallel()
	m := tmux.NewMockClient()

	err := m.NewSession(context.Background(), tmux.NewSessionOpts{
		Name:       "claude",
		WindowName: "lc-abc12345",
		Command:    "claude",
		Detached:   true,
	})
	require.NoError(t, err)

	ok, _ := m.HasSession(context.Background(), "claude")
	assert.True(t, ok)

	windows, _ := m.ListWindows(context.Background(), "claude")
	require.Len(t, windows, 1)
	assert.Equal(t, "lc-abc12345", windows[0].Name)
}

func TestMockClient_NewWindow(t *testing.T) {
	t.Parallel()
	m := tmux.NewMockClient()

	_ = m.NewSession(context.Background(), tmux.NewSessionOpts{
		Name:       "claude",
		WindowName: "lc-first",
	})
	err := m.NewWindow(context.Background(), tmux.NewWindowOpts{
		Session: "claude",
		Name:    "lc-second",
	})
	require.NoError(t, err)

	windows, _ := m.ListWindows(context.Background(), "claude")
	require.Len(t, windows, 2)
	assert.Equal(t, "lc-second", windows[1].Name)
}

func TestMockClient_KillWindow(t *testing.T) {
	t.Parallel()
	m := tmux.NewMockClient()

	_ = m.NewSession(context.Background(), tmux.NewSessionOpts{
		Name:       "claude",
		WindowName: "lc-test",
	})

	windows, _ := m.ListWindows(context.Background(), "claude")
	require.Len(t, windows, 1)

	err := m.KillWindow(context.Background(), windows[0].ID)
	require.NoError(t, err)

	windows, _ = m.ListWindows(context.Background(), "claude")
	assert.Empty(t, windows)
}

func TestMockClient_FindActiveClient(t *testing.T) {
	t.Parallel()
	m := tmux.NewMockClient()
	m.Clients = []tmux.ClientInfo{
		{Name: "/dev/ttys001", Session: "main", Activity: 100},
		{Name: "/dev/ttys002", Session: "claude", Activity: 200},
		{Name: "/dev/ttys003", Session: "main", Activity: 150},
	}

	client, err := m.FindActiveClient(context.Background())
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, "/dev/ttys002", client.Name)
}

func TestMockClient_FindActiveClient_Empty(t *testing.T) {
	t.Parallel()
	m := tmux.NewMockClient()

	client, err := m.FindActiveClient(context.Background())
	require.NoError(t, err)
	assert.Nil(t, client)
}

func TestMockClient_SendKeys(t *testing.T) {
	t.Parallel()
	m := tmux.NewMockClient()

	err := m.SendKeys(context.Background(), "claude:lc-test", "1")
	require.NoError(t, err)
	err = m.SendKeys(context.Background(), "claude:lc-test", "y")
	require.NoError(t, err)

	assert.Equal(t, []string{"1", "y"}, m.SentKeys["claude:lc-test"])
}

func TestMockClient_DisplayPopup(t *testing.T) {
	t.Parallel()
	m := tmux.NewMockClient()

	opts := tmux.PopupOpts{
		Client: "/dev/ttys001",
		Width:  80,
		Height: 60,
		Cmd:    "lazyclaude diff --window lc-test",
	}
	err := m.DisplayPopup(context.Background(), opts)
	require.NoError(t, err)
	require.Len(t, m.Popups, 1)
	assert.Equal(t, opts, m.Popups[0])
}

func TestMockClient_CapturePaneContent(t *testing.T) {
	t.Parallel()
	m := tmux.NewMockClient()
	m.Captured["claude:lc-test"] = "$ claude\nHello, how can I help?"

	content, err := m.CapturePaneContent(context.Background(), "claude:lc-test")
	require.NoError(t, err)
	assert.Contains(t, content, "Hello, how can I help?")
}

func TestMockClient_ListPanes_AllSessions(t *testing.T) {
	t.Parallel()
	m := tmux.NewMockClient()
	m.Panes["claude"] = []tmux.PaneInfo{
		{ID: "%1", Window: "@1", PID: 1001},
		{ID: "%2", Window: "@2", PID: 1002},
	}
	m.Panes["main"] = []tmux.PaneInfo{
		{ID: "%3", Window: "@3", PID: 2001},
	}

	panes, err := m.ListPanes(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, panes, 3)
}

func TestMockClient_ErrorInjection(t *testing.T) {
	t.Parallel()
	testErr := errors.New("test error")
	ctx := context.Background()

	tests := []struct {
		name string
		setup func(*tmux.MockClient)
		call  func(*tmux.MockClient) error
	}{
		{"NewSession", func(m *tmux.MockClient) { m.ErrNewSession = testErr }, func(m *tmux.MockClient) error { return m.NewSession(ctx, tmux.NewSessionOpts{Name: "x"}) }},
		{"ListWindows", func(m *tmux.MockClient) { m.ErrListWindows = testErr }, func(m *tmux.MockClient) error { _, e := m.ListWindows(ctx, "x"); return e }},
		{"NewWindow", func(m *tmux.MockClient) { m.ErrNewWindow = testErr }, func(m *tmux.MockClient) error { return m.NewWindow(ctx, tmux.NewWindowOpts{}) }},
		{"KillWindow", func(m *tmux.MockClient) { m.ErrKillWindow = testErr }, func(m *tmux.MockClient) error { return m.KillWindow(ctx, "x") }},
		{"RespawnPane", func(m *tmux.MockClient) { m.ErrRespawnPane = testErr }, func(m *tmux.MockClient) error { return m.RespawnPane(ctx, "x", "cmd") }},
		{"SendKeys", func(m *tmux.MockClient) { m.ErrSendKeys = testErr }, func(m *tmux.MockClient) error { return m.SendKeys(ctx, "x", "y") }},
		{"DisplayPopup", func(m *tmux.MockClient) { m.ErrDisplayPopup = testErr }, func(m *tmux.MockClient) error { return m.DisplayPopup(ctx, tmux.PopupOpts{}) }},
		{"CapturePaneContent", func(m *tmux.MockClient) { m.ErrCapture = testErr }, func(m *tmux.MockClient) error { _, e := m.CapturePaneContent(ctx, "x"); return e }},
		{"ListPanes", func(m *tmux.MockClient) { m.ErrListPanes = testErr }, func(m *tmux.MockClient) error { _, e := m.ListPanes(ctx, ""); return e }},
		{"ShowMessage", func(m *tmux.MockClient) { m.ErrShowMessage = testErr }, func(m *tmux.MockClient) error { _, e := m.ShowMessage(ctx, "x", "fmt"); return e }},
		{"GetOption", func(m *tmux.MockClient) { m.ErrGetOption = testErr }, func(m *tmux.MockClient) error { _, e := m.GetOption(ctx, "x", "opt"); return e }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := tmux.NewMockClient()
			tt.setup(m)
			err := tt.call(m)
			assert.ErrorIs(t, err, testErr)
		})
	}
}

func TestMockClient_SendKeysLiteral(t *testing.T) {
	t.Parallel()
	m := tmux.NewMockClient()

	err := m.SendKeysLiteral(context.Background(), "claude:lc-test", ";")
	require.NoError(t, err)
	err = m.SendKeysLiteral(context.Background(), "claude:lc-test", "あ")
	require.NoError(t, err)

	assert.Equal(t, []string{";", "あ"}, m.SentKeys["claude:lc-test"])
}

func TestMockClient_SendKeysLiteral_Error(t *testing.T) {
	t.Parallel()
	m := tmux.NewMockClient()
	m.ErrSendKeys = errors.New("test error")

	err := m.SendKeysLiteral(context.Background(), "target", "x")
	assert.Error(t, err)
}

// Verify MockClient implements Client interface at compile time.
var _ tmux.Client = (*tmux.MockClient)(nil)