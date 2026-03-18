package gui

import (
	"context"
	"sync"

	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
)

// InputForwarder sends keystrokes to a tmux pane.
type InputForwarder interface {
	ForwardKey(target string, key string) error
}

// TmuxInputForwarder forwards keys via tmux send-keys.
type TmuxInputForwarder struct {
	client tmux.Client
}

// NewTmuxInputForwarder creates a forwarder backed by a tmux client.
func NewTmuxInputForwarder(client tmux.Client) *TmuxInputForwarder {
	return &TmuxInputForwarder{client: client}
}

func (f *TmuxInputForwarder) ForwardKey(target string, key string) error {
	return f.client.SendKeys(context.Background(), target, key)
}

// MockInputForwarder records forwarded keys for testing.
type MockInputForwarder struct {
	mu     sync.Mutex
	keys   []string
	target string
}

func (f *MockInputForwarder) ForwardKey(target string, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.target = target
	f.keys = append(f.keys, key)
	return nil
}

// Keys returns all forwarded keys.
func (f *MockInputForwarder) Keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]string, len(f.keys))
	copy(result, f.keys)
	return result
}

// LastTarget returns the last target pane.
func (f *MockInputForwarder) LastTarget() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.target
}

// RuneToTmuxKey converts a rune to a tmux send-keys compatible string.
// Currently handles printable runes only. Special keys (Enter, Esc, Tab, etc.)
// are handled separately via SpecialKeyToTmux in keybinding setup.
func RuneToTmuxKey(ch rune) string {
	return string(ch)
}

