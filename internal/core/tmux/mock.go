package tmux

import (
	"context"
	"fmt"
)

// MockClient implements Client for testing.
type MockClient struct {
	Sessions map[string][]WindowInfo
	Panes    map[string][]PaneInfo // keyed by session
	Clients  []ClientInfo
	Captured      map[string]string // target -> content
	RangeCaptures map[string]string // "target:start:end" -> content
	SentKeys map[string][]string
	Options  map[string]string
	Messages map[string]string

	// Captured arguments for assertions
	LastNewSessionOpts NewSessionOpts
	LastNewWindowOpts  NewWindowOpts

	// Error injection
	ErrListClients     error
	ErrHasSession      error
	ErrNewSession      error
	ErrListWindows     error
	ErrNewWindow       error
	ErrRespawnPane     error
	ErrKillWindow      error
	ErrListPanes       error
	ErrCapture         error
	ErrSendKeys        error
	ErrShowMessage     error
	ErrGetOption       error
}

// NewMockClient creates a MockClient with initialized maps.
func NewMockClient() *MockClient {
	return &MockClient{
		Sessions: make(map[string][]WindowInfo),
		Panes:    make(map[string][]PaneInfo),
		Captured:      make(map[string]string),
		RangeCaptures: make(map[string]string),
		SentKeys: make(map[string][]string),
		Options:  make(map[string]string),
		Messages: make(map[string]string),
	}
}

func (m *MockClient) ListClients(_ context.Context) ([]ClientInfo, error) {
	if m.ErrListClients != nil {
		return nil, m.ErrListClients
	}
	return m.Clients, nil
}

func (m *MockClient) FindActiveClient(_ context.Context) (*ClientInfo, error) {
	if m.ErrListClients != nil {
		return nil, m.ErrListClients
	}
	if len(m.Clients) == 0 {
		return nil, nil
	}
	best := m.Clients[0]
	for _, c := range m.Clients[1:] {
		if c.Activity > best.Activity {
			best = c
		}
	}
	return &best, nil
}

func (m *MockClient) HasSession(_ context.Context, name string) (bool, error) {
	if m.ErrHasSession != nil {
		return false, m.ErrHasSession
	}
	_, ok := m.Sessions[name]
	return ok, nil
}

func (m *MockClient) NewSession(_ context.Context, opts NewSessionOpts) error {
	if m.ErrNewSession != nil {
		return m.ErrNewSession
	}
	m.LastNewSessionOpts = opts
	m.Sessions[opts.Name] = []WindowInfo{
		{ID: "@0", Index: 0, Name: opts.WindowName, Session: opts.Name, Active: true},
	}
	return nil
}

func (m *MockClient) ListWindows(_ context.Context, session string) ([]WindowInfo, error) {
	if m.ErrListWindows != nil {
		return nil, m.ErrListWindows
	}
	windows, ok := m.Sessions[session]
	if !ok {
		return nil, nil
	}
	return windows, nil
}

func (m *MockClient) NewWindow(_ context.Context, opts NewWindowOpts) error {
	if m.ErrNewWindow != nil {
		return m.ErrNewWindow
	}
	m.LastNewWindowOpts = opts
	windows := m.Sessions[opts.Session]
	newIdx := len(windows)
	m.Sessions[opts.Session] = append(windows, WindowInfo{
		ID:      fmt.Sprintf("@%d", newIdx),
		Index:   newIdx,
		Name:    opts.Name,
		Session: opts.Session,
	})
	return nil
}

func (m *MockClient) RespawnPane(_ context.Context, _, _ string) error {
	return m.ErrRespawnPane
}

func (m *MockClient) KillWindow(_ context.Context, target string) error {
	if m.ErrKillWindow != nil {
		return m.ErrKillWindow
	}
	for session, windows := range m.Sessions {
		for i, w := range windows {
			if w.ID == target || w.Name == target {
				result := make([]WindowInfo, 0, len(windows)-1)
				result = append(result, windows[:i]...)
				result = append(result, windows[i+1:]...)
				m.Sessions[session] = result
				return nil
			}
		}
	}
	return nil
}

func (m *MockClient) ListPanes(_ context.Context, session string) ([]PaneInfo, error) {
	if m.ErrListPanes != nil {
		return nil, m.ErrListPanes
	}
	if session == "" {
		var all []PaneInfo
		for _, panes := range m.Panes {
			all = append(all, panes...)
		}
		return all, nil
	}
	return m.Panes[session], nil
}

func (m *MockClient) CapturePaneContent(_ context.Context, target string) (string, error) {
	if m.ErrCapture != nil {
		return "", m.ErrCapture
	}
	return m.Captured[target], nil
}

func (m *MockClient) CapturePaneANSI(_ context.Context, target string) (string, error) {
	if m.ErrCapture != nil {
		return "", m.ErrCapture
	}
	return m.Captured[target], nil
}

func (m *MockClient) CapturePaneANSIRange(_ context.Context, target string, start, end int) (string, error) {
	if m.ErrCapture != nil {
		return "", m.ErrCapture
	}
	key := fmt.Sprintf("%s:%d:%d", target, start, end)
	if content, ok := m.RangeCaptures[key]; ok {
		return content, nil
	}
	return m.Captured[target], nil
}

func (m *MockClient) SendKeys(_ context.Context, target string, keys ...string) error {
	if m.ErrSendKeys != nil {
		return m.ErrSendKeys
	}
	m.SentKeys[target] = append(m.SentKeys[target], keys...)
	return nil
}

func (m *MockClient) SendKeysLiteral(_ context.Context, target string, text string) error {
	if m.ErrSendKeys != nil {
		return m.ErrSendKeys
	}
	m.SentKeys[target] = append(m.SentKeys[target], text)
	return nil
}

func (m *MockClient) PasteToPane(_ context.Context, target string, text string) error {
	if m.ErrSendKeys != nil {
		return m.ErrSendKeys
	}
	m.SentKeys[target] = append(m.SentKeys[target], text)
	return nil
}

func (m *MockClient) ShowMessage(_ context.Context, target, _ string) (string, error) {
	if m.ErrShowMessage != nil {
		return "", m.ErrShowMessage
	}
	return m.Messages[target], nil
}

func (m *MockClient) GetOption(_ context.Context, target, option string) (string, error) {
	if m.ErrGetOption != nil {
		return "", m.ErrGetOption
	}
	key := target + ":" + option
	return m.Options[key], nil
}

func (m *MockClient) ResizeWindow(_ context.Context, _ string, _, _ int) error { return nil }
